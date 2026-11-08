// Package crypto defines the version-1 canonical bytes and authentication rules.
// Protobuf bytes are never used as signing or hashing input.
package crypto

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"

	"blockchain-lab01/src/types"
)

const encodingVersion byte = 1

type encoder struct {
	data []byte
	err  error
}

func (e *encoder) u64(v uint64) { e.data = binary.BigEndian.AppendUint64(e.data, v) }
func (e *encoder) blob(v []byte) {
	if uint64(len(v)) > math.MaxUint32 {
		e.err = fmt.Errorf("field exceeds uint32 length")
		return
	}
	e.data = binary.BigEndian.AppendUint32(e.data, uint32(len(v)))
	e.data = append(e.data, v...)
}
func (e *encoder) str(v string) {
	if !utf8.ValidString(v) {
		e.err = fmt.Errorf("string is not valid UTF-8")
		return
	}
	e.blob([]byte(v))
}
func newEncoder(domain, chainID string) *encoder {
	e := &encoder{data: []byte{encodingVersion}}
	if chainID == "" {
		e.err = fmt.Errorf("empty chain ID")
	}
	e.str(domain + ":" + chainID)
	return e
}

// CanonicalTxBytes excludes the signature. Strings/blobs have uint32 byte
// lengths; integers are big-endian uint64; fixed arrays have no length prefix.
func CanonicalTxBytes(tx types.Transaction) ([]byte, error) {
	e := newEncoder("TX", tx.ChainID)
	e.data = append(e.data, tx.Sender[:]...)
	e.str(tx.TargetKey)
	e.blob(tx.Value)
	e.u64(tx.Nonce)
	return e.data, e.err
}

// CanonicalSignedTxBytes appends the fixed 64-byte signature to its payload.
func CanonicalSignedTxBytes(tx types.Transaction) ([]byte, error) {
	b, err := CanonicalTxBytes(tx)
	if err != nil {
		return nil, err
	}
	return append(b, tx.Signature[:]...), nil
}

func CanonicalHeaderBytes(h types.BlockHeader) ([]byte, error) {
	e := newEncoder("HEADER", h.ChainID)
	e.u64(h.Height)
	e.data = append(e.data, h.ParentHash[:]...)
	e.data = append(e.data, h.BodyHash[:]...)
	e.data = append(e.data, h.StateRoot[:]...)
	e.data = append(e.data, h.Proposer[:]...)
	return e.data, e.err
}

func CanonicalVoteBytes(v types.Vote) ([]byte, error) {
	if v.Phase != types.Prevote && v.Phase != types.Precommit {
		return nil, fmt.Errorf("invalid vote phase")
	}
	if v.IsNil != (v.BlockHash == (types.Hash{})) {
		return nil, fmt.Errorf("inconsistent NIL vote and block hash")
	}
	e := newEncoder("VOTE", v.ChainID)
	e.u64(v.Height)
	e.u64(v.Round)
	e.data = append(e.data, byte(v.Phase))
	e.data = append(e.data, v.Validator[:]...)
	if v.IsNil {
		e.data = append(e.data, 1)
	} else {
		e.data = append(e.data, 0)
	}
	e.data = append(e.data, v.BlockHash[:]...)
	return e.data, e.err
}

func CanonicalBodyBytes(chainID string, body types.BlockBody) ([]byte, error) {
	e := newEncoder("BODY", chainID)
	if uint64(len(body.Transactions)) > math.MaxUint32 {
		return nil, fmt.Errorf("too many transactions")
	}
	e.data = binary.BigEndian.AppendUint32(e.data, uint32(len(body.Transactions)))
	for _, tx := range body.Transactions {
		if tx.ChainID != chainID {
			return nil, fmt.Errorf("body transaction chain mismatch")
		}
		payload, err := CanonicalTxBytes(tx)
		if err != nil {
			return nil, err
		}
		e.blob(payload)
		e.data = append(e.data, tx.Signature[:]...)
	}
	return e.data, e.err
}
