package realnet

import (
	"container/heap"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/network"
	"blockchain-lab01/src/node"
	pb "blockchain-lab01/src/proto"
	"blockchain-lab01/src/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type Result struct {
	NodeID, PID int
	StateRoot   string
	Ledger      []cons.Finalized
}
type logWriter struct {
	mu    sync.Mutex
	file  *os.File
	start time.Time
	seq   uint64
	err   error
}

func (l *logWriter) record(e network.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return
	}
	l.seq++
	e.Seq = l.seq
	e.At = uint64(time.Since(l.start) / time.Millisecond)
	l.err = json.NewEncoder(l.file).Encode(struct {
		WallTime string
		network.Event
	}{time.Now().UTC().Format(time.RFC3339Nano), e})
}
func (l *logWriter) failure() error { l.mu.Lock(); defer l.mu.Unlock(); return l.err }

type task struct {
	at  time.Time
	seq uint64
	run func() error
}
type tasks []task

func (q tasks) Len() int { return len(q) }
func (q tasks) Less(i, j int) bool {
	if q[i].at.Equal(q[j].at) {
		return q[i].seq < q[j].seq
	}
	return q[i].at.Before(q[j].at)
}
func (q tasks) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *tasks) Push(x any)   { *q = append(*q, x.(task)) }
func (q *tasks) Pop() any     { a := *q; x := a[len(a)-1]; *q = a[:len(a)-1]; return x }

type transfer struct {
	peer int
	ref  types.Hash
}
type received struct {
	frame *pb.Envelope
	done  chan error
}
type arrival struct {
	window uint64
	count  int
}

// localNode is the owner-loop endpoint. Production uses node.Runtime; tests can
// run an actual faulty peer implementation without modifying honest engines.
type localNode interface {
	Start() error
	Receive(network.Packet) error
	Ledger() []cons.Finalized
	StateRoot() (types.Hash, error)
}
type process struct {
	pb.UnimplementedTransportServer
	cfg                       Config
	ctx                       context.Context
	key                       ed25519.PrivateKey
	ids                       []types.ValidatorID
	runtime                   localNode
	log                       *logWriter
	in                        chan received
	jobs                      chan *pb.Envelope
	queue                     tasks
	seq, packet               uint64
	queued                    atomic.Int64 // Scheduled + dispatcher + per-peer/inflight messages.
	faults                    *faultLayer
	seen, accepted, requested map[transfer]bool
	blocked                   map[int]uint64
	arrivals                  map[int]arrival
	started                   bool
	rateMu                    sync.Mutex
	nextSend                  time.Time
}

func (p *process) now() uint64            { return uint64(time.Since(p.log.start) / time.Millisecond) }
func (p *process) Record(e network.Event) { p.log.record(e) }
func (p *process) Schedule(delay uint64, f func() error) error {
	if delay > uint64((time.Duration(1<<63-1))/time.Millisecond) || f == nil {
		return fmt.Errorf("real timer overflow")
	}
	p.seq++
	heap.Push(&p.queue, task{time.Now().Add(time.Duration(delay) * time.Millisecond), p.seq, f})
	return nil
}
func (p *process) Send(peer int, m network.Message) error {
	if peer < 0 || peer >= len(p.ids) || peer == p.cfg.NodeID {
		return fmt.Errorf("invalid real peer")
	}
	k := transfer{peer, m.Ref}
	if m.Kind == network.Header && crypt.HashBytes(m.Data) != m.Ref {
		return fmt.Errorf("header reference mismatch")
	}
	if m.Kind == network.Request && !p.accepted[k] {
		return fmt.Errorf("request without accepted header")
	}
	if m.Kind == network.Body && !p.requested[k] {
		return fmt.Errorf("body without acceptance request")
	}
	if p.queued.Load() >= int64(p.cfg.QueueLimit) {
		p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "queue_full", Type: m.Kind, Height: m.Height, Round: m.Round})
		return network.ErrQueueFull
	}
	p.packet++
	e := &pb.Envelope{ChainId: p.cfg.ChainID, Sender: uint32(p.cfg.NodeID), Recipient: uint32(peer), Sequence: p.packet, Kind: string(m.Kind), Reference: append([]byte(nil), m.Ref[:]...), Height: m.Height, Round: m.Round, Payload: append([]byte(nil), m.Data...)}
	b, err := TransportBytes(e)
	if err != nil {
		return err
	}
	e.Signature = ed25519.Sign(p.key, b)
	faultNow := p.now()
	if p.cfg.StartAtUnixMS > 0 {
		elapsed := time.Now().UnixMilli() - p.cfg.StartAtUnixMS
		if elapsed < 0 {
			elapsed = 0
		}
		faultNow = uint64(elapsed)
	}
	plans, dropped := p.faults.plan(faultNow, e)
	if p.queued.Load()+int64(len(plans)) > int64(p.cfg.QueueLimit) {
		p.frameEvent(e, "queue_full", "fault copies exceed remaining outbound capacity")
		return network.ErrQueueFull
	}
	if dropped {
		p.frameEvent(e, "fault_drop", "")
	}
	for _, d := range plans {
		d := d
		p.queued.Add(1)
		p.frameEvent(d.frame, "fault_"+d.reason, fmt.Sprintf("hold %d ms", d.after))
		if err := p.Schedule(d.after, func() error {
			select {
			case p.jobs <- d.frame:
			default:
				p.queued.Add(-1)
				p.frameEvent(d.frame, "queue_full", "transmit queue full; peer retry required")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
func (p *process) frameEvent(e *pb.Envelope, kind, reason string) {
	var ref types.Hash
	copy(ref[:], e.Reference)
	p.Record(network.Event{Node: int(e.Sender), Peer: int(e.Recipient), Event: kind, Type: network.Kind(e.Kind), Height: e.Height, Round: e.Round, Ref: ref, Packet: e.Sequence, Reason: reason})
}
func (p *process) AcceptHeader(peer int, ref types.Hash, h, r uint64) error {
	k := transfer{peer, ref}
	if !p.seen[k] {
		return fmt.Errorf("real header not received")
	}
	p.accepted[k] = true
	p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "header_accept", Type: network.Header, Height: h, Round: r, Ref: ref})
	return p.Send(peer, network.Message{Kind: network.Request, Ref: ref, Height: h, Round: r})
}

func (p *process) Deliver(ctx context.Context, e *pb.Envelope) (*pb.Ack, error) {
	if err := VerifyEnvelope(p.cfg.ChainID, p.cfg.NodeID, p.ids, e); err != nil {
		p.log.record(network.Event{Node: p.cfg.NodeID, Peer: int(e.GetSender()), Event: "reject_transport", Reason: err.Error()})
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	r := received{e, make(chan error, 1)}
	select {
	case p.in <- r:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	case <-p.ctx.Done():
		return nil, status.Error(codes.Unavailable, "node stopping")
	}
	select {
	case err := <-r.done:
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &pb.Ack{}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	case <-p.ctx.Done():
		return nil, status.Error(codes.Unavailable, "node stopping")
	}
}
func (p *process) receive(e *pb.Envelope) error {
	if !p.started {
		return network.Reject(fmt.Errorf("node awaiting experiment start"))
	}
	peer := int(e.Sender)
	now := p.now()
	if until := p.blocked[peer]; until > now {
		p.frameEvent(e, "peer_drop", "temporarily blocked")
		return nil
	}
	a := p.arrivals[peer]
	window := now / p.cfg.ReceiveWindowMS
	if window != a.window {
		a = arrival{window: window}
	}
	a.count++
	p.arrivals[peer] = a
	if a.count > p.cfg.ReceiveLimit {
		until := now + p.cfg.BlockMS
		p.blocked[peer] = until
		p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "block", Height: e.Height, Round: e.Round, Reason: "peer rate exceeded"})
		return p.Schedule(p.cfg.BlockMS, func() error {
			if p.blocked[peer] == until {
				delete(p.blocked, peer)
				p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "unblock", Height: e.Height, Round: e.Round})
			}
			return nil
		})
	}
	var ref types.Hash
	copy(ref[:], e.Reference)
	k := transfer{peer, ref}
	kind := network.Kind(e.Kind)
	if kind == network.Header {
		if crypt.HashBytes(e.Payload) != ref {
			return network.Reject(fmt.Errorf("wire header reference mismatch"))
		}
		p.seen[k] = true
	}
	if kind == network.Request {
		p.requested[k] = true
	}
	if kind == network.Body && !p.accepted[k] {
		return network.Reject(fmt.Errorf("wire body without accepted header"))
	}
	p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "receive", Type: kind, Height: e.Height, Round: e.Round, Ref: ref, Packet: e.Sequence})
	return p.runtime.Receive(network.Packet{Message: network.Message{Kind: kind, Ref: ref, Height: e.Height, Round: e.Round, Data: append([]byte(nil), e.Payload...)}, ID: e.Sequence, From: peer, To: p.cfg.NodeID})
}

// Each peer has independent IO. A shared send gate preserves the node-wide
// rate at actual transmission time without holding the gate during an RPC.
func (p *process) sender(wg *sync.WaitGroup) {
	defer wg.Done()
	var workers sync.WaitGroup
	queues := make([]chan *pb.Envelope, len(p.ids))
	for peer := range p.ids {
		if peer == p.cfg.NodeID {
			continue
		}
		jobs := make(chan *pb.Envelope, p.cfg.QueueLimit)
		queues[peer] = jobs
		workers.Add(1)
		go p.sendPeer(peer, jobs, &workers)
	}
	defer workers.Wait()
	for {
		select {
		case <-p.ctx.Done():
			return
		case e := <-p.jobs:
			select {
			case queues[int(e.Recipient)] <- e:
			default:
				p.queued.Add(-1)
				p.frameEvent(e, "queue_full", "peer transmit queue full; retry required")
			}
		}
	}
}
func (p *process) sendPeer(peer int, jobs <-chan *pb.Envelope, wg *sync.WaitGroup) {
	defer wg.Done()
	conn, err := grpc.NewClient(p.cfg.Peers[peer].Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "connection_error", Reason: err.Error()})
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			p.Record(network.Event{Node: p.cfg.NodeID, Peer: peer, Event: "close_error", Reason: err.Error()})
		}
	}()
	client := pb.NewTransportClient(conn)
	for {
		select {
		case <-p.ctx.Done():
			return
		case e := <-jobs:
			p.rateMu.Lock()
			if wait := time.Until(p.nextSend); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-p.ctx.Done():
					timer.Stop()
					p.rateMu.Unlock()
					return
				}
			}
			if p.ctx.Err() != nil {
				p.rateMu.Unlock()
				return
			}
			p.frameEvent(e, "send", "")
			p.nextSend = time.Now().Add(time.Duration(p.cfg.SendGapMS) * time.Millisecond)
			p.rateMu.Unlock()
			ctx, cancel := context.WithTimeout(p.ctx, 250*time.Millisecond)
			_, err := client.Deliver(ctx, e)
			cancel()
			p.queued.Add(-1)
			if err != nil {
				p.frameEvent(e, "send_error", err.Error())
			} else {
				p.frameEvent(e, "delivered", "")
			}
		}
	}
}
func (p *process) save() error {
	root, err := p.runtime.StateRoot()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(Result{NodeID: p.cfg.NodeID, PID: os.Getpid(), StateRoot: hex.EncodeToString(root[:]), Ledger: p.runtime.Ledger()}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(p.cfg.OutputDir, "result.json")
	if err = os.WriteFile(path+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// Run owns one node. It serves until its context/deadline so peers can fetch
// decisions after this node finishes. Existing outputs cannot be resumed safely.
func Run(parent context.Context, c Config) (resultErr error) {
	return runWithNode(parent, c, func(p *process) (localNode, error) {
		return node.New(node.Config{ID: c.NodeID, ChainID: c.ChainID, F: c.F, Validators: p.ids, PrivateKey: p.key, BaseTimeout: c.BaseTimeoutMS, RetryInterval: c.RetryMS, TargetHeights: c.TargetHeights, Workload: c.Workload}, p)
	})
}

func runWithNode(parent context.Context, c Config, makeNode func(*process) (localNode, error)) (resultErr error) {
	if err := c.Validate(); err != nil {
		return err
	}
	ids, key, err := c.keys()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(c.OutputDir, 0700); err != nil {
		return err
	}
	lockPath := filepath.Join(c.OutputDir, "running.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Close(); err != nil && resultErr == nil {
			resultErr = err
		}
		if err := os.Remove(lockPath); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	if _, err = os.Stat(filepath.Join(c.OutputDir, "result.json")); err == nil {
		return fmt.Errorf("existing ledger output: use a fresh run directory; recovery is not implemented")
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(filepath.Join(c.OutputDir, "events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	ctx, cancel := context.WithTimeout(parent, time.Duration(c.RunForMS)*time.Millisecond)
	defer cancel()
	p := &process{cfg: c, ctx: ctx, key: key, ids: ids, log: &logWriter{file: file, start: time.Now()}, in: make(chan received, c.QueueLimit), jobs: make(chan *pb.Envelope, c.QueueLimit), faults: newFaultLayer(c.Faults), seen: make(map[transfer]bool), accepted: make(map[transfer]bool), requested: make(map[transfer]bool), blocked: make(map[int]uint64), arrivals: make(map[int]arrival)}
	p.runtime, err = makeNode(p)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Peers[c.NodeID].Address)
	if err != nil {
		return err
	}
	server := grpc.NewServer(grpc.MaxRecvMsgSize(4 << 20))
	pb.RegisterTransportServer(server, p)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	var wg sync.WaitGroup
	wg.Add(1)
	go p.sender(&wg)
	defer func() {
		cancel()
		server.Stop()
		wg.Wait()
		if err := p.log.failure(); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	p.Record(network.Event{Node: c.NodeID, Peer: -1, Event: "listen", Reason: fmt.Sprintf("pid=%d address=%s", os.Getpid(), listener.Addr())})
	start := func() error {
		p.started = true
		p.Record(network.Event{Node: c.NodeID, Peer: -1, Event: "experiment_start"})
		if c.Faults.DisableAfterMS > 0 {
			if err := p.Schedule(c.Faults.DisableAfterMS, func() error { p.Record(network.Event{Node: c.NodeID, Peer: -1, Event: "faults_disabled"}); return nil }); err != nil {
				return err
			}
		}
		return p.runtime.Start()
	}
	if delay := c.StartAtUnixMS - time.Now().UnixMilli(); c.StartAtUnixMS > 0 && delay > 0 {
		if err = p.Schedule(uint64(delay), start); err != nil {
			return err
		}
	} else if err = start(); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(c.OutputDir, "ready"), []byte(listener.Addr().String()), 0600); err != nil {
		return err
	}
	for {
		if err = p.log.failure(); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return p.save()
		}
		if len(p.queue) > 0 && !p.queue[0].at.After(time.Now()) {
			t := heap.Pop(&p.queue).(task)
			if err = t.run(); err != nil {
				return err
			}
			continue
		}
		wait := time.Hour
		if len(p.queue) > 0 {
			wait = time.Until(p.queue[0].at)
			if wait < 0 {
				wait = 0
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return p.save()
		case err := <-serveErr:
			timer.Stop()
			if err != nil {
				return err
			}
			return fmt.Errorf("gRPC server unexpectedly stopped")
		case r := <-p.in:
			timer.Stop()
			err = p.receive(r.frame)
			if err != nil {
				p.frameEvent(r.frame, "reject", err.Error())
				var rejection *network.Rejection
				if !errors.As(err, &rejection) {
					r.done <- err
					return err
				}
			}
			r.done <- nil
		case <-timer.C:
		}
	}
}
