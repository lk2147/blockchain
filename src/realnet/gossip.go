package realnet

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/network"
	pb "blockchain-lab01/src/proto"
	libp2p "github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/control"
	lcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	lnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"google.golang.org/protobuf/proto"
)

// This fingerprint identifies the ordered static configuration, not chain state.
// It is exchanged in signed stream handshakes and names the gossip topic.
func membershipBytes(c Config) []byte {
	b, _ := json.Marshal(struct {
		Version string
		Chain   string
		F       int
		Peers   []Peer
	}{"LAB01-NET-2", c.ChainID, c.F, c.Peers}) // These fields cannot fail JSON encoding.
	h := crypt.HashBytes(b)
	return h[:]
}

type memberGate map[peer.ID]bool

func (g memberGate) InterceptPeerDial(id peer.ID) bool                 { return g[id] }
func (g memberGate) InterceptAddrDial(id peer.ID, _ ma.Multiaddr) bool { return g[id] }
func (g memberGate) InterceptAccept(_ lnet.ConnMultiaddrs) bool        { return true }
func (g memberGate) InterceptSecured(_ lnet.Direction, id peer.ID, _ lnet.ConnMultiaddrs) bool {
	return g[id]
}
func (g memberGate) InterceptUpgraded(c lnet.Conn) (bool, control.DisconnectReason) {
	return g[c.RemotePeer()], 0
}

type gossipNetwork struct {
	p     *process
	host  host.Host
	topic *pubsub.Topic
	sub   *pubsub.Subscription
	peers []peer.AddrInfo
	done  chan struct{}
}

func (p *process) newGossip() (*gossipNetwork, error) {
	key, err := lcrypto.UnmarshalEd25519PrivateKey(p.key)
	if err != nil {
		return nil, err
	}
	g := &gossipNetwork{p: p, done: make(chan struct{})}
	gate := memberGate{}
	for i, id := range p.ids {
		pub, err := lcrypto.UnmarshalEd25519PublicKey(ed25519.PublicKey(id[:]))
		if err != nil {
			return nil, err
		}
		pid, err := peer.IDFromPublicKey(pub)
		if err != nil {
			return nil, err
		}
		address, err := net.ResolveTCPAddr("tcp", p.cfg.Peers[i].GossipAddress)
		if err != nil {
			return nil, err
		}
		multi, err := manet.FromNetAddr(address)
		if err != nil {
			return nil, err
		}
		gate[pid] = true
		g.peers = append(g.peers, peer.AddrInfo{ID: pid, Addrs: []ma.Multiaddr{multi}})
	}
	h, err := libp2p.New(libp2p.Identity(key), libp2p.ListenAddrs(g.peers[p.cfg.NodeID].Addrs...),
		libp2p.NoTransports, libp2p.Transport(tcp.NewTCPTransport), libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport), libp2p.ConnectionGater(gate), libp2p.DisableRelay(), libp2p.DisableMetrics())
	if err != nil {
		return nil, err
	}
	g.host = h
	cleanup := func(err error) (*gossipNetwork, error) { _ = h.Close(); return nil, err }
	params := pubsub.DefaultGossipSubParams()
	params.HeartbeatInterval = 200 * time.Millisecond
	ps, err := pubsub.NewGossipSub(p.ctx, h, pubsub.WithGossipSubParams(params), pubsub.WithMaxMessageSize(4<<20))
	if err != nil {
		return cleanup(err)
	}
	name := fmt.Sprintf("/lab01/2/%x", membershipBytes(p.cfg))
	if err = ps.RegisterTopicValidator(name, func(_ context.Context, _ peer.ID, msg *pubsub.Message) bool {
		var e pb.Envelope
		if proto.Unmarshal(msg.Data, &e) != nil || e.Recipient != ^uint32(0) || int(e.Sender) >= len(g.peers) {
			return false
		}
		if msg.GetFrom() != g.peers[e.Sender].ID || VerifyEnvelope(p.cfg.ChainID, p.cfg.NodeID, p.ids, &e) != nil {
			return false
		}
		msg.ValidatorData = &e
		return true
	}); err != nil {
		return cleanup(err)
	}
	g.topic, err = ps.Join(name)
	if err != nil {
		return cleanup(err)
	}
	g.sub, err = g.topic.Subscribe()
	if err != nil {
		_ = g.topic.Close()
		return cleanup(err)
	}
	p.Record(network.Event{Node: p.cfg.NodeID, Peer: -1, Event: "membership", Reason: fmt.Sprintf("sha256=%x gossip=%s peer_id=%s", membershipBytes(p.cfg), p.cfg.Peers[p.cfg.NodeID].GossipAddress, h.ID())})
	go g.read()
	return g, nil
}

func (g *gossipNetwork) read() {
	defer close(g.done)
	for {
		msg, err := g.sub.Next(g.p.ctx)
		if err != nil {
			return
		}
		if msg.GetFrom() == g.host.ID() {
			continue
		} // The engine already processes its own output.
		e, ok := msg.ValidatorData.(*pb.Envelope)
		if !ok {
			continue
		} // Only topic-authenticated messages reach the owner loop.
		g.p.Record(network.Event{Node: g.p.cfg.NodeID, Peer: int(e.Sender), Event: "gossip_receive", Type: network.Kind(e.Kind), Height: e.Height, Round: e.Round, Packet: e.Sequence, Reason: "via=" + msg.ReceivedFrom.String()})
		if err := g.p.enqueueVerified(g.p.ctx, e); err != nil && g.p.ctx.Err() == nil {
			g.p.Record(network.Event{Node: g.p.cfg.NodeID, Peer: int(e.Sender), Event: "gossip_reject", Reason: err.Error()})
		}
	}
}

func (g *gossipNetwork) publish(e *pb.Envelope) error {
	b, err := proto.Marshal(e)
	if err != nil {
		return err
	}
	if err = g.topic.Publish(g.p.ctx, b); err != nil {
		return err
	}
	g.p.frameEvent(e, "gossip_publish", "")
	return nil
}

// Bootstrap/reconnect to the configured identities; discovery never changes
// validator membership. GossipSub itself selects and repairs its forwarding mesh.
func (g *gossipNetwork) connect() {
	var workers sync.WaitGroup
	for i, info := range g.peers {
		if i == g.p.cfg.NodeID {
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			for g.p.ctx.Err() == nil {
				if g.host.Network().Connectedness(info.ID) != lnet.Connected {
					ctx, cancel := context.WithTimeout(g.p.ctx, time.Second)
					err := g.host.Connect(ctx, info)
					cancel()
					if err == nil {
						g.p.Record(network.Event{Node: g.p.cfg.NodeID, Peer: i, Event: "gossip_connect"})
					}
				}
				if !pause(g.p.ctx, 300*time.Millisecond) {
					return
				}
			}
		}()
	}
	workers.Wait()
}

func (g *gossipNetwork) close() error {
	g.sub.Cancel()
	<-g.done
	topicErr := g.topic.Close()
	// PubSub shares the run context; normal deadline/cancellation already
	// closes its topic manager and is not a cleanup failure.
	if g.p.ctx.Err() != nil && errors.Is(topicErr, g.p.ctx.Err()) {
		topicErr = nil
	}
	return errors.Join(topicErr, g.host.Close())
}
