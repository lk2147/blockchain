package realnet

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"

	"blockchain-lab01/src/block"
	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/network"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

// These test-only peers run in their own OS processes. They possess only their
// own configured private key. No honest state/key is exposed to this driver.
type attackHeader struct {
	Proposal *cons.Proposal  `json:",omitempty"`
	Decision *cons.Finalized `json:",omitempty"`
}
type attackBlock struct {
	header []byte
	body   []byte
	hash   types.Hash
	branch string
}
type faultyPeer struct {
	p        *process
	role     string
	ads      map[types.Hash]attackBlock
	incoming map[transfer]attackHeader
	branches map[string]types.Hash
	voted    bool
	replayed map[int]bool
	workload [][]types.Transaction
	begun    bool
}

func newFaultyPeer(p *process, role string) (localNode, error) {
	switch role {
	case "silent", "invalid", "transaction_replay", "equivocate":
	default:
		return nil, fmt.Errorf("unknown test role")
	}
	raw, err := os.ReadFile(os.Getenv("LAB01_TEST_WORKLOAD"))
	if err != nil {
		return nil, err
	}
	var workload [][]types.Transaction
	if err = json.Unmarshal(raw, &workload); err != nil {
		return nil, err
	}
	return &faultyPeer{p: p, role: role, ads: make(map[types.Hash]attackBlock), incoming: make(map[transfer]attackHeader), branches: make(map[string]types.Hash), replayed: make(map[int]bool), workload: workload}, nil
}
func (a *faultyPeer) Submit(types.Transaction) error {
	return network.Reject(fmt.Errorf("faulty test peer does not accept client input"))
}
func (a *faultyPeer) Ledger() []cons.Finalized       { return nil } // Faulty peers do not claim honest finality.
func (a *faultyPeer) StateRoot() (types.Hash, error) { return (state.State{}).Root(a.p.cfg.ChainID) }
func (a *faultyPeer) Start() error {
	a.p.Record(network.Event{Node: a.p.cfg.NodeID, Peer: -1, Event: "adversary_start", Reason: a.role})
	return nil // Begin the attack only after the real client transaction arrives.
}
func (a *faultyPeer) begin() error {
	if a.role == "silent" || a.p.cfg.NodeID != 0 {
		return nil
	}
	if a.role == "equivocate" {
		if err := a.makeBlock("A", false, false); err != nil {
			return err
		}
		if err := a.makeBlock("B", false, false); err != nil {
			return err
		}
	} else if err := a.makeBlock("A", a.role == "transaction_replay", a.role == "invalid"); err != nil {
		return err
	}
	return a.campaign()
}
func (a *faultyPeer) makeBlock(branch string, duplicate, badSignature bool) error {
	tx := a.workload[0][0]
	tx.Value = append([]byte(nil), tx.Value...)
	if branch == "B" {
		tx.Value = []byte("conflicting Byzantine value")
		var err error
		tx, err = crypt.SignTransaction(tx, a.p.key)
		if err != nil {
			return err
		}
	}
	b, err := block.Build(block.Context{ChainID: a.p.cfg.ChainID, Height: 1, Proposer: a.p.ids[0]}, state.State{}, []types.Transaction{tx}, a.p.key)
	if err != nil {
		return err
	}
	if duplicate {
		b.Body.Transactions = append(b.Body.Transactions, tx)
		b.Header.BodyHash, err = crypt.BodyHash(a.p.cfg.ChainID, b.Body)
		if err != nil {
			return err
		}
		// Sign the inconsistent candidate intentionally. Honest execution must
		// reject the repeated nonce rather than silently applying its prefix.
		b.Header, err = crypt.SignHeader(b.Header, a.p.key)
		if err != nil {
			return err
		}
	}
	p, err := cons.SignProposal(cons.Proposal{ChainID: a.p.cfg.ChainID, Height: 1, Proposer: a.p.ids[0], Block: b}, a.p.key)
	if err != nil {
		return err
	}
	if badSignature {
		p.Signature[0] ^= 1
	}
	body, err := json.Marshal(b.Body)
	if err != nil {
		return err
	}
	p.Block.Body = types.BlockBody{}
	header, err := json.Marshal(attackHeader{Proposal: &p})
	if err != nil {
		return err
	}
	hash, err := crypt.BlockHash(b.Header)
	if err != nil {
		return err
	}
	ref := crypt.HashBytes(header)
	a.ads[ref] = attackBlock{header, body, hash, branch}
	a.branches[branch] = hash
	return nil
}
func (a *faultyPeer) sendHeader(peer int, ref types.Hash, b attackBlock) error {
	return a.p.Send(peer, network.Message{Kind: network.Header, Ref: ref, Height: 1, Data: b.header})
}
func (a *faultyPeer) campaign() error {
	// Branch order is fixed; never iterate a map to select what a peer sees.
	for _, branch := range []string{"A", "B"} {
		for ref, b := range a.ads {
			if b.branch != branch {
				continue
			}
			for peer := 1; peer < len(a.p.ids); peer++ {
				if a.role == "equivocate" && peer >= 3 && ((peer <= 6) != (branch == "A")) {
					continue
				}
				if err := a.sendHeader(peer, ref, b); err != nil {
					return err
				}
			}
		}
	}
	if a.role == "invalid" {
		v, err := crypt.SignVote(types.Vote{ChainID: a.p.cfg.ChainID, Height: 1, Phase: types.Prevote, Validator: a.p.ids[0], BlockHash: a.branches["A"]}, a.p.key)
		if err != nil {
			return err
		}
		payload, err := crypt.CanonicalVoteBytes(v)
		if err != nil {
			return err
		}
		payload[5] = 'X'
		v.Signature = types.Signature(ed25519.Sign(a.p.key, payload))
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		a.p.Record(network.Event{Node: 0, Peer: -1, Event: "adversary_wrong_domain", Ref: v.BlockHash, Reason: string(data)})
		for peer := 1; peer < len(a.p.ids); peer++ {
			if err = a.p.Send(peer, network.Message{Kind: network.Vote, Ref: v.BlockHash, Height: 1, Data: data}); err != nil {
				return err
			}
		}
	}
	if a.role == "equivocate" && !a.voted {
		if err := a.voteBoth(); err != nil {
			return err
		}
	}
	// Bad proposers repeat their messages, not their effect on honest state.
	return a.p.Schedule(300, a.campaign)
}
func (a *faultyPeer) voteBoth() error {
	if len(a.branches) != 2 || a.voted {
		return nil
	}
	a.voted = true
	for peer := 3; peer < len(a.p.ids); peer++ {
		first, second := "A", "B"
		if peer >= 7 {
			first, second = second, first
		}
		for _, branch := range []string{first, second} {
			for _, phase := range []types.VotePhase{types.Prevote, types.Precommit} {
				v, err := crypt.SignVote(types.Vote{ChainID: a.p.cfg.ChainID, Height: 1, Phase: phase, Validator: a.p.ids[a.p.cfg.NodeID], BlockHash: a.branches[branch]}, a.p.key)
				if err != nil {
					return err
				}
				data, err := json.Marshal(v)
				if err != nil {
					return err
				}
				if err = a.p.Send(peer, network.Message{Kind: network.Vote, Ref: v.BlockHash, Height: 1, Data: data}); err != nil {
					return err
				}
				a.p.Record(network.Event{Node: a.p.cfg.NodeID, Peer: peer, Event: "adversary_vote", Packet: a.p.packet, Phase: uint8(phase), Ref: v.BlockHash, Reason: string(data)})
			}
		}
	}
	return nil
}
func (a *faultyPeer) Receive(packet network.Packet) error {
	if a.role == "silent" {
		return nil
	}
	switch packet.Kind {
	case network.Transaction:
		if a.p.cfg.NodeID != 0 || a.begun || a.role == "silent" {
			return nil
		}
		var tx types.Transaction
		if err := json.Unmarshal(packet.Data, &tx); err != nil {
			return err
		}
		if err := crypt.VerifyTransaction(a.p.cfg.ChainID, tx); err != nil {
			return network.Reject(err)
		}
		a.workload[0] = []types.Transaction{tx}
		a.begun = true
		return a.begin()
	case network.Request:
		if b, ok := a.ads[packet.Ref]; ok {
			return a.p.Send(packet.From, network.Message{Kind: network.Body, Ref: packet.Ref, Height: 1, Data: b.body})
		}
		return nil
	case network.Header:
		var h attackHeader
		if err := json.Unmarshal(packet.Data, &h); err != nil {
			return network.Reject(err)
		}
		if a.role == "transaction_replay" && h.Decision != nil && h.Decision.Block.Header.Height == 1 && !a.replayed[packet.From] {
			a.replayed[packet.From] = true
			for ref, b := range a.ads {
				if err := a.sendHeader(packet.From, ref, b); err != nil {
					return err
				}
				a.p.Record(network.Event{Node: a.p.cfg.NodeID, Peer: packet.From, Event: "adversary_transaction_replay", Ref: ref, Packet: a.p.packet})
			}
		}
		if a.role != "equivocate" || a.p.cfg.NodeID == 0 || h.Proposal == nil || h.Proposal.Height != 1 || h.Proposal.Round != 0 {
			return nil
		}
		if err := cons.VerifyProposal(a.p.cfg.ChainID, *h.Proposal); err != nil {
			return network.Reject(err)
		}
		a.incoming[transfer{packet.From, packet.Ref}] = h
		return a.p.AcceptHeader(packet.From, packet.Ref, 1, 0)
	case network.Body:
		if a.role != "equivocate" || a.p.cfg.NodeID == 0 {
			return nil
		}
		h, ok := a.incoming[transfer{packet.From, packet.Ref}]
		if !ok {
			return network.Reject(fmt.Errorf("unknown adversary transfer"))
		}
		var body types.BlockBody
		if err := json.Unmarshal(packet.Data, &body); err != nil {
			return network.Reject(err)
		}
		bh, err := crypt.BodyHash(a.p.cfg.ChainID, body)
		if err != nil {
			return err
		}
		if bh != h.Proposal.Block.Header.BodyHash {
			return network.Reject(fmt.Errorf("adversary body mismatch"))
		}
		known, err := crypt.BodyHash(a.p.cfg.ChainID, types.BlockBody{Transactions: a.workload[0]})
		if err != nil {
			return err
		}
		branch := "A"
		if bh != known {
			branch = "B"
		}
		hash, err := crypt.BlockHash(h.Proposal.Block.Header)
		if err != nil {
			return err
		}
		a.branches[branch] = hash
		return a.voteBoth()
	}
	return nil
}
