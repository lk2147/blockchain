package realnet

import (
	"net/http/httptest"
	"testing"

	cons "blockchain-lab01/src/consensus"
	"blockchain-lab01/src/types"
)

func TestLedgerRange(t *testing.T) {
	blocks := []cons.Finalized{
		{Block: types.Block{Header: types.BlockHeader{Height: 1}}},
		{Block: types.Block{Header: types.BlockHeader{Height: 2}}},
		{Block: types.Block{Header: types.BlockHeader{Height: 3}}},
	}
	request := httptest.NewRequest("GET", "/ledger?from=2&limit=1", nil)
	from, limit, err := ledgerRange(request)
	if err != nil || from != 2 || limit != 1 {
		t.Fatalf("range parse: %d %d %v", from, limit, err)
	}
	selected := selectLedgerRange(blocks, from, limit)
	if len(selected) != 1 || selected[0].Block.Header.Height != 2 {
		t.Fatal("wrong ledger range")
	}
	if empty := selectLedgerRange(nil, 1, 0); empty == nil || len(empty) != 0 {
		t.Fatal("empty ledger must encode as an empty JSON array")
	}
	for _, target := range []string{"/ledger?from=0", "/ledger?limit=0", "/ledger?from=x"} {
		if _, _, err := ledgerRange(httptest.NewRequest("GET", target, nil)); err == nil {
			t.Fatalf("accepted invalid range %s", target)
		}
	}
}
