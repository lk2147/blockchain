package consensus_test

import (
	"testing"

	cons "blockchain-lab01/src/consensus"
	"blockchain-lab01/src/types"
)

func TestHeaderValidationHasNoConsensusSideEffects(t *testing.T) {
	c := setup(t, 4)
	e := c.nodes[3]
	p := proposal(t, c, 0, "A")
	h := blockHash(t, p)
	header := p
	header.Block.Body = types.BlockBody{}
	before := e.Status()
	if err := e.ValidateProposalHeader(header); err != nil {
		t.Fatal(err)
	}
	if e.Status() != before || len(e.Drain().Votes) != 0 || len(e.Ledger()) != 0 {
		t.Fatal("header validation changed consensus")
	}
	if e.ReceiveProposal(header) == nil {
		t.Fatal("header-only proposal accepted as full block")
	}
	f := cons.Finalized{Block: header.Block, Proof: proofFor(t, c, 0, types.Precommit, h, 0, 1, 2)}
	if err := e.ValidateDecisionHeader(f); err != nil {
		t.Fatal(err)
	}
	if e.Status() != before || len(e.Drain().Votes) != 0 || len(e.Ledger()) != 0 {
		t.Fatal("decision header finalized without body")
	}
	if e.ReceiveFinalized(f) == nil {
		t.Fatal("header-only decision finalized")
	}
	f.Block = p.Block
	if err := e.ReceiveFinalized(f); err != nil {
		t.Fatal(err)
	}
	if len(e.Ledger()) != 1 {
		t.Fatal("full validated decision not finalized")
	}
}
