package consensus

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"blockchain-lab01/src/block"
	crypt "blockchain-lab01/src/crypto"
	"blockchain-lab01/src/state"
	"blockchain-lab01/src/types"
)

var (
	ErrDuplicate    = errors.New("duplicate vote")
	ErrEquivocation = errors.New("conflicting vote")
)

type Step uint8

const (
	Propose Step = iota + 1
	Prevote
	Precommit
)

type Timer struct {
	Height, Round uint64
	Step          Step
	Delay         uint64
}
type Event struct {
	Kind, Reason  string
	Height, Round uint64
	Hash          types.Hash
	Step          Step
	Validator     types.ValidatorID
	Phase         types.VotePhase
}

// Output is a local queue of instructions, not network delivery. The owner
// must broadcast votes/proposals and schedule timers. Draining transfers copies.
type Output struct {
	Votes     []types.Vote
	Proposals []Proposal
	Timers    []Timer
	Events    []Event
	Finalized []Finalized
}
type Finalized struct {
	Block              types.Block
	OriginRound, Round uint64
	Proof              []types.Vote
}
type Equivocation struct{ First, Second types.Vote }
type Status struct {
	Height, Round uint64
	Step          Step
	Locked        bool
	LockedHash    types.Hash
	LockedRound   uint64
}

type Config struct {
	ChainID     string
	Validators  []types.ValidatorID // Fixed order determines round-robin selection.
	F           int
	PrivateKey  ed25519.PrivateKey
	BaseTimeout uint64 // Logical ticks; increases by round, saturating on overflow.
}
type voteKey struct {
	round     uint64
	phase     types.VotePhase
	validator types.ValidatorID
}
type choice struct {
	nilVote bool
	hash    types.Hash
}
type candidate struct {
	block  types.Block
	origin uint64
	post   state.State
}

// Engine owns all mutable consensus data. Do not call it concurrently. No
// restart recovery is implemented; recreating an identity mid-run is forbidden.
type Engine struct {
	chain         string
	validators    []types.ValidatorID
	members       map[types.ValidatorID]bool
	id            types.ValidatorID
	key           ed25519.PrivateKey
	quorum        int
	faults        int
	base          uint64
	height, round uint64
	step          Step
	parent        types.Hash
	committed     state.State
	ledger        []Finalized
	equivocations []Equivocation
	votes         map[voteKey]types.Vote
	sent          map[voteKey]types.Vote
	blocks        map[types.Hash]candidate
	proposals     map[uint64]Proposal
	locked        bool
	lockedHash    types.Hash
	lockedRound   uint64
	valid         bool
	validHash     types.Hash
	validRound    uint64
	validProof    []types.Vote
	out           Output
}

func New(c Config) (*Engine, error) {
	if c.F < 0 || len(c.Validators) == 0 || (len(c.Validators)-1)%3 != 0 || c.F != (len(c.Validators)-1)/3 {
		return nil, fmt.Errorf("require n = 3f + 1")
	}
	if c.BaseTimeout == 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}
	if c.ChainID == "" || !utf8.ValidString(c.ChainID) {
		return nil, fmt.Errorf("invalid chain ID")
	}
	if len(c.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key")
	}
	key := ed25519.NewKeyFromSeed(c.PrivateKey[:ed25519.SeedSize])
	if !bytes.Equal(key, c.PrivateKey) {
		return nil, fmt.Errorf("inconsistent private key")
	}
	id := types.ValidatorID(key[ed25519.SeedSize:])
	members := make(map[types.ValidatorID]bool)
	for _, v := range c.Validators {
		if members[v] {
			return nil, fmt.Errorf("duplicate validator")
		}
		if v == (types.ValidatorID{}) {
			return nil, fmt.Errorf("zero validator key")
		}
		members[v] = true
	}
	if !members[id] {
		return nil, fmt.Errorf("local identity not a validator")
	}
	e := &Engine{chain: c.ChainID, validators: append([]types.ValidatorID(nil), c.Validators...), members: members, id: id, key: key, quorum: 2*c.F + 1, faults: c.F, base: c.BaseTimeout, height: 1}
	e.resetHeight()
	e.enter(Propose)
	return e, nil
}

func (e *Engine) Proposer(height, round uint64) types.ValidatorID {
	n := uint64(len(e.validators))
	return e.validators[((height-1)%n+round%n)%n]
}
func (e *Engine) Status() Status {
	return Status{e.height, e.round, e.step, e.locked, e.lockedHash, e.lockedRound}
}
func (e *Engine) State() state.State { return e.committed }
func (e *Engine) Equivocations() []Equivocation {
	return append([]Equivocation(nil), e.equivocations...)
}
func (e *Engine) Ledger() []Finalized {
	result := make([]Finalized, len(e.ledger))
	for i, f := range e.ledger {
		result[i] = f
		result[i].Block = cloneBlock(f.Block)
		result[i].Proof = append([]types.Vote(nil), f.Proof...)
	}
	return result
}
func (e *Engine) Drain() Output {
	out := e.out
	e.out = Output{}
	return out
}
func (e *Engine) event(kind, reason string, h types.Hash) {
	e.out.Events = append(e.out.Events, Event{Kind: kind, Reason: reason, Height: e.height, Round: e.round, Hash: h, Step: e.step, Validator: e.id})
}
func (e *Engine) reject(err error) error { e.event("reject", err.Error(), types.Hash{}); return err }
func (e *Engine) resetHeight() {
	e.round = 0
	e.locked = false
	e.lockedHash = types.Hash{}
	e.lockedRound = 0
	e.valid = false
	e.validHash = types.Hash{}
	e.validRound = 0
	e.validProof = nil
	e.votes = make(map[voteKey]types.Vote)
	e.sent = make(map[voteKey]types.Vote)
	e.blocks = make(map[types.Hash]candidate)
	e.proposals = make(map[uint64]Proposal)
}
func (e *Engine) enter(step Step) {
	e.step = step
	delay := uint64(math.MaxUint64)
	if e.round < math.MaxUint64 && e.base <= math.MaxUint64/(e.round+1) {
		delay = e.base * (e.round + 1)
	}
	e.out.Timers = append(e.out.Timers, Timer{e.height, e.round, step, delay})
	e.event("step", "", types.Hash{})
}

// Timeout tokens include height, round and step. Stale scheduled callbacks
// have no effect; a node can never vote at a stale height or round.
func (e *Engine) Timeout(t Timer) error {
	if t.Height != e.height || t.Round != e.round || t.Step != e.step {
		return e.reject(fmt.Errorf("stale timeout"))
	}
	e.event("timeout", "", types.Hash{})
	switch e.step {
	case Propose:
		e.enter(Prevote)
		if err := e.emit(types.Prevote, choice{nilVote: true}); err != nil {
			return err
		}
		return e.progress()
	case Prevote:
		e.enter(Precommit)
		if err := e.emit(types.Precommit, choice{nilVote: true}); err != nil {
			return err
		}
		return e.progress()
	case Precommit:
		if e.round == math.MaxUint64 {
			return e.reject(fmt.Errorf("round exhausted"))
		}
		e.round++
		e.enter(Propose)
		return e.progress()
	}
	return e.reject(fmt.Errorf("invalid timeout step"))
}

// MakeProposal is invoked only by the current proposer. A known quorum-backed
// candidate takes precedence over new transactions, ensuring locks can survive
// round changes. Publication is separate from local validation/prevoting.
func (e *Engine) MakeProposal(txs []types.Transaction) (Proposal, error) {
	if e.Proposer(e.height, e.round) != e.id || e.step != Propose {
		return Proposal{}, e.reject(fmt.Errorf("not current proposing validator/step"))
	}
	if _, exists := e.proposals[e.round]; exists {
		return Proposal{}, e.reject(fmt.Errorf("proposal already accepted"))
	}
	p := Proposal{ChainID: e.chain, Height: e.height, Round: e.round, OriginRound: e.round, Proposer: e.id}
	if e.valid {
		if e.validRound >= e.round {
			return Proposal{}, e.reject(fmt.Errorf("current-round quorum already observed; await proposal or timeout"))
		}
		c := e.blocks[e.validHash]
		p.Block = cloneBlock(c.block)
		p.OriginRound = c.origin
		p.Proof = append([]types.Vote(nil), e.validProof...)
	} else {
		b, err := block.Build(block.Context{ChainID: e.chain, Height: e.height, ParentHash: e.parent, Proposer: e.id}, e.committed, txs, e.key)
		if err != nil {
			return Proposal{}, e.reject(err)
		}
		p.Block = b
	}
	p, err := SignProposal(p, e.key)
	if err != nil {
		return Proposal{}, e.reject(err)
	}
	if err = e.ReceiveProposal(p); err != nil {
		return Proposal{}, err
	}
	e.out.Proposals = append(e.out.Proposals, cloneProposal(p))
	return cloneProposal(p), nil
}

func (e *Engine) checkProof(p Proposal, hash types.Hash) (uint64, error) {
	if len(p.Proof) == 0 {
		if p.OriginRound != p.Round {
			return 0, fmt.Errorf("reproposal requires prior PREVOTE quorum")
		}
		return 0, nil
	}
	r := p.Proof[0].Round
	if r >= p.Round || r < p.OriginRound {
		return 0, fmt.Errorf("invalid proof round")
	}
	seen := make(map[types.ValidatorID]bool)
	for _, v := range p.Proof {
		if !e.members[v.Validator] || seen[v.Validator] {
			return 0, fmt.Errorf("unknown or repeated proof validator")
		}
		if v.Height != e.height || v.Round != r || v.Phase != types.Prevote || v.IsNil || v.BlockHash != hash {
			return 0, fmt.Errorf("proof vote scope mismatch")
		}
		if err := crypt.VerifyVote(e.chain, v); err != nil {
			return 0, err
		}
		seen[v.Validator] = true
	}
	if len(seen) < e.quorum {
		return 0, fmt.Errorf("insufficient proof quorum")
	}
	return r, nil
}

func (e *Engine) ReceiveProposal(p Proposal) error {
	if err := e.ValidateProposalHeader(p); err != nil {
		return e.reject(err)
	}
	hash, err := crypt.BlockHash(p.Block.Header)
	if err != nil {
		return e.reject(err)
	}
	if _, exists := e.proposals[e.round]; exists {
		return e.reject(fmt.Errorf("duplicate/conflicting proposal"))
	}
	post, err := block.Validate(block.Context{ChainID: e.chain, Height: e.height, ParentHash: e.parent, Proposer: e.Proposer(e.height, p.OriginRound)}, e.committed, p.Block)
	if err != nil {
		return e.reject(err)
	}
	proofRound := uint64(0)
	if len(p.Proof) > 0 {
		proofRound = p.Proof[0].Round
	}
	e.blocks[hash] = candidate{cloneBlock(p.Block), p.OriginRound, post}
	e.proposals[e.round] = cloneProposal(p)
	if len(p.Proof) > 0 {
		e.observeQuorum(hash, proofRound, p.Proof)
	}
	e.event("proposal", "validated", hash)
	if e.step == Propose {
		e.enter(Prevote)
		c := choice{hash: hash}
		if e.locked && e.lockedHash != hash {
			c = choice{nilVote: true}
		}
		if err = e.emit(types.Prevote, c); err != nil {
			return err
		}
	}
	return e.progress()
}

// ValidateProposalHeader authenticates the envelope, header and quorum evidence
// without reading a body or changing consensus state. Header acceptance is not
// proposal acceptance: ReceiveProposal still validates and executes the body.
func (e *Engine) ValidateProposalHeader(p Proposal) error {
	if p.Height != e.height || p.Round != e.round || p.OriginRound > p.Round {
		return fmt.Errorf("unexpected proposal height/round")
	}
	if p.Proposer != e.Proposer(e.height, e.round) {
		return fmt.Errorf("wrong round proposer")
	}
	if err := VerifyProposal(e.chain, p); err != nil {
		return err
	}
	hash, err := crypt.BlockHash(p.Block.Header)
	if err != nil {
		return err
	}
	err = block.ValidateHeader(block.Context{ChainID: e.chain, Height: e.height, ParentHash: e.parent, Proposer: e.Proposer(e.height, p.OriginRound)}, p.Block.Header)
	if err != nil {
		return err
	}
	_, err = e.checkProof(p, hash)
	if err != nil {
		return err
	}
	return nil
}

func (e *Engine) emit(phase types.VotePhase, c choice) error {
	k := voteKey{e.round, phase, e.id}
	if _, ok := e.sent[k]; ok {
		return nil
	}
	if prior, ok := e.votes[k]; ok {
		// Even an externally replayed, genuine local vote must not be followed
		// by a conflicting signature. Normal callers own one Engine per key.
		if prior.IsNil != c.nilVote || prior.BlockHash != c.hash {
			return e.reject(fmt.Errorf("local signing history conflict"))
		}
		e.sent[k] = prior
		e.out.Votes = append(e.out.Votes, prior)
		return nil
	}
	v, err := crypt.SignVote(types.Vote{ChainID: e.chain, Height: e.height, Round: e.round, Phase: phase, Validator: e.id, IsNil: c.nilVote, BlockHash: c.hash}, e.key)
	if err != nil {
		return e.reject(err)
	}
	e.sent[k] = v
	e.votes[k] = v
	e.out.Votes = append(e.out.Votes, v)
	e.event("vote_signed", "", c.hash)
	e.out.Events[len(e.out.Events)-1].Phase = phase
	return nil
}

// ReceiveVote accepts authenticated votes for the current height, including
// earlier rounds for late decisions and future rounds as evidence. It never
// signs in another round. Catch-up requires f+1 distinct future-round senders,
// so Byzantine validators alone cannot force an unbounded round jump.
func (e *Engine) ReceiveVote(v types.Vote) error {
	if v.Height != e.height {
		return e.reject(fmt.Errorf("unexpected vote height"))
	}
	if !e.members[v.Validator] {
		return e.reject(fmt.Errorf("unknown validator"))
	}
	if err := crypt.VerifyVote(e.chain, v); err != nil {
		return e.reject(err)
	}
	k := voteKey{v.Round, v.Phase, v.Validator}
	if prior, ok := e.votes[k]; ok {
		if prior.IsNil == v.IsNil && prior.BlockHash == v.BlockHash {
			return e.reject(ErrDuplicate)
		}
		e.equivocations = append(e.equivocations, Equivocation{prior, v})
		return e.reject(ErrEquivocation)
	}
	e.votes[k] = v
	e.event("vote_received", "", v.BlockHash)
	e.out.Events[len(e.out.Events)-1].Validator = v.Validator
	e.out.Events[len(e.out.Events)-1].Phase = v.Phase
	e.out.Events[len(e.out.Events)-1].Round = v.Round
	e.catchUpRound()
	return e.progress()
}

func (e *Engine) catchUpRound() {
	for _, r := range e.rounds() {
		if r <= e.round {
			continue
		}
		count := 0
		for _, id := range e.validators {
			_, pv := e.votes[voteKey{r, types.Prevote, id}]
			_, pc := e.votes[voteKey{r, types.Precommit, id}]
			if pv || pc {
				count++
			}
		}
		if count > e.faults {
			e.round = r
			e.event("round_catchup", "f+1 authenticated validators", types.Hash{})
			e.enter(Propose)
		}
	}
}

// certificate order follows the fixed validator list, avoiding map iteration
// in outputs, proposal signatures and local evidence ordering.
func (e *Engine) certificate(round uint64, phase types.VotePhase, c choice) []types.Vote {
	var proof []types.Vote
	for _, id := range e.validators {
		v, ok := e.votes[voteKey{round, phase, id}]
		if ok && v.IsNil == c.nilVote && v.BlockHash == c.hash {
			proof = append(proof, v)
		}
	}
	return proof
}

func (e *Engine) observeQuorum(hash types.Hash, round uint64, proof []types.Vote) {
	if _, ok := e.blocks[hash]; !ok {
		return
	}
	if !e.valid || round > e.validRound {
		e.valid = true
		e.validHash = hash
		e.validRound = round
		e.validProof = append([]types.Vote(nil), proof...)
	}
	if !e.locked || round > e.lockedRound {
		e.locked = true
		e.lockedHash = hash
		e.lockedRound = round
		e.event("lock", "PREVOTE quorum", hash)
	}
}

// progress scans received votes in validator/round order and never relies on
// map iteration to choose between outcomes. Unknown bodies cannot finalize.
func (e *Engine) progress() error {
	// Gather rounds with received evidence, sorted deterministically.
	rounds := e.rounds()
	for _, r := range rounds {
		for _, v := range e.roundChoices(r, types.Prevote) {
			if !v.nilVote && len(e.certificate(r, types.Prevote, v)) >= e.quorum && r <= e.round {
				e.observeQuorum(v.hash, r, e.certificate(r, types.Prevote, v))
			}
		}
		for _, v := range e.roundChoices(r, types.Precommit) {
			if !v.nilVote && r <= e.round && len(e.certificate(r, types.Precommit, v)) >= e.quorum {
				if c, ok := e.blocks[v.hash]; ok {
					return e.finalize(c, r, v.hash, e.certificate(r, types.Precommit, v))
				}
			}
		}
	}
	if e.step == Propose {
		// Waiting for a current-round proposal (or its timeout) before voting.
		return nil
	}
	if e.step == Prevote {
		for _, c := range e.roundChoices(e.round, types.Prevote) {
			if len(e.certificate(e.round, types.Prevote, c)) < e.quorum {
				continue
			}
			if !c.nilVote {
				if _, ok := e.blocks[c.hash]; !ok {
					continue
				}
			}
			e.enter(Precommit)
			if err := e.emit(types.Precommit, c); err != nil {
				return err
			}
			return e.progress()
		}
	}
	return nil
}

func (e *Engine) finalize(c candidate, round uint64, hash types.Hash, proof []types.Vote) error {
	if e.height == math.MaxUint64 {
		return e.reject(fmt.Errorf("height exhausted"))
	}
	f := Finalized{Block: cloneBlock(c.block), OriginRound: c.origin, Round: round, Proof: append([]types.Vote(nil), proof...)}
	e.ledger = append(e.ledger, f)
	e.out.Finalized = append(e.out.Finalized, Finalized{Block: cloneBlock(c.block), OriginRound: c.origin, Round: round, Proof: append([]types.Vote(nil), proof...)})
	e.committed = c.post
	e.parent = hash
	e.event("finalize", "PRECOMMIT quorum", hash)
	e.height++
	e.resetHeight()
	e.enter(Propose)
	return nil
}

// ReceiveFinalized lets a lagging node validate a decision certificate for its
// next height without voting in the decision's round. Every block and unique
// PRECOMMIT is independently checked; a peer's finalized flag is never trusted.
// Missing earlier heights must be delivered sequentially.
func (e *Engine) ReceiveFinalized(f Finalized) error {
	if err := e.ValidateDecisionHeader(f); err != nil {
		return e.reject(err)
	}
	post, err := block.Validate(block.Context{ChainID: e.chain, Height: e.height, ParentHash: e.parent, Proposer: e.Proposer(e.height, f.OriginRound)}, e.committed, f.Block)
	if err != nil {
		return e.reject(err)
	}
	hash, err := crypt.BlockHash(f.Block.Header)
	if err != nil {
		return e.reject(err)
	}
	return e.finalize(candidate{cloneBlock(f.Block), f.OriginRound, post}, f.Round, hash, f.Proof)
}

// ValidateDecisionHeader checks the signed header and complete finality proof
// before a receiver requests its body. It does not finalize or change state.
func (e *Engine) ValidateDecisionHeader(f Finalized) error {
	if f.Block.Header.Height != e.height || f.OriginRound > f.Round {
		return fmt.Errorf("unexpected decision height/round")
	}
	err := block.ValidateHeader(block.Context{ChainID: e.chain, Height: e.height, ParentHash: e.parent, Proposer: e.Proposer(e.height, f.OriginRound)}, f.Block.Header)
	if err != nil {
		return err
	}
	hash, err := crypt.BlockHash(f.Block.Header)
	if err != nil {
		return err
	}
	seen := make(map[types.ValidatorID]bool)
	for _, v := range f.Proof {
		if !e.members[v.Validator] || seen[v.Validator] {
			return fmt.Errorf("unknown or repeated decision validator")
		}
		if v.Height != e.height || v.Round != f.Round || v.Phase != types.Precommit || v.IsNil || v.BlockHash != hash {
			return fmt.Errorf("decision vote scope mismatch")
		}
		if err = crypt.VerifyVote(e.chain, v); err != nil {
			return err
		}
		seen[v.Validator] = true
	}
	if len(seen) < e.quorum {
		return fmt.Errorf("insufficient decision quorum")
	}
	return nil
}
