// Package types defines transport-independent Lab01 messages.
package types

type Hash [32]byte
type PublicKey [32]byte
type Signature [64]byte
type ValidatorID PublicKey

// Transaction requests an update to <lowercase sender public key hex>/<record>.
// Execution will enforce ownership and the next nonce (starting at 1).
type Transaction struct {
	ChainID   string
	Sender    PublicKey
	TargetKey string
	Value     []byte
	Nonce     uint64 // Per-sender sequence starts at 1; execution rejects reuse.
	Signature Signature
}

// BlockHeader binds an ordered body and the resulting state to a parent.
// Its identity excludes its signature and is independent of voting rounds.
type BlockHeader struct {
	ChainID    string
	Height     uint64 // blockchain_seq
	ParentHash Hash
	BodyHash   Hash // SHA-256 of the canonical ordered, signed transaction body.
	StateRoot  Hash // output: result state (!)
	Proposer   ValidatorID
	Signature  Signature
}

type BlockBody struct{ Transactions []Transaction }

type Block struct {
	Header BlockHeader
	Body   BlockBody
}

type VotePhase uint8

const (
	Prevote   VotePhase = 1
	Precommit VotePhase = 2
)

type Vote struct {
	ChainID   string
	Height    uint64
	Round     uint64
	Phase     VotePhase
	Validator ValidatorID
	IsNil     bool
	BlockHash Hash
	Signature Signature
}
