package realnet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pb "blockchain-lab01/src/proto"
	"blockchain-lab01/src/types"
	lnet "github.com/libp2p/go-libp2p/core/network"
	"google.golang.org/grpc"
)

// These are actual socket tests of the transport boundary. The inbox stand-in
// acknowledges authenticated frames; consensus acceptance is checked separately
// by the ten-process T1-T8 suite, never inferred from this acknowledgement.
func transportFixtures(t *testing.T, count int) ([]*process, []chan *pb.Envelope, context.CancelFunc) {
	t.Helper()
	addresses := reserveAddresses(t, 20)
	var peers []Peer
	var ids []types.ValidatorID
	var keys []ed25519.PrivateKey
	for i := 0; i < 10; i++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(101 + i)}, 32)) // Public TEST-ONLY fixtures.
		keys = append(keys, key)
		ids = append(ids, types.ValidatorID(key[32:]))
		peers = append(peers, Peer{Address: addresses[i], PublicKey: hex.EncodeToString(key[32:]), GossipAddress: addresses[10+i]})
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var processes []*process
	var inboxes []chan *pb.Envelope
	for i := 0; i < count; i++ {
		file, err := os.Create(filepath.Join(t.TempDir(), "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := file.Close(); err != nil {
				t.Error(err)
			}
		})
		p := &process{cfg: Config{NodeID: i, ChainID: "transport-test", F: 3, Peers: peers, SendGapMS: 1, QueueLimit: 128}, ctx: ctx, key: keys[i], ids: ids, log: &logWriter{file: file, start: time.Now()}, in: make(chan received, 128), sessions: make(map[int]*peerSession)}
		inbox := make(chan *pb.Envelope, 128)
		processes = append(processes, p)
		inboxes = append(inboxes, inbox)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case r := <-p.in:
					inbox <- r.frame
					r.done <- nil
				}
			}
		}()
	}
	return processes, inboxes, cancel
}

func transportFrame(t *testing.T, p *process, to uint32, sequence uint64) *pb.Envelope {
	t.Helper()
	e := &pb.Envelope{ChainId: p.cfg.ChainID, Sender: uint32(p.cfg.NodeID), Recipient: to, Sequence: sequence, Kind: "vote", Height: 1, Reference: make([]byte, 32), Payload: []byte("transport fixture; not a consensus vote")}
	b, err := TransportBytes(e)
	if err != nil {
		t.Fatal(err)
	}
	e.Signature = ed25519.Sign(p.key, b)
	return e
}

func await(t *testing.T, condition func() bool, reason string) {
	t.Helper()
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(reason)
}

func TestGossipRelaysWithoutDirectConnection(t *testing.T) {
	ps, inboxes, cancel := transportFixtures(t, 3)
	var gs []*gossipNetwork
	defer func() {
		cancel()
		for _, g := range gs {
			if err := g.close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, p := range ps {
		g, err := p.newGossip()
		if err != nil {
			t.Fatal(err)
		}
		gs = append(gs, g)
	}
	// Only 0--1--2 exists. No bootstrap worker is running in this isolated test.
	ctx, stop := context.WithTimeout(ps[0].ctx, 3*time.Second)
	defer stop()
	if err := gs[0].host.Connect(ctx, gs[0].peers[1]); err != nil {
		t.Fatal(err)
	}
	if err := gs[1].host.Connect(ctx, gs[1].peers[2]); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool {
		return len(gs[0].topic.ListPeers()) == 1 && len(gs[1].topic.ListPeers()) == 2 && len(gs[2].topic.ListPeers()) == 1
	}, "gossip subscriptions not established")
	frame := transportFrame(t, ps[0], ^uint32(0), 1)
	// Publication retry tolerates initial mesh formation; acceptance requires a
	// genuine relay, not direct delivery or manufactured shared protocol state.
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		if err := gs[0].publish(frame); err != nil {
			t.Fatal(err)
		}
		select {
		case e := <-inboxes[2]:
			if e.Sender != 0 || VerifyEnvelope(ps[2].cfg.ChainID, 2, ps[2].ids, e) != nil {
				t.Fatal("relayed frame lost original authentication")
			}
			if gs[0].host.Network().Connectedness(gs[2].host.ID()) == lnet.Connected {
				t.Fatal("relay test accidentally had a direct connection")
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("signed gossip did not cross the intermediate peer")
}

func TestStreamBidirectionalReconnectAndMembership(t *testing.T) {
	ps, inboxes, cancel := transportFixtures(t, 2)
	var servers []*grpc.Server
	var workers sync.WaitGroup
	var serveWorkers sync.WaitGroup
	defer func() {
		cancel()
		for _, s := range servers {
			s.Stop()
		}
		serveWorkers.Wait()
		workers.Wait()
		for _, p := range ps {
			p.streamWG.Wait()
		}
	}()
	jobs := []chan *pb.Envelope{make(chan *pb.Envelope, 8), make(chan *pb.Envelope, 8)}
	for i, p := range ps {
		listener, err := net.Listen("tcp", p.cfg.Peers[i].Address)
		if err != nil {
			t.Fatal(err)
		}
		server := grpc.NewServer(grpc.WaitForHandlers(true))
		pb.RegisterTransportServer(server, p)
		servers = append(servers, server)
		serveWorkers.Add(1)
		go func() {
			defer serveWorkers.Done()
			if err := server.Serve(listener); err != nil && p.ctx.Err() == nil {
				t.Error(err)
			}
		}()
		workers.Add(1)
		go p.sendPeer(1-i, jobs[i], &workers)
	}
	await(t, func() bool { return ps[0].session(1) != nil && ps[1].session(0) != nil }, "bidirectional session not established")
	old := ps[0].session(1)
	for i := 0; i < 2; i++ {
		ps[i].queued.Add(1)
		jobs[i] <- transportFrame(t, ps[i], uint32(1-i), 1)
	}
	for i := 0; i < 2; i++ {
		select {
		case e := <-inboxes[i]:
			if int(e.Sender) != 1-i {
				t.Fatal("wrong stream sender")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("both directions did not carry protocol frames")
		}
	}
	old.close()
	await(t, func() bool { s := ps[0].session(1); return s != nil && s != old && ps[1].session(0) != nil }, "stream did not reconnect")
	ps[1].queued.Add(1)
	jobs[1] <- transportFrame(t, ps[1], 0, 2)
	select {
	case e := <-inboxes[0]:
		if e.Sequence != 2 {
			t.Fatal("wrong post-reconnect frame")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reverse direction failed after reconnect")
	}
	other := &process{cfg: ps[1].cfg, key: ps[1].key}
	other.cfg.F = 2
	hello, err := other.hello(0)
	if err != nil {
		t.Fatal(err)
	}
	if ps[0].verifyHello(hello) == nil {
		t.Fatal("different membership accepted")
	}
	hello, err = ps[1].hello(0)
	if err != nil {
		t.Fatal(err)
	}
	hello.Signature[0] ^= 1
	if ps[0].verifyHello(hello) == nil {
		t.Fatal("unauthenticated handshake accepted")
	}
}
