// Command txload creates one non-validator Ed25519 identity and submits a burst
// of sequential transactions through a running validator's client RPC.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/realnet"
	"blockchain-lab01/src/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	configPath := flag.String("config", "", "node config used to discover the chain and ingress address")
	keyPath := flag.String("key", "", "new external private-key file; must not already exist")
	via := flag.Int("via", 0, "validator receiving client submissions")
	count := flag.Uint64("count", 20000, "number of transactions to submit")
	record := flag.String("record", "bulk", "record owned by the external sender")
	valueSize := flag.Int("value-size", 256, "approximate transaction value size in bytes")
	rate := flag.Int("rate", 200, "maximum accepted transactions per second; zero is unlimited")
	retry := flag.Duration("retry", 250*time.Millisecond, "wait before retrying temporary admission failures")
	requestTimeout := flag.Duration("request-timeout", 5*time.Second, "timeout for one RPC attempt")
	maxRetry := flag.Duration("max-retry", 60*time.Second, "maximum retry time for one transaction")
	flag.Parse()

	if err := run(*configPath, *keyPath, *via, *count, *record, *valueSize, *rate, *retry, *requestTimeout, *maxRetry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(configPath, keyPath string, via int, count uint64, record string, valueSize, rate int, retry, requestTimeout, maxRetry time.Duration) error {
	if configPath == "" || keyPath == "" || count == 0 || record == "" || valueSize < 1 || valueSize > 32768 || rate < 0 || retry <= 0 || requestTimeout <= 0 || maxRetry <= 0 {
		return fmt.Errorf("invalid load-generator arguments")
	}
	cfg, err := realnet.LoadConfig(configPath)
	if err != nil {
		return err
	}
	if via < 0 || via >= len(cfg.Peers) {
		return fmt.Errorf("unknown ingress node %d", via)
	}
	pub, key, err := createExternalKey(keyPath, cfg.Peers)
	if err != nil {
		return err
	}
	client, err := realnet.NewTransactionClient(cfg.Peers[via].Address)
	if err != nil {
		return err
	}
	defer client.Close()

	var gap time.Duration
	if rate > 0 {
		gap = time.Second / time.Duration(rate)
	}
	started := time.Now()
	for nonce := uint64(1); nonce <= count; nonce++ {
		if nonce > 1 && gap > 0 {
			time.Sleep(gap)
		}
		value := loadValue(nonce, valueSize)
		tx, err := crypt.SignTransaction(types.Transaction{
			ChainID: cfg.ChainID, Sender: pub,
			TargetKey: hex.EncodeToString(pub[:]) + "/" + record,
			Value:     value, Nonce: nonce,
		}, key)
		if err != nil {
			return err
		}
		id, err := submitWithRetry(client, tx, retry, requestTimeout, maxRetry)
		if err != nil {
			return fmt.Errorf("nonce %d: %w", nonce, err)
		}
		if nonce == 1 || nonce%100 == 0 || nonce == count {
			fmt.Printf("accepted %d/%d nonce=%d tx=%x\n", nonce, count, nonce, id)
		}
	}
	fmt.Printf("external public key: %x\n", pub)
	fmt.Printf("submitted %d transactions in %s; finalization continues asynchronously\n", count, time.Since(started).Round(time.Millisecond))
	return nil
}

func createExternalKey(path string, peers []realnet.Peer) (types.PublicKey, ed25519.PrivateKey, error) {
	pub, key, err := crypt.GenerateIdentity()
	if err != nil {
		return types.PublicKey{}, nil, err
	}
	encodedPub := hex.EncodeToString(pub[:])
	for _, peer := range peers {
		if strings.EqualFold(encodedPub, peer.PublicKey) {
			return types.PublicKey{}, nil, fmt.Errorf("generated identity unexpectedly belongs to validator set")
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return types.PublicKey{}, nil, fmt.Errorf("create fresh external key: %w", err)
	}
	_, writeErr := file.WriteString(hex.EncodeToString(key))
	closeErr := file.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return types.PublicKey{}, nil, err
	}
	return pub, key, nil
}

func loadValue(nonce uint64, size int) []byte {
	prefix := fmt.Sprintf("bulk-%08d:", nonce)
	if len(prefix) >= size {
		return []byte(prefix)
	}
	return []byte(prefix + strings.Repeat("x", size-len(prefix)))
}

func submitWithRetry(client *realnet.TransactionClient, tx types.Transaction, retry, requestTimeout, maxRetry time.Duration) (types.Hash, error) {
	deadline := time.Now().Add(maxRetry)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		id, err := client.Submit(ctx, tx)
		cancel()
		if err == nil {
			return id, nil
		}
		message := status.Convert(err).Message()
		code := status.Code(err)
		alreadyAccepted := strings.Contains(message, "duplicate transaction") || strings.Contains(message, "replayed transaction nonce")
		if alreadyAccepted {
			return crypt.TransactionID(tx)
		}
		temporary := strings.Contains(message, "nonce too far ahead") || strings.Contains(message, "mempool capacity exceeded") || code == codes.Unavailable || code == codes.DeadlineExceeded || code == codes.ResourceExhausted
		if !temporary {
			return types.Hash{}, err
		}
		if time.Now().Add(retry).After(deadline) {
			return types.Hash{}, fmt.Errorf("temporary admission failure did not recover within %s: %w", maxRetry, err)
		}
		time.Sleep(retry)
	}
}
