// bayesian.go — agent A: a strict partially-observable navigator.
// With hazards removed, every perceived walkable cell is enterable;
// the agent still respects partial observability — it only routes
// through cells it has perceived (a.KnownCells) and only heads for
// the goal once it has actually sensed the goal cell. Two-stage
// decision pipeline:
//
//  1. GOAL: once the goal is perceived, plan a path to it.
//  2. FRONTIER: otherwise walk to the nearest perception-boundary
//     cell to expand the perceived map.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// BayesianStrategy is the entry-point for agent T (solo PO Bayesian).
// Applies a per-agent graph prune to a.KnownCells (so interior
// dead-ends and unreachable loops drop out of the planning view),
// then runs the shared planning core. SwarmBayesianStrategy reuses
// the core directly — the swarm has its own pruning pass.
func BayesianStrategy(w *world.World, a *world.Agent) world.Pos {
	restore := applyAgentPrunedView(w, a)
	defer restore()
	return bayesianStrategyPlan(w, a)
}

// applyAgentPrunedView swaps a.KnownCells for the lazy-cached pruned
// view (rebuilt when stale via World.RecomputeAgentPrunedViewIfStale)
// and returns a restore closure. a.Pos is always kept in the pruned
// view so the planner can plan from the agent's current cell even
// if pruning would otherwise have dropped it.
func applyAgentPrunedView(w *world.World, a *world.Agent) func() {
	w.RecomputeAgentPrunedViewIfStale(a)
	if a.PrunedKnownCells == nil {
		return func() {}
	}
	orig := a.KnownCells
	view := make(map[world.Pos]bool, len(a.PrunedKnownCells)+1)
	for p := range a.PrunedKnownCells {
		view[p] = true
	}
	view[a.Pos] = true
	a.KnownCells = view
	return func() { a.KnownCells = orig }
}

// bayesianStrategyPlan is the inner planning core. Assumes
// a.KnownCells has already been set to whatever view the caller
// wants the planner to see (raw, solo-pruned, or swarm-pruned).
func bayesianStrategyPlan(w *world.World, a *world.Agent) world.Pos {
	if step, ok := w.CachedStepFor(a); ok {
		return step
	}
	UpdateAgentBeliefs(w, a)
	if len(a.Plan) == 0 || !wwCellOK(w, a, a.Plan[0]) {
		a.Plan = wwPlanPath(w, a)
	}
	if len(a.Plan) == 0 {
		return a.Pos
	}
	next := a.Plan[0]
	a.Plan = a.Plan[1:]
	return next
}

// wwCellOK reports whether `p` is enterable under the agent's KB.
// With hazards removed, a perceived walkable cell is always OK.
func wwCellOK(w *world.World, a *world.Agent, p world.Pos) bool {
	return knownWalkable(w, a, p)
}

// wwCellOKLoose: same predicate as wwCellOK now that hazards are gone.
// Still gated on `a.KnownCells` — the agent never routes through
// unseen cells.
func wwCellOKLoose(w *world.World, a *world.Agent, p world.Pos) bool {
	return knownWalkable(w, a, p)
}

// wwPlanPath runs the strict-PO decision pipeline. Crucially the
// agent NEVER reads w.Maze.GoalPos until it has perceived the goal
// cell (added to KnownCells via MarkAgentSensed). Before then it
// expands purely via frontier exploration.
//
//  1. GOAL (only if KnownCells already contains the goal cell —
//     i.e. some agent past life sensed it OR this life perceived
//     it via the sensing radius): BFS to goal.
//  2. FRONTIER: walk to the nearest perceived-but-boundary cell.
func wwPlanPath(w *world.World, a *world.Agent) []world.Pos {
	goalPerceived := a.KnownCells != nil && a.KnownCells[w.Maze.GoalPos]
	if goalPerceived {
		if p := wwBFS(w, a, a.Pos, w.Maze.GoalPos, true); len(p) > 0 {
			return p
		}
	}
	if frontier, ok := wwNearestSafeFrontier(w, a); ok {
		if p := wwBFS(w, a, a.Pos, frontier, true); len(p) > 0 {
			return p
		}
	}
	if goalPerceived {
		return wwBFS(w, a, a.Pos, w.Maze.GoalPos, false)
	}
	if frontier, ok := wwNearestSafeFrontier(w, a); ok {
		return wwBFS(w, a, a.Pos, frontier, false)
	}
	return nil
}

// wwBFS finds a min-cost path through cells permitted by the
// chosen Wumpus-World safety predicate. Strict mode uses wwCellOK
// (proven-safe only); loose mode uses wwCellOKLoose (no proven
// pit). The destination cell is always considered legal so plans
// can end at the goal even if it's unknown to the KB.
//
// Despite the historical "BFS" name, this is a Dijkstra call —
// edges are weighted (cardinal=10, diagonal=14) and corner-clipping
// is enforced, matching the global movement model.
func wwBFS(w *world.World, a *world.Agent, from, to world.Pos, strict bool) []world.Pos {
	if from == to {
		return nil
	}
	okFn := wwCellOKLoose
	if strict {
		okFn = wwCellOK
	}
	return w.DijkstraPath(from, to, func(p world.Pos) bool {
		if !knownWalkable(w, a, p) {
			return false
		}
		if p == to {
			return true
		}
		return okFn(w, a, p)
	})
}

// wwNearestSafeFrontier walks the safe-set BFS from the agent's
// current cell and returns the nearest safe cell on the *perception
// boundary* — a perceived (in a.KnownCells) walkable cell that has
// at least one neighbor the agent has NOT perceived. Walking there
// expands the agent's KnownCells past the current sight horizon.
//
// Under sight=10 this is much sparser than "any safe unvisited cell"
// (the previous semantics): interior perceived cells are skipped
// because no new perception is gained from stepping onto them.
// Combined with the per-agent prune (RecomputeAgentPrunedViewIfStale)
// this stops the agent from threading through already-perceived
// dead-ends just because it hasn't physically stood on them.
func wwNearestSafeFrontier(w *world.World, a *world.Agent) (world.Pos, bool) {
	if a.Beliefs == nil {
		return world.Pos{}, false
	}
	isPerceptionBoundary := func(p world.Pos) bool {
		for _, d := range world.Cardinals {
			np := world.Pos{X: p.X + d.X, Y: p.Y + d.Y}
			if !world.InBounds(np.X, np.Y) {
				continue
			}
			if !a.KnownCells[np] {
				return true
			}
		}
		return false
	}
	queue := []world.Pos{a.Pos}
	visited := map[world.Pos]bool{a.Pos: true}
	// Swarm dispersion: when this agent has swarm peers (and the goal
	// isn't perceived yet), collect the nearest handful of safe
	// frontier cells and head for the one FARTHEST from swarm-mates,
	// so members fan out. Solo agents keep the original behavior:
	// return the very first (nearest) safe frontier.
	disperse := len(a.SwarmPeers) > 0 && !(a.KnownCells != nil && a.KnownCells[w.Maze.GoalPos])
	var candidates []world.Pos
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range world.Cardinals {
			np := world.Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if !knownWalkable(w, a, np) {
				continue
			}
			if visited[np] {
				continue
			}
			if !wwCellOK(w, a, np) {
				continue
			}
			if isPerceptionBoundary(np) {
				if !disperse {
					return np, true
				}
				candidates = append(candidates, np)
				visited[np] = true
				if len(candidates) >= 8 {
					return farthestFromPeers(a, candidates), true
				}
				continue
			}
			visited[np] = true
			queue = append(queue, np)
		}
	}
	if len(candidates) > 0 {
		return farthestFromPeers(a, candidates), true
	}
	return world.Pos{}, false
}

// UpdateAgentBeliefs marks the agent's current cell (and its
// perceived Moore neighbors) as Observed. With hazards removed there
// is no longer any pit/wumpus inference to perform; the Observed
// bookkeeping is retained so callers that gate on it still behave.
func UpdateAgentBeliefs(w *world.World, a *world.Agent) {
	if a.Beliefs == nil {
		return
	}
	b := a.Beliefs
	p := a.Pos
	b.Observed[p] = true
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			np := world.Pos{X: p.X + dx, Y: p.Y + dy}
			if !world.InBounds(np.X, np.Y) {
				continue
			}
			b.Observed[np] = true
		}
	}
}
