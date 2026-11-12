package consensus_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"blockchain-lab01/src/block"
	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

type cluster struct {
	nodes []*cons.Engine
	keys  []ed25519.PrivateKey
	ids   []types.ValidatorID
}

func TestDiskFailureCannotCommit(t *testing.T) {
	c := setup(t, 4)
	writes := 0
	e, err := cons.New(cons.Config{ChainID: "lab", Validators: c.ids, F: 1, PrivateKey: c.keys[0], BaseTimeout: 10, Persist: func(cons.Finalized) error { writes++; return errors.New("disk full") }})
	if err != nil {
		t.Fatal(err)
	}
	before, err := e.State().Root("lab")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.MakeProposal(nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := blockHash(t, p)
	feed(t, e, signedVote(t, c, 1, 0, types.Prevote, hash))
	feed(t, e, signedVote(t, c, 2, 0, types.Prevote, hash))
	feed(t, e, signedVote(t, c, 1, 0, types.Precommit, hash))
	err = e.ReceiveVote(signedVote(t, c, 2, 0, types.Precommit, hash))
	if !errors.Is(err, cons.ErrPersistence) || writes != 1 {
		t.Fatalf("disk failure not propagated: %v writes=%d", err, writes)
	}
	after, err := e.State().Root("lab")
	if err != nil {
		t.Fatal(err)
	}
	if before != after || e.Status().Height != 1 || len(e.Ledger()) != 0 {
		t.Fatal("committed despite failed durable write")
	}
	if len(e.Drain().Finalized) != 0 {
		t.Fatal("announced a decision before durable storage")
	}
}

func setup(t *testing.T, n int) cluster {
	t.Helper()
	var c cluster
	for i := 0; i < n; i++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, 32))
		c.keys = append(c.keys, key)
		c.ids = append(c.ids, types.ValidatorID(key[32:]))
	}
	for i := 0; i < n; i++ {
		e, err := cons.New(cons.Config{ChainID: "lab", Validators: c.ids, F: (n - 1) / 3, PrivateKey: c.keys[i], BaseTimeout: 10})
		if err != nil {
			t.Fatal(err)
		}
		c.nodes = append(c.nodes, e)
		e.Drain()
	}
	return c
}
func signedVote(t *testing.T, c cluster, id int, round uint64, phase types.VotePhase, hash types.Hash) types.Vote {
	t.Helper()
	v, err := crypt.SignVote(types.Vote{ChainID: "lab", Height: 1, Round: round, Phase: phase, Validator: c.ids[id], IsNil: hash == (types.Hash{}), BlockHash: hash}, c.keys[id])
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func proposal(t *testing.T, c cluster, round uint64, value string) cons.Proposal {
	t.Helper()
	idx := int(round % uint64(len(c.ids)))
	pub := types.PublicKey(c.ids[idx])
	tx, err := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte(value), Nonce: 1}, c.keys[idx])
	if err != nil {
		t.Fatal(err)
	}
	b, err := block.Build(block.Context{ChainID: "lab", Height: 1, Proposer: c.ids[idx]}, state.State{}, []types.Transaction{tx}, c.keys[idx])
	if err != nil {
		t.Fatal(err)
	}
	p, err := cons.SignProposal(cons.Proposal{ChainID: "lab", Height: 1, Round: round, OriginRound: round, Proposer: c.ids[idx], Block: b}, c.keys[idx])
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func blockHash(t *testing.T, p cons.Proposal) types.Hash {
	t.Helper()
	h, err := crypt.BlockHash(p.Block.Header)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func expire(t *testing.T, e *cons.Engine) {
	t.Helper()
	s := e.Status()
	if err := e.Timeout(cons.Timer{Height: s.Height, Round: s.Round, Step: s.Step}); err != nil {
		t.Fatal(err)
	}
}
func reachRound(t *testing.T, e *cons.Engine, r uint64) {
	t.Helper()
	for i := 0; i < 30 && e.Status().Round < r; i++ {
		expire(t, e)
	}
	if e.Status().Round != r {
		t.Fatal("round did not advance")
	}
}
func feed(t *testing.T, e *cons.Engine, v types.Vote) {
	t.Helper()
	err := e.ReceiveVote(v)
	if err != nil && !errors.Is(err, cons.ErrDuplicate) {
		t.Fatal(err)
	}
}

// This harness only copies emitted messages. It never supplies a decision,
// modifies node internals or assigns ledger/state. Each receiver checks votes.
func pump(t *testing.T, c cluster, duplicate, reverse bool) {
	t.Helper()
	type msg struct {
		sender int
		p      *cons.Proposal
		v      *types.Vote
	}
	var queue []msg
	type signedSlot struct {
		height, round uint64
		phase         types.VotePhase
		id            types.ValidatorID
	}
	observed := make(map[signedSlot]bool)
	collect := func() {
		for i, e := range c.nodes {
			o := e.Drain()
			for _, p := range o.Proposals {
				p := p
				queue = append(queue, msg{sender: i, p: &p})
			}
			for _, v := range o.Votes {
				slot := signedSlot{v.Height, v.Round, v.Phase, v.Validator}
				if observed[slot] {
					t.Fatal("engine emitted more than one vote per signing slot")
				}
				observed[slot] = true
				v := v
				queue = append(queue, msg{sender: i, v: &v})
			}
		}
	}
	collect()
	for count := 0; len(queue) > 0; count++ {
		if count > 10000 {
			t.Fatal("message loop")
		}
		idx := 0
		if reverse {
			idx = len(queue) - 1
		}
		m := queue[idx]
		queue = append(queue[:idx], queue[idx+1:]...)
		for i, e := range c.nodes {
			if i == m.sender {
				continue
			}
			for repeat := 0; repeat < 1+btoi(duplicate); repeat++ {
				var err error
				if m.p != nil {
					err = e.ReceiveProposal(*m.p)
				} else {
					err = e.ReceiveVote(*m.v)
				}
				if err != nil && !errors.Is(err, cons.ErrDuplicate) && !strings.Contains(err.Error(), "duplicate/conflicting proposal") && !strings.Contains(err.Error(), "unexpected vote height") && !strings.Contains(err.Error(), "unexpected proposal height/round") {
					t.Fatal(err)
				}
			}
		}
		collect()
	}
}
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestTenNodeAgreementAndHeightAdvance(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordered", true: "duplicate-reverse"}[fault], func(t *testing.T) {
			c := setup(t, 10)
			for height := uint64(1); height <= 2; height++ {
				idx := int((height - 1) % 10)
				pub := types.PublicKey(c.ids[idx])
				tx, err := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte("value"), Nonce: 1}, c.keys[idx])
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.nodes[idx].MakeProposal([]types.Transaction{tx}); err != nil {
					t.Fatal(err)
				}
				pump(t, c, fault, fault)
				var hash, root types.Hash
				for i, e := range c.nodes {
					ledger := e.Ledger()
					if len(ledger) != int(height) || e.Status().Height != height+1 {
						t.Fatalf("node %d did not finalize height %d", i, height)
					}
					f := ledger[height-1]
					if len(f.Proof) < 7 {
						t.Fatal("missing quorum")
					}
					h, err := crypt.BlockHash(f.Block.Header)
					if err != nil {
						t.Fatal(err)
					}
					r, err := e.State().Root("lab")
					if err != nil {
						t.Fatal(err)
					}
					if i == 0 {
						hash = h
						root = r
					} else if h != hash || r != root {
						t.Fatal("conflicting finalized history/state")
					}
					if value, ok := e.State().Get(tx.TargetKey); !ok || string(value) != "value" || e.State().LastNonce(pub) != 1 {
						t.Fatal("wrong committed state")
					}
				}
			}
		})
	}
}

func TestVoteCountingAndRejections(t *testing.T) {
	c := setup(t, 4)
	e := c.nodes[3]
	p := proposal(t, c, 0, "A")
	hash := blockHash(t, p)
	if err := e.ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	e.Drain()
	v := signedVote(t, c, 0, 0, types.Prevote, hash)
	feed(t, e, v)
	if err := e.ReceiveVote(v); !errors.Is(err, cons.ErrDuplicate) {
		t.Fatal("duplicate not rejected", err)
	}
	if e.Status().Locked {
		t.Fatal("duplicate created quorum")
	}
	conflict := signedVote(t, c, 0, 0, types.Prevote, types.Hash{9})
	if err := e.ReceiveVote(conflict); !errors.Is(err, cons.ErrEquivocation) {
		t.Fatal("equivocation not rejected", err)
	}
	if len(e.Equivocations()) != 1 || e.Equivocations()[0].First != v || e.Equivocations()[0].Second != conflict {
		t.Fatal("equivocation evidence missing")
	}
	feed(t, e, signedVote(t, c, 1, 0, types.Prevote, hash))
	if !e.Status().Locked || e.Status().LockedHash != hash {
		t.Fatal("three unique prevotes did not lock")
	}
	if len(e.Ledger()) != 0 {
		t.Fatal("prevotes finalized block")
	}
	for _, v := range []types.Vote{signedVote(t, c, 0, 0, types.Precommit, hash), signedVote(t, c, 1, 1, types.Precommit, hash)} {
		feed(t, e, v)
	}
	if len(e.Ledger()) != 0 {
		t.Fatal("mixed round/phase votes formed quorum")
	}
	feed(t, e, signedVote(t, c, 1, 0, types.Precommit, hash))
	if len(e.Ledger()) != 1 {
		t.Fatal("precommit quorum failed")
	}
	if err := e.ReceiveVote(v); err == nil {
		t.Fatal("old-height replay accepted")
	}
	// Scope and authentication faults are tested with fresh receiver state.
	for _, fault := range []string{"chain", "height", "phase", "signature", "domain", "unknown"} {
		t.Run(fault, func(t *testing.T) {
			x := setup(t, 4)
			bad := signedVote(t, x, 0, 0, types.Prevote, hash)
			switch fault {
			case "chain":
				bad.ChainID = "other"
			case "height":
				bad.Height = 2
			case "phase":
				bad.Phase = 9
			case "signature":
				bad.Signature[0] ^= 1
			case "unknown":
				bad.Validator = types.ValidatorID{9}
			case "domain":
				payload, err := crypt.CanonicalVoteBytes(bad)
				if err != nil {
					t.Fatal(err)
				}
				payload[5] = 'X'
				bad.Signature = types.Signature(ed25519.Sign(x.keys[0], payload))
			}
			if err := x.nodes[3].ReceiveVote(bad); err == nil {
				t.Fatal("invalid vote accepted")
			}
			o := x.nodes[3].Drain()
			if len(o.Events) == 0 || o.Events[len(o.Events)-1].Kind != "reject" || o.Events[len(o.Events)-1].Reason == "" {
				t.Fatal("missing rejection reason")
			}
		})
	}
}

func TestTimeoutNilAndSilentProposer(t *testing.T) {
	c := setup(t, 4)
	for _, e := range c.nodes {
		expire(t, e)
		o := e.Drain()
		if len(o.Votes) != 1 || !o.Votes[0].IsNil || o.Votes[0].Phase != types.Prevote {
			t.Fatal("proposal timeout must prevote NIL")
		}
		expire(t, e)
		o = e.Drain()
		if len(o.Votes) != 1 || !o.Votes[0].IsNil || o.Votes[0].Phase != types.Precommit {
			t.Fatal("prevote timeout must precommit NIL")
		}
		expire(t, e)
		o = e.Drain()
		if e.Status().Round != 1 || len(o.Timers) == 0 || o.Timers[len(o.Timers)-1].Delay != 20 {
			t.Fatal("round/timeouts failed")
		}
	}
	stale := cons.Timer{Height: 1, Round: 0, Step: cons.Propose}
	before := c.nodes[1].Status()
	if c.nodes[1].Timeout(stale) == nil || c.nodes[1].Status() != before {
		t.Fatal("stale timer changed state")
	}
	if _, err := c.nodes[1].MakeProposal(nil); err != nil {
		t.Fatal(err)
	}
	pump(t, c, false, false)
	for _, e := range c.nodes {
		if len(e.Ledger()) != 1 || e.Ledger()[0].Round != 1 {
			t.Fatal("failed after silent proposer")
		}
	}
}

func TestLockSurvivesTimeoutAndReproposal(t *testing.T) {
	c := setup(t, 4)
	e := c.nodes[1]
	p := proposal(t, c, 0, "locked")
	h := blockHash(t, p)
	if err := e.ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{0, 2} {
		feed(t, e, signedVote(t, c, id, 0, types.Prevote, h))
	}
	if !e.Status().Locked {
		t.Fatal("missing lock")
	}
	reachRound(t, e, 1)
	e.Drain()
	rep, err := e.MakeProposal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if blockHash(t, rep) != h || rep.OriginRound != 0 || len(rep.Proof) != 3 {
		t.Fatal("reproposal changed block or lost proof")
	}
	if err := cons.VerifyProposal("lab", rep); err != nil {
		t.Fatal(err)
	}
	// Receiver knows none of the original votes or block; envelope proves it.
	reachRound(t, c.nodes[2], 1)
	if err := c.nodes[2].ReceiveProposal(rep); err != nil {
		t.Fatal(err)
	}
	if !c.nodes[2].Status().Locked || c.nodes[2].Status().LockedHash != h {
		t.Fatal("proof not used")
	}
	// The proof must remain available for another reproposal, not be lost when
	// the node has not received those votes as independent messages.
	reachRound(t, c.nodes[2], 2)
	rep2, err := c.nodes[2].MakeProposal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep2.Proof) != 3 || blockHash(t, rep2) != h {
		t.Fatal("relayed evidence lost")
	}
	// Fully isolated locks persist through NIL quorum and round changes.
	reachRound(t, e, 2)
	if !e.Status().Locked || e.Status().LockedHash != h {
		t.Fatal("timeout removed lock")
	}
	other := proposal(t, c, 2, "different")
	if err = e.ReceiveProposal(other); err != nil {
		t.Fatal(err)
	}
	o := e.Drain()
	var found bool
	for _, v := range o.Votes {
		if v.Round == 2 && v.Phase == types.Prevote {
			found = true
			if !v.IsNil {
				t.Fatal("locked validator switched without later evidence")
			}
		}
	}
	if !found {
		t.Fatal("no current-round prevote")
	}
}
