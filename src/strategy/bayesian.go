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

// BayesianStrategy is the entry-point for agent A.
func BayesianStrategy(w *world.World, a *world.Agent) world.Pos {
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
// agent's KB. Safe iff walkable, no known pit, no current stench,
// AND either visited OR inductively proven pit-free.
func wwCellOK(w *world.World, a *world.Agent, p world.Pos) bool {
	if !world.InBounds(p.X, p.Y) || !w.Maze.IsWalkable(p) {
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

// wwCellOKLoose: relaxed "calculated risk" predicate.
func wwCellOKLoose(w *world.World, a *world.Agent, p world.Pos) bool {
	if !world.InBounds(p.X, p.Y) || !w.Maze.IsWalkable(p) {
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

// wwPlanPath runs the four-stage decision pipeline.
//
//  0. WATER (if NeedsWater): try a strict-safe BFS to the nearest
//     water pit. Water is "free life insurance" — grab it whenever
//     a proven-safe path exists. Falls through to (1) if no such
//     path exists; the agent doesn't abandon the goal long-term.
//  1. STRICT goal: BFS to goal through proven-safe cells only.
//  2. FRONTIER: walk to the nearest safe-but-unvisited cell.
//  3. RISK: relaxed predicate, plan toward goal anyway.
func wwPlanPath(w *world.World, a *world.Agent) []world.Pos {
	goal := w.Maze.GoalPos
	if NeedsWater(w, a) {
		if pit, ok := NearestWaterPit(w, a.Pos); ok {
			if p := wwBFS(w, a, a.Pos, pit, true); len(p) > 0 {
				return p
			}
		}
	}
	if p := wwBFS(w, a, a.Pos, goal, true); len(p) > 0 {
		return p
	}
	if frontier, ok := wwNearestSafeFrontier(w, a); ok {
		if p := wwBFS(w, a, a.Pos, frontier, true); len(p) > 0 {
			return p
		}
	}
	return wwBFS(w, a, a.Pos, goal, false)
}

// wwBFS: BFS through cells permitted by the chosen predicate. The
// destination cell is always considered legal so plans can end at
// the goal even if it's unknown to the KB.
func wwBFS(w *world.World, a *world.Agent, from, to world.Pos, strict bool) []world.Pos {
	if from == to {
		return nil
	}
	okFn := wwCellOKLoose
	if strict {
		okFn = wwCellOK
	}
	type node struct {
		world.Pos
		parent int
	}
	nodes := []node{{from, -1}}
	visited := map[world.Pos]int{from: 0}
	for head := 0; head < len(nodes); head++ {
		cur := nodes[head]
		if cur.Pos == to {
			var path []world.Pos
			for i := head; i != -1; i = nodes[i].parent {
				path = append([]world.Pos{nodes[i].Pos}, path...)
			}
			if len(path) > 0 && path[0] == from {
				path = path[1:]
			}
			return path
		}
		for _, d := range world.Cardinals {
			np := world.Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if _, seen := visited[np]; seen {
				continue
			}
			if np != to && !okFn(w, a, np) {
				continue
			}
			visited[np] = len(nodes)
			nodes = append(nodes, node{np, head})
		}
	}
	return nil
}

// wwNearestSafeFrontier walks the safe-set BFS from the agent's
// current cell and returns the first safe-but-unvisited cell.
func wwNearestSafeFrontier(w *world.World, a *world.Agent) (world.Pos, bool) {
	if a.Beliefs == nil {
		return world.Pos{}, false
	}
	queue := []world.Pos{a.Pos}
	visited := map[world.Pos]bool{a.Pos: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range world.Cardinals {
			np := world.Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if visited[np] {
				continue
			}
			if !wwCellOK(w, a, np) {
				continue
			}
			if !a.Beliefs.Observed[np] {
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
