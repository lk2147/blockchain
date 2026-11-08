// Package node owns blockchain message handling independently of gRPC IO.
package node

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/network"
	"blockchain-lab01/src/types"
)

// Port isolates the node event loop from gRPC IO and real timer/log handling.
// Runtime and its Port are owned by one event loop; do not call concurrently.
type Port interface {
	Send(int, network.Message) error
	AcceptHeader(int, types.Hash, uint64, uint64) error
	Schedule(uint64, func() error) error
	Record(network.Event)
}
type Config struct {
	ID                                        int
	ChainID                                   string
	F                                         int
	Validators                                []types.ValidatorID
	PrivateKey                                ed25519.PrivateKey
	BaseTimeout, RetryInterval, TargetHeights uint64
	Workload                                  [][]types.Transaction
	Silent                                    bool
}
type wireHeader struct {
	Proposal *cons.Proposal  `json:",omitempty"`
	Decision *cons.Finalized `json:",omitempty"`
}
type advertised struct {
	ref           types.Hash
	header, body  []byte
	height, round uint64
	decision      bool
}
type remoteKey struct {
	peer int
	ref  types.Hash
}
type slot struct{ height, round uint64 }
type Runtime struct {
	port          Port
	peers         int
	id            int
	identity      types.ValidatorID
	engine        *cons.Engine
	chain         string
	target, retry uint64
	workload      [][]types.Transaction
	silent        bool
	attempted     map[slot]bool
	ads           []advertised
	votes         []types.Vote
	remote        map[remoteKey]wireHeader
}

func New(c Config, port Port) (*Runtime, error) {
	if port == nil || c.ID < 0 || c.ID >= len(c.Validators) || c.TargetHeights == 0 || c.RetryInterval == 0 {
		return nil, fmt.Errorf("invalid node configuration")
	}
	e, err := cons.New(cons.Config{ChainID: c.ChainID, Validators: c.Validators, F: c.F, PrivateKey: c.PrivateKey, BaseTimeout: c.BaseTimeout})
	if err != nil {
		return nil, err
	}
	if types.ValidatorID(c.PrivateKey[32:]) != c.Validators[c.ID] {
		return nil, fmt.Errorf("node ID does not match private key")
	}
	n := &Runtime{port: port, peers: len(c.Validators), id: c.ID, identity: c.Validators[c.ID], engine: e, chain: c.ChainID, target: c.TargetHeights, retry: c.RetryInterval, silent: c.Silent, attempted: make(map[slot]bool), remote: make(map[remoteKey]wireHeader)}
	for _, txs := range c.Workload {
		copyTX := make([]types.Transaction, len(txs))
		for i, tx := range txs {
			copyTX[i] = tx
			copyTX[i].Value = append([]byte(nil), tx.Value...)
		}
		n.workload = append(n.workload, copyTX)
	}
	return n, nil
}
func (n *Runtime) Start() error {
	if err := n.process(); err != nil {
		return err
	}
	return n.scheduleRetry()
}
func (n *Runtime) Ledger() []cons.Finalized       { return n.engine.Ledger() }
func (n *Runtime) StateRoot() (types.Hash, error) { return n.engine.State().Root(n.chain) }
func (n *Runtime) Status() cons.Status            { return n.engine.Status() }
func (n *Runtime) send(peer int, m network.Message) error {
	if n.silent {
		return nil
	}
	err := n.port.Send(peer, m)
	if errors.Is(err, network.ErrQueueFull) {
		return nil
	} // Logged admission failure; periodic retry will try again.
	return err
}
func (n *Runtime) broadcast(m network.Message) error {
	for peer := 0; peer < n.peers; peer++ {
		if peer != n.id {
			if err := n.send(peer, m); err != nil {
				return err
			}
		}
	}
	return nil
}
func (n *Runtime) announce(a advertised) error {
	return n.broadcast(network.Message{Kind: network.Header, Ref: a.ref, Height: a.height, Round: a.round, Data: a.header})
}
func (n *Runtime) advertise(h wireHeader, body types.BlockBody, height, round uint64, decision bool) error {
	header, err := json.Marshal(h)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	a := advertised{ref: crypt.HashBytes(header), header: header, body: payload, height: height, round: round, decision: decision}
	n.ads = append(n.ads, a)
	return n.announce(a)
}

func (n *Runtime) process() error {
	s := n.engine.Status()
	if !n.silent && s.Height <= n.target && s.Step == cons.Propose && !n.attempted[slot{s.Height, s.Round}] && n.engine.Proposer(s.Height, s.Round) == n.identity {
		n.attempted[slot{s.Height, s.Round}] = true
		var txs []types.Transaction
		if s.Height <= uint64(len(n.workload)) {
			txs = n.workload[s.Height-1]
		}
		if _, err := n.engine.MakeProposal(txs); err != nil {
			n.port.Record(network.Event{Node: n.id, Peer: -1, Event: "proposal_failed", Height: s.Height, Round: s.Round, Reason: err.Error()})
		}
	}
	o := n.engine.Drain()
	for _, e := range o.Events {
		n.port.Record(network.Event{Node: n.id, Peer: -1, Event: "consensus_" + e.Kind, Height: e.Height, Round: e.Round, Ref: e.Hash, Reason: e.Reason, Phase: uint8(e.Phase), Step: uint8(e.Step), Validator: hex.EncodeToString(e.Validator[:])})
	}
	for _, timer := range o.Timers {
		timer := timer
		if timer.Height > n.target {
			continue
		}
		if err := n.port.Schedule(timer.Delay, func() error {
			if n.engine.Status().Height > n.target {
				return nil
			}
			if err := n.engine.Timeout(timer); err != nil {
				n.port.Record(network.Event{Node: n.id, Peer: -1, Event: "timer_reject", Height: timer.Height, Round: timer.Round, Reason: err.Error()})
			}
			return n.process()
		}); err != nil {
			return err
		}
	}
	for _, p := range o.Proposals {
		p := p
		body := p.Block.Body
		p.Block.Body = types.BlockBody{}
		if err := n.advertise(wireHeader{Proposal: &p}, body, p.Height, p.Round, false); err != nil {
			return err
		}
	}
	for _, v := range o.Votes {
		n.votes = append(n.votes, v)
		payload, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err = n.broadcast(network.Message{Kind: network.Vote, Height: v.Height, Round: v.Round, Data: payload}); err != nil {
			return err
		}
	}
	for _, f := range o.Finalized {
		f := f
		n.port.Record(network.Event{Node: n.id, Peer: -1, Event: "state_commit", Height: f.Block.Header.Height, Round: f.Round, StateRoot: hex.EncodeToString(f.Block.Header.StateRoot[:])})
		body := f.Block.Body
		f.Block.Body = types.BlockBody{}
		if err := n.advertise(wireHeader{Decision: &f}, body, f.Block.Header.Height, f.Round, true); err != nil {
			return err
		}
	}
	return nil
}

func (n *Runtime) scheduleRetry() error {
	return n.port.Schedule(n.retry, func() error {
		if !n.silent {
			s := n.engine.Status()
			for _, a := range n.ads {
				if a.decision || (a.height == s.Height && a.round == s.Round) {
					if err := n.announce(a); err != nil {
						return err
					}
				}
			}
			for _, v := range n.votes {
				if v.Height != s.Height {
					continue
				}
				payload, err := json.Marshal(v)
				if err != nil {
					return err
				}
				if err = n.broadcast(network.Message{Kind: network.Vote, Height: v.Height, Round: v.Round, Data: payload}); err != nil {
					return err
				}
			}
		}
		return n.scheduleRetry()
	})
}

func (n *Runtime) Receive(p network.Packet) error {
	if n.silent {
		return nil
	}
	switch p.Kind {
	case network.Header:
		var h wireHeader
		if err := json.Unmarshal(p.Data, &h); err != nil {
			return network.Reject(err)
		}
		if (h.Proposal == nil) == (h.Decision == nil) {
			return network.Reject(fmt.Errorf("header must contain exactly one envelope"))
		}
		var err error
		if h.Proposal != nil {
			if h.Proposal.Height != p.Height || h.Proposal.Round != p.Round {
				return network.Reject(fmt.Errorf("proposal transport scope mismatch"))
			}
			if len(h.Proposal.Block.Body.Transactions) > 0 {
				return network.Reject(fmt.Errorf("header contains body"))
			}
			err = n.engine.ValidateProposalHeader(*h.Proposal)
		} else {
			if h.Decision.Block.Header.Height != p.Height || h.Decision.Round != p.Round {
				return network.Reject(fmt.Errorf("decision transport scope mismatch"))
			}
			if len(h.Decision.Block.Body.Transactions) > 0 {
				return network.Reject(fmt.Errorf("decision header contains body"))
			}
			err = n.engine.ValidateDecisionHeader(*h.Decision)
		}
		if err != nil {
			return network.Reject(err)
		}
		n.remote[remoteKey{p.From, p.Ref}] = h
		err = n.port.AcceptHeader(p.From, p.Ref, p.Height, p.Round)
		if errors.Is(err, network.ErrQueueFull) {
			return nil
		}
		return err
	case network.Request:
		for _, a := range n.ads {
			if a.ref == p.Ref {
				return n.send(p.From, network.Message{Kind: network.Body, Ref: p.Ref, Height: a.height, Round: a.round, Data: a.body})
			}
		}
		return network.Reject(fmt.Errorf("requested body unavailable"))
	case network.Body:
		h, ok := n.remote[remoteKey{p.From, p.Ref}]
		if !ok {
			return network.Reject(fmt.Errorf("body has no accepted matching header"))
		}
		var body types.BlockBody
		if err := json.Unmarshal(p.Data, &body); err != nil {
			return network.Reject(err)
		}
		var header types.BlockHeader
		if h.Proposal != nil {
			header = h.Proposal.Block.Header
		} else {
			header = h.Decision.Block.Header
		}
		if header.Height != p.Height {
			return network.Reject(fmt.Errorf("body transport height mismatch"))
		}
		hash, err := crypt.BodyHash(n.chain, body)
		if err != nil {
			return network.Reject(err)
		}
		if hash != header.BodyHash {
			return network.Reject(fmt.Errorf("body commitment mismatch"))
		}
		if h.Proposal != nil {
			proposal := *h.Proposal
			proposal.Block.Body = body
			err = n.engine.ReceiveProposal(proposal)
		} else {
			decision := *h.Decision
			decision.Block.Body = body
			err = n.engine.ReceiveFinalized(decision)
		}
		if err != nil {
			n.port.Record(network.Event{Node: n.id, Peer: p.From, Event: "reject", Type: p.Kind, Height: p.Height, Round: p.Round, Ref: p.Ref, Reason: err.Error()})
		}
		return n.process()
	case network.Vote:
		var v types.Vote
		if err := json.Unmarshal(p.Data, &v); err != nil {
			return network.Reject(err)
		}
		if v.Height != p.Height || v.Round != p.Round {
			return network.Reject(fmt.Errorf("vote transport scope mismatch"))
		}
		if err := n.engine.ReceiveVote(v); err != nil {
			n.port.Record(network.Event{Node: n.id, Peer: p.From, Event: "reject", Type: p.Kind, Height: v.Height, Round: v.Round, Ref: v.BlockHash, Phase: uint8(v.Phase), Validator: hex.EncodeToString(v.Validator[:]), Reason: err.Error()})
		}
		return n.process()
	}
	return network.Reject(fmt.Errorf("unknown message kind"))
}
