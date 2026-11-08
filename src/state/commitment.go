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

// CanonicalLeaves returns records sorted by raw UTF-8 key bytes, followed by
// nonce entries sorted by raw public key bytes. Record payloads are
// 0x00 || uint32 key length || key || uint32 value length || value.
// Nonce payloads are 0x01 || 32-byte sender || uint64 last nonce.
// Integers are big-endian. All returned bytes are independent copies.
func (s State) CanonicalLeaves() ([][]byte, error) {
	keys := make([]string, 0, len(s.records))
	for key := range s.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	leaves := make([][]byte, 0)
	for _, key := range keys {
		if !utf8.ValidString(key) {
			return nil, fmt.Errorf("invalid state key UTF-8")
		}
		value := s.records[key]
		if uint64(len(key)) > math.MaxUint32 || uint64(len(value)) > math.MaxUint32 {
			return nil, fmt.Errorf("state field exceeds uint32 length")
		}
		payload := binary.BigEndian.AppendUint32([]byte{0}, uint32(len(key)))
		payload = append(payload, key...)
		payload = binary.BigEndian.AppendUint32(payload, uint32(len(value)))
		payload = append(payload, value...)
		leaves = append(leaves, payload)
	}
	senders := make([]types.PublicKey, 0, len(s.nonces))
	for sender := range s.nonces {
		senders = append(senders, sender)
	}
	sort.Slice(senders, func(i, j int) bool { return bytes.Compare(senders[i][:], senders[j][:]) < 0 })
	for _, sender := range senders {
		payload := append([]byte{1}, sender[:]...)
		payload = binary.BigEndian.AppendUint64(payload, s.nonces[sender])
		leaves = append(leaves, payload)
	}
	return leaves, nil
}

// merkleHash distinguishes leaves (0x00), internal nodes (0x01) and the empty
// tree (0x02). At every odd level the last hash is paired with itself.
func merkleHash(leaves [][]byte) types.Hash {
	if len(leaves) == 0 {
		return crypt.HashBytes([]byte{2})
	}
	level := make([]types.Hash, len(leaves))
	for i, payload := range leaves {
		level[i] = crypt.HashBytes(append([]byte{0}, payload...))
	}
	for len(level) > 1 {
		next := make([]types.Hash, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			right := level[i]
			if i+1 < len(level) {
				right = level[i+1]
			}
			data := append([]byte{1}, level[i][:]...)
			data = append(data, right[:]...)
			next = append(next, crypt.HashBytes(data))
		}
		level = next
	}
	return level[0]
}

// Root binds the tree to version 1, its chain and its uint64 leaf count:
// SHA256(0x03 || 0x01 || uint32 context length || STATE-MERKLE:<chain> ||
//
//	uint64 leaf count || merkleHash).
//
// The count prevents duplicate-last padding from hiding a different tree size.
// This implements commitments only, not membership/non-membership proofs.
func (s State) Root(chainID string) (types.Hash, error) {
	if chainID == "" || !utf8.ValidString(chainID) {
		return types.Hash{}, fmt.Errorf("invalid chain ID")
	}
	context := "STATE-MERKLE:" + chainID
	if uint64(len(context)) > math.MaxUint32 {
		return types.Hash{}, fmt.Errorf("state context exceeds uint32 length")
	}
	leaves, err := s.CanonicalLeaves()
	if err != nil {
		return types.Hash{}, err
	}
	tree := merkleHash(leaves)
	data := binary.BigEndian.AppendUint32([]byte{3, 1}, uint32(len(context)))
	data = append(data, context...)
	data = binary.BigEndian.AppendUint64(data, uint64(len(leaves)))
	data = append(data, tree[:]...)
	return crypt.HashBytes(data), nil
}
