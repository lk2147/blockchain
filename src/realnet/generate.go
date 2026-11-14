package realnet

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	crypt "blockchain-lab01/src/crypto"
)

// Generate creates a fresh local development cluster. Key randomness is always
// crypto/rand through GenerateIdentity, independently of Faults.Seed.
func Generate(dir string, count, basePort int, runMS uint64, faults Policy) error {
	if count < 8 || (count-1)%3 != 0 || basePort < 1024 || basePort+3*count-1 > 65535 {
		return fmt.Errorf("invalid cluster size or ports")
	}
	if err := faults.Validate(); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	} // Refuse to overwrite an existing cluster.
	var peers []Peer
	for i := 0; i < count; i++ {
		pub, key, err := crypt.GenerateIdentity()
		if err != nil {
			return err
		}
		name := fmt.Sprintf("key-%02d.hex", i)
		if err = os.WriteFile(filepath.Join(dir, name), []byte(hex.EncodeToString(key)), 0600); err != nil {
			return err
		}
		peers = append(peers, Peer{Address: fmt.Sprintf("127.0.0.1:%d", basePort+i), PublicKey: hex.EncodeToString(pub[:]), GossipAddress: fmt.Sprintf("127.0.0.1:%d", basePort+count+i)})
	}
	shared := struct {
		ChainID string
		F       int
		Peers   []Peer
	}{"lab01-local", (count - 1) / 3, peers}
	b, err := json.MarshalIndent(shared, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "network.json"), b, 0600); err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		policy := faults
		policy.Seed += int64(i)
		c := struct {
			NodeID       int
			NetworkFile  string
			Behavior     string
			AdminAddress string
			SendGapMS    uint64
			RunForMS     uint64  `json:",omitempty"`
			Faults       *Policy `json:",omitempty"`
		}{NodeID: i, NetworkFile: "network.json", Behavior: "honest", AdminAddress: fmt.Sprintf("127.0.0.1:%d", basePort+2*count+i), SendGapMS: 1, RunForMS: runMS}
		if faults != (Policy{}) && faults != (Policy{Seed: 1}) {
			c.Faults = &policy
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
