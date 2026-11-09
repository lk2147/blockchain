package realnet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blockchain-lab01/src/block"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/network"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

type caseSpec struct {
	name    string
	roles   map[int]string
	crash   bool
	policy  Policy
	target  int
	timeout uint64
}
type caseEvidence struct {
	Root         string
	Transactions []types.Hash
	Counts       map[string]int
	Directory    string
}
type eventFields network.Event
type logRow struct {
	WallTime string
	eventFields
}

func TestStage5(t *testing.T) {
	cases := []caseSpec{
		{name: "T1", target: 2, timeout: 600},
		{name: "T2", target: 2, timeout: 1000, policy: Policy{Seed: 7, MinDelayMS: 2, MaxDelayMS: 8, DuplicateBPS: 10000, ReorderEvery: 4, ReorderHoldMS: 40}},
		{name: "T3", target: 2, timeout: 1000, roles: map[int]string{0: "invalid"}},
		{name: "T4", target: 2, timeout: 1000, roles: map[int]string{0: "transaction_replay"}},
		{name: "T5", target: 2, timeout: 600, policy: Policy{Seed: 5, DropBPS: 10000, DisableAfterMS: 1200}},
		{name: "T7", target: 1, timeout: 1000, roles: map[int]string{0: "equivocate", 1: "equivocate", 2: "equivocate"}},
	}
	for _, spec := range cases {
		t.Run(spec.name, func(t *testing.T) { runStage5Case(t, spec) })
	}
	t.Run("T6", func(t *testing.T) {
		t.Run("silent", func(t *testing.T) {
			runStage5Case(t, caseSpec{name: "T6-silent", target: 2, timeout: 600, roles: map[int]string{0: "silent"}})
		})
		t.Run("crashed", func(t *testing.T) {
			runStage5Case(t, caseSpec{name: "T6-crashed", target: 2, timeout: 600, crash: true})
		})
	})
	t.Run("T8", func(t *testing.T) {
		spec := caseSpec{name: "T8-first", target: 2, timeout: 1000, policy: Policy{Seed: 42, MinDelayMS: 2, MaxDelayMS: 20, DropBPS: 1000, DuplicateBPS: 1500, ReorderEvery: 7, ReorderHoldMS: 40, ReplayEvery: 5, ReplayAgeMS: 50, DisableAfterMS: 1200}}
		first := runStage5Case(t, spec)
		spec.name = "T8-second"
		second := runStage5Case(t, spec)
		if first.Root != second.Root || len(first.Transactions) != len(second.Transactions) {
			t.Fatal("same controlled workload changed results")
		}
		for i, h := range first.Transactions {
			if h != second.Transactions[i] {
				t.Fatal("rerun changed exact transaction order")
			}
		}
	})
}

func caseDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("LAB01_EVIDENCE_DIR")
	if root == "" {
		return t.TempDir()
	}
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runStage5Case(t *testing.T, spec caseSpec) caseEvidence {
	t.Helper()
	dir := caseDir(t, spec.name)
	addresses := reserveAddresses(t, 20)
	var ids []types.ValidatorID
	var keys []ed25519.PrivateKey
	var peers []Peer
	for i := 0; i < 10; i++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, 32))
		keys = append(keys, key)
		ids = append(ids, types.ValidatorID(key[32:]))
		peers = append(peers, Peer{Address: addresses[i], PublicKey: hex.EncodeToString(key[32:]), GossipAddress: addresses[10+i]})
	}
	var workload [][]types.Transaction
	for height := 1; height <= spec.target; height++ {
		pub := types.PublicKey(ids[0])
		tx, err := crypt.SignTransaction(types.Transaction{ChainID: "stage5", Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/message", Value: []byte(fmt.Sprintf("value-%d", height)), Nonce: uint64(height)}, keys[0])
		if err != nil {
			t.Fatal(err)
		}
		workload = append(workload, []types.Transaction{tx})
	}
	// Real libp2p hosts, secure sessions and subscription mesh need bootstrap time.
	// Keep eight seconds for the same protocol workload after the shared start.
	startAt := time.Now().Add(4 * time.Second).UnixMilli()
	var configs []Config
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	waited := make(map[int]bool)
	t.Cleanup(func() {
		cancel()
		for i, cmd := range commands {
			if !waited[i] {
				if err := cmd.Wait(); err != nil {
					t.Logf("cleanup child %d: %v", i, err)
				}
			}
		}
	})
	for i, key := range keys {
		keyPath := filepath.Join(dir, fmt.Sprintf("key-%02d.hex", i))
		if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
			t.Fatal(err)
		}
		policy := spec.policy
		policy.Seed += int64(i)
		cfg := Config{ChainID: "stage5", NodeID: i, F: 3, Peers: peers, PrivateKeyFile: keyPath, OutputDir: filepath.Join(dir, fmt.Sprintf("node-%02d", i)), BaseTimeoutMS: spec.timeout, RetryMS: 300, RunForMS: 12000, TargetHeights: uint64(spec.target), SendGapMS: 1, QueueLimit: 2048, ReceiveLimit: 3000, ReceiveWindowMS: 1000, BlockMS: 200, Faults: policy, Workload: workload, StartAtUnixMS: startAt}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("node-%02d.json", i))
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestProcessNodeEntry$", "-test.v")
		cmd.Env = append(os.Environ(), "LAB01_CHILD_CONFIG="+path, "LAB01_FAULTY_ROLE="+spec.roles[i])
		out := &bytes.Buffer{}
		cmd.Stdout = out
		cmd.Stderr = out
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
		outputs = append(outputs, out)
		configs = append(configs, cfg)
	}
	ready := false
	for time.Now().UnixMilli() < startAt {
		count := 0
		for _, cfg := range configs {
			if _, err := os.Stat(filepath.Join(cfg.OutputDir, "ready")); err == nil {
				count++
			}
		}
		if count == 10 {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("not all real node processes became ready before the common experiment start")
	}
	if spec.crash {
		if err := commands[0].Process.Kill(); err != nil {
			t.Fatal(err)
		}
		t.Logf("killed actual proposer PID %d before consensus start", commands[0].Process.Pid)
	}
	for i, cmd := range commands {
		err := cmd.Wait()
		waited[i] = true
		if spec.crash && i == 0 {
			if err == nil {
				t.Fatal("crashed process unexpectedly exited successfully")
			}
			continue
		}
		if err != nil {
			t.Fatalf("node %d: %v\n%s", i, err, outputs[i].String())
		}
	}
	evidence := verifyStage5(t, spec, configs, commands, ids, workload)
	evidence.Directory = dir
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "verified.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: independently verified honest ledgers; root=%s; events=%v; evidence=%s", spec.name, evidence.Root, evidence.Counts, dir)
	return evidence
}

func verifyStage5(t *testing.T, spec caseSpec, configs []Config, commands []*exec.Cmd, ids []types.ValidatorID, workload [][]types.Transaction) caseEvidence {
	t.Helper()
	ev := caseEvidence{Counts: make(map[string]int)}
	known := make(map[types.ValidatorID]bool)
	for _, id := range ids {
		known[id] = true
	}
	var expectedState state.State
	for _, batch := range workload {
		var err error
		expectedState, err = state.Execute("stage5", expectedState, batch)
		if err != nil {
			t.Fatal(err)
		}
		for _, tx := range batch {
			h, err := crypt.TransactionID(tx)
			if err != nil {
				t.Fatal(err)
			}
			ev.Transactions = append(ev.Transactions, h)
		}
	}
	expectedRoot, err := expectedState.Root("stage5")
	if err != nil {
		t.Fatal(err)
	}
	ev.Root = hex.EncodeToString(expectedRoot[:])
	var expectedHashes []types.Hash
	globalFinalized := make(map[uint64]types.Hash)
	equivocated := make(map[int]map[types.VotePhase]map[types.Hash]bool)
	actuallySent := make(map[int]map[types.VotePhase]map[types.Hash]bool)
	for id, cfg := range configs {
		faulty := spec.roles[id] != "" || (spec.crash && id == 0)
		rows := readCaseLog(t, filepath.Join(cfg.OutputDir, "events.jsonl"))
		if spec.name == "T1" || spec.name == "T2" || strings.HasPrefix(spec.name, "T8") {
			observed := map[string]bool{}
			for _, row := range rows {
				observed[row.Event] = true
			}
			for _, event := range []string{"membership", "gossip_connect", "gossip_publish", "gossip_receive", "stream_open", "stream_send", "stream_receive"} {
				if !observed[event] {
					t.Fatalf("node %d did not exercise real transport event %s", id, event)
				}
			}
		}
		if !(spec.crash && id == 0) {
			data, err := os.ReadFile(filepath.Join(cfg.OutputDir, "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			var result Result
			if err = json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.PID != commands[id].Process.Pid || result.PID == os.Getpid() || result.NodeID != id {
				t.Fatal("result is not from the expected independent process")
			}
			if !faulty {
				if len(result.Ledger) != spec.target {
					t.Fatalf("%s node %d finalized %d blocks, want %d", spec.name, id, len(result.Ledger), spec.target)
				}
				var current state.State
				var parent types.Hash
				var hashes []types.Hash
				for j, f := range result.Ledger {
					height := uint64(j + 1)
					if f.OriginRound > f.Round {
						t.Fatal("invalid origin/decision round")
					}
					if len(f.Block.Body.Transactions) != len(workload[j]) {
						t.Fatal("unexpected workload length")
					}
					for k, tx := range f.Block.Body.Transactions {
						got, err := crypt.TransactionID(tx)
						if err != nil {
							t.Fatal(err)
						}
						want, err := crypt.TransactionID(workload[j][k])
						if err != nil {
							t.Fatal(err)
						}
						if got != want {
							t.Fatal("finalized transaction order/content changed")
						}
					}
					proposer := ids[((height-1)%10+f.OriginRound%10)%10]
					current, err = block.Validate(block.Context{ChainID: "stage5", Height: height, ParentHash: parent, Proposer: proposer}, current, f.Block)
					if err != nil {
						t.Fatal(err)
					}
					parent, err = crypt.BlockHash(f.Block.Header)
					if err != nil {
						t.Fatal(err)
					}
					hashes = append(hashes, parent)
					seen := make(map[types.ValidatorID]bool)
					for _, v := range f.Proof {
						if !known[v.Validator] || seen[v.Validator] || v.Height != height || v.Round != f.Round || v.Phase != types.Precommit || v.IsNil || v.BlockHash != parent {
							t.Fatal("invalid finality certificate")
						}
						if err := crypt.VerifyVote("stage5", v); err != nil {
							t.Fatal(err)
						}
						seen[v.Validator] = true
					}
					if len(seen) < 7 {
						t.Fatal("less than seven authenticated unique precommits")
					}
					if (spec.crash || spec.roles[0] == "silent" || spec.roles[0] == "invalid" || spec.roles[0] == "transaction_replay") && height == 1 && f.Round == 0 {
						t.Fatal("faulty round-zero proposer did not require a later round")
					}
				}
				root, err := current.Root("stage5")
				if err != nil {
					t.Fatal(err)
				}
				if root != expectedRoot || result.StateRoot != ev.Root {
					t.Fatal("state differs from independently expected ordered workload")
				}
				for _, batch := range workload {
					tx := batch[0]
					if current.LastNonce(tx.Sender) != uint64(len(workload)) {
						t.Fatal("nonce history does not match at-most-once execution")
					}
				}
				if expectedHashes == nil {
					expectedHashes = hashes
				} else {
					for i, h := range hashes {
						if expectedHashes[i] != h {
							t.Fatal("honest processes finalized conflicting history")
						}
					}
				}
			}
		}
		checkCaseRows(t, spec, id, faulty, rows, globalFinalized, ev.Counts, equivocated, actuallySent, ids, cfg.StartAtUnixMS)
	}
	if spec.name == "T2" {
		for _, name := range []string{"sent_fault_duplicate", "sent_fault_reorder", "duplicate vote"} {
			if ev.Counts[name] == 0 {
				t.Fatalf("T2 did not exercise %s", name)
			}
		}
	}
	if spec.name == "T5" {
		if ev.Counts["fault_drop"] == 0 || ev.Counts["faults_disabled"] != 10 {
			t.Fatal("T5 did not exercise loss then stabilization")
		}
	}
	if spec.name == "T4" && (ev.Counts["adversary_transaction_replay"] != 9 || ev.Counts["sent_transaction_replay"] != 9) {
		t.Fatal("T4 did not replay the old transaction candidate to every finalized honest peer")
	}
	if strings.HasPrefix(spec.name, "T8") {
		for _, kind := range []string{"fault_drop", "sent_fault_duplicate", "sent_fault_reorder", "sent_fault_replay"} {
			if ev.Counts[kind] == 0 {
				t.Fatalf("controlled rerun did not exercise %s", kind)
			}
		}
	}
	if strings.HasPrefix(spec.name, "T6") {
		if ev.Counts["consensus_timeout"] == 0 {
			t.Fatal("no round timeout observed")
		}
	}
	if spec.name == "T7" {
		for id := 0; id < 3; id++ {
			for _, phase := range []types.VotePhase{types.Prevote, types.Precommit} {
				if len(equivocated[id][phase]) != 2 {
					t.Fatalf("validator %d did not genuinely sign both conflicting votes", id)
				}
				for hash := range equivocated[id][phase] {
					if !actuallySent[id][phase][hash] {
						t.Fatal("equivocation signed but not transmitted")
					}
				}
			}
		}
		if ev.Counts["conflicting vote"] == 0 {
			t.Fatal("no honest receiver detected equivocation")
		}
	}
	return ev
}

func readCaseLog(t *testing.T, path string) []logRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []logRow
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var row logRow
		if err = json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}

type pendingPlan struct {
	due  uint64
	kind string
}

func checkCaseRows(t *testing.T, spec caseSpec, id int, faulty bool, rows []logRow, finalized map[uint64]types.Hash, counts map[string]int, equiv map[int]map[types.VotePhase]map[types.Hash]bool, sentHashes map[int]map[types.VotePhase]map[types.Hash]bool, ids []types.ValidatorID, startAt int64) {
	t.Helper()
	plans := make(map[uint64][]pendingPlan)
	requests := make(map[transfer]bool)
	signed := make(map[[3]uint64]bool)
	localFinal := make(map[uint64]bool)
	reasons := make(map[string]bool)
	equivPackets := make(map[uint64]types.Vote)
	replayPackets := make(map[uint64]bool)
	var previous uint64
	sent := 0
	for _, r := range rows {
		counts[r.Event]++
		if r.Event == "adversary_transaction_replay" {
			replayPackets[r.Packet] = true
		}
		if r.Reason != "" {
			reasons[r.Reason] = true
			if r.Reason == "duplicate vote" || r.Reason == "conflicting vote" {
				counts[r.Reason]++
			}
		}
		if r.Event == "fault_normal" || r.Event == "fault_duplicate" || r.Event == "fault_reorder" || r.Event == "fault_replay" {
			var hold uint64
			if _, err := fmt.Sscanf(r.Reason, "hold %d ms", &hold); err != nil {
				t.Fatal(err)
			}
			plans[r.Packet] = append(plans[r.Packet], pendingPlan{r.At + hold, r.Event})
		}
		if r.Event == "receive" && r.Type == network.Request {
			requests[transfer{r.Peer, r.Ref}] = true
		}
		if r.Event == "send" {
			if replayPackets[r.Packet] {
				counts["sent_transaction_replay"]++
			}
			if sent > 0 && r.At < previous+1 {
				t.Fatal("node-wide transmission rate violated")
			}
			sent++
			previous = r.At
			choices := plans[r.Packet]
			which := -1
			for i, p := range choices {
				if p.due <= r.At && (which < 0 || p.due < choices[which].due) {
					which = i
				}
			}
			if which < 0 {
				t.Fatal("transmitted before planned hold")
			}
			counts["sent_"+choices[which].kind]++
			plans[r.Packet] = append(choices[:which], choices[which+1:]...)
			if r.Type == network.Body && !requests[transfer{r.Peer, r.Ref}] {
				t.Fatal("body sent without matching signed acceptance")
			}
			if r.Type == network.Vote && faulty && spec.name == "T7" {
				v, ok := equivPackets[r.Packet]
				if !ok || v.BlockHash != r.Ref {
					t.Fatal("actual equivocation transmission lacks matching signed evidence")
				}
				if sentHashes[id] == nil {
					sentHashes[id] = make(map[types.VotePhase]map[types.Hash]bool)
				}
				if sentHashes[id][v.Phase] == nil {
					sentHashes[id][v.Phase] = make(map[types.Hash]bool)
				}
				sentHashes[id][v.Phase][v.BlockHash] = true
			}
			if spec.name == "T5" {
				at, err := time.Parse(time.RFC3339Nano, r.WallTime)
				if err != nil {
					t.Fatal(err)
				}
				if at.UnixMilli() < startAt+1200 {
					t.Fatal("T5 delivered a deliberately dropped pre-stable transmission")
				}
			}
		}
		if !faulty && r.Event == "consensus_vote_signed" {
			slot := [3]uint64{r.Height, r.Round, uint64(r.Phase)}
			if signed[slot] {
				t.Fatal("honest validator signed twice in a voting slot")
			}
			signed[slot] = true
		}
		if !faulty && r.Event == "consensus_finalize" {
			if localFinal[r.Height] {
				t.Fatal("node finalized a height twice")
			}
			localFinal[r.Height] = true
			if prior, ok := finalized[r.Height]; ok && prior != r.Ref {
				t.Fatal("conflicting finalization in event history")
			}
			finalized[r.Height] = r.Ref
			if spec.name == "T5" {
				at, err := time.Parse(time.RFC3339Nano, r.WallTime)
				if err != nil {
					t.Fatal(err)
				}
				if at.UnixMilli() < startAt+1200 {
					t.Fatal("T5 finalization occurred before the loss window ended")
				}
			}
		}
		if faulty && r.Event == "adversary_vote" {
			var v types.Vote
			if err := json.Unmarshal([]byte(r.Reason), &v); err != nil {
				t.Fatal(err)
			}
			if err := crypt.VerifyVote("stage5", v); err != nil {
				t.Fatal("faulty peer forged invalid signature instead of equivocating", err)
			}
			if v.Validator != ids[id] || v.Height != 1 || v.Round != 0 || v.IsNil {
				t.Fatal("bad equivocation evidence identity/scope")
			}
			equivPackets[r.Packet] = v
			if equiv[id] == nil {
				equiv[id] = make(map[types.VotePhase]map[types.Hash]bool)
			}
			if equiv[id][v.Phase] == nil {
				equiv[id][v.Phase] = make(map[types.Hash]bool)
			}
			equiv[id][v.Phase][v.BlockHash] = true
		}
	}
	if !faulty {
		if len(localFinal) != spec.target {
			t.Fatal("finalization events do not match final ledger")
		}
		if spec.name == "T3" {
			if !reasons["invalid proposal signature"] || !reasons["invalid signature"] {
				t.Fatalf("node %d did not log both signature/domain rejections", id)
			}
		}
		if spec.name == "T4" {
			nonceRejected := false
			oldRejected := false
			for reason := range reasons {
				if strings.Contains(reason, "transaction 1: nonce 1, expected 2") {
					nonceRejected = true
				}
				if reason == "unexpected proposal height/round" {
					oldRejected = true
				}
			}
			if !nonceRejected || !oldRejected {
				t.Fatalf("node %d did not reject duplicated and replayed transaction candidates", id)
			}
		}
	}
}
