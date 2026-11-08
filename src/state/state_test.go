package state

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"math"
	"strings"
	"testing"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

// Public, fixed TEST-ONLY seeds; unrelated to any simulation PRNG.
func identity(n byte) (types.PublicKey, ed25519.PrivateKey) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{n}, 32))
	return types.PublicKey(key[32:]), key
}
func transaction(t *testing.T, who byte, nonce uint64, record, value string) types.Transaction {
	t.Helper()
	pub, key := identity(who)
	tx, err := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/" + record, Value: []byte(value), Nonce: nonce}, key)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}
func root(t *testing.T, s State) types.Hash {
	t.Helper()
	r, err := s.Root("lab")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func resign(t *testing.T, tx types.Transaction) types.Transaction {
	t.Helper()
	_, key := identity(1)
	signed, err := crypt.SignTransaction(tx, key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestOrderedExecutionAndIsolation(t *testing.T) {
	var empty State
	a := transaction(t, 1, 1, "message", "hello")
	b := transaction(t, 2, 1, "message", "hi")
	c := transaction(t, 1, 2, "message", "updated")
	next, err := Execute("lab", empty, []types.Transaction{a, b, c})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{a.TargetKey: "updated", b.TargetKey: "hi"} {
		got, ok := next.Get(key)
		if !ok || string(got) != want {
			t.Fatalf("%s = %q, %v", key, got, ok)
		}
	}
	if next.LastNonce(a.Sender) != 2 || next.LastNonce(b.Sender) != 1 {
		t.Fatal("wrong per-sender nonce")
	}
	if _, ok := empty.Get(a.TargetKey); ok || empty.LastNonce(a.Sender) != 0 {
		t.Fatal("initial state changed")
	}
	want := root(t, next)
	// Neither input slices nor returned values expose state storage.
	c.Value[0] = 'X'
	got, _ := next.Get(a.TargetKey)
	got[0] = 'X'
	if root(t, next) != want {
		t.Fatal("state aliases external bytes")
	}
	branch, err := Execute("lab", next, []types.Transaction{transaction(t, 1, 3, "message", "branch")})
	if err != nil {
		t.Fatal(err)
	}
	if root(t, next) != want || root(t, branch) == want {
		t.Fatal("branches are not isolated")
	}
	zero := transaction(t, 1, 1, "empty", "")
	emptyValue, err := Execute("lab", empty, []types.Transaction{zero})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := emptyValue.Get(zero.TargetKey); !ok || len(v) != 0 {
		t.Fatal("empty value not stored")
	}
}

func TestRejectedBatchesAreAtomic(t *testing.T) {
	a := transaction(t, 1, 1, "message", "first")
	first, err := Execute("lab", State{}, []types.Transaction{a})
	if err != nil {
		t.Fatal(err)
	}
	valid := transaction(t, 1, 2, "message", "second")
	badSig := transaction(t, 1, 3, "message", "bad")
	badSig.Signature[0] ^= 1
	other := transaction(t, 2, 1, "message", "other")
	unauthorized := transaction(t, 1, 3, "message", "bad")
	unauthorized.TargetKey = other.TargetKey
	emptyRecord := unauthorized
	emptyRecord.TargetKey = hex.EncodeToString(a.Sender[:]) + "/"
	uppercase := unauthorized
	uppercase.TargetKey = strings.ToUpper(hex.EncodeToString(a.Sender[:])) + "/message"
	foreign := transaction(t, 1, 3, "message", "foreign")
	foreign.ChainID = "other"
	for name, txs := range map[string][]types.Transaction{
		"bad signature":                {valid, badSig},
		"wrong owner":                  {valid, resign(t, unauthorized)},
		"empty record":                 {valid, resign(t, emptyRecord)},
		"uppercase namespace":          {valid, resign(t, uppercase)},
		"foreign chain":                {valid, resign(t, foreign)},
		"replayed earlier transaction": {valid, a},
		"duplicate in batch":           {valid, valid},
		"nonce zero":                   {transaction(t, 1, 0, "message", "bad")},
		"nonce gap":                    {transaction(t, 1, 3, "message", "bad")},
		"reversed sender order":        {transaction(t, 1, 3, "message", "bad"), valid},
	} {
		t.Run(name, func(t *testing.T) {
			before := root(t, first)
			returned, err := Execute("lab", first, txs)
			if err == nil {
				t.Fatal("invalid batch accepted")
			}
			if root(t, first) != before || root(t, returned) != before {
				t.Fatal("partial batch leaked")
			}
			if first.LastNonce(a.Sender) != 1 {
				t.Fatal("nonce changed")
			}
		})
	}
}

func TestCommitmentCanonicalAndDeterministic(t *testing.T) {
	a := transaction(t, 1, 1, "message", "A")
	b := transaction(t, 2, 1, "message", "B")
	x, err := Execute("lab", State{}, []types.Transaction{a, b})
	if err != nil {
		t.Fatal(err)
	}
	y, err := Execute("lab", State{}, []types.Transaction{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if root(t, x) != root(t, y) {
		t.Fatal("map insertion order affects root")
	}
	for i := 0; i < 20; i++ {
		if root(t, x) != root(t, y) {
			t.Fatal("unstable map iteration")
		}
	}
	// A repeated write changes no visible data but must change nonce commitment.
	updated, err := Execute("lab", x, []types.Transaction{transaction(t, 1, 2, "message", "A")})
	if err != nil {
		t.Fatal(err)
	}
	if root(t, x) == root(t, updated) {
		t.Fatal("nonce history omitted from commitment")
	}
	other, err := x.Root("other")
	if err != nil {
		t.Fatal(err)
	}
	if other == root(t, x) {
		t.Fatal("state domain lacks chain")
	}
	// Independent hand-written leaf bytes: records a,z then sender keys 1,2.
	s := State{records: map[string][]byte{"z": {2}, "a": {1}}, nonces: map[types.PublicKey]uint64{{2}: 9, {1}: 8}}
	wantHex := []string{"0000000001610000000101", "00000000017a0000000102", "0101" + strings.Repeat("00", 31) + "0000000000000008", "0102" + strings.Repeat("00", 31) + "0000000000000009"}
	got, err := s.CanonicalLeaves()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(wantHex) {
		t.Fatal("wrong leaf count")
	}
	for i, want := range wantHex {
		if hex.EncodeToString(got[i]) != want {
			t.Fatalf("leaf %d = %x", i, got[i])
		}
	}
	before := root(t, s)
	got[0][0] ^= 1
	if root(t, s) != before {
		t.Fatal("leaf encoding exposes state memory")
	}
}

func TestInvalidContextAndNonceExhaustion(t *testing.T) {
	for _, chain := range []string{"", "\xff"} {
		if _, err := Execute(chain, State{}, nil); err == nil {
			t.Fatal("invalid execution chain accepted")
		}
		if _, err := (State{}).Root(chain); err == nil {
			t.Fatal("invalid commitment chain accepted")
		}
	}
	pub, _ := identity(1)
	s := State{nonces: map[types.PublicKey]uint64{pub: math.MaxUint64}}
	before := root(t, s)
	got, err := Execute("lab", s, []types.Transaction{transaction(t, 1, 0, "message", "bad")})
	if err == nil || root(t, got) != before {
		t.Fatal("nonce wrapped around")
	}
}
