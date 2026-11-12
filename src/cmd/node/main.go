package main

import (
	"blockchain-lab01/src/ledger"
	"blockchain-lab01/src/node"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"blockchain-lab01/src/realnet"
)

func main() {
	config := flag.String("config", "", "per-node JSON configuration")
	initDir := flag.String("init-dir", "", "create a fresh local cluster directory instead of starting a node")
	nodes := flag.Int("nodes", 10, "validator count for init")
	port := flag.Int("base-port", 29000, "first loopback port for init")
	duration := flag.Uint64("run-ms", 0, "run duration; zero runs until Ctrl+C")
	submit := flag.String("submit", "", "submit this value as a signed transaction using the node's configured key")
	record := flag.String("record", "message", "owned record name")
	nonce := flag.Uint64("nonce", 0, "next sender nonce, starting at 1")
	via := flag.Int("via", -1, "ingress node for submission; defaults to signing node")
	inspect := flag.Bool("inspect", false, "read the on-disk ledger after stopping the node")
	faults := flag.Bool("faults", false, "generate example delay/drop/duplicate/reorder/replay policy")
	flag.Parse()
	submitRequested := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "submit" {
			submitRequested = true
		}
	})
	var err error
	if *initDir != "" {
		policy := realnet.Policy{Seed: 1}
		if *faults {
			policy = realnet.Policy{Seed: 1, MinDelayMS: 2, MaxDelayMS: 30, DropBPS: 1000, DuplicateBPS: 1500, ReorderEvery: 7, ReorderHoldMS: 60, ReplayEvery: 5, ReplayAgeMS: 300, DisableAfterMS: 3000}
		}
		err = realnet.Generate(*initDir, *nodes, *port, *duration, policy)
	} else if *config != "" {
		var c realnet.Config
		c, err = realnet.LoadConfig(*config)
		if err == nil {
			if *inspect {
				var db *ledger.Store
				db, err = ledger.Open(filepath.Join(c.OutputDir, "ledger.db"), c.ChainID, true)
				if err == nil {
					blocks, readErr := db.ReadAll()
					closeErr := db.Close()
					err = errors.Join(readErr, closeErr)
					if err == nil {
						err = json.NewEncoder(os.Stdout).Encode(blocks)
					}
				}
			} else if submitRequested {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				id, submitErr := realnet.SubmitValue(ctx, c, *record, *submit, *nonce, *via)
				err = submitErr
				if err == nil {
					fmt.Printf("transaction admitted: %x (not yet finalized)\n", id)
				}
			} else {
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
				defer cancel()
				err = realnet.Run(ctx, c)
			}
		}
	} else {
		err = fmt.Errorf("provide -config or -init-dir")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, node.ErrCrash) {
			os.Exit(86)
		}
		os.Exit(1)
	}
}
