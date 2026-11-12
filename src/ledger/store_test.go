package ledger_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"blockchain-lab01/src/consensus"
	"blockchain-lab01/src/ledger"
	"blockchain-lab01/src/types"
)

func TestDurableSequentialLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := ledger.Open(path, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	f := consensus.Finalized{Block: types.Block{Header: types.BlockHeader{ChainID: "test", Height: 1}, Body: types.BlockBody{Transactions: []types.Transaction{{Value: []byte("durable bytes")}}}}}
	if err = s.Append(f); err != nil {
		t.Fatal(err)
	}
	if s.Append(f) == nil {
		t.Fatal("overwrote finalized height")
	}
	gap := f
	gap.Block.Header.Height = 3
	if s.Append(gap) == nil {
		t.Fatal("accepted missing height")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = ledger.Open(path, "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Height() != 1 {
		t.Fatal("height did not survive reopen")
	}
	got, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Block.Body.Transactions[0].Value) != "durable bytes" {
		t.Fatal("block body did not survive reopen")
	}
	if _, err = s.Get(2); err == nil {
		t.Fatal("invented missing block")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err := ledger.Open(path, "wrong-chain", true); err == nil {
		db.Close()
		t.Fatal("accepted foreign chain database")
	}
}

func TestLedgerWriterChild(t *testing.T) {
	path := os.Getenv("LAB01_LEDGER_CHILD")
	if path == "" {
		t.Skip("durability child process")
	}
	s, err := ledger.Open(path, "crash-test", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Append(consensus.Finalized{Block: types.Block{Header: types.BlockHeader{ChainID: "crash-test", Height: 1}, Body: types.BlockBody{Transactions: []types.Transaction{{Value: []byte("survives kill")}}}}}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".ready", []byte("synced"), 0600); err != nil {
		t.Fatal(err)
	}
	// Deliberately killed by the parent without Close or orderly shutdown.
	time.Sleep(time.Minute)
	t.Fatal("parent did not kill writer")
}
func TestLedgerSurvivesProcessKill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestLedgerWriterChild$")
	cmd.Env = append(os.Environ(), "LAB01_LEDGER_CHILD="+path)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	ready := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		if _, err := os.Stat(path + ".ready"); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("writer did not sync block")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("writer did not crash")
	}
	finished = true
	s, err := ledger.Open(path, "crash-test", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, err := s.Get(1)
	if err != nil || string(f.Block.Body.Transactions[0].Value) != "survives kill" {
		t.Fatalf("lost finalized disk block after kill: %v", err)
	}
}
