// swarm_qmdp.go — the universal swarm wrapper shared by every
// non-benchmark strategy (S, T, U, V, W, X). The omniscient benchmark
// R never swarms.
//
// The swarm AMPLIFIES each algorithm rather than replacing it:
//
//   - Each clone (and the leader) runs THAT strategy's own inner
//     planning core, so exploration stays unique per algorithm.
//   - All members SHARE KnownCells / Beliefs (collective perception):
//     each clone senses into, and plans against, the leader's pooled
//     map. Spreading across the maze is emergent — once a region is in
//     the shared map, each algorithm's own explore logic naturally
//     looks elsewhere.
//   - The swarm's graph is PRUNED per group (= per algorithm): leaf-
//     trim + articulation pruning remove dead-ends and useless loops
//     from the planning view, improving every algorithm (and killing
//     the dead-end thrash without overriding the planner).
//   - A clone is PROMOTED to leader when the leader dies (KillAgent).
//   - A clone that THRASHES (oscillates over a couple cells) is
//     terminated and respawned from the leader next tick.
package strategy

import (
	"fmt"

	"maze-of-wumpus/src/world"
)

const (
	// swarmTrailWindow is how many recent clone positions are tracked
	// for thrash detection; swarmThrashDistinctMax is the most distinct
	// cells those positions may cover before the clone is judged to be
	// thrashing (oscillating) and terminated.
	swarmTrailWindow       = 8
	swarmThrashDistinctMax = 2
)

// SwarmStrategy is the per-tick entry point for every swarming letter.
// It reads a.CurrentStrategy to pick the underlying inner planner that
// every member runs against the shared, swarm-pruned view.
func SwarmStrategy(w *world.World, a *world.Agent) world.Pos {
	pruneDeadSwarmClones(a)
	mergeSwarmKnowledge(w, a)

	// Members plan on the swarm's full pooled perception. Each member's
	// own planner applies (and restores) its mild per-agent dead-end
	// prune internally, so sensing after the move persists into the
	// shared map and the frontier keeps advancing. (An earlier design
	// swapped a transient swarm-pruned map in for the whole tick, which
	// both discarded every clone's freshly-sensed cells and — via the
	// aggressive articulation prune — boxed solo/small swarms in near
	// the origin.)
	orig := a.KnownCells

	plan := planFor(a.CurrentStrategy)
	policy := spawnPolicyFor(a.CurrentStrategy)

	// Explore only until the swarm has a route to the goal — a cached
	// entrance→goal path from a prior solve, or the goal perceived this
	// run. Once a solution exists, exploration is redundant: every off-
	// solution branch is, by definition, wasted effort (the maze was
	// already solved by path A, so re-walking path B is inefficient). So
	// stop forking new clones and let the members converge onto the known
	// path instead. Computed from the LEADER's state before any clone-
	// position swap below.
	exploring := !swarmHasSolution(w, a)

	// forks accumulates branch cells to materialize as new clones after
	// every existing member has moved (so a clone forked this tick
	// doesn't also move this tick). Pass `orig` as the full perceived
	// set for unexplored-branch detection.
	var forks []forkReq
	leaderPos := a.Pos
	clones := a.SwarmClones // snapshot: only pre-existing clones move now
	for _, c := range clones {
		if c == nil || !c.Alive {
			continue
		}
		moveOneSwarmMember(w, a, c, plan, policy, orig, leaderPos, &forks, exploring)
	}
	// Leader move + its own adjacent-branch fork decision (a.Pos is the
	// leader's cell).
	a.SwarmPeers = swarmPeerPositions(a, leaderPos, a.Pos)
	seedGoalConvergencePath(w, a)
	taken := plan(w, a)
	a.SwarmPeers = nil
	if exploring {
		collectForks(w, a, orig, taken, policy, &forks)
		// Swarm-level region pass: fill remaining slots toward distinct,
		// uncovered frontier directions (open rooms saturate; corridors,
		// having frontier in only a sector or two, stay split at
		// junctions).
		swarmRegionForks(w, a, orig, leaderPos, &forks)
	}
	applyForks(w, a, forks)
	return taken
}

// swarmHasSolution reports whether the swarm already has a route to the
// goal: a cached entrance→goal path from a prior solve (KnownShortestPath,
// which survives respawn for the life of the map) or the goal perceived
// in the pooled KnownCells this run. Once true, forking exploratory clones
// down off-solution branches is wasted effort — the members should
// converge on the known path instead.
func swarmHasSolution(w *world.World, a *world.Agent) bool {
	if len(a.KnownShortestPath) >= 2 {
		return true
	}
	return a.KnownCells != nil && a.KnownCells[w.Maze.GoalPos]
}

// planFor maps a swarm letter to its full strategy function — the
// PUBLIC wrapper, which applies and then restores that algorithm's
// per-agent dead-end prune around the decision. Using the wrapper
// (rather than the bare inner core) means the prune is scoped to the
// plan call only, so the member's post-move sensing still writes the
// shared, persistent KnownCells. S and T both resolve to the Bayesian
// planner (so they behave near-identically, as intended). R never
// swarms, so it never reaches here.
func planFor(letter rune) world.Strategy {
	switch letter {
	case StrategySwarmBayesian, StrategyBayesian:
		return BayesianStrategy
	case StrategyPOMCP:
		return POMCPStrategy
	case StrategyQMDP:
		return QMDPStrategy
	}
	return QMDPStrategy
}

// pruneDeadSwarmClones removes terminated clones (thrashed or
// otherwise dead) from the leader's roster, freeing slots for future
// lazy forking. Under the lazy model a dead clone is NOT respawned —
// the swarm re-forks at the next decision point if it has the slots.
// (Leader DEATH is handled separately by world.KillAgent's clone-
// promotion path.)
func pruneDeadSwarmClones(a *world.Agent) {
	if len(a.SwarmClones) == 0 {
		return
	}
	live := a.SwarmClones[:0]
	for _, c := range a.SwarmClones {
		if c != nil && c.Alive {
			live = append(live, c)
		}
	}
	a.SwarmClones = live
}

// moveOneSwarmMember plans + commits one step for a clone using its
// strategy's inner core, then runs thrash detection. The leader's
// Pos/Plan/KnownShortestPath are swapped to the clone's for the
// duration so the planner operates from the clone's viewpoint against
// the SHARED (swarm-pruned) beliefs/known map; they're restored after.
func moveOneSwarmMember(w *world.World, a *world.Agent, c *world.SwarmClone, plan world.Strategy, policy spawnPolicy, fullKnown map[world.Pos]bool, leaderPos world.Pos, forks *[]forkReq, exploring bool) {
	origPos, origPlan, origPath := a.Pos, a.Plan, a.KnownShortestPath
	cloneFrom := c.Pos // the clone's pre-move cell (for the decision log)
	a.Pos, a.Plan, a.KnownShortestPath = c.Pos, c.Plan, c.KnownShortestPath

	// Dispersion peers (other members) + convergence path, from this
	// clone's viewpoint.
	a.SwarmPeers = swarmPeerPositions(a, leaderPos, c.Pos)
	seedGoalConvergencePath(w, a)

	target := plan(w, a)

	a.SwarmPeers = nil
	// Persist planner mutations back onto the clone.
	c.Plan, c.KnownShortestPath = a.Plan, a.KnownShortestPath

	// Fork decision from the clone's CURRENT cell (a.Pos == c.Pos here,
	// before the move commits) — only while still exploring; once the
	// swarm has a solution, no new clones are spawned.
	if exploring {
		collectForks(w, a, fullKnown, target, policy, forks)
	}

	moved := false
	if target != c.Pos && w.CanMoveTo(a, target) { // a.Pos == c.Pos here
		c.Pos = target
		moved = true
	}

	// Sense from the clone's new cell so the SHARED KnownCells grows in
	// every member's direction each tick (collective perception).
	a.Pos = c.Pos
	w.MarkAgentSensed(a)
	if moved {
		c.Dist++ // individual travel — judged against TTL on its own
		// Deposit through putScent so parallel runs buffer the write to
		// the group (leader a) instead of racing on the shared grid.
		w.PutScent(a, c.Pos.X, c.Pos.Y)
	}
	if w.DecisionLogEnabled {
		w.LogDecision(fmt.Sprintf("t%d %c~ (%d,%d)->(%d,%d) d%d",
			w.Cycle, a.Label, cloneFrom.X, cloneFrom.Y, c.Pos.X, c.Pos.Y, c.Dist))
	}

	// Per-clone TTL: a clone expires only after IT personally travels
	// past the budget (TTLMultiplier × ttlBudget), independent of the
	// leader and other clones. Terminate + free the slot.
	if limit := swarmTTLLimit(w, a); limit > 0 && c.Dist > limit {
		c.Alive = false
		if w.DecisionLogEnabled {
			w.LogDecision(fmt.Sprintf("t%d %c~ (%d,%d) X ttl", w.Cycle, a.Label, c.Pos.X, c.Pos.Y))
		}
	}

	// Thrash detection: record the new position and terminate the
	// clone if it's been oscillating over a tiny cell set.
	recordCloneTrail(c)
	if c.Alive && cloneIsThrashing(c) {
		c.Alive = false
		if w.DecisionLogEnabled {
			w.LogDecision(fmt.Sprintf("t%d %c~ (%d,%d) X thrash", w.Cycle, a.Label, c.Pos.X, c.Pos.Y))
		}
	}

	a.Pos, a.Plan, a.KnownShortestPath = origPos, origPlan, origPath
}

// swarmTTLLimit returns the per-entity TTL travel budget
// (TTLMultiplier × ttlBudget) for this swarm, matching the leader's
// own TTL rule. Returns 0 (no limit) when TTL is disabled or the
// budget is unknown. Clones inherit the leader's entrance→goal optimal
// since they share its entrance.
func swarmTTLLimit(w *world.World, a *world.Agent) int {
	if w.TTLDisabled {
		return 0
	}
	budget := a.OptimalDistance
	if budget <= 0 {
		budget = w.Stats.OptimalDistance
	}
	if budget <= 0 {
		return 0
	}
	return world.TTLMultiplier * budget
}

// recordCloneTrail appends the clone's current position to its recency
// ring, capped at swarmTrailWindow.
func recordCloneTrail(c *world.SwarmClone) {
	c.Trail = append(c.Trail, c.Pos)
	if len(c.Trail) > swarmTrailWindow {
		c.Trail = c.Trail[len(c.Trail)-swarmTrailWindow:]
	}
}

// cloneIsThrashing reports whether the clone's recent trail covers at
// most swarmThrashDistinctMax distinct cells over a full window —
// i.e., it's stuck oscillating rather than making progress.
func cloneIsThrashing(c *world.SwarmClone) bool {
	if len(c.Trail) < swarmTrailWindow {
		return false
	}
	distinct := map[world.Pos]bool{}
	for _, p := range c.Trail {
		distinct[p] = true
	}
	return len(distinct) <= swarmThrashDistinctMax
}
