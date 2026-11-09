// Package realnet runs the blockchain node over GossipSub and gRPC streams.
package realnet

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"blockchain-lab01/src/types"
)

type Peer struct {
	Address       string
	PublicKey     string
	GossipAddress string
}
type Config struct {
	ChainID                          string
	NodeID, F                        int
	Peers                            []Peer
	PrivateKeyFile, OutputDir        string
	BaseTimeoutMS, RetryMS, RunForMS uint64
	TargetHeights                    uint64
	SendGapMS                        uint64
	QueueLimit, ReceiveLimit         int
	ReceiveWindowMS, BlockMS         uint64
	Faults                           Policy
	Workload                         [][]types.Transaction
	StartAtUnixMS                    int64 // Optional common real-clock start for controlled experiments.
}

func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	base := filepath.Dir(path)
	if !filepath.IsAbs(c.PrivateKeyFile) {
		c.PrivateKeyFile = filepath.Join(base, c.PrivateKeyFile)
	}
	if !filepath.IsAbs(c.OutputDir) {
		c.OutputDir = filepath.Join(base, c.OutputDir)
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.StartAtUnixMS < 0 || c.StartAtUnixMS > time.Now().Add(24*time.Hour).UnixMilli() {
		return fmt.Errorf("invalid experiment start time")
	}
	if len(c.Peers) < 8 || (len(c.Peers)-1)%3 != 0 || c.F != (len(c.Peers)-1)/3 || c.NodeID < 0 || c.NodeID >= len(c.Peers) {
		return fmt.Errorf("require n>=8, n=3f+1 and valid node ID")
	}
	if c.ChainID == "" || c.PrivateKeyFile == "" || c.OutputDir == "" || c.TargetHeights == 0 || c.BaseTimeoutMS == 0 || c.RetryMS == 0 || c.RunForMS == 0 || c.SendGapMS == 0 || c.QueueLimit < 1 || c.ReceiveLimit < 1 || c.ReceiveWindowMS == 0 || c.BlockMS == 0 {
		return fmt.Errorf("missing node settings")
	}
	for _, ms := range []uint64{c.BaseTimeoutMS, c.RetryMS, c.RunForMS, c.SendGapMS, c.ReceiveWindowMS, c.BlockMS} {
		if ms > uint64((24*time.Hour)/time.Millisecond) {
			return fmt.Errorf("duration exceeds one-day lab limit")
		}
	}
	seenAddr := map[string]bool{}
	seenKey := map[string]bool{}
	for _, p := range c.Peers {
		for _, address := range []string{p.Address, p.GossipAddress} {
			host, port, err := net.SplitHostPort(address)
			if err != nil || port == "" || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || seenAddr[address] {
				return fmt.Errorf("require distinct loopback gRPC and GossipAddress endpoints; regenerate old configurations")
			}
			seenAddr[address] = true
		}
		key, err := hex.DecodeString(p.PublicKey)
		if err != nil || len(key) != 32 || seenKey[string(key)] {
			return fmt.Errorf("invalid/duplicate peer identity or address")
		}
		seenKey[string(key)] = true
	}
	return c.Faults.Validate()
}
func (c Config) keys() ([]types.ValidatorID, ed25519.PrivateKey, error) {
	var ids []types.ValidatorID
	for _, p := range c.Peers {
		b, err := hex.DecodeString(p.PublicKey)
		if err != nil || len(b) != 32 {
			return nil, nil, fmt.Errorf("invalid public key")
		}
		ids = append(ids, types.ValidatorID(b))
	}
	b, err := os.ReadFile(c.PrivateKeyFile)
	if err != nil {
		return nil, nil, err
	}
	raw, err := hex.DecodeString(string(b))
	if err != nil || len(raw) != 64 {
		return nil, nil, fmt.Errorf("invalid private key file")
	}
	return ids, ed25519.PrivateKey(raw), nil
}
