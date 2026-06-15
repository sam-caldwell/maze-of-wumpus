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

// frontierCandidateCap bounds how many of the nearest perception-
// boundary cells the frontier search collects before scoring. The BFS
// yields them nearest-first, so this is a local choice set: enough to
// steer toward the believed goal (and away from swarm-mates) without
// sweeping the whole known map every tick.
const frontierCandidateCap = 24

// wwNearestSafeFrontier walks the safe-set BFS from the agent's
// current cell, collects the nearest cells on the *perception
// boundary* — perceived (in a.KnownCells) walkable cells with at least
// one neighbor the agent has NOT perceived — and returns the BEST one
// to head for. Walking to a boundary cell expands the agent's
// KnownCells past the current sight horizon.
//
// "Best" is the cell that maximizes expected progress under the
// goal-location belief (world/goal_belief.go): among the local choice
// set, prefer the boundary cell nearest the expected goal location, so
// exploration is pulled toward the region the goal probably occupies
// rather than the merely-closest unknown. When the agent has swarm
// peers and the goal isn't perceived yet, a secondary dispersion term
// nudges members apart so the team fans out across that region instead
// of piling onto one frontier.
//
// Under sight=10 the boundary set is much sparser than "any safe
// unvisited cell": interior perceived cells are skipped because no new
// perception is gained from stepping onto them. Combined with the
// per-agent prune (RecomputeAgentPrunedViewIfStale) this stops the
// agent from threading through already-perceived dead-ends just because
// it hasn't physically stood on them.
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
	var candidates []world.Pos
	for len(queue) > 0 && len(candidates) < frontierCandidateCap {
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
			visited[np] = true
			if isPerceptionBoundary(np) {
				candidates = append(candidates, np)
				continue
			}
			queue = append(queue, np)
		}
	}
	if len(candidates) == 0 {
		return world.Pos{}, false
	}
	return bestFrontierForGoalBelief(w, a, candidates), true
}

// goalPullWeight / dispersionWeight balance the two competing pulls on a
// swarm member's frontier choice. They act on MIN-MAX NORMALIZED terms
// (see below), so equal weights really do mean equal influence — members
// steer toward the goal region AND fan out across distinct frontiers.
const (
	goalPullWeight   = 1.0
	dispersionWeight = 1.0
)

// bestFrontierForGoalBelief scores frontier candidates and returns the
// one maximizing expected progress: a goal-pull term (proximity to the
// believed goal location) plus, for swarm members still hunting the
// goal, a dispersion term (distance from swarm-mates).
//
// Both terms are min-max normalized to [0,1] ACROSS THE CANDIDATE SET
// before they're combined. This matters: the raw goal distance is to a
// far centroid (hundreds of cells) while peers sit right next to the
// member (tens), so combining the raw values lets goal-pull swamp
// dispersion and the whole swarm collapses onto a single path. Normalized,
// the two compete on equal footing — the member heads toward the goal
// region but picks a frontier the others aren't already taking.
//
// When the goal belief is exhausted (essentially everything observed —
// the goal is surely perceived by now) the goal-pull term drops out and
// behavior reduces to the prior nearest/farthest-from-peers selection.
func bestFrontierForGoalBelief(w *world.World, a *world.Agent, candidates []world.Pos) world.Pos {
	expectedGoal, haveGoalBelief := w.ExpectedGoalLocation(a)
	goalPerceived := a.KnownCells != nil && a.KnownCells[w.Maze.GoalPos]
	disperse := len(a.SwarmPeers) > 0 && !goalPerceived

	if !haveGoalBelief {
		if disperse {
			return farthestFromPeers(a, candidates)
		}
		return candidates[0] // nearest
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	// First pass: raw goal distance (want small) and summed peer distance
	// (want large) per candidate, plus the min/max of each for normalizing.
	goalDist := make([]int, len(candidates))
	peerDist := make([]int, len(candidates))
	var gMin, gMax, pMin, pMax int
	for i, c := range candidates {
		gd := chebDist(c, expectedGoal)
		pd := 0
		if disperse {
			for _, p := range a.SwarmPeers {
				pd += chebDist(c, p)
			}
		}
		goalDist[i], peerDist[i] = gd, pd
		if i == 0 || gd < gMin {
			gMin = gd
		}
		if i == 0 || gd > gMax {
			gMax = gd
		}
		if i == 0 || pd < pMin {
			pMin = pd
		}
		if i == 0 || pd > pMax {
			pMax = pd
		}
	}

	norm := func(v, lo, hi int) float64 {
		if hi == lo {
			return 0
		}
		return float64(v-lo) / float64(hi-lo)
	}
	best := candidates[0]
	bestScore := 0.0
	for i, c := range candidates {
		// goalScore: 1 == closest to the believed goal.
		score := goalPullWeight * (1 - norm(goalDist[i], gMin, gMax))
		if disperse {
			// dispScore: 1 == farthest from swarm-mates.
			score += dispersionWeight * norm(peerDist[i], pMin, pMax)
		}
		if i == 0 || score > bestScore {
			bestScore = score
			best = c
		}
	}
	return best
}

// chebDist is the Chebyshev (king-move) distance between two cells,
// matching the engine's 8-connected Moore movement model.
func chebDist(a, b world.Pos) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// UpdateAgentBeliefs marks the agent's current cell (and its
// perceived Moore neighbors) as Observed. With hazards removed there
// is no longer any pit/wumpus inference to perform, but the Observed
// set still drives the goal-location belief: every newly-perceived
// cell is a cell the goal is NOT in, so MarkObserved subtracts its
// prior mass from the posterior (see world/goal_belief.go).
func UpdateAgentBeliefs(w *world.World, a *world.Agent) {
	if a.Beliefs == nil {
		return
	}
	b := a.Beliefs
	p := a.Pos
	b.MarkObserved(w.Maze, p)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			np := world.Pos{X: p.X + dx, Y: p.Y + dy}
			if !world.InBounds(np.X, np.Y) {
				continue
			}
			b.MarkObserved(w.Maze, np)
		}
	}
}
