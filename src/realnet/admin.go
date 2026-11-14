package realnet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

type runtimeSnapshot struct {
	transactions []types.Transaction
	root         types.Hash
	err          error
}

type pendingTransaction struct {
	TransactionID string
	Transaction   types.Transaction
}

func (p *process) adminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mempool", p.handleMempool)
	mux.HandleFunc("/ledger", p.handleLedger)
	mux.HandleFunc("/status", p.handleStatus)
	return mux
}

func (p *process) snapshot(ctx context.Context) (runtimeSnapshot, error) {
	reply := make(chan runtimeSnapshot, 1)
	select {
	case p.in <- received{query: reply}:
	case <-ctx.Done():
		return runtimeSnapshot{}, ctx.Err()
	case <-p.ctx.Done():
		return runtimeSnapshot{}, fmt.Errorf("node stopping")
	}
	select {
	case snapshot := <-reply:
		return snapshot, snapshot.err
	case <-ctx.Done():
		return runtimeSnapshot{}, ctx.Err()
	case <-p.ctx.Done():
		return runtimeSnapshot{}, fmt.Errorf("node stopping")
	}
}

func requireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", http.MethodGet)
	http.Error(w, "GET required", http.StatusMethodNotAllowed)
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAdminError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, struct{ Error string }{err.Error()})
}

func (p *process) handleMempool(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	snapshot, err := p.snapshot(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, err)
		return
	}
	entries := make([]pendingTransaction, 0, len(snapshot.transactions))
	for _, tx := range snapshot.transactions {
		id, err := crypt.TransactionID(tx)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, err)
			return
		}
		entries = append(entries, pendingTransaction{TransactionID: hex.EncodeToString(id[:]), Transaction: tx})
	}
	writeJSON(w, http.StatusOK, struct {
		NodeID       int
		Count        int
		Transactions []pendingTransaction
	}{p.cfg.NodeID, len(entries), entries})
}

func (p *process) handleLedger(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	from, limit, err := ledgerRange(r)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err)
		return
	}
	all, err := p.store.ReadAll()
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err)
		return
	}
	blocks := selectLedgerRange(all, from, limit)
	writeJSON(w, http.StatusOK, struct {
		NodeID int
		Height uint64
		From   uint64
		Count  int
		Blocks []cons.Finalized
	}{p.cfg.NodeID, p.store.Height(), from, len(blocks), blocks})
}

func (p *process) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	snapshot, err := p.snapshot(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		NodeID      int
		ChainID     string
		Height      uint64
		MempoolSize int
		StateRoot   string
	}{p.cfg.NodeID, p.cfg.ChainID, p.store.Height(), len(snapshot.transactions), hex.EncodeToString(snapshot.root[:])})
}

func ledgerRange(r *http.Request) (uint64, int, error) {
	from := uint64(1)
	limit := 0
	var err error
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || from == 0 {
			return 0, 0, fmt.Errorf("from must be a positive block height")
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit64, parseErr := strconv.ParseUint(raw, 10, 31)
		if parseErr != nil || limit64 == 0 {
			return 0, 0, fmt.Errorf("limit must be positive")
		}
		limit = int(limit64)
	}
	return from, limit, nil
}

func selectLedgerRange(all []cons.Finalized, from uint64, limit int) []cons.Finalized {
	start := len(all)
	for i := range all {
		if all[i].Block.Header.Height >= from {
			start = i
			break
		}
	}
	end := len(all)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	out := make([]cons.Finalized, end-start)
	copy(out, all[start:end])
	return out
}
