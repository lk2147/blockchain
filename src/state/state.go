// Package state executes owned key-value updates on independent snapshots.
package state

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

// State has no exported mutable maps. Its zero value is an empty state.
// Copying a State is safe because all updates create new maps and values.
type State struct {
	records map[string][]byte
	nonces  map[types.PublicKey]uint64 // Last successfully executed nonce.
}

func (s State) Get(key string) ([]byte, bool) {
	v, ok := s.records[key]
	return append([]byte(nil), v...), ok
}

func (s State) LastNonce(sender types.PublicKey) uint64 { return s.nonces[sender] }

func (s State) clone() State {
	next := State{records: make(map[string][]byte, len(s.records)), nonces: make(map[types.PublicKey]uint64, len(s.nonces))}
	for k, v := range s.records {
		next.records[k] = append([]byte(nil), v...)
	}
	for k, v := range s.nonces {
		next.nonces[k] = v
	}
	return next
}

// Execute applies the supplied order exactly. On any failure it returns the
// original snapshot, not a partially applied batch. Even successful execution
// is only a candidate state; consensus must later decide whether to commit it.
func Execute(chainID string, initial State, txs []types.Transaction) (State, error) {
	if chainID == "" || !utf8.ValidString(chainID) {
		return initial, fmt.Errorf("invalid chain ID")
	}
	next := initial.clone()
	for i, tx := range txs {
		if err := apply(chainID, &next, tx); err != nil {
			return initial, fmt.Errorf("transaction %d: %w", i, err)
		}
	}
	return next, nil
}

func apply(chainID string, s *State, tx types.Transaction) error {
	if err := ValidateTransaction(chainID, tx); err != nil {
		return err
	}
	last := s.nonces[tx.Sender]
	if last == math.MaxUint64 {
		return fmt.Errorf("sender nonce exhausted")
	}
	if tx.Nonce != last+1 {
		return fmt.Errorf("nonce %d, expected %d", tx.Nonce, last+1)
	}
	s.records[tx.TargetKey] = append([]byte(nil), tx.Value...)
	s.nonces[tx.Sender] = tx.Nonce
	return nil
}

// ValidateTransaction checks admission rules shared by execution and mempool.
// Nonce ordering against a particular state is checked separately.
func ValidateTransaction(chainID string, tx types.Transaction) error {
	if err := crypt.VerifyTransaction(chainID, tx); err != nil {
		return fmt.Errorf("authentication: %w", err)
	}
	prefix := hex.EncodeToString(tx.Sender[:]) + "/"
	if !strings.HasPrefix(tx.TargetKey, prefix) || len(tx.TargetKey) == len(prefix) {
		return fmt.Errorf("target key is not an owned nonempty record")
	}
	if tx.Nonce == 0 {
		return fmt.Errorf("nonce must start at 1")
	}
	return nil
}
