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
//     become wall-equivalent for the planner).
//  4. Run the shared Bayesian planning core directly (bypassing
//     BayesianStrategy's per-agent prune wrapper — the swarm
//     pruning replaces it).
func SwarmBayesianStrategy(w *world.World, a *world.Agent) world.Pos {
	mergeSwarmKnowledge(w, a)
	w.RecomputeSwarmGraphIfStale(a.SwarmGroupID)
	orig := a.KnownCells
	pruned := make(map[world.Pos]bool, len(orig))
	for p := range orig {
		if w.SwarmAliveCell(a.SwarmGroupID, p) {
			pruned[p] = true
		}
	}
	// Always keep a.Pos AND every clone's Pos perceivable so the
	// planner can plan FROM any swarm member's current cell even if
	// pruning otherwise dropped it.
	pruned[a.Pos] = true
	for _, c := range a.SwarmClones {
		if c != nil && c.Alive {
			pruned[c.Pos] = true
		}
	}
	a.KnownCells = pruned
	defer func() { a.KnownCells = orig }()
	// Move every alive clone using the SAME Bayesian planning core
	// the leader uses, against the SAME shared (now pruned) view.
	// Each clone is its own goal-seeker: parallel Bayesian agents
	// pooling state, not 10 dumb followers behind one planner.
	moveSwarmClones(w, a)
	return bayesianStrategyPlan(w, a)
}

// moveSwarmClones advances every alive clone of the swarm by one
// Bayesian-planned step against the shared (pruned) KnownCells. The
// leader's Pos/Plan/KnownShortestPath temporarily take over the
// clone's slots while bayesianStrategyPlan runs, so the existing
// planner machinery operates from the clone's viewpoint. The leader
// state is restored before each clone — and again at function exit
// — so the leader's own planning call (which happens after this
// returns) sees its real Pos.
func moveSwarmClones(w *world.World, a *world.Agent) {
	for _, c := range a.SwarmClones {
		if c == nil || !c.Alive {
			continue
		}
		moveOneSwarmClone(w, a, c)
	}
}

// moveOneSwarmClone plans + commits a Bayesian step for one clone.
func moveOneSwarmClone(w *world.World, a *world.Agent, c *world.SwarmClone) {
	origPos := a.Pos
	origPlan := a.Plan
	origPath := a.KnownShortestPath

	a.Pos = c.Pos
	a.Plan = c.Plan
	a.KnownShortestPath = c.KnownShortestPath

	target := bayesianStrategyPlan(w, a)

	// Save the planner's mutations back to the clone.
	c.Plan = a.Plan
	c.KnownShortestPath = a.KnownShortestPath

	// Validate the move against the same predicates an agent uses.
	// a.Pos is still c.Pos here so CanMoveTo / WumpusAt checks are
	// relative to the clone's position.
	moved := false
	if target != a.Pos && w.CanMoveTo(a, target) {
		blocked := false
		if !w.WumpusDisabled {
			if wm := w.WumpusAt[target.Y][target.X]; wm != nil && wm.Alive {
				blocked = true
			}
		}
		if !blocked {
			c.Pos = target
			moved = true
		}
	}

	// Sense from the clone's NEW position so the swarm's shared
	// KnownCells grows in 11 directions every tick.
	a.Pos = c.Pos
	w.MarkAgentSensed(a)
	if moved {
		// Drop a scent breadcrumb so scent-aware peers can read the
		// swarm trail (uses leader's label — the swarm acts as one).
		w.ScentOwner[c.Pos.Y][c.Pos.X] = a.Label
		w.ScentCycle[c.Pos.Y][c.Pos.X] = w.Cycle
	}

	// Restore.
	a.Pos = origPos
	a.Plan = origPlan
	a.KnownShortestPath = origPath
}

// mergeSwarmKnowledge syncs `a` with every other alive agent in the
// SAME swarm group (matching SwarmGroupID). Distinct swarms are
// walled off — an agent on swarm A never reads cells from swarm B's
// members. KnownCells is unioned; AgentBeliefs merges Observed and
// SafeFromPit as unions and PitProb/WumpusProb as element-wise max
// (cautious-bias preserves safety guarantees).
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
		if peer == a || !peer.Alive || peer.CurrentStrategy != StrategySwarmBayesian {
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
