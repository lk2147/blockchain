package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"blockchain-lab01/src/realnet"
)

func main() {
	config := flag.String("config", "", "per-node JSON configuration")
	initDir := flag.String("init-dir", "", "create a fresh local cluster directory instead of starting a node")
	nodes := flag.Int("nodes", 10, "validator count for init")
	port := flag.Int("base-port", 29000, "first loopback port for init")
	duration := flag.Uint64("run-ms", 10000, "per-node run duration for init")
	faults := flag.Bool("faults", false, "generate example delay/drop/duplicate/reorder/replay policy")
	flag.Parse()
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
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			err = realnet.Run(ctx, c)
		}
	} else {
		err = fmt.Errorf("provide -config or -init-dir")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
