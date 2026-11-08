package realnet

import (
	"fmt"
	"math/rand"

	pb "blockchain-lab01/src/proto"
	"google.golang.org/protobuf/proto"
)

type Policy struct {
	Seed                   int64
	MinDelayMS, MaxDelayMS uint64
	DropBPS, DuplicateBPS  int
	ReorderEvery           int
	ReorderHoldMS          uint64
	ReplayEvery            int
	ReplayAgeMS            uint64
	DisableAfterMS         uint64 // Zero means faults remain enabled for the whole run.
}

func (p Policy) Validate() error {
	if p.MinDelayMS > p.MaxDelayMS || p.MaxDelayMS > 60000 || p.DropBPS < 0 || p.DropBPS > 10000 || p.DuplicateBPS < 0 || p.DuplicateBPS > 10000 || p.ReorderEvery < 0 || p.ReplayEvery < 0 || p.ReorderHoldMS > 60000 || p.ReplayAgeMS > 60000 {
		return fmt.Errorf("invalid fault policy")
	}
	return nil
}

type delivery struct {
	after  uint64
	frame  *pb.Envelope
	reason string
}
type historyEntry struct {
	at    uint64
	frame *pb.Envelope
}
type faultLayer struct {
	policy  Policy
	rng     *rand.Rand
	count   int
	history []historyEntry
}

func newFaultLayer(p Policy) *faultLayer {
	return &faultLayer{policy: p, rng: rand.New(rand.NewSource(p.Seed))}
}

// plan preserves signed bytes: delay/reorder affect scheduling; duplicate/replay
// transmit copies of the original envelope, never re-signing old votes.
func (f *faultLayer) plan(now uint64, e *pb.Envelope) ([]delivery, bool) {
	f.count++
	p := f.policy
	enabled := p.DisableAfterMS == 0 || now < p.DisableAfterMS
	var out []delivery
	dropped := false
	if enabled && f.rng.Intn(10000) < p.DropBPS {
		dropped = true
	} else {
		delay := uint64(0)
		reason := "normal"
		if enabled {
			delay = p.MinDelayMS + f.rng.Uint64()%(p.MaxDelayMS-p.MinDelayMS+1)
			if p.ReorderEvery > 0 && f.count%p.ReorderEvery == 0 {
				delay += p.ReorderHoldMS
				reason = "reorder"
			}
		}
		out = append(out, delivery{delay, proto.Clone(e).(*pb.Envelope), reason})
		if enabled && f.rng.Intn(10000) < p.DuplicateBPS {
			out = append(out, delivery{delay + 1, proto.Clone(e).(*pb.Envelope), "duplicate"})
		}
	}
	if enabled && p.ReplayEvery > 0 && f.count%p.ReplayEvery == 0 {
		for _, h := range f.history {
			if h.frame.Kind == "vote" && h.frame.Recipient == e.Recipient && now-h.at >= p.ReplayAgeMS {
				out = append(out, delivery{0, proto.Clone(h.frame).(*pb.Envelope), "replay"})
				break
			}
		}
	}
	f.history = append(f.history, historyEntry{now, proto.Clone(e).(*pb.Envelope)})
	if len(f.history) > 256 {
		f.history = f.history[1:]
	}
	return out, dropped
}
