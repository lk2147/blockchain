package state

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"blockchain-lab01/src/types"
)

// Explicit small-tree formulas use the standard hash directly, not production
// encoding or tree helpers, to independently check shape and hash domains.
func TestMerkleTreeShapes(t *testing.T) {
	hash := func(data []byte) types.Hash { return types.Hash(sha256.Sum256(data)) }
	leaf := func(s string) types.Hash { return hash(append([]byte{0}, []byte(s)...)) }
	pair := func(a, b types.Hash) types.Hash {
		data := append([]byte{1}, a[:]...)
		return hash(append(data, b[:]...))
	}
	a, b, c, d, e := leaf("a"), leaf("b"), leaf("c"), leaf("d"), leaf("e")
	for _, test := range []struct {
		name  string
		input []string
		want  types.Hash
	}{
		{"empty", nil, hash([]byte{2})},
		{"single", []string{"a"}, a},
		{"two", []string{"a", "b"}, pair(a, b)},
		{"three", []string{"a", "b", "c"}, pair(pair(a, b), pair(c, c))},
		{"four", []string{"a", "b", "c", "d"}, pair(pair(a, b), pair(c, d))},
		{"five", []string{"a", "b", "c", "d", "e"}, pair(pair(pair(a, b), pair(c, d)), pair(pair(e, e), pair(e, e)))},
	} {
		t.Run(test.name, func(t *testing.T) {
			var leaves [][]byte
			for _, s := range test.input {
				leaves = append(leaves, []byte(s))
			}
			if got := merkleHash(leaves); got != test.want {
				t.Fatalf("got %x want %x", got, test.want)
			}
		})
	}
	if merkleHash([][]byte{[]byte("a"), []byte("b")}) == merkleHash([][]byte{[]byte("b"), []byte("a")}) {
		t.Fatal("left-right order not committed")
	}
	// An internal preimage presented as a leaf must receive a different prefix.
	payload := append(a[:], b[:]...)
	if merkleHash([][]byte{payload}) == pair(a, b) {
		t.Fatal("leaf/internal hash domains overlap")
	}
}

func TestMerkleRootGoldenAndMutations(t *testing.T) {
	// These hard-coded roots were independently calculated with Python hashlib
	// from literal preimages. Chain c; sorted records a=1, b=2, c=3.
	for _, test := range []struct {
		name    string
		records map[string][]byte
		want    string
	}{
		{"empty", nil, "6669b6e772347d12f2402f32126038c7eec78f168db48ec1aa97910b0ce5aaff"},
		{"single", map[string][]byte{"a": {1}}, "25083bfec5d26eab066a0bdc14385989466f995b731a07feee344e2b83a7e5e7"},
		{"odd", map[string][]byte{"c": {3}, "a": {1}, "b": {2}}, "69c06002b8753e68dd7d3c1c5633b964391507a1e076e630b85ad92a0915cb2d"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, err := (State{records: test.records}).Root("c")
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(r[:]) != test.want {
				t.Fatalf("got %x want %s", r, test.want)
			}
		})
	}
	base := State{records: map[string][]byte{"a": {1}}, nonces: map[types.PublicKey]uint64{{1}: 1}}
	original := root(t, base)
	for name, s := range map[string]State{
		"record key":     {records: map[string][]byte{"b": {1}}, nonces: base.nonces},
		"record value":   {records: map[string][]byte{"a": {2}}, nonces: base.nonces},
		"record added":   {records: map[string][]byte{"a": {1}, "b": {2}}, nonces: base.nonces},
		"record removed": {nonces: base.nonces},
		"nonce value":    {records: base.records, nonces: map[types.PublicKey]uint64{{1}: 2}},
		"nonce sender":   {records: base.records, nonces: map[types.PublicKey]uint64{{2}: 1}},
		"nonce removed":  {records: base.records},
	} {
		t.Run(name, func(t *testing.T) {
			if root(t, s) == original {
				t.Fatal("changed state has same root")
			}
		})
	}
	if _, err := (State{records: map[string][]byte{"\xff": {}}}).Root("c"); err == nil {
		t.Fatal("invalid UTF-8 key accepted")
	}
	// Namespace tags distinguish a byte-like record from nonce metadata.
	leaves, err := base.CanonicalLeaves()
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 2 || leaves[0][0] != 0 || leaves[1][0] != 1 {
		t.Fatal("record/nonce tags missing")
	}
	// Bound leaf count at the root wrapper: these padded trees have equal inner
	// hashes by design, but distinct wrapped commitments must result.
	three := State{records: map[string][]byte{"a": {1}, "b": {2}, "c": {3}}}
	p, err := three.CanonicalLeaves()
	if err != nil {
		t.Fatal(err)
	}
	q := append(append([][]byte{}, p...), p[2])
	if merkleHash(p) != merkleHash(q) {
		t.Fatal("unexpected duplicate-last behavior")
	}
	wrap := func(count byte) [32]byte {
		prefix, err := hex.DecodeString("03010000000e53544154452d4d45524b4c453a6300000000000000")
		if err != nil {
			t.Fatal(err)
		}
		prefix = append(prefix, count)
		tree := merkleHash(p)
		return sha256.Sum256(append(prefix, tree[:]...))
	}
	actual, err := three.Root("c")
	if err != nil {
		t.Fatal(err)
	}
	if actual != types.Hash(wrap(3)) || actual == types.Hash(wrap(4)) {
		t.Fatal("root does not bind actual leaf count")
	}
}
