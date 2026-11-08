package crypto_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

// Public RFC 8032 section 7.1 fixture. TEST ONLY: its private seed is public.
func fixture(t *testing.T) (types.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := unhex(t, "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	key := ed25519.NewKeyFromSeed(seed)
	return types.PublicKey(key[32:]), key
}
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestReferenceVectors(t *testing.T) {
	pub, key := fixture(t)
	if hex.EncodeToString(pub[:]) != "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a" {
		t.Fatal("RFC public key mismatch")
	}
	want := unhex(t, "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b")
	if !bytes.Equal(ed25519.Sign(key, nil), want) {
		t.Fatal("RFC signature mismatch")
	}
	h := crypt.HashBytes([]byte("abc"))
	if hex.EncodeToString(h[:]) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("SHA-256 known answer mismatch")
	}
}

func TestAuthentication(t *testing.T) {
	pub, key := fixture(t)
	tx, err := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte("hello"), Nonce: 1}, key)
	if err != nil {
		t.Fatal(err)
	}
	h, err := crypt.SignHeader(types.BlockHeader{ChainID: "lab", Height: 1, ParentHash: types.Hash{1}, BodyHash: types.Hash{2}, StateRoot: types.Hash{3}, Proposer: types.ValidatorID(pub)}, key)
	if err != nil {
		t.Fatal(err)
	}
	v, err := crypt.SignVote(types.Vote{ChainID: "lab", Height: 1, Round: 2, Phase: types.Prevote, Validator: types.ValidatorID(pub), BlockHash: types.Hash{4}}, key)
	if err != nil {
		t.Fatal(err)
	}
	for name, check := range map[string]func(string) error{
		"TX":     func(c string) error { return crypt.VerifyTransaction(c, tx) },
		"HEADER": func(c string) error { return crypt.VerifyHeader(c, h) },
		"VOTE":   func(c string) error { return crypt.VerifyVote(c, v) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := check("lab"); err != nil {
				t.Fatal(err)
			}
			// The message is correctly signed for lab, but configuration disagrees.
			for _, chain := range []string{"other", ""} {
				if check(chain) == nil {
					t.Fatal("accepted unexpected configured chain")
				}
			}
		})
	}
	// Mutate every exported field independently, retaining the old signature.
	for _, original := range []any{tx, h, v} {
		rv := reflect.ValueOf(original)
		for i := 0; i < rv.NumField(); i++ {
			t.Run(rv.Type().Name()+"/tamper/"+rv.Type().Field(i).Name, func(t *testing.T) {
				copy := reflect.New(rv.Type()).Elem()
				copy.Set(rv)
				f := copy.Field(i)
				switch f.Kind() {
				case reflect.String:
					f.SetString(f.String() + "x")
				case reflect.Uint64:
					f.SetUint(f.Uint() + 1)
				case reflect.Uint8:
					f.SetUint(2)
				case reflect.Bool:
					f.SetBool(!f.Bool())
				case reflect.Array:
					f.Index(0).SetUint(f.Index(0).Uint() ^ 1)
				case reflect.Slice:
					f.SetBytes([]byte("changed"))
				default:
					t.Fatal("unhandled field")
				}
				var err error
				switch m := copy.Interface().(type) {
				case types.Transaction:
					err = crypt.VerifyTransaction("lab", m)
				case types.BlockHeader:
					err = crypt.VerifyHeader("lab", m)
				case types.Vote:
					err = crypt.VerifyVote("lab", m)
				}
				if err == nil {
					t.Fatal("tampered field accepted")
				}
			})
		}
	}
	// Even a valid signature under another context must fail. Payloads have
	// independent expected context prefixes, avoiding an encoder self-check.
	txb, err := crypt.CanonicalTxBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := crypt.CanonicalHeaderBytes(h)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := crypt.CanonicalVoteBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"TX", "HEADER", "VOTE"} {
		for _, m := range []struct {
			name    string
			payload []byte
			set     func(types.Signature) error
		}{
			{"TX", txb, func(s types.Signature) error { x := tx; x.Signature = s; return crypt.VerifyTransaction("lab", x) }},
			{"HEADER", hb, func(s types.Signature) error { x := h; x.Signature = s; return crypt.VerifyHeader("lab", x) }},
			{"VOTE", vb, func(s types.Signature) error { x := v; x.Signature = s; return crypt.VerifyVote("lab", x) }},
		} {
			if domain == m.name {
				continue
			}
			t.Run(m.name+"/wrong-domain/"+domain, func(t *testing.T) {
				// Skip version, uint32 context length, and original context.
				context := domain + ":lab"
				wrong := append([]byte{1, 0, 0, 0, byte(len(context))}, []byte(context)...)
				wrong = append(wrong, m.payload[5+len(m.name+":lab"):]...)
				if m.set(types.Signature(ed25519.Sign(key, wrong))) == nil {
					t.Fatal("wrong-domain signature accepted")
				}
			})
		}
	}
	for _, phase := range []types.VotePhase{types.Prevote, types.Precommit} {
		nilVote := v
		nilVote.Phase = phase
		nilVote.IsNil = true
		nilVote.BlockHash = types.Hash{}
		nilVote, err = crypt.SignVote(nilVote, key)
		if err != nil {
			t.Fatal(err)
		}
		if err = crypt.VerifyVote("lab", nilVote); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanonicalGoldenBytes(t *testing.T) {
	// Small values make lengths, ordering and endianness inspectable by hand.
	tx := types.Transaction{ChainID: "c", TargetKey: "k", Value: []byte("v"), Nonce: 0x0102030405060708}
	txb, err := crypt.CanonicalTxBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	txHex := "010000000454583a63" + strings.Repeat("00", 32) + "000000016b00000001760102030405060708"
	if hex.EncodeToString(txb) != txHex {
		t.Fatalf("TX bytes: %x", txb)
	}
	h := types.BlockHeader{ChainID: "c", Height: 1, ParentHash: types.Hash{1}, BodyHash: types.Hash{2}, StateRoot: types.Hash{3}, Proposer: types.ValidatorID{4}}
	hb, err := crypt.CanonicalHeaderBytes(h)
	if err != nil {
		t.Fatal(err)
	}
	hexpected := "01000000084845414445523a630000000000000001"
	for _, b := range []string{"01", "02", "03", "04"} {
		hexpected += b + strings.Repeat("00", 31)
	}
	if hex.EncodeToString(hb) != hexpected {
		t.Fatalf("header bytes: %x", hb)
	}
	v := types.Vote{ChainID: "c", Height: 1, Round: 2, Phase: types.Precommit, IsNil: true}
	vb, err := crypt.CanonicalVoteBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	vexpected := "0100000006564f54453a630000000000000001000000000000000202" + strings.Repeat("00", 32) + "01" + strings.Repeat("00", 32)
	if hex.EncodeToString(vb) != vexpected {
		t.Fatalf("vote bytes: %x", vb)
	}
	body, err := crypt.CanonicalBodyBytes("c", types.BlockBody{Transactions: []types.Transaction{tx}})
	if err != nil {
		t.Fatal(err)
	}
	// TX payload is 9 + 32 + 5 + 5 + 8 = 59 bytes (0x3b).
	bexpected := "0100000006424f44593a63000000010000003b" + txHex + strings.Repeat("00", 64)
	if hex.EncodeToString(body) != bexpected {
		t.Fatalf("body bytes: %x", body)
	}
	signed, err := crypt.CanonicalSignedTxBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(signed) != txHex+strings.Repeat("00", 64) {
		t.Fatal("signed TX representation")
	}
}

func TestEncodingAndHashes(t *testing.T) {
	pub, key := fixture(t)
	a, err := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub, TargetKey: "k", Value: []byte("v"), Nonce: 1}, key)
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.Nonce = 2
	b, err = crypt.SignTransaction(b, key)
	if err != nil {
		t.Fatal(err)
	}
	base := types.BlockBody{Transactions: []types.Transaction{a, b}}
	want, err := crypt.BodyHash("lab", base)
	if err != nil {
		t.Fatal(err)
	}
	mutated := a
	mutated.Value = []byte("other")
	sigChanged := a
	sigChanged.Signature[0] ^= 1
	for name, body := range map[string]types.BlockBody{
		"modified":  {Transactions: []types.Transaction{mutated, b}},
		"signature": {Transactions: []types.Transaction{sigChanged, b}},
		"deleted":   {Transactions: []types.Transaction{a}},
		"inserted":  {Transactions: []types.Transaction{a, b, a}},
		"reordered": {Transactions: []types.Transaction{b, a}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := crypt.BodyHash("lab", body)
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatal("body change not committed")
			}
		})
	}
	id, err := crypt.TransactionID(a)
	if err != nil {
		t.Fatal(err)
	}
	changedID, err := crypt.TransactionID(sigChanged)
	if err != nil {
		t.Fatal(err)
	}
	if id == changedID {
		t.Fatal("transaction ID excludes signature")
	}
	encoded, err := crypt.CanonicalSignedTxBytes(a)
	if err != nil {
		t.Fatal(err)
	}
	if id != crypt.HashBytes(encoded) {
		t.Fatal("ID is not hash of complete signed TX")
	}
	h := types.BlockHeader{ChainID: "lab"}
	hash, err := crypt.BlockHash(h)
	if err != nil {
		t.Fatal(err)
	}
	h.Signature[0] = 1
	other, err := crypt.BlockHash(h)
	if err != nil {
		t.Fatal(err)
	}
	if hash != other {
		t.Fatal("header identity includes signature")
	}
	// Reconstruct values with independent slices; canonical bytes must agree.
	copyTX := a
	copyTX.Value = append([]byte{}, a.Value...)
	first, err := crypt.CanonicalTxBytes(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := crypt.CanonicalTxBytes(copyTX)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("equivalent objects encode differently")
	}
	x := a
	x.TargetKey = "ab"
	x.Value = []byte("c")
	y := a
	y.TargetKey = "a"
	y.Value = []byte("bc")
	xb, err := crypt.CanonicalTxBytes(x)
	if err != nil {
		t.Fatal(err)
	}
	yb, err := crypt.CanonicalTxBytes(y)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(xb, yb) {
		t.Fatal("ambiguous field boundaries")
	}
	x.Value = nil
	y = x
	y.Value = []byte{}
	xb, err = crypt.CanonicalTxBytes(x)
	if err != nil {
		t.Fatal(err)
	}
	yb, err = crypt.CanonicalTxBytes(y)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(xb, yb) {
		t.Fatal("nil/empty values differ")
	}
}

func TestMalformedInputs(t *testing.T) {
	pub, key := fixture(t)
	for name, check := range map[string]func() error{
		"empty TX chain":     func() error { _, e := crypt.CanonicalTxBytes(types.Transaction{}); return e },
		"empty header chain": func() error { _, e := crypt.CanonicalHeaderBytes(types.BlockHeader{}); return e },
		"invalid chain UTF8": func() error { _, e := crypt.CanonicalHeaderBytes(types.BlockHeader{ChainID: "\xff"}); return e },
		"invalid key UTF8": func() error {
			_, e := crypt.CanonicalTxBytes(types.Transaction{ChainID: "lab", TargetKey: "\xff"})
			return e
		},
		"invalid phase": func() error { _, e := crypt.CanonicalVoteBytes(types.Vote{ChainID: "lab", IsNil: true}); return e },
		"NIL with hash": func() error {
			_, e := crypt.CanonicalVoteBytes(types.Vote{ChainID: "lab", Phase: types.Prevote, IsNil: true, BlockHash: types.Hash{1}})
			return e
		},
		"non-NIL zero hash": func() error {
			_, e := crypt.CanonicalVoteBytes(types.Vote{ChainID: "lab", Phase: types.Prevote})
			return e
		},
		"empty body chain": func() error { _, e := crypt.BodyHash("", types.BlockBody{}); return e },
		"body chain mismatch": func() error {
			_, e := crypt.BodyHash("lab", types.BlockBody{Transactions: []types.Transaction{{ChainID: "other"}}})
			return e
		},
		"short private key": func() error { _, e := crypt.SignTransaction(types.Transaction{ChainID: "lab"}, key[:10]); return e },
		"wrong signing identity": func() error {
			_, e := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: types.PublicKey{1}}, key)
			return e
		},
		"inconsistent private key": func() error {
			k := append(ed25519.PrivateKey{}, key...)
			k[63] ^= 1
			_, e := crypt.SignTransaction(types.Transaction{ChainID: "lab", Sender: pub}, k)
			return e
		},
		"unsigned transaction": func() error { return crypt.VerifyTransaction("lab", types.Transaction{ChainID: "lab", Sender: pub}) },
	} {
		t.Run(name, func(t *testing.T) {
			if check() == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
