// swarm_bayesian.go — strategy 'S': partial-observability Bayesian
// reasoning that SHARES knowledge state across every agent currently
// using strategy S. Effectively a "hive mind" variant of T: agents
// pool their KnownCells and AgentBeliefs into a swarm view, so any
// one agent benefits from what all the others have observed.
//
// Mechanics each tick:
//
//  1. Merge a.KnownCells INTO the world's swarm-known set, then
//     pull the swarm view back into a.KnownCells. After this both
//     directions of the merge are complete and a.KnownCells reflects
//     the union of all S-strategy agents' perceptions so far.
//
//  2. Merge a.Beliefs INTO the swarm beliefs the same way (Observed
//     and SafeFromPit are unions; PitProb / WumpusProb take the
//     MAXIMUM — the most cautious estimate wins).
//
//  3. Run the regular BayesianStrategy decision pipeline on top of
//     the now-shared knowledge.
//
// Strict-PO is preserved: every cell in the swarm set was observed
// by some agent on this team. Swarm members do not learn cells that
// nobody saw. This is collective intelligence, not omniscience.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// SwarmBayesianStrategy: 'S' (replaces the omniscient DFS that
// previously held this letter).
//
// Decision pipeline:
//
//  1. Merge KnownCells + Beliefs across alive S-peers (hive sync).
//  2. Recompute the swarm's pruned graph (leaf trim + articulation
//     pruning) if the union has grown since last tick.
//  3. Restrict the agent's effective KnownCells to the pruned set
//     for the duration of this call (dead-ends and useless loops
//     become wall-equivalent for the BayesianStrategy planner).
//  4. Run BayesianStrategy on the pruned view; restore the
//     agent's full KnownCells on return.
func SwarmBayesianStrategy(w *world.World, a *world.Agent) world.Pos {
	mergeSwarmKnowledge(w, a)
	w.RecomputeSwarmGraphIfStale()
	// Build the pruned view of a.KnownCells — anything in the
	// swarm dead set drops out. Swap in for this call only.
	orig := a.KnownCells
	pruned := make(map[world.Pos]bool, len(orig))
	for p := range orig {
		if w.SwarmAliveCell(p) {
			pruned[p] = true
		}
	}
	// Always keep a.Pos perceivable so the planner can plan FROM
	// the agent's current cell even if pruning otherwise dropped it.
	pruned[a.Pos] = true
	a.KnownCells = pruned
	defer func() { a.KnownCells = orig }()
	return BayesianStrategy(w, a)
}

// mergeSwarmKnowledge syncs `a` with every other alive agent
// currently using strategy S. KnownCells is unioned; AgentBeliefs
// merges Observed/SafeFromPit as unions and PitProb/WumpusProb as
// element-wise max (cautious-bias preserves safety guarantees).
func mergeSwarmKnowledge(w *world.World, a *world.Agent) {
	if a.KnownCells == nil {
		a.KnownCells = map[world.Pos]bool{}
	}
	if a.Beliefs == nil {
		a.Beliefs = world.NewAgentBeliefs()
	}
	for _, peer := range w.Agents {
		if peer == a || !peer.Alive || peer.CurrentStrategy != StrategySwarmBayesian {
			continue
		}
		for p := range peer.KnownCells {
			a.KnownCells[p] = true
		}
		if peer.Beliefs == nil {
			continue
		}
		for p := range peer.Beliefs.Observed {
			a.Beliefs.Observed[p] = true
		}
		for p, v := range peer.Beliefs.SafeFromPit {
			if v {
				a.Beliefs.SafeFromPit[p] = true
			}
		}
		for p, v := range peer.Beliefs.PitProb {
			if v > a.Beliefs.PitProb[p] {
				a.Beliefs.PitProb[p] = v
			}
		}
		for p, v := range peer.Beliefs.WumpusProb {
			if v > a.Beliefs.WumpusProb[p] {
				a.Beliefs.WumpusProb[p] = v
			}
		}
	}
}
