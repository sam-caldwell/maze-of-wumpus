// swarm_bayesian.go — shared-knowledge merge for the swarm wrapper.
// Every swarm member (any non-R letter) pools its KnownCells and
// AgentBeliefs with the other alive members of the SAME swarm group,
// so the collective perceives more than any one member could alone.
// The movement / branch-spreading lives in the universal swarm
// wrapper (SwarmStrategy, swarm_qmdp.go); this file is just hive sync.
//
// Strict-PO is preserved: every cell in the swarm set was observed by
// some member on this team — collective intelligence, not omniscience.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// mergeSwarmKnowledge syncs `a` with every other alive agent in the
// SAME swarm group (matching SwarmGroupID). Distinct swarms are
// walled off — an agent on swarm A never reads cells from swarm B's
// members. KnownCells is unioned; AgentBeliefs merges Observed as a
// union.
func mergeSwarmKnowledge(w *world.World, a *world.Agent) {
	if a.KnownCells == nil {
		a.KnownCells = map[world.Pos]bool{}
	}
	if a.Beliefs == nil {
		a.Beliefs = world.NewAgentBeliefs()
	}
	if a.SwarmGroupID == 0 {
		return // not in a swarm
	}
	for _, peer := range w.Agents {
		// Pool only with same-strategy swarm peers in the same group:
		// two distinct swarms (or different letters) never share state.
		// Same SwarmGroupID already implies same letter, but gating on
		// both is explicit and cheap.
		if peer == a || !peer.Alive || peer.CurrentStrategy != a.CurrentStrategy {
			continue
		}
		if peer.SwarmGroupID != a.SwarmGroupID {
			continue
		}
		for p := range peer.KnownCells {
			a.KnownCells[p] = true
		}
		if peer.Beliefs == nil {
			continue
		}
		for p := range peer.Beliefs.Observed {
			a.Beliefs.MarkObserved(w.Maze, p)
		}
	}
}
