// Package realnet runs the blockchain node over GossipSub and gRPC streams.
package realnet

import (
	"blockchain-lab01/src/types"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Peer struct {
	Address       string
	PublicKey     string
	GossipAddress string
}
type Config struct {
	ChainID                                                            string
	NodeID, F                                                          int
	Peers                                                              []Peer
	PrivateKeyFile, OutputDir                                          string
	BaseTimeoutMS, RetryMS, RunForMS                                   uint64
	TargetHeights                                                      uint64
	SendGapMS                                                          uint64
	QueueLimit, ReceiveLimit                                           int
	ReceiveWindowMS, BlockMS                                           uint64
	Faults                                                             Policy
	NetworkFile                                                        string `json:",omitempty"`
	Behavior                                                           string `json:",omitempty"`
	Trace                                                              bool   `json:",omitempty"`
	MempoolLimit, MempoolBytes, MaxTxBytes, MaxBlockTXs, MaxBlockBytes int
	StartAtUnixMS                                                      int64 // Optional common real-clock start for controlled experiments.
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
	if c.NetworkFile != "" {
		networkPath := c.NetworkFile
		if !filepath.IsAbs(networkPath) {
			networkPath = filepath.Join(base, networkPath)
		}
		raw, err := os.ReadFile(networkPath)
		if err != nil {
			return c, err
		}
		var shared struct {
			ChainID string
			F       int
			Peers   []Peer
		}
		if err = json.Unmarshal(raw, &shared); err != nil {
			return c, err
		}
		c.ChainID, c.F, c.Peers = shared.ChainID, shared.F, shared.Peers
	}
	c.Defaults()
	if !filepath.IsAbs(c.PrivateKeyFile) {
		c.PrivateKeyFile = filepath.Join(base, c.PrivateKeyFile)
	}
	if !filepath.IsAbs(c.OutputDir) {
		c.OutputDir = filepath.Join(base, c.OutputDir)
	}
	return c, c.Validate()
}
func (c *Config) Defaults() {
	if c.Behavior == "" {
		c.Behavior = "honest"
	}
	if c.PrivateKeyFile == "" {
		c.PrivateKeyFile = fmt.Sprintf("key-%02d.hex", c.NodeID)
	}
	if c.OutputDir == "" {
		c.OutputDir = fmt.Sprintf("node-%02d", c.NodeID)
	}
	if c.BaseTimeoutMS == 0 {
		c.BaseTimeoutMS = 2000
	}
	if c.RetryMS == 0 {
		c.RetryMS = 400
	}
	if c.TargetHeights == 0 {
		c.TargetHeights = ^uint64(0)
	}
	if c.SendGapMS == 0 {
		c.SendGapMS = 1
	}
	if c.QueueLimit == 0 {
		c.QueueLimit = 256
	}
	if c.ReceiveLimit == 0 {
		c.ReceiveLimit = 1000
	}
	if c.ReceiveWindowMS == 0 {
		c.ReceiveWindowMS = 1000
	}
	if c.BlockMS == 0 {
		c.BlockMS = 500
	}
	if c.MempoolLimit == 0 {
		c.MempoolLimit = 4096
	}
	if c.MempoolBytes == 0 {
		c.MempoolBytes = 8 << 20
	}
	if c.MaxTxBytes == 0 {
		c.MaxTxBytes = 64 << 10
	}
	if c.MaxBlockTXs == 0 {
		c.MaxBlockTXs = 128
	}
	if c.MaxBlockBytes == 0 {
		c.MaxBlockBytes = (4 << 20) - (16 << 10)
	}
}
func (c Config) Validate() error {
	switch c.Behavior {
	case "", "honest", "invalid_signature", "wrong_domain", "duplicate_transaction", "replay_transaction", "silent_proposer", "crashed_proposer":
	default:
		return fmt.Errorf("unknown node behavior %q", c.Behavior)
	}
	if c.StartAtUnixMS < 0 || c.StartAtUnixMS > time.Now().Add(24*time.Hour).UnixMilli() {
		return fmt.Errorf("invalid experiment start time")
	}
	if len(c.Peers) < 8 || (len(c.Peers)-1)%3 != 0 || c.F != (len(c.Peers)-1)/3 || c.NodeID < 0 || c.NodeID >= len(c.Peers) {
		return fmt.Errorf("require n>=8, n=3f+1 and valid node ID")
	}
	if c.ChainID == "" || c.PrivateKeyFile == "" || c.OutputDir == "" || c.TargetHeights == 0 || c.BaseTimeoutMS == 0 || c.RetryMS == 0 || c.SendGapMS == 0 || c.QueueLimit < 1 || c.ReceiveLimit < 1 || c.ReceiveWindowMS == 0 || c.BlockMS == 0 {
		return fmt.Errorf("missing node settings")
	}
	if c.MaxTxBytes < 1 || c.MaxTxBytes > 1<<20 || c.MempoolLimit < 1 || c.MempoolBytes < c.MaxTxBytes || c.MaxBlockTXs < 1 || c.MaxBlockBytes < c.MaxTxBytes || c.MaxBlockBytes > (4<<20)-(16<<10) {
		return fmt.Errorf("invalid mempool/block capacity")
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
