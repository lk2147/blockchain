package realnet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
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

func TestLiveAdminInspection(t *testing.T) {
	dir := t.TempDir()
	addresses := reserveAddresses(t, 20)
	var peers []Peer
	var keys []ed25519.PrivateKey
	for i := 0; i < 10; i++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, 32))
		keys = append(keys, key)
		peers = append(peers, Peer{Address: addresses[i], PublicKey: hex.EncodeToString(key[32:]), GossipAddress: addresses[10+i]})
	}
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(keys[0])), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ChainID: "admin-test", NodeID: 0, F: 3, Peers: peers, PrivateKeyFile: keyPath, OutputDir: filepath.Join(dir, "node"), AdminAddress: "127.0.0.1:0", BaseTimeoutMS: 1500, RetryMS: 400, RunForMS: 3000, TargetHeights: 1, SendGapMS: 1, QueueLimit: 2048, ReceiveLimit: 1000, ReceiveWindowMS: 1000, BlockMS: 200}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	defer cancel()

	var adminAddress string
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		data, err := os.ReadFile(filepath.Join(cfg.OutputDir, "admin"))
		if err == nil {
			adminAddress = string(data)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if adminAddress == "" {
		t.Fatal("admin endpoint did not become ready")
	}
	external := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{99}, 32))
	pub := types.PublicKey(external[32:])
	tx, err := crypt.SignTransaction(types.Transaction{ChainID: cfg.ChainID, Sender: pub, TargetKey: hex.EncodeToString(pub[:]) + "/live", Value: []byte("pending"), Nonce: 1}, external)
	if err != nil {
		t.Fatal(err)
	}
	clientCtx, stopClient := context.WithTimeout(context.Background(), time.Second)
	_, err = SubmitTransaction(clientCtx, peers[0].Address, tx)
	stopClient()
	if err != nil {
		t.Fatal(err)
	}

	get := func(path string, target any) int {
		t.Helper()
		response, err := http.Get("http://" + adminAddress + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if target != nil && json.NewDecoder(response.Body).Decode(target) != nil {
			t.Fatal("invalid admin JSON")
		}
		return response.StatusCode
	}
	var pool struct {
		Count        int
		Transactions []pendingTransaction
	}
	if status := get("/mempool", &pool); status != http.StatusOK || pool.Count != 1 || len(pool.Transactions) != 1 || pool.Transactions[0].Transaction.Nonce != 1 {
		t.Fatalf("unexpected mempool response: status=%d count=%d", status, pool.Count)
	}
	var ledgerView struct {
		Height uint64
		Count  int
	}
	if status := get("/ledger?from=1&limit=10", &ledgerView); status != http.StatusOK || ledgerView.Height != 0 || ledgerView.Count != 0 {
		t.Fatalf("unexpected ledger response: status=%d height=%d count=%d", status, ledgerView.Height, ledgerView.Count)
	}
	var statusView struct {
		NodeID      int
		MempoolSize int
	}
	if status := get("/status", &statusView); status != http.StatusOK || statusView.NodeID != 0 || statusView.MempoolSize != 1 {
		t.Fatalf("unexpected status response: %d %+v", status, statusView)
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+adminAddress+"/mempool", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d, want 405", response.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRealSocketRejectsInvalidHeadersAndBodies(t *testing.T) {
	dir := t.TempDir()
	addrs := reserveAddresses(t, 20)
	var peers []Peer
	var keys []ed25519.PrivateKey
	var ids []types.ValidatorID
	for i := 0; i < 10; i++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, 32))
		keys = append(keys, key)
		ids = append(ids, types.ValidatorID(key[32:]))
		peers = append(peers, Peer{Address: addrs[i], PublicKey: hex.EncodeToString(key[32:]), GossipAddress: addrs[10+i]})
	}
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(keys[0])), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ChainID: "wire-test", NodeID: 0, F: 3, Peers: peers, PrivateKeyFile: keyPath, OutputDir: filepath.Join(dir, "output"), BaseTimeoutMS: 1500, RetryMS: 400, RunForMS: 4000, TargetHeights: 1, SendGapMS: 1, QueueLimit: 2048, ReceiveLimit: 4, ReceiveWindowMS: 10000, BlockMS: 200, Trace: true}
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
	streamCtx, stopStream := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopStream()
	stream, err := client.Exchange(streamCtx)
	if err != nil {
		t.Fatal(err)
	}
	initiator := &process{cfg: cfg, key: keys[1]}
	initiator.cfg.NodeID = 1
	hello, err := initiator.hello(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(hello); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil || response.Kind != "hello" {
		t.Fatalf("stream handshake: %v", err)
	}
	responses := make(chan *pb.Envelope, 32)
	go func() {
		defer close(responses)
		for {
			e, err := stream.Recv()
			if err != nil {
				return
			}
			select {
			case responses <- e:
			case <-streamCtx.Done():
				return
			}
		}
	}()
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
		if err = stream.Send(e); err != nil {
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
	// The response travels back on the very same bidirectional RPC, not a
	// reverse unary call or a second client connection.
	select {
	case e := <-responses:
		if e == nil || e.Kind != "body_request" || !bytes.Equal(e.Reference, goodRef[:]) || VerifyEnvelope(cfg.ChainID, 1, ids, e) != nil {
			t.Fatal("matching accepted-header body request did not return over the stream")
		}
	case <-time.After(time.Second):
		t.Fatal("no reverse protocol traffic on bidirectional stream")
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

// This is the child-process entry point used by TestStage5.
func TestProcessNodeEntry(t *testing.T) {
	path := os.Getenv("LAB01_CHILD_CONFIG")
	if path == "" {
		t.Skip("child-process entry point; executed by TestStage5")
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if role := os.Getenv("LAB01_FAULTY_ROLE"); role != "" {
		err = runWithNode(context.Background(), c, func(p *process) (localNode, error) { return newFaultyPeer(p, role) })
	} else {
		err = Run(context.Background(), c)
	}
	if err != nil {
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
