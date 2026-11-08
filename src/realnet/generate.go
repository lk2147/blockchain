package realnet

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/types"
)

// Generate creates a fresh local development cluster. Key randomness is always
// crypto/rand through GenerateIdentity, independently of Faults.Seed.
func Generate(dir string, count, basePort int, runMS uint64, faults Policy) error {
	if count < 8 || (count-1)%3 != 0 || basePort < 1024 || basePort+count > 65535 {
		return fmt.Errorf("invalid cluster size or ports")
	}
	if err := faults.Validate(); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	} // Refuse to overwrite an existing cluster.
	var peers []Peer
	var privatePaths []string
	var workload [][]types.Transaction
	for i := 0; i < count; i++ {
		pub, key, err := crypt.GenerateIdentity()
		if err != nil {
			return err
		}
		name := fmt.Sprintf("key-%02d.hex", i)
		if err = os.WriteFile(filepath.Join(dir, name), []byte(hex.EncodeToString(key)), 0600); err != nil {
			return err
		}
		privatePaths = append(privatePaths, name)
		peers = append(peers, Peer{Address: fmt.Sprintf("127.0.0.1:%d", basePort+i), PublicKey: hex.EncodeToString(pub[:])})
		if i < 2 {
			tx, err := crypt.SignTransaction(types.Transaction{ChainID: "lab01-local", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte(fmt.Sprintf("hello from participant %d", i)), Nonce: 1}, key)
			if err != nil {
				return err
			}
			workload = append(workload, []types.Transaction{tx})
		}
	}
	for i := 0; i < count; i++ {
		policy := faults
		policy.Seed += int64(i)
		c := Config{ChainID: "lab01-local", NodeID: i, F: (count - 1) / 3, Peers: peers, PrivateKeyFile: privatePaths[i], OutputDir: fmt.Sprintf("node-%02d", i), BaseTimeoutMS: 1000, RetryMS: 400, RunForMS: runMS, TargetHeights: 2, SendGapMS: 1, QueueLimit: 2048, ReceiveLimit: 500, ReceiveWindowMS: 1000, BlockMS: 500, Faults: policy, Workload: workload}
		if err := c.Validate(); err != nil {
			return err
		}
		b, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, fmt.Sprintf("node-%02d.json", i)), b, 0600); err != nil {
			return err
		}
	}
	return nil
}
