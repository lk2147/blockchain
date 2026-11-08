package consensus

import (
	"blockchain-lab01/src/types"
	"sort"
)

func (e *Engine) rounds() []uint64 {
	seen := make(map[uint64]bool)
	for k := range e.votes {
		seen[k.round] = true
	}
	result := make([]uint64, 0, len(seen))
	for r := range seen {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func (e *Engine) roundChoices(round uint64, phase types.VotePhase) []choice {
	seen := make(map[choice]bool)
	var result []choice
	for _, id := range e.validators {
		if v, ok := e.votes[voteKey{round, phase, id}]; ok {
			c := choice{v.IsNil, v.BlockHash}
			if !seen[c] {
				seen[c] = true
				result = append(result, c)
			}
		}
	}
	return result
}
