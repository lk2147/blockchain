package realnet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blockchain-lab01/src/block"
	cons "blockchain-lab01/src/consensus"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/network"
	pb "blockchain-lab01/src/proto"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

func TestTransportAuthenticationAndDomains(t *testing.T) {
	// Fixed, public TEST-ONLY key, not the fault layer's seed.
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	ids := []types.ValidatorID{types.ValidatorID(key[32:]), {2}}
	e := &pb.Envelope{ChainId: "c", Sender: 0, Recipient: 1, Sequence: 3, Kind: "vote", Reference: make([]byte, 32), Height: 2, Round: 4, Payload: []byte("payload")}
	b, err := TransportBytes(e)
	if err != nil {
		t.Fatal(err)
	}
	e.Signature = ed25519.Sign(key, b)
	if err = VerifyEnvelope("c", 1, ids, e); err != nil {
		t.Fatal(err)
	}
	if VerifyEnvelope("other", 1, ids, e) == nil || VerifyEnvelope("c", 0, ids, e) == nil {
		t.Fatal("configured scope ignored")
	}
	for _, field := range []string{"chain", "sender", "recipient", "sequence", "kind", "reference", "height", "round", "payload", "signature", "domain"} {
		t.Run(field, func(t *testing.T) {
			x := proto.Clone(e).(*pb.Envelope)
			switch field {
			case "chain":
				x.ChainId = "x"
			case "sender":
				x.Sender = 1
			case "recipient":
				x.Recipient = 0
			case "sequence":
				x.Sequence++
			case "kind":
				x.Kind = "header"
			case "reference":
				x.Reference[0] = 1
			case "height":
				x.Height++
			case "round":
				x.Round++
			case "payload":
				x.Payload[0] ^= 1
			case "signature":
				x.Signature[0] ^= 1
			case "domain":
				wrong := append([]byte(nil), b...)
				wrong[5] = 'X'
				x.Signature = ed25519.Sign(key, wrong)
			}
			if VerifyEnvelope("c", 1, ids, x) == nil {
				t.Fatal("tampered envelope accepted")
			}
		})
	}
	if got := hex.EncodeToString(b[:16]); got != "010000000b5452414e53504f52543a63" {
		t.Fatalf("transport prefix %s", got)
	}
}
func TestFaultPolicyPreservesSignedReplays(t *testing.T) {
	p := Policy{Seed: 1, MinDelayMS: 5, MaxDelayMS: 5, DuplicateBPS: 10000, ReorderEvery: 2, ReorderHoldMS: 10, ReplayEvery: 2, ReplayAgeMS: 10, DisableAfterMS: 100}
	f := newFaultLayer(p)
	e := &pb.Envelope{Kind: "vote", Recipient: 1, Payload: []byte("old"), Signature: []byte("original signature")}
	first, dropped := f.plan(0, e)
	if dropped || len(first) != 2 || first[0].after != 5 || first[1].reason != "duplicate" {
		t.Fatal("delay/duplication plan wrong")
	}
	e.Payload = []byte("new")
	out, _ := f.plan(20, e)
	if len(out) != 3 || out[0].after != 15 || out[0].reason != "reorder" {
		t.Fatal("reordering plan wrong")
	}
	if out[2].reason != "replay" || string(out[2].frame.Payload) != "old" || string(out[2].frame.Signature) != "original signature" {
		t.Fatal("replay did not preserve original signed message")
	}
	stable, drop := f.plan(101, e)
	if drop || len(stable) != 1 || stable[0].after != 0 {
		t.Fatal("faults continued after disable time")
	}
	f = newFaultLayer(Policy{DropBPS: 10000})
	if plan, drop := f.plan(0, e); !drop || len(plan) != 0 {
		t.Fatal("drop did not discard transmission")
	}
}

func TestRealSocketRejectsInvalidHeadersAndBodies(t *testing.T) {
	dir := t.TempDir()
	addrs := reserveAddresses(t, 10)
	var peers []Peer
	var keys []ed25519.PrivateKey
	var ids []types.ValidatorID
	for i := 0; i < 10; i++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, 32))
		keys = append(keys, key)
		ids = append(ids, types.ValidatorID(key[32:]))
		peers = append(peers, Peer{Address: addrs[i], PublicKey: hex.EncodeToString(key[32:])})
	}
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(keys[0])), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ChainID: "wire-test", NodeID: 0, F: 3, Peers: peers, PrivateKeyFile: keyPath, OutputDir: filepath.Join(dir, "output"), BaseTimeoutMS: 1500, RetryMS: 400, RunForMS: 4000, TargetHeights: 1, SendGapMS: 1, QueueLimit: 2048, ReceiveLimit: 4, ReceiveWindowMS: 10000, BlockMS: 200}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			if err := <-done; err != nil {
				t.Log(err)
			}
		}
	})
	ready := false
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		if _, err := os.Stat(filepath.Join(cfg.OutputDir, "ready")); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("real server not ready")
	}
	conn, err := grpc.NewClient(peers[0].Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewTransportClient(conn)
	sequence := uint64(0)
	deliver := func(kind network.Kind, ref types.Hash, data []byte) {
		t.Helper()
		sequence++
		e := &pb.Envelope{ChainId: cfg.ChainID, Sender: 1, Recipient: 0, Sequence: sequence, Kind: string(kind), Reference: ref[:], Height: 1, Payload: data}
		b, err := TransportBytes(e)
		if err != nil {
			t.Fatal(err)
		}
		e.Signature = ed25519.Sign(keys[1], b)
		rpcCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if _, err = client.Deliver(rpcCtx, e); err != nil {
			t.Fatal(err)
		}
	}
	pub := types.PublicKey(ids[0])
	tx, err := crypt.SignTransaction(types.Transaction{ChainID: cfg.ChainID, Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Nonce: 1, Value: []byte("expected")}, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := block.Build(block.Context{ChainID: cfg.ChainID, Height: 1, Proposer: ids[0]}, state.State{}, []types.Transaction{tx}, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	p, err := cons.SignProposal(cons.Proposal{ChainID: cfg.ChainID, Height: 1, Proposer: ids[0], Block: b}, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	p.Block.Body = types.BlockBody{}
	p.Signature[0] ^= 1
	bad, err := json.Marshal(struct{ Proposal *cons.Proposal }{&p})
	if err != nil {
		t.Fatal(err)
	}
	badRef := crypt.HashBytes(bad)
	deliver(network.Header, badRef, bad)
	p.Signature[0] ^= 1
	good, err := json.Marshal(struct{ Proposal *cons.Proposal }{&p})
	if err != nil {
		t.Fatal(err)
	}
	goodRef := crypt.HashBytes(good)
	deliver(network.Header, goodRef, good)
	wrongBody, err := json.Marshal(types.BlockBody{})
	if err != nil {
		t.Fatal(err)
	}
	deliver(network.Body, goodRef, wrongBody)
	deliver(network.Body, types.Hash{9}, wrongBody)
	// Four authenticated arrivals used this peer's allowance. More real RPCs
	// must trigger a temporary block and then a scheduled unblock.
	deliver(network.Vote, types.Hash{}, []byte("{}"))
	deliver(network.Vote, types.Hash{}, []byte("{}"))
	unblocked := false
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		data, err := os.ReadFile(filepath.Join(cfg.OutputDir, "events.jsonl"))
		if err == nil && bytes.Contains(data, []byte(`"Event":"unblock"`)) {
			unblocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !unblocked {
		t.Fatal("real peer block did not expire")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	finished = true
	logs, err := os.ReadFile(filepath.Join(cfg.OutputDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var badRejected, goodAccepted, mismatch, unsolicited bool
	var blocked, dropped bool
	candidateHash, err := crypt.BlockHash(b.Header)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(logs), []byte{'\n'}) {
		var e network.Event
		if err = json.Unmarshal(line, &e); err != nil {
			t.Fatal(err)
		}
		if e.Event == "header_accept" && e.Ref == badRef {
			t.Fatal("invalid header accepted")
		}
		if e.Event == "header_accept" && e.Ref == goodRef {
			goodAccepted = true
		}
		if strings.Contains(e.Reason, "invalid proposal signature") {
			badRejected = true
		}
		if e.Reason == "body commitment mismatch" {
			mismatch = true
		}
		if e.Reason == "wire body without accepted header" {
			unsolicited = true
		}
		if e.Event == "block" {
			blocked = true
		}
		if e.Event == "peer_drop" {
			dropped = true
		}
		if e.Event == "consensus_finalize" || (e.Event == "consensus_vote_signed" && e.Ref == candidateHash) {
			t.Fatal("bad wire data caused consensus support")
		}
	}
	if !badRejected || !goodAccepted || !mismatch || !unsolicited {
		t.Fatalf("missing rejection evidence: %v %v %v %v", badRejected, goodAccepted, mismatch, unsolicited)
	}
	if !blocked || !dropped {
		t.Fatal("real overactive-peer mitigation not exercised")
	}
}

// This is the child-process entry point used by TestRealProcessClusters.
func TestProcessNodeEntry(t *testing.T) {
	path := os.Getenv("LAB01_CHILD_CONFIG")
	if path == "" {
		t.Skip("child-process entry point; executed by TestRealProcessClusters")
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func reserveAddresses(t *testing.T, n int) []string {
	t.Helper()
	var listeners []net.Listener
	var addrs []string
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, l)
		addrs = append(addrs, l.Addr().String())
	}
	for _, l := range listeners {
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return addrs
}

func TestRealProcessClusters(t *testing.T) {
	var rootAcrossRuns string
	if os.Getenv("LAB01_CHILD_CONFIG") != "" {
		t.Fatal("cluster test must run in parent")
	}
	for _, faulty := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "injected-faults"}[faulty], func(t *testing.T) {
			dir := t.TempDir()
			addrs := reserveAddresses(t, 10)
			var peers []Peer
			var keys []ed25519.PrivateKey
			var ids []types.ValidatorID
			var workload [][]types.Transaction
			for i := 0; i < 10; i++ {
				key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, 32))
				pub := types.PublicKey(key[32:])
				keys = append(keys, key)
				ids = append(ids, types.ValidatorID(pub))
				peers = append(peers, Peer{Address: addrs[i], PublicKey: hex.EncodeToString(pub[:])})
				if i < 2 {
					tx, err := crypt.SignTransaction(types.Transaction{ChainID: "real-test", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte(fmt.Sprintf("value-%d", i)), Nonce: 1}, key)
					if err != nil {
						t.Fatal(err)
					}
					workload = append(workload, []types.Transaction{tx})
				}
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			var commands []*exec.Cmd
			var outputs []*bytes.Buffer
			waited := false
			t.Cleanup(func() {
				if !waited {
					cancel()
					for _, cmd := range commands {
						if err := cmd.Wait(); err != nil {
							t.Logf("cleanup reaped child: %v", err)
						}
					}
				}
			})
			for i, key := range keys {
				keyPath := filepath.Join(dir, fmt.Sprintf("key%d", i))
				if err = os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
					t.Fatal(err)
				}
				policy := Policy{Seed: int64(i + 1)}
				duration := uint64(8000)
				if faulty {
					duration = 10000
					policy = Policy{Seed: int64(i + 1), MinDelayMS: 2, MaxDelayMS: 25, DropBPS: 1000, DuplicateBPS: 1500, ReorderEvery: 7, ReorderHoldMS: 50, ReplayEvery: 3, ReplayAgeMS: 100, DisableAfterMS: 3000}
				}
				c := Config{ChainID: "real-test", NodeID: i, F: 3, Peers: peers, PrivateKeyFile: keyPath, OutputDir: filepath.Join(dir, fmt.Sprintf("node%d", i)), BaseTimeoutMS: 1000, RetryMS: 400, RunForMS: duration, TargetHeights: 2, SendGapMS: 1, QueueLimit: 2048, ReceiveLimit: 1000, ReceiveWindowMS: 1000, BlockMS: 200, Faults: policy, Workload: workload}
				b, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, fmt.Sprintf("node%d.json", i))
				if err = os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestProcessNodeEntry$", "-test.v")
				cmd.Env = append(os.Environ(), "LAB01_CHILD_CONFIG="+path)
				out := &bytes.Buffer{}
				cmd.Stdout = out
				cmd.Stderr = out
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				commands = append(commands, cmd)
				outputs = append(outputs, out)
			}
			for i, cmd := range commands {
				if err := cmd.Wait(); err != nil {
					for j := i + 1; j < len(commands); j++ {
						cancel()
						_ = commands[j].Wait()
					}
					waited = true
					t.Fatalf("child %d failed: %v\n%s", i, err, outputs[i].String())
				}
			}
			waited = true
			pids := map[int]bool{}
			var expected []types.Hash
			var expectedRoot string
			faultCounts := map[string]int{}
			for i := 0; i < 10; i++ {
				b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("node%d/result.json", i)))
				if err != nil {
					t.Fatal(err)
				}
				var result Result
				if err = json.Unmarshal(b, &result); err != nil {
					t.Fatal(err)
				}
				if result.PID == os.Getpid() || pids[result.PID] || result.NodeID != i || len(result.Ledger) != 2 {
					t.Fatalf("invalid independent result at node %d: pid=%d blocks=%d", i, result.PID, len(result.Ledger))
				}
				pids[result.PID] = true
				var s state.State
				var parent types.Hash
				for j, f := range result.Ledger {
					if len(f.Block.Body.Transactions) != len(workload[j]) {
						t.Fatal("unexpected ordered workload")
					}
					for k, tx := range f.Block.Body.Transactions {
						actual, err := crypt.TransactionID(tx)
						if err != nil {
							t.Fatal(err)
						}
						want, err := crypt.TransactionID(workload[j][k])
						if err != nil {
							t.Fatal(err)
						}
						if actual != want {
							t.Fatal("transaction order/content changed")
						}
					}
					h := uint64(j + 1)
					proposer := ids[((h-1)%10+f.OriginRound%10)%10]
					s, err = block.Validate(block.Context{ChainID: "real-test", Height: h, ParentHash: parent, Proposer: proposer}, s, f.Block)
					if err != nil {
						t.Fatal("independent execution failed", err)
					}
					parent, err = crypt.BlockHash(f.Block.Header)
					if err != nil {
						t.Fatal(err)
					}
					seen := map[types.ValidatorID]bool{}
					for _, v := range f.Proof {
						known := false
						for _, id := range ids {
							if id == v.Validator {
								known = true
							}
						}
						if !known || seen[v.Validator] || v.Height != h || v.Round != f.Round || v.Phase != types.Precommit || v.IsNil || v.BlockHash != parent {
							t.Fatal("bad independent quorum evidence")
						}
						if err = crypt.VerifyVote("real-test", v); err != nil {
							t.Fatal(err)
						}
						seen[v.Validator] = true
					}
					if len(seen) < 7 {
						t.Fatal("insufficient actual precommit quorum")
					}
					if i == 0 {
						expected = append(expected, parent)
					} else if expected[j] != parent {
						t.Fatal("real processes finalized conflicting history")
					}
				}
				root, err := s.Root("real-test")
				if err != nil {
					t.Fatal(err)
				}
				if result.StateRoot != hex.EncodeToString(root[:]) {
					t.Fatal("reported root disagrees with independent execution")
				}
				if i == 0 {
					expectedRoot = result.StateRoot
				} else if result.StateRoot != expectedRoot {
					t.Fatal("real process roots disagree")
				}
				for _, batch := range workload {
					tx := batch[0]
					value, ok := s.Get(tx.TargetKey)
					if !ok || !bytes.Equal(value, tx.Value) || s.LastNonce(tx.Sender) != 1 {
						t.Fatal("workload missing or replayed")
					}
				}
				logs, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("node%d/events.jsonl", i)))
				if err != nil {
					t.Fatal(err)
				}
				requests := map[transfer]bool{}
				planned := map[uint64][]uint64{}
				plannedReasons := map[uint64][]string{}
				var sent int
				var lastSend uint64
				for _, line := range bytes.Split(bytes.TrimSpace(logs), []byte{'\n'}) {
					var event network.Event
					if err = json.Unmarshal(line, &event); err != nil {
						t.Fatal(err)
					}
					faultCounts[event.Event]++
					if event.Event == "fault_normal" || event.Event == "fault_reorder" || event.Event == "fault_duplicate" || event.Event == "fault_replay" {
						var hold uint64
						if _, err := fmt.Sscanf(event.Reason, "hold %d ms", &hold); err != nil {
							t.Fatal(err)
						}
						planned[event.Packet] = append(planned[event.Packet], event.At+hold)
						plannedReasons[event.Packet] = append(plannedReasons[event.Packet], event.Event)
					}
					if event.Event == "receive" && event.Type == network.Request {
						requests[transfer{event.Peer, event.Ref}] = true
					}
					if event.Event == "send" {
						if sent > 0 && event.At < lastSend+1 {
							t.Fatal("actual outbound rate limit violated")
						}
						lastSend = event.At
						due := planned[event.Packet]
						which := -1
						for j, at := range due {
							if at <= event.At && (which < 0 || at < due[which]) {
								which = j
							}
						}
						if which < 0 {
							t.Fatalf("node %d sent packet %d before its configured hold elapsed", i, event.Packet)
						}
						planned[event.Packet] = append(due[:which], due[which+1:]...)
						reasons := plannedReasons[event.Packet]
						faultCounts["sent_"+reasons[which]]++
						plannedReasons[event.Packet] = append(reasons[:which], reasons[which+1:]...)
						sent++
						if event.Type == network.Body && !requests[transfer{event.Peer, event.Ref}] {
							t.Fatal("real body sent without received acceptance")
						}
					}
					if event.Event == "reject" && strings.Contains(event.Reason, "unexpected vote height") {
						faultCounts["old_vote_rejected"]++
					}
				}
				if sent == 0 {
					t.Fatal("process did not actually transmit")
				}
			}
			if faulty {
				for _, kind := range []string{"fault_drop", "sent_fault_duplicate", "sent_fault_reorder", "sent_fault_replay", "old_vote_rejected"} {
					if faultCounts[kind] == 0 {
						t.Fatalf("fault not exercised: %s", kind)
					}
				}
			}
			if rootAcrossRuns == "" {
				rootAcrossRuns = expectedRoot
			} else if rootAcrossRuns != expectedRoot {
				t.Fatal("same ordered workload produced a different result across real runs")
			}
			t.Logf("10 distinct processes; 2 independently verified blocks each; root=%s; faults=%v", expectedRoot, faultCounts)
		})
	}
}
