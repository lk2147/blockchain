package block_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"blockchain-lab01/src/block"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

// Fixed TEST-ONLY fixture; never a production identity.
func fixture(t *testing.T) (block.Context, ed25519.PrivateKey, types.Transaction) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	pub := types.PublicKey(key[32:])
	c := block.Context{ChainID: "lab", Height: 1, Proposer: types.ValidatorID(pub)}
	tx, err := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte("hello"), Nonce: 1}, key)
	if err != nil {
		t.Fatal(err)
	}
	return c, key, tx
}
func stateRoot(t *testing.T, s state.State) types.Hash {
	t.Helper()
	h, e := s.Root("lab")
	if e != nil {
		t.Fatal(e)
	}
	return h
}

func TestBuildValidateAndParentLink(t *testing.T) {
	c, key, tx := fixture(t)
	var initial state.State
	b, err := block.Build(c, initial, []types.Transaction{tx}, key)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := block.Build(c, initial, []types.Transaction{tx}, key)
	if err != nil {
		t.Fatal(err)
	}
	if b.Header != repeated.Header {
		t.Fatal("identical inputs changed signed header")
	}
	if err := block.ValidateHeader(c, b.Header); err != nil {
		t.Fatal(err)
	}
	a, err := block.Validate(c, initial, b)
	if err != nil {
		t.Fatal(err)
	}
	other, err := block.Validate(c, state.State{}, b)
	if err != nil {
		t.Fatal(err)
	}
	if stateRoot(t, a) != stateRoot(t, other) || stateRoot(t, a) != b.Header.StateRoot {
		t.Fatal("independent states disagree")
	}
	if got, ok := a.Get(tx.TargetKey); !ok || string(got) != "hello" || a.LastNonce(tx.Sender) != 1 {
		t.Fatal("wrong explicit post-state")
	}
	if _, ok := initial.Get(tx.TargetKey); ok {
		t.Fatal("build/validation committed input state")
	}
	// Build owns the body's bytes, not the caller's mutable transaction value.
	tx.Value[0] = 'X'
	if string(b.Body.Transactions[0].Value) != "hello" {
		t.Fatal("body aliases caller")
	}
	parent, err := crypt.BlockHash(b.Header)
	if err != nil {
		t.Fatal(err)
	}
	c.Height = 2
	c.ParentHash = parent
	// Empty blocks are valid deterministic identity state transitions.
	next, err := block.Build(c, a, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	post, err := block.Validate(c, a, next)
	if err != nil {
		t.Fatal(err)
	}
	if next.Header.ParentHash != parent || stateRoot(t, post) != stateRoot(t, a) {
		t.Fatal("bad parent or empty transition")
	}
	_, err = block.Validate(c, a, b)
	if err == nil {
		t.Fatal("old block accepted at new height")
	}
	// A second height cannot replay an already executed transaction.
	if _, err = block.Build(c, a, b.Body.Transactions, key); err == nil {
		t.Fatal("replayed transaction built")
	}
}

func TestRejectInvalidBlocks(t *testing.T) {
	c, key, tx := fixture(t)
	base, err := block.Build(c, state.State{}, []types.Transaction{tx}, key)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*types.Block){
		"wrong chain":          func(b *types.Block) { b.Header.ChainID = "other" },
		"wrong height":         func(b *types.Block) { b.Header.Height++ },
		"wrong parent":         func(b *types.Block) { b.Header.ParentHash[0] = 1 },
		"wrong proposer":       func(b *types.Block) { b.Header.Proposer[0] ^= 1 },
		"bad header signature": func(b *types.Block) { b.Header.Signature[0] ^= 1 },
		"body changed":         func(b *types.Block) { b.Body.Transactions[0].Value = []byte("changed") },
		"body missing":         func(b *types.Block) { b.Body.Transactions = nil },
		"bad body commitment":  func(b *types.Block) { b.Header.BodyHash[0] ^= 1 },
		"bad state root":       func(b *types.Block) { b.Header.StateRoot[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			b := base
			b.Body.Transactions = append([]types.Transaction{}, base.Body.Transactions...)
			mutate(&b)
			// Re-sign modified commitments to test validation beyond authentication.
			if name == "bad body commitment" || name == "bad state root" || name == "wrong chain" || name == "wrong height" || name == "wrong parent" {
				b.Header, err = crypt.SignHeader(b.Header, key)
				if err != nil {
					t.Fatal(err)
				}
			}
			var initial state.State
			before := stateRoot(t, initial)
			returned, err := block.Validate(c, initial, b)
			if err == nil {
				t.Fatal("invalid block accepted")
			}
			if stateRoot(t, initial) != before || stateRoot(t, returned) != before {
				t.Fatal("failed validation leaked state")
			}
		})
	}
	// A genuine signature from another proposer still violates local context.
	otherKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32))
	wrong := base.Header
	wrong.Proposer = types.ValidatorID(otherKey[32:])
	wrong, err = crypt.SignHeader(wrong, otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = crypt.VerifyHeader(c.ChainID, wrong); err != nil {
		t.Fatal(err)
	}
	if block.ValidateHeader(c, wrong) == nil {
		t.Fatal("authentic unexpected proposer accepted")
	}
	// Fully authenticated malicious candidates: update the body commitment and
	// re-sign the header, so transaction rules must independently reject them.
	for _, fault := range []string{"signature", "owner", "nonce", "duplicate"} {
		t.Run("authenticated invalid TX/"+fault, func(t *testing.T) {
			tx := base.Body.Transactions[0]
			switch fault {
			case "signature":
				tx.Signature[0] ^= 1
			case "owner":
				tx.TargetKey = "someone-else/message"
			case "nonce":
				tx.Nonce = 2
			}
			if fault == "owner" || fault == "nonce" {
				tx, err = crypt.SignTransaction(tx, key)
				if err != nil {
					t.Fatal(err)
				}
			}
			b := base
			b.Body.Transactions = []types.Transaction{tx}
			if fault == "duplicate" {
				b.Body.Transactions = append(b.Body.Transactions, tx)
			}
			b.Header.BodyHash, err = crypt.BodyHash(c.ChainID, b.Body)
			if err != nil {
				t.Fatal(err)
			}
			b.Header, err = crypt.SignHeader(b.Header, key)
			if err != nil {
				t.Fatal(err)
			}
			if err = block.ValidateHeader(c, b.Header); err != nil {
				t.Fatal("test requires valid header", err)
			}
			returned, err := block.Validate(c, state.State{}, b)
			if err == nil {
				t.Fatal("invalid transactions accepted")
			}
			if returned.LastNonce(tx.Sender) != 0 {
				t.Fatal("failed block advanced nonce")
			}
		})
	}
}

func TestRejectInvalidBuildContext(t *testing.T) {
	c, key, tx := fixture(t)
	for name, mutate := range map[string]func(*block.Context){
		"empty chain":          func(c *block.Context) { c.ChainID = "" },
		"invalid UTF8":         func(c *block.Context) { c.ChainID = "\xff" },
		"zero height":          func(c *block.Context) { c.Height = 0 },
		"first parent":         func(c *block.Context) { c.ParentHash[0] = 1 },
		"missing later parent": func(c *block.Context) { c.Height = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			mutate(&bad)
			if _, err := block.Build(bad, state.State{}, nil, key); err == nil {
				t.Fatal("invalid context accepted")
			}
		})
	}
	if _, err := block.Build(c, state.State{}, []types.Transaction{tx}, key[:5]); err == nil {
		t.Fatal("bad private key accepted")
	}
	invalid := tx
	invalid.Nonce = 3
	invalid, err := crypt.SignTransaction(invalid, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = block.Build(c, state.State{}, []types.Transaction{tx, invalid}, key); err == nil {
		t.Fatal("invalid batch built")
	}
}
