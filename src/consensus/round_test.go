package consensus_test

import (
	"testing"

	cons "blockchain-lab01/src/consensus"
	"blockchain-lab01/src/types"
)

func TestRoundCatchupNeedsDistinctHonestEvidence(t *testing.T) {
	c := setup(t, 4)
	e := c.nodes[3]
	// At most f senders, even voting in both phases, cannot force a jump.
	feed(t, e, signedVote(t, c, 0, 4, types.Prevote, types.Hash{}))
	feed(t, e, signedVote(t, c, 0, 4, types.Precommit, types.Hash{}))
	if e.Status().Round != 0 {
		t.Fatal("one peer forced catch-up")
	}
	feed(t, e, signedVote(t, c, 1, 4, types.Prevote, types.Hash{}))
	if e.Status().Round != 4 || e.Status().Step != cons.Propose {
		t.Fatal("f+1 evidence did not catch up")
	}
	if len(e.Drain().Votes) != 0 {
		t.Fatal("catch-up signed before proposal/timeout")
	}
	p := proposal(t, c, 4, "A")
	if err := e.ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	o := e.Drain()
	if len(o.Votes) != 1 || o.Votes[0].Round != 4 {
		t.Fatal("signed in skipped round")
	}
	// A late prior-round proposal is rejected without generating another vote.
	if e.ReceiveProposal(proposal(t, c, 0, "old")) == nil {
		t.Fatal("old proposal accepted")
	}
	if len(e.Drain().Votes) != 0 {
		t.Fatal("old proposal caused vote")
	}
}

func TestLateQuorumCannotCauseSecondPrecommit(t *testing.T) {
	c := setup(t, 4)
	e := c.nodes[3]
	p := proposal(t, c, 0, "A")
	h := blockHash(t, p)
	if err := e.ReceiveProposal(p); err != nil {
		t.Fatal(err)
	}
	e.Drain()
	expire(t, e)
	o := e.Drain()
	if len(o.Votes) != 1 || o.Votes[0].Phase != types.Precommit || !o.Votes[0].IsNil {
		t.Fatal("missing timeout NIL precommit")
	}
	feed(t, e, signedVote(t, c, 0, 0, types.Prevote, h))
	feed(t, e, signedVote(t, c, 1, 0, types.Prevote, h))
	if !e.Status().Locked {
		t.Fatal("late quorum not retained")
	}
	if len(e.Drain().Votes) != 0 {
		t.Fatal("late quorum caused second precommit")
	}
	expire(t, e)
	if !e.Status().Locked || e.Status().Round != 1 {
		t.Fatal("lock lost across timeout")
	}
}

func TestNilQuorumDoesNotFinalize(t *testing.T) {
	c := setup(t, 4)
	e := c.nodes[3]
	expire(t, e)
	e.Drain()
	feed(t, e, signedVote(t, c, 0, 0, types.Prevote, types.Hash{}))
	feed(t, e, signedVote(t, c, 1, 0, types.Prevote, types.Hash{}))
	o := e.Drain()
	if len(o.Votes) != 1 || o.Votes[0].Phase != types.Precommit || !o.Votes[0].IsNil {
		t.Fatal("NIL quorum must yield NIL precommit")
	}
	for _, v := range proofFor(t, c, 0, types.Precommit, types.Hash{}, 0, 1, 2) {
		feed(t, e, v)
	}
	if len(e.Ledger()) != 0 || e.Status().Locked {
		t.Fatal("NIL quorum finalized or locked")
	}
	expire(t, e)
	if e.Status().Round != 1 {
		t.Fatal("NIL round did not progress")
	}
}
