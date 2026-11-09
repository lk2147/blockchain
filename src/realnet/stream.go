package realnet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"sync"
	"time"

	"blockchain-lab01/src/network"
	pb "blockchain-lab01/src/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type peerSession struct {
	send   func(*pb.Envelope) error // Only that peer's send worker calls Send.
	done   chan struct{}
	once   sync.Once
	cancel context.CancelFunc
}

func (s *peerSession) close() { s.once.Do(func() { close(s.done); s.cancel() }) }

func pause(ctx context.Context, delay time.Duration) bool {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (p *process) hello(peer int) (*pb.Envelope, error) {
	e := &pb.Envelope{ChainId: p.cfg.ChainID, Sender: uint32(p.cfg.NodeID), Recipient: uint32(peer), Kind: "hello", Reference: make([]byte, 32), Payload: membershipBytes(p.cfg)}
	b, err := TransportBytes(e)
	if err != nil {
		return nil, err
	}
	e.Signature = ed25519.Sign(p.key, b)
	return e, nil
}
func (p *process) verifyHello(e *pb.Envelope) error {
	if err := VerifyEnvelope(p.cfg.ChainID, p.cfg.NodeID, p.ids, e); err != nil {
		return err
	}
	if e.Kind != "hello" || int(e.Sender) == p.cfg.NodeID || !bytes.Equal(e.Payload, membershipBytes(p.cfg)) {
		return fmt.Errorf("stream handshake requires matching fixed membership")
	}
	return nil
}

func (p *process) session(peer int) *peerSession {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	return p.sessions[peer]
}
func (p *process) installSession(peer int, s *peerSession) bool {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	if p.sessions[peer] != nil {
		return false
	}
	p.sessions[peer] = s
	p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "stream_open"})
	return true
}
func (p *process) removeSession(peer int, s *peerSession) {
	s.close()
	p.sessionMu.Lock()
	if p.sessions[peer] == s {
		delete(p.sessions, peer)
	}
	p.sessionMu.Unlock()
	p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "stream_close"})
}

func (p *process) readStream(ctx context.Context, peer int, recv func() (*pb.Envelope, error)) error {
	for {
		e, err := recv()
		if err != nil {
			return err
		}
		if e.Kind == "hello" || int(e.Sender) != peer || e.Recipient != uint32(p.cfg.NodeID) {
			return fmt.Errorf("frame does not belong to authenticated stream peer")
		}
		if err = p.acceptFrame(ctx, e); err != nil {
			return err
		}
		p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "stream_receive", Type: network.Kind(e.Kind), Height: e.Height, Round: e.Round, Packet: e.Sequence})
	}
}

// Exchange actually carries protocol frames in both directions. An accepted
// incoming session is also the outgoing path to that peer, avoiding a second
// connection just to return a requested body.
func (p *process) Exchange(stream grpc.BidiStreamingServer[pb.Envelope, pb.Envelope]) error {
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	if err = p.verifyHello(hello); err != nil {
		p.Record(network.Event{Node: p.cfg.NodeID, Peer: int(hello.Sender), Event: "reject_handshake", Reason: err.Error()})
		return status.Error(codes.PermissionDenied, err.Error())
	}
	peer := int(hello.Sender)
	response, err := p.hello(peer)
	if err != nil {
		return err
	}
	if err = stream.Send(response); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(stream.Context())
	s := &peerSession{send: stream.Send, done: make(chan struct{}), cancel: cancel}
	if !p.installSession(peer, s) {
		s.close()
		return status.Error(codes.AlreadyExists, "peer session already active")
	}
	defer p.removeSession(peer, s)
	p.streamWG.Add(1)
	go func() {
		defer p.streamWG.Done()
		if err := p.readStream(ctx, peer, stream.Recv); err != nil && err != io.EOF && ctx.Err() == nil {
			p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "stream_error", Reason: err.Error()})
		}
		s.close()
	}()
	select {
	case <-ctx.Done():
		return nil
	case <-p.ctx.Done():
		return nil
	}
}

func (p *process) dialSession(peer int, conn *grpc.ClientConn) *peerSession {
	ctx, cancel := context.WithCancel(p.ctx)
	stream, err := pb.NewTransportClient(conn).Exchange(ctx)
	if err != nil {
		cancel()
		return nil
	}
	hello, err := p.hello(peer)
	if err != nil {
		cancel()
		return nil
	}
	// A dead peer cannot leave the handshake stuck for the whole run.
	timer := time.AfterFunc(time.Second, cancel)
	if err = stream.Send(hello); err == nil {
		var response *pb.Envelope
		response, err = stream.Recv()
		if err == nil {
			err = p.verifyHello(response)
			if err == nil && int(response.Sender) != peer {
				err = fmt.Errorf("wrong stream peer")
			}
		}
	}
	timer.Stop()
	if err != nil {
		cancel()
		return nil
	}
	s := &peerSession{send: stream.Send, done: make(chan struct{}), cancel: cancel}
	if !p.installSession(peer, s) {
		s.close()
		return nil
	}
	p.streamWG.Add(1)
	go func() {
		defer p.streamWG.Done()
		defer p.removeSession(peer, s)
		if err := p.readStream(ctx, peer, stream.Recv); err != nil && err != io.EOF && ctx.Err() == nil {
			p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "stream_error", Reason: err.Error()})
		}
	}()
	return s
}

func (p *process) sendPeer(peer int, jobs <-chan *pb.Envelope, wg *sync.WaitGroup) {
	defer wg.Done()
	conn, err := grpc.NewClient(p.cfg.Peers[peer].Address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20), grpc.MaxCallSendMsgSize(4<<20)))
	if err != nil {
		p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "connection_error", Reason: err.Error()})
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "close_error", Reason: err.Error()})
		}
	}()
	for p.ctx.Err() == nil {
		s := p.session(peer)
		if s == nil {
			// One dialer per pair. Either side can write once the session exists.
			if p.cfg.NodeID < peer {
				s = p.dialSession(peer, conn)
			}
			if s == nil {
				if !pause(p.ctx, 100*time.Millisecond) {
					return
				}
				continue
			}
		}
		select {
		case <-p.ctx.Done():
			return
		case <-s.done:
			continue
		case e := <-jobs:
			err := p.transmit(e, func() error {
				// Closing either kind of session cancels its RPC and unblocks Send.
				timer := time.AfterFunc(time.Second, s.close)
				defer timer.Stop()
				return s.send(e)
			})
			p.queued.Add(-1)
			if err != nil {
				s.close()
				p.frameEvent(e, "send_error", err.Error())
			} else {
				p.frameEvent(e, "stream_send", "")
			}
		}
	}
}
