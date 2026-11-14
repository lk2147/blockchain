// Package mempool keeps bounded, independently validated pending transactions in RAM.
package mempool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

type entry struct {
	tx   types.Transaction
	size int
}

// Bound future nonce gaps as well as total pool count/bytes.
const maxFutureNonceGap = 1024

// Pool is owned by the node event loop; callers must not mutate it concurrently.
type Pool struct {
	entries                         map[types.Hash]entry
	used, maxCount, maxBytes, maxTX int
}

func New(count, bytesLimit, txLimit int) *Pool {
	return &Pool{entries: make(map[types.Hash]entry), maxCount: count, maxBytes: bytesLimit, maxTX: txLimit}
}
func (p *Pool) Len() int { return len(p.entries) }
func (p *Pool) Admit(chain string, s state.State, tx types.Transaction) (types.Hash, error) {
	var zero types.Hash
	id, err := crypt.TransactionID(tx)
	if err != nil {
		return zero, err
	}
	if _, ok := p.entries[id]; ok {
		return id, fmt.Errorf("duplicate transaction")
	}
	if err := state.ValidateTransaction(chain, tx); err != nil {
		return zero, err
	}
	last := s.LastNonce(tx.Sender)
	if tx.Nonce <= last {
		return id, fmt.Errorf("replayed transaction nonce %d, committed %d", tx.Nonce, last)
	}
	if tx.Nonce-last > maxFutureNonceGap {
		return id, fmt.Errorf("nonce too far ahead")
	}
	for _, e := range p.entries {
		if e.tx.Sender == tx.Sender && e.tx.Nonce == tx.Nonce {
			return id, fmt.Errorf("conflicting pending sender nonce")
		}
	}
	b, err := json.Marshal(tx)
	if err != nil {
		return id, err
	}
	if len(b) > p.maxTX || len(p.entries) >= p.maxCount || p.used+len(b) > p.maxBytes {
		return id, fmt.Errorf("mempool capacity exceeded")
	}
	tx.Value = append([]byte(nil), tx.Value...)
	p.entries[id] = entry{tx, len(b)}
	p.used += len(b)
	return id, nil
}
func (p *Pool) Select(s state.State, count, byteLimit int) []types.Transaction {
	entries := make([]entry, 0, len(p.entries))
	for _, e := range p.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].tx, entries[j].tx
		if c := bytes.Compare(a.Sender[:], b.Sender[:]); c != 0 {
			return c < 0
		}
		return a.Nonce < b.Nonce
	})
	nonces := make(map[types.PublicKey]uint64)
	var txs []types.Transaction
	used := len(`{"Transactions":[]}`)
	for _, e := range entries {
		last, ok := nonces[e.tx.Sender]
		if !ok {
			last = s.LastNonce(e.tx.Sender)
		}
		if e.tx.Nonce != last+1 || len(txs) >= count || used+e.size+1 > byteLimit {
			continue
		}
		tx := e.tx
		tx.Value = append([]byte(nil), tx.Value...)
		txs = append(txs, tx)
		used += e.size + 1
		nonces[tx.Sender] = tx.Nonce
	}
	return txs
}
func (p *Pool) Prune(s state.State) {
	for id, e := range p.entries {
		if e.tx.Nonce <= s.LastNonce(e.tx.Sender) {
			delete(p.entries, id)
			p.used -= e.size
		}
	}
}

// Snapshot returns a deterministic deep copy for read-only observation. The
// node owner loop calls it, so inspection cannot race admission or pruning.
func (p *Pool) Snapshot() []types.Transaction {
	entries := make([]entry, 0, len(p.entries))
	for _, e := range p.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].tx, entries[j].tx
		if c := bytes.Compare(a.Sender[:], b.Sender[:]); c != 0 {
			return c < 0
		}
		return a.Nonce < b.Nonce
	})
	out := make([]types.Transaction, len(entries))
	for i, e := range entries {
		out[i] = e.tx
		out[i].Value = append([]byte(nil), e.tx.Value...)
	}
	return out
}

func (p *Pool) Contains(id types.Hash) bool { _, ok := p.entries[id]; return ok }
