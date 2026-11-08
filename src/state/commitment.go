package state

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"unicode/utf8"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

// CanonicalBytes uses version 1, length-prefixed STATE:<chain>, a uint32 record
// count, sorted (UTF-8 key, value) pairs, a uint32 nonce count, and sorted
// (32-byte sender, uint64 last nonce) pairs. Lengths are uint32; integers are
// big-endian. Sorting uses byte order. This is a flat commitment, not a tree.
func (s State) CanonicalBytes(chainID string) ([]byte, error) {
	if chainID == "" || !utf8.ValidString(chainID) {
		return nil, fmt.Errorf("invalid chain ID")
	}
	data := []byte{1}
	appendBlob := func(value []byte) error {
		if uint64(len(value)) > math.MaxUint32 {
			return fmt.Errorf("state field exceeds uint32 length")
		}
		data = binary.BigEndian.AppendUint32(data, uint32(len(value)))
		data = append(data, value...)
		return nil
	}
	if err := appendBlob([]byte("STATE:" + chainID)); err != nil {
		return nil, err
	}
	if uint64(len(s.records)) > math.MaxUint32 || uint64(len(s.nonces)) > math.MaxUint32 {
		return nil, fmt.Errorf("state count exceeds uint32")
	}
	keys := make([]string, 0, len(s.records))
	for key := range s.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	data = binary.BigEndian.AppendUint32(data, uint32(len(keys)))
	for _, key := range keys {
		if !utf8.ValidString(key) {
			return nil, fmt.Errorf("invalid state key UTF-8")
		}
		if err := appendBlob([]byte(key)); err != nil {
			return nil, err
		}
		if err := appendBlob(s.records[key]); err != nil {
			return nil, err
		}
	}
	senders := make([]types.PublicKey, 0, len(s.nonces))
	for sender := range s.nonces {
		senders = append(senders, sender)
	}
	sort.Slice(senders, func(i, j int) bool { return bytes.Compare(senders[i][:], senders[j][:]) < 0 })
	data = binary.BigEndian.AppendUint32(data, uint32(len(senders)))
	for _, sender := range senders {
		data = append(data, sender[:]...)
		data = binary.BigEndian.AppendUint64(data, s.nonces[sender])
	}
	return data, nil
}

func (s State) Root(chainID string) (types.Hash, error) {
	data, err := s.CanonicalBytes(chainID)
	if err != nil {
		return types.Hash{}, err
	}
	return crypt.HashBytes(data), nil
}
