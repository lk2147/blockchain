// Package consensus implements Lab01's two-phase, round-based locking protocol.
// It is a single-threaded state machine; a simulator supplies message delivery
// and timeout events. It neither owns a network nor uses wall-clock time.
package consensus

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

// Proposal separates the current proposer/round from the immutable block's
// original proposer. A reproposal requires a prior-round PREVOTE certificate.
// Proof ordering is signed exactly as transmitted, not reordered by receivers.
type Proposal struct {
	ChainID     string
	Height      uint64
	Round       uint64
	OriginRound uint64
	Proposer    types.ValidatorID
	Block       types.Block
	Proof       []types.Vote
	Signature   types.Signature
}

// CanonicalProposalBytes extends the domain scheme with PROPOSAL:<chain>.
// Format: version=1, uint32 context length + context, uint64 height/round/
// originRound, 32-byte proposer, block hash, original header signature,
// uint32 proof count, then each uint32-length-prefixed canonical VOTE payload
// and its fixed signature. All integers are big-endian. BodyHash authenticates
// the body; the header signature is bound here too. Own signature is excluded.
func CanonicalProposalBytes(p Proposal) ([]byte, error) {
	h, err := crypt.BlockHash(p.Block.Header)
	if err != nil {
		return nil, err
	}
	if p.ChainID == "" || !utf8.ValidString(p.ChainID) {
		return nil, fmt.Errorf("invalid proposal chain ID")
	}
	context := "PROPOSAL:" + p.ChainID
	if uint64(len(context)) > math.MaxUint32 || uint64(len(p.Proof)) > math.MaxUint32 {
		return nil, fmt.Errorf("proposal field too large")
	}
	b := binary.BigEndian.AppendUint32([]byte{1}, uint32(len(context)))
	b = append(b, context...)
	b = binary.BigEndian.AppendUint64(b, p.Height)
	b = binary.BigEndian.AppendUint64(b, p.Round)
	b = binary.BigEndian.AppendUint64(b, p.OriginRound)
	b = append(b, p.Proposer[:]...)
	b = append(b, h[:]...)
	b = append(b, p.Block.Header.Signature[:]...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(p.Proof)))
	for _, v := range p.Proof {
		payload, err := crypt.CanonicalVoteBytes(v)
		if err != nil {
			return nil, err
		}
		if uint64(len(payload)) > math.MaxUint32 {
			return nil, fmt.Errorf("vote payload too large")
		}
		b = binary.BigEndian.AppendUint32(b, uint32(len(payload)))
		b = append(b, payload...)
		b = append(b, v.Signature[:]...)
	}
	return b, nil
}

func SignProposal(p Proposal, key ed25519.PrivateKey) (Proposal, error) {
	if len(key) != ed25519.PrivateKeySize {
		return p, fmt.Errorf("invalid private key length")
	}
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	if !bytes.Equal(derived, key) || types.ValidatorID(derived[ed25519.SeedSize:]) != p.Proposer {
		return p, fmt.Errorf("proposal signing identity mismatch")
	}
	b, err := CanonicalProposalBytes(p)
	if err != nil {
		return p, err
	}
	p.Signature = types.Signature(ed25519.Sign(key, b))
	return p, nil
}

func VerifyProposal(chainID string, p Proposal) error {
	if chainID == "" || p.ChainID != chainID {
		return fmt.Errorf("unexpected proposal chain")
	}
	b, err := CanonicalProposalBytes(p)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(p.Proposer[:]), b, p.Signature[:]) {
		return fmt.Errorf("invalid proposal signature")
	}
	return nil
}

func cloneBlock(b types.Block) types.Block {
	txs := make([]types.Transaction, len(b.Body.Transactions))
	for i, tx := range b.Body.Transactions {
		txs[i] = tx
		txs[i].Value = append([]byte(nil), tx.Value...)
	}
	b.Body.Transactions = txs
	return b
}
func cloneProposal(p Proposal) Proposal {
	p.Block = cloneBlock(p.Block)
	p.Proof = append([]types.Vote(nil), p.Proof...)
	return p
}
