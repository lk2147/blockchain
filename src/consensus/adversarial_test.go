package consensus_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

func signedProposal(t *testing.T, c cluster, p cons.Proposal) cons.Proposal {
	t.Helper()
	idx := int(p.Round % uint64(len(c.ids)))
	p.Proposer = c.ids[idx]
	out, err := cons.SignProposal(p, c.keys[idx])
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func proofFor(t *testing.T, c cluster, r uint64, phase types.VotePhase, h types.Hash, ids ...int) []types.Vote {
	t.Helper()
	var proof []types.Vote
	for _, id := range ids {
		proof = append(proof, signedVote(t, c, id, r, phase, h))
	}
	return proof
}
func lockA(t *testing.T, c cluster, e *cons.Engine, p cons.Proposal) {
	t.Helper()
	if err := e.ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{0, 1} {
		feed(t, e, signedVote(t, c, id, 0, types.Prevote, blockHash(t, p)))
	}
	if !e.Status().Locked {
		t.Fatal("fixture failed to lock")
	}
	e.Drain()
}

func TestOnlyLaterQuorumChangesLock(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(map[bool]string{true: "observed-votes", false: "proposal-certificate"}[direct], func(t *testing.T) {
			c := setup(t, 4)
			e := c.nodes[3]
			a := proposal(t, c, 0, "A")
			lockA(t, c, e, a)
			reachRound(t, e, 1)
			e.Drain()
			b := proposal(t, c, 1, "B")
			h := blockHash(t, b)
			if direct {
				if err := e.ReceiveProposal(b); err != nil {
					t.Fatal(err)
				}
				o := e.Drain()
				if len(o.Votes) != 1 || !o.Votes[0].IsNil {
					t.Fatal("switched before evidence")
				}
				for _, v := range proofFor(t, c, 1, types.Prevote, h, 0, 1, 2) {
					feed(t, e, v)
				}
				if e.Status().LockedHash != h || e.Status().LockedRound != 1 {
					t.Fatal("later quorum did not update lock")
				}
				o = e.Drain()
				for _, v := range o.Votes {
					if v.Phase == types.Prevote {
						t.Fatal("signed second prevote after evidence")
					}
				}
			} else {
				// A NIL quorum never removes the existing non-NIL lock.
				for _, v := range proofFor(t, c, 1, types.Prevote, types.Hash{}, 0, 1, 2) {
					feed(t, e, v)
				}
				if e.Status().LockedHash != blockHash(t, a) {
					t.Fatal("NIL quorum unlocked block")
				}
			}
			reachRound(t, e, 2)
			e.Drain()
			b.Round = 2
			b.Proof = proofFor(t, c, 1, types.Prevote, h, 0, 1, 2)
			b = signedProposal(t, c, b)
			if err := e.ReceiveProposal(b); err != nil {
				t.Fatal(err)
			}
			if e.Status().LockedHash != h || e.Status().LockedRound != 1 {
				t.Fatal("valid later certificate did not justify switch")
			}
			o := e.Drain()
			if len(o.Votes) != 1 || o.Votes[0].Phase != types.Prevote || o.Votes[0].IsNil || o.Votes[0].BlockHash != h {
				t.Fatal("did not prevote justified block")
			}
		})
	}
}

func TestInvalidProposalAndProofCannotUnlock(t *testing.T) {
	for _, fault := range []string{"signature", "envelope-chain", "height", "round", "origin", "proposer", "header-state", "body", "missing-proof", "duplicate-proof", "insufficient-proof", "wrong-phase", "wrong-hash", "proof-signature", "future-proof", "unknown-proof"} {
		t.Run(fault, func(t *testing.T) {
			c := setup(t, 4)
			e := c.nodes[3]
			a := proposal(t, c, 0, "A")
			lockA(t, c, e, a)
			reachRound(t, e, 2)
			e.Drain()
			p := proposal(t, c, 1, "B")
			p.Round = 2
			p.Proof = proofFor(t, c, 1, types.Prevote, blockHash(t, p), 0, 1, 2)
			switch fault {
			case "envelope-chain":
				p.ChainID = "other"
			case "height":
				p.Height = 2
			case "round":
				p.Round = 3
			case "origin":
				p.OriginRound = 3
			case "header-state":
				p.Block.Header.StateRoot[0] ^= 1
				p.Block.Header, _ = crypt.SignHeader(p.Block.Header, c.keys[1])
			case "body":
				p.Block.Body.Transactions[0].Value = []byte("changed")
			case "missing-proof":
				p.Proof = nil
			case "duplicate-proof":
				p.Proof[2] = p.Proof[0]
			case "insufficient-proof":
				p.Proof = p.Proof[:2]
			case "wrong-phase":
				p.Proof[0] = signedVote(t, c, 0, 1, types.Precommit, blockHash(t, p))
			case "wrong-hash":
				p.Proof[0] = signedVote(t, c, 0, 1, types.Prevote, types.Hash{9})
			case "proof-signature":
				p.Proof[0].Signature[0] ^= 1
			case "future-proof":
				p.Proof = proofFor(t, c, 2, types.Prevote, blockHash(t, p), 0, 1, 2)
			case "unknown-proof":
				p.Proof[0].Validator = types.ValidatorID{9}
			}
			p = signedProposal(t, c, p)
			if fault == "signature" {
				p.Signature[0] ^= 1
			}
			if fault == "proposer" {
				p.Proposer = c.ids[1]
				var err error
				p, err = cons.SignProposal(p, c.keys[1])
				if err != nil {
					t.Fatal(err)
				}
			}
			before := e.Status()
			if err := e.ReceiveProposal(p); err == nil {
				t.Fatal("invalid proposal accepted")
			}
			if e.Status() != before || len(e.Ledger()) != 0 {
				t.Fatal("invalid proposal changed lock/step/ledger")
			}
			if len(e.Drain().Votes) != 0 {
				t.Fatal("invalid proposal caused vote")
			}
		})
	}
}

func TestLateVotesAndDecisionCertificates(t *testing.T) {
	c := setup(t, 4)
	p := proposal(t, c, 0, "A")
	h := blockHash(t, p)
	e := c.nodes[3]
	// A quorum naming an unknown body does not make it valid or finalized.
	proof := proofFor(t, c, 0, types.Precommit, h, 0, 1, 2)
	for _, v := range proof {
		feed(t, e, v)
	}
	if len(e.Ledger()) != 0 || e.Status().Locked {
		t.Fatal("unknown body finalized")
	}
	if err := e.ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	if len(e.Ledger()) != 1 {
		t.Fatal("available valid body with prior certificate failed")
	}
	f := e.Ledger()[0]
	lag := c.nodes[2]
	reachRound(t, lag, 3)
	lag.Drain()
	if err := lag.ReceiveFinalized(f); err != nil {
		t.Fatal(err)
	}
	if len(lag.Ledger()) != 1 || lag.Status().Height != 2 {
		t.Fatal("lagging node did not validate certificate")
	}
	if lag.ReceiveFinalized(f) == nil {
		t.Fatal("finalized-height replay accepted")
	}
	// Returned ledgers, proposal inputs and outputs do not expose internal blocks.
	f.Block.Body.Transactions[0].Value[0] = 'X'
	f.Proof[0].Signature[0] ^= 1
	if string(e.Ledger()[0].Block.Body.Transactions[0].Value) != "A" {
		t.Fatal("ledger aliases returned block")
	}
	// Older round precommits remain useful after local round advancement.
	x := setup(t, 4)
	if err := x.nodes[3].ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	reachRound(t, x.nodes[3], 2)
	for _, v := range proof {
		feed(t, x.nodes[3], v)
	}
	if len(x.nodes[3].Ledger()) != 1 {
		t.Fatal("late certificate lost")
	}
	for _, fault := range []string{"duplicate", "short", "phase", "nil", "round", "hash", "signature", "unknown", "height", "body", "origin"} {
		t.Run(fault, func(t *testing.T) {
			x := setup(t, 4)
			f := cons.Finalized{Block: p.Block, OriginRound: 0, Round: 0, Proof: append([]types.Vote(nil), proof...)}
			switch fault {
			case "duplicate":
				f.Proof[2] = f.Proof[0]
			case "short":
				f.Proof = f.Proof[:2]
			case "phase":
				f.Proof[0] = signedVote(t, x, 0, 0, types.Prevote, h)
			case "nil":
				f.Proof[0] = signedVote(t, x, 0, 0, types.Precommit, types.Hash{})
			case "round":
				f.Proof[0] = signedVote(t, x, 0, 1, types.Precommit, h)
			case "hash":
				f.Proof[0] = signedVote(t, x, 0, 0, types.Precommit, types.Hash{9})
			case "signature":
				f.Proof[0].Signature[0] ^= 1
			case "unknown":
				f.Proof[0].Validator = types.ValidatorID{9}
			case "height":
				f.Block.Header.Height = 2
			case "body":
				f.Block.Body.Transactions = nil
			case "origin":
				f.OriginRound = 1
			}
			if err := x.nodes[3].ReceiveFinalized(f); err == nil {
				t.Fatal("invalid certificate finalized")
			}
			if len(x.nodes[3].Ledger()) != 0 || x.nodes[3].State().LastNonce(types.PublicKey(x.ids[0])) != 0 {
				t.Fatal("invalid decision committed")
			}
		})
	}
}

func TestByzantineEquivocationAtFaultBound(t *testing.T) {
	c := setup(t, 10)
	a := proposal(t, c, 0, "A")
	b := proposal(t, c, 0, "B")
	ha, hb := blockHash(t, a), blockHash(t, b)
	// Validators 0..2 are Byzantine. Honest nodes 3..6 see A, 7..9 see B.
	// All honest outputs are recorded; only Byzantine votes are synthesized.
	var honestVotes []types.Vote
	collect := func() {
		for _, e := range c.nodes[3:] {
			honestVotes = append(honestVotes, e.Drain().Votes...)
		}
	}
	for i := 3; i < 10; i++ {
		p := a
		if i >= 7 {
			p = b
		}
		if err := c.nodes[i].ReceiveProposal(p); err != nil {
			t.Fatal(err)
		}
	}
	collect()
	for i := 3; i < 10; i++ {
		h := ha
		if i >= 7 {
			h = hb
		}
		for _, v := range proofFor(t, c, 0, types.Prevote, h, 0, 1, 2) {
			feed(t, c.nodes[i], v)
		}
	}
	collect()
	// Deliver all honest prevotes, including to the opposite group.
	initial := append([]types.Vote(nil), honestVotes...)
	for _, v := range initial {
		if v.Phase != types.Prevote {
			continue
		}
		for i := 3; i < 10; i++ {
			feed(t, c.nodes[i], v)
		}
	}
	collect()
	for i := 3; i < 7; i++ {
		if !c.nodes[i].Status().Locked || c.nodes[i].Status().LockedHash != ha {
			t.Fatal("A quorum not locked")
		}
	}
	for i := 7; i < 10; i++ {
		if c.nodes[i].Status().Locked {
			t.Fatal("six votes falsely formed seven-vote quorum")
		}
	}
	for i := 3; i < 10; i++ {
		h := ha
		if i >= 7 {
			h = hb
		}
		for _, v := range proofFor(t, c, 0, types.Precommit, h, 0, 1, 2) {
			feed(t, c.nodes[i], v)
		}
	}
	for _, v := range honestVotes {
		if v.Phase != types.Precommit {
			continue
		}
		for i := 3; i < 10; i++ {
			if c.nodes[i].Status().Height == v.Height {
				feed(t, c.nodes[i], v)
			} else if c.nodes[i].ReceiveVote(v) == nil {
				t.Fatal("accepted stale vote after decision")
			}
		}
	}
	f := c.nodes[3].Ledger()
	if len(f) != 1 {
		t.Fatal("A did not finalize with genuine quorum")
	}
	for i := 7; i < 10; i++ {
		if len(c.nodes[i].Ledger()) != 0 {
			t.Fatal("conflicting block finalized")
		}
		if err := c.nodes[i].ReceiveFinalized(f[0]); err != nil {
			t.Fatal(err)
		}
	}
	for i := 3; i < 10; i++ {
		ledger := c.nodes[i].Ledger()
		if len(ledger) != 1 {
			t.Fatal("missing decision")
		}
		h, err := crypt.BlockHash(ledger[0].Block.Header)
		if err != nil {
			t.Fatal(err)
		}
		if h != ha {
			t.Fatal("conflicting finalization")
		}
	}
}

func TestProposalSignatureScope(t *testing.T) {
	c := setup(t, 4)
	p := proposal(t, c, 0, "A")
	b, err := cons.CanonicalProposalBytes(p)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(b[:17]) != "010000000c50524f504f53414c3a6c6162" {
		t.Fatalf("proposal prefix = %x", b[:17])
	}
	if cons.VerifyProposal("other", p) == nil {
		t.Fatal("configured chain ignored")
	}
	for _, field := range []string{"chain", "height", "round", "origin", "proposer", "block", "header signature", "proof", "signature", "domain"} {
		t.Run(field, func(t *testing.T) {
			x := p
			switch field {
			case "chain":
				x.ChainID = "other"
			case "height":
				x.Height++
			case "round":
				x.Round++
			case "origin":
				x.OriginRound++
			case "proposer":
				x.Proposer[0] ^= 1
			case "block":
				x.Block.Header.StateRoot[0] ^= 1
			case "header signature":
				x.Block.Header.Signature[0] ^= 1
			case "proof":
				x.Proof = proofFor(t, c, 0, types.Prevote, blockHash(t, p), 0, 1, 2)
			case "signature":
				x.Signature[0] ^= 1
			case "domain":
				wrong := append([]byte(nil), b...)
				wrong[5] = 'X'
				x.Signature = types.Signature(ed25519.Sign(c.keys[0], wrong))
			}
			if cons.VerifyProposal("lab", x) == nil {
				t.Fatal("modified signed field/domain accepted")
			}
		})
	}
	copyP := p
	copyP.Block.Body.Transactions = append([]types.Transaction(nil), p.Block.Body.Transactions...)
	copyP.Block.Body.Transactions[0].Value = []byte("A")
	again, err := cons.CanonicalProposalBytes(copyP)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(b) {
		t.Fatal("equivalent proposal bytes differ")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	c := setup(t, 4)
	for _, fault := range []string{"n", "f", "negative f", "duplicate", "zero validator", "unknown local", "short key", "corrupt key", "empty chain", "invalid chain", "zero timeout"} {
		t.Run(fault, func(t *testing.T) {
			cfg := cons.Config{ChainID: "lab", Validators: append([]types.ValidatorID(nil), c.ids...), F: 1, PrivateKey: append(ed25519.PrivateKey(nil), c.keys[0]...), BaseTimeout: 10}
			switch fault {
			case "n":
				cfg.Validators = cfg.Validators[:3]
			case "f":
				cfg.F = 2
			case "negative f":
				cfg.F = -1
			case "duplicate":
				cfg.Validators[1] = cfg.Validators[0]
			case "zero validator":
				cfg.Validators[1] = types.ValidatorID{}
			case "unknown local":
				cfg.Validators[0] = types.ValidatorID{9}
			case "short key":
				cfg.PrivateKey = cfg.PrivateKey[:3]
			case "corrupt key":
				cfg.PrivateKey[63] ^= 1
			case "empty chain":
				cfg.ChainID = ""
			case "invalid chain":
				cfg.ChainID = "\xff"
			case "zero timeout":
				cfg.BaseTimeout = 0
			}
			if _, err := cons.New(cfg); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
