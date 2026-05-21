// bayesian.go — agent A: a faithful Wumpus-World agent. Inductive
// + Bayesian reasoning over heat / stench observations at visited
// cells. Three-stage decision pipeline:
//
//  1. STRICT: plan a BFS to goal through cells proven safe by KB.
//  2. FRONTIER: walk to the nearest safe-but-unvisited cell to gather
//     more observations.
//  3. RISK: relax to "not provably hazardous" and re-plan to goal.
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

// wwCellOK reports whether `p` is strictly safe to enter under the
// agent's KB. Safe iff perceived (in `a.KnownCells`), walkable, no
// known pit, no current stench, AND either visited OR inductively
// proven pit-free.
func wwCellOK(w *world.World, a *world.Agent, p world.Pos) bool {
	if !knownWalkable(w, a, p) {
		return false
	}
	if a.Beliefs == nil {
		return false
	}
	if a.Beliefs.PitProb[p] >= 0.5 {
		return false
	}
	if a.Beliefs.WumpusProb[p] > 0 {
		return false
	}
	if a.Beliefs.Observed[p] {
		return true
	}
	return a.Beliefs.SafeFromPit[p]
}

// wwCellOKLoose: relaxed "calculated risk" predicate. Still gated on
// `a.KnownCells` — the agent never routes through unseen cells.
func wwCellOKLoose(w *world.World, a *world.Agent, p world.Pos) bool {
	if !knownWalkable(w, a, p) {
		return false
	}
	if a.Beliefs == nil {
		return true
	}
	if a.Beliefs.PitProb[p] >= 1.0 {
		return false
	}
	if a.Beliefs.WumpusProb[p] > 0 {
		return false
	}
	return true
}

// wwPlanPath runs the strict-PO decision pipeline. Crucially the
// agent NEVER reads w.Maze.GoalPos until it has perceived the goal
// cell (added to KnownCells via MarkAgentSensed). Before then it
// expands purely via frontier exploration.
//
//  0. WATER (if NeedsWater): try a strict-safe BFS to the nearest
//     water pit. Water is "free life insurance" — grab it whenever
//     a proven-safe path exists.
//  1. GOAL (only if KnownCells already contains the goal cell —
//     i.e. some agent past life sensed it OR this life perceived
//     it via the sensing radius): strict-safe BFS to goal.
//  2. FRONTIER: walk to the nearest safe-but-unvisited cell.
//  3. RISK FALLBACK: if goal is perceived, try a relaxed path to
//     it. Otherwise loose-predicate frontier expansion.
func wwPlanPath(w *world.World, a *world.Agent) []world.Pos {
	if NeedsKnownWater(w, a) {
		if pit, ok := NearestKnownWaterPit(w, a, a.Pos); ok {
			if p := wwBFS(w, a, a.Pos, pit, true); len(p) > 0 {
				return p
			}
		}
	}
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
				return np, true
			}
			visited[np] = true
			queue = append(queue, np)
		}
	}
	return world.Pos{}, false
}

// UpdateAgentBeliefs runs inductive + Bayesian updates from heat /
// stench at the agent's current cell.
func UpdateAgentBeliefs(w *world.World, a *world.Agent) {
	if a.Beliefs == nil {
		return
	}
	b := a.Beliefs
	p := a.Pos
	b.Observed[p] = true
	b.SafeFromPit[p] = true
	delete(b.PitProb, p)
	delete(b.WumpusProb, p)

	if !w.HeatAt(p.X, p.Y) {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				np := world.Pos{X: p.X + dx, Y: p.Y + dy}
				if !world.InBounds(np.X, np.Y) {
					continue
				}
				b.SafeFromPit[np] = true
				delete(b.PitProb, np)
			}
		}
	} else {
		var candidates []world.Pos
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				np := world.Pos{X: p.X + dx, Y: p.Y + dy}
				if !world.InBounds(np.X, np.Y) {
					continue
				}
				if b.SafeFromPit[np] {
					continue
				}
				candidates = append(candidates, np)
			}
		}
		if len(candidates) == 1 {
			b.PitProb[candidates[0]] = 1.0
		} else if len(candidates) > 1 {
			share := 1.0 / float64(len(candidates))
			for _, np := range candidates {
				cur := b.PitProb[np]
				b.PitProb[np] = cur + (1-cur)*share
			}
		}
	}

	b.WumpusProb = map[world.Pos]float64{}
	if w.StenchAt(p.X, p.Y) {
		var candidates []world.Pos
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				np := world.Pos{X: p.X + dx, Y: p.Y + dy}
				if !world.InBounds(np.X, np.Y) {
					continue
				}
				candidates = append(candidates, np)
			}
		}
		if len(candidates) > 0 {
			share := 1.0 / float64(len(candidates))
			for _, np := range candidates {
				b.WumpusProb[np] = share
			}
		}
	}
}
