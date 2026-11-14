package mempool_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/mempool"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

func tx(t *testing.T, nonce uint64) types.Transaction {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{71}, 32)) // Public TEST-ONLY fixture.
	pub := types.PublicKey(key[32:])
	v, err := crypt.SignTransaction(types.Transaction{ChainID: "pool", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/value", Value: []byte("dummy"), Nonce: nonce}, key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestPoolOrderingReplayAndIsolation(t *testing.T) {
	p := mempool.New(20, 20000, 2000)
	var s state.State
	third, first, second := tx(t, 3), tx(t, 1), tx(t, 2)
	if _, err := p.Admit("pool", s, third); err != nil {
		t.Fatal(err)
	}
	if len(p.Select(s, 10, 10000)) != 0 {
		t.Fatal("selected transaction with missing nonce")
	}
	if _, err := p.Admit("pool", s, first); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Admit("pool", s, first); err == nil {
		t.Fatal("accepted duplicate")
	}
	first.Value[0] ^= 1
	if _, err := p.Admit("pool", s, second); err != nil {
		t.Fatal(err)
	}
	batch := p.Select(s, 2, 10000)
	if len(batch) != 2 || batch[0].Nonce != 1 || batch[1].Nonce != 2 || string(batch[0].Value) != "dummy" {
		t.Fatal("ordering/ownership of buffers broken")
	}
	next, err := state.Execute("pool", s, batch)
	if err != nil {
		t.Fatal(err)
	}
	if p.Len() != 3 {
		t.Fatal("removed transactions before commit")
	}
	p.Prune(next)
	if p.Len() != 1 {
		t.Fatal("finalized transactions not pruned")
	}
	if _, err = p.Admit("pool", next, tx(t, 1)); err == nil {
		t.Fatal("accepted committed replay")
	}
	if got := p.Select(next, 10, 10000); len(got) != 1 || got[0].Nonce != 3 {
		t.Fatal("lost pending nonce after finalization")
	}
}
func TestPoolRejectsBadAndBoundedInputs(t *testing.T) {
	var s state.State
	for _, bad := range []string{"signature", "chain", "owner", "zero", "future"} {
		t.Run(bad, func(t *testing.T) {
			v := tx(t, 1)
			switch bad {
			case "signature":
				v.Signature[0] ^= 1
			case "chain":
				v.ChainID = "other"
			case "owner":
				v.TargetKey = "another-owner/value"
			case "zero":
				v = tx(t, 0)
			case "future":
				v = tx(t, 1025)
			}
			if bad == "owner" || bad == "chain" {
				key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{71}, 32))
				var err error
				v, err = crypt.SignTransaction(v, key)
				if err != nil {
					t.Fatal(err)
				}
			}
			p := mempool.New(10, 20000, 2000)
			if _, err := p.Admit("pool", s, v); err == nil || p.Len() != 0 {
				t.Fatal("bad transaction entered pool")
			}
		})
	}
	p := mempool.New(1, 2000, 2000)
	if _, err := p.Admit("pool", s, tx(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Admit("pool", s, tx(t, 2)); err == nil {
		t.Fatal("ignored count bound")
	}
	if len(p.Select(s, 10, 10)) != 0 {
		t.Fatal("ignored block byte bound")
	}
}

func TestSnapshotIsOrderedAndIsolated(t *testing.T) {
	p := mempool.New(10, 20000, 2000)
	var s state.State
	for _, nonce := range []uint64{3, 1, 2} {
		if _, err := p.Admit("pool", s, tx(t, nonce)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := p.Snapshot()
	if len(snapshot) != 3 || snapshot[0].Nonce != 1 || snapshot[1].Nonce != 2 || snapshot[2].Nonce != 3 {
		t.Fatal("snapshot is not deterministically ordered")
	}
	snapshot[0].Value[0] ^= 1
	if got := p.Snapshot(); string(got[0].Value) != "dummy" {
		t.Fatal("snapshot exposed mutable mempool storage")
	}
}
