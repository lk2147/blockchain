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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/ledger"
	"blockchain-lab01/src/network"
	"blockchain-lab01/src/node"
	pb "blockchain-lab01/src/proto"
	"blockchain-lab01/src/types"
	"google.golang.org/grpc"
)

type Result struct {
	NodeID, PID int
	StateRoot   string
	Height      uint64
}
type logWriter struct {
	mu    sync.Mutex
	file  *os.File
	start time.Time
	seq   uint64
	err   error
	trace bool
	last  map[string]time.Time
}

func (l *logWriter) record(e network.Event) {
	if !l.trace && !importantEvent(e.Event) {
		if (e.Event != "reject" && e.Event != "consensus_reject") || (!strings.Contains(e.Reason, "signature") && !strings.Contains(e.Reason, "domain") && !strings.Contains(e.Reason, "conflicting vote")) {
			return
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return
	}
	if !l.trace && (e.Event == "transaction_reject" || strings.HasPrefix(e.Event, "fault_") || e.Event == "queue_full" || e.Event == "reject" || e.Event == "consensus_reject") {
		if l.last == nil {
			l.last = make(map[string]time.Time)
		}
		key := e.Event + string(e.Type)
		if e.Event == "transaction_reject" || e.Event == "reject" || e.Event == "consensus_reject" {
			key += e.Reason
		}
		now := time.Now()
		if now.Sub(l.last[key]) < time.Second {
			return
		}
		l.last[key] = now
	}
	l.seq++
	e.Seq = l.seq
	e.At = uint64(time.Since(l.start) / time.Millisecond)
	if l.trace {
		l.err = json.NewEncoder(l.file).Encode(struct {
			WallTime string
			network.Event
		}{time.Now().UTC().Format(time.RFC3339Nano), e})
	} else {
		ref := ""
		if e.Ref != (types.Hash{}) {
			ref = hex.EncodeToString(e.Ref[:])
		}
		l.err = json.NewEncoder(l.file).Encode(struct {
			Time   string
			Node   int
			Event  string
			Height uint64 `json:",omitempty"`
			Round  uint64 `json:",omitempty"`
			Hash   string `json:",omitempty"`
			Root   string `json:",omitempty"`
			Detail string `json:",omitempty"`
		}{time.Now().Format("15:04:05.000"), e.Node, e.Event, e.Height, e.Round, ref, e.StateRoot, e.Reason})
		fmt.Printf("%s node=%d %-22s height=%d round=%d %s %s\n", time.Now().Format("15:04:05"), e.Node, e.Event, e.Height, e.Round, e.Reason, e.StateRoot)
	}
}
func importantEvent(e string) bool {
	switch e {
	case "node_stop":
		return true
	case "listen", "membership", "mempool_accept", "transaction_reject", "transaction_replay", "consensus_proposal", "consensus_timeout", "consensus_lock", "consensus_vote_signed", "state_commit", "proposer_crash", "block", "unblock", "reject_handshake", "ledger_error", "fault_drop", "fault_duplicate", "fault_reorder", "fault_replay", "queue_full", "behavior", "behavior_message":
		return true
	}
	return false
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
	tx    *types.Transaction
	query chan runtimeSnapshot
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
	Submit(types.Transaction) error
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
	gossip                    *gossipNetwork
	sessionMu                 sync.Mutex
	sessions                  map[int]*peerSession
	streamWG                  sync.WaitGroup
	store                     *ledger.Store
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
	if peer < network.Broadcast || peer >= len(p.ids) || peer == p.cfg.NodeID {
		return fmt.Errorf("invalid real peer")
	}
	if peer == network.Broadcast && m.Kind != network.Header && m.Kind != network.Vote && m.Kind != network.Transaction {
		return fmt.Errorf("only headers and votes use GossipSub broadcast")
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
	peer := int(e.Recipient)
	if e.Recipient == ^uint32(0) {
		peer = network.Broadcast
	}
	p.Record(network.Event{Node: int(e.Sender), Peer: peer, Event: kind, Type: network.Kind(e.Kind), Height: e.Height, Round: e.Round, Ref: ref, Packet: e.Sequence, Reason: reason})
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

func (p *process) acceptFrame(ctx context.Context, e *pb.Envelope) error {
	if err := VerifyEnvelope(p.cfg.ChainID, p.cfg.NodeID, p.ids, e); err != nil {
		p.log.record(network.Event{Node: p.cfg.NodeID, Peer: int(e.GetSender()), Event: "reject_transport", Reason: err.Error()})
		return err
	}
	return p.enqueueVerified(ctx, e)
}

// Called only after transport authentication, either by acceptFrame or the
// GossipSub topic validator. Both paths still execute all protocol validation.
func (p *process) enqueueVerified(ctx context.Context, e *pb.Envelope) error {
	r := received{frame: e, done: make(chan error, 1)}
	select {
	case p.in <- r:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
	select {
	case err := <-r.done:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return p.ctx.Err()
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
			if e.Recipient == ^uint32(0) {
				if err := p.transmit(e, func() error { return p.gossip.publish(e) }); err != nil && p.ctx.Err() == nil {
					p.frameEvent(e, "send_error", err.Error())
				}
				p.queued.Add(-1)
				continue
			}
			select {
			case queues[int(e.Recipient)] <- e:
			default:
				p.queued.Add(-1)
				p.frameEvent(e, "queue_full", "peer transmit queue full; retry required")
			}
		}
	}
}

// Both transports share the actual-send gate, after their configured holds.
func (p *process) transmit(e *pb.Envelope, send func() error) error {
	p.rateMu.Lock()
	if wait := time.Until(p.nextSend); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-p.ctx.Done():
			timer.Stop()
			p.rateMu.Unlock()
			return p.ctx.Err()
		}
	}
	if p.ctx.Err() != nil {
		p.rateMu.Unlock()
		return p.ctx.Err()
	}
	p.frameEvent(e, "send", "")
	p.nextSend = time.Now().Add(time.Duration(p.cfg.SendGapMS) * time.Millisecond)
	p.rateMu.Unlock()
	return send()
}
func (p *process) save() error {
	root, err := p.runtime.StateRoot()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(Result{NodeID: p.cfg.NodeID, PID: os.Getpid(), StateRoot: hex.EncodeToString(root[:]), Height: p.store.Height()}, "", "  ")
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
		c = p.cfg
		return node.New(node.Config{ID: c.NodeID, ChainID: c.ChainID, F: c.F, Validators: p.ids, PrivateKey: p.key, BaseTimeout: c.BaseTimeoutMS, RetryInterval: c.RetryMS, TargetHeights: c.TargetHeights, Store: p.store, Behavior: c.Behavior, MempoolLimit: c.MempoolLimit, MempoolBytes: c.MempoolBytes, MaxTxBytes: c.MaxTxBytes, MaxBlockTXs: c.MaxBlockTXs, MaxBlockBytes: c.MaxBlockBytes}, p)
	})
}

func runWithNode(parent context.Context, c Config, makeNode func(*process) (localNode, error)) (resultErr error) {
	c.Defaults()
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
	ctx, cancel := context.WithCancel(parent)
	if c.RunForMS > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(parent, time.Duration(c.RunForMS)*time.Millisecond)
	}
	defer cancel()
	store, err := ledger.Open(filepath.Join(c.OutputDir, "ledger.db"), c.ChainID, false)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	if store.Height() != 0 {
		return fmt.Errorf("ledger exists on disk; validator signing-state recovery is not implemented; inspect it or use a fresh run directory")
	}
	p := &process{cfg: c, ctx: ctx, key: key, ids: ids, log: &logWriter{file: file, start: time.Now(), trace: c.Trace}, in: make(chan received, c.QueueLimit), jobs: make(chan *pb.Envelope, c.QueueLimit), faults: newFaultLayer(c.Faults), seen: make(map[transfer]bool), accepted: make(map[transfer]bool), requested: make(map[transfer]bool), blocked: make(map[int]uint64), arrivals: make(map[int]arrival), sessions: make(map[int]*peerSession), store: store}
	defer func() {
		if resultErr != nil {
			p.Record(network.Event{Node: c.NodeID, Peer: -1, Event: "node_stop", Reason: resultErr.Error()})
		}
	}()
	p.runtime, err = makeNode(p)
	if err != nil {
		return err
	}
	p.gossip, err = p.newGossip()
	if err != nil {
		return err
	}
	defer func() {
		cancel()
		if err := p.gossip.close(); err != nil && resultErr == nil {
			resultErr = err
		}
		if err := p.log.failure(); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	listener, err := net.Listen("tcp", c.Peers[c.NodeID].Address)
	if err != nil {
		return err
	}
	server := grpc.NewServer(grpc.MaxRecvMsgSize(4<<20), grpc.WaitForHandlers(true))
	pb.RegisterTransportServer(server, p)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	adminListener, err := net.Listen("tcp", c.AdminAddress)
	if err != nil {
		server.Stop()
		return err
	}
	adminServer := &http.Server{Handler: p.adminHandler(), ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 30 * time.Second}
	adminErr := make(chan error, 1)
	go func() { adminErr <- adminServer.Serve(adminListener) }()
	var wg sync.WaitGroup
	wg.Add(1)
	go p.sender(&wg)
	defer func() {
		cancel()
		_ = adminServer.Close()
		server.Stop()
		wg.Wait()
		p.streamWG.Wait()
		if err := p.log.failure(); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	p.streamWG.Add(1)
	go func() { defer p.streamWG.Done(); p.gossip.connect() }()
	p.Record(network.Event{Node: c.NodeID, Peer: -1, Event: "listen", Reason: fmt.Sprintf("pid=%d address=%s admin=%s", os.Getpid(), listener.Addr(), adminListener.Addr())})
	p.Record(network.Event{Node: c.NodeID, Peer: -1, Event: "behavior", Reason: c.Behavior})
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
	if err = os.WriteFile(filepath.Join(c.OutputDir, "admin"), []byte(adminListener.Addr().String()), 0600); err != nil {
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
		case err := <-adminErr:
			timer.Stop()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return fmt.Errorf("admin HTTP server unexpectedly stopped")
		case r := <-p.in:
			timer.Stop()
			if r.query != nil {
				snapshot := runtimeSnapshot{}
				reader, ok := p.runtime.(interface{ MempoolSnapshot() []types.Transaction })
				if !ok {
					snapshot.err = fmt.Errorf("mempool inspection unavailable for this node runtime")
				} else {
					snapshot.transactions = reader.MempoolSnapshot()
					snapshot.root, snapshot.err = p.runtime.StateRoot()
				}
				r.query <- snapshot
				continue
			}
			if r.tx != nil {
				if !p.started {
					err = network.Reject(fmt.Errorf("node awaiting experiment start"))
				} else {
					err = p.runtime.Submit(*r.tx)
				}
				if err != nil {
					var rejection *network.Rejection
					if !errors.As(err, &rejection) {
						r.done <- err
						return err
					}
				}
				r.done <- err
				continue
			}
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
