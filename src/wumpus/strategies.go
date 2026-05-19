// wumpus_strategies.go — five hunting strategies for wumpus,
// mirroring the agent-side family A/B/C/D/E.
//
//	ScentStrategy  (A-style): hill-climb toward agent scent.
//	BfsStrategy    (B-style): BFS toward nearest live agent.
//	DfsStrategy    (C-style): DFS toward nearest live agent.
//	QLStrategy     (D-style): tabular Q-learning, reward +100 for
//	                          ending the tick adjacent to a live agent.
//	DqnStrategy    (E-style): same reward, NN policy.
//
// Wumpus DON'T treat agents as obstacles (the whole point is to walk
// INTO them). They DO avoid fire pits, walls, and OTHER wumpus.
package wumpus

import (
	"math/rand"

	"maze-of-wumpus/src/world"
)

// QL/DQN hyperparameters mirror the agent-side strategies; duplicated
// here to keep the wumpus package independent of strategy.
const (
	qLearnAlpha   = 0.1
	qLearnGamma   = 0.95
	qLearnEpsilon = 0.05

	dqnLearnRate = 0.01
	dqnGamma     = 0.95
	dqnEpsilon   = 0.05
)

// PickStrategy returns one of the five strategies, chosen uniformly
// at random from `rng`. Suitable for use as world.Config.WumpusStrategy.
func PickStrategy(rng *rand.Rand) world.WumpusStrategy {
	switch rng.Intn(5) {
	case 0:
		return ScentStrategy
	case 1:
		return BfsStrategy
	case 2:
		return DfsStrategy
	case 3:
		return QLStrategy
	default:
		return DqnStrategy
	}
}

// blocked: a cell that this wumpus cannot legally enter. Walls / fire
// pits / other wumpus all block; agents do NOT block.
func blocked(w *world.World, p world.Pos, ignore *world.Wumpus) bool {
	if !world.InBounds(p.X, p.Y) {
		return true
	}
	if !w.Maze.IsWalkable(p) {
		return true
	}
	if w.Maze.Cells[p.Y][p.X] == world.CellFirePit {
		return true
	}
	if other := w.WumpusAt[p.Y][p.X]; other != nil && other != ignore {
		return true
	}
	return false
}

// nearestLiveAgent: position of the closest live agent by Manhattan
// distance, or (_, false) if none alive.
func nearestLiveAgent(w *world.World, from world.Pos) (world.Pos, bool) {
	best := world.Pos{}
	bestDist := 1 << 30
	found := false
	for _, a := range w.Agents {
		if !a.Alive {
			continue
		}
		d := world.AbsInt(a.Pos.X-from.X) + world.AbsInt(a.Pos.Y-from.Y)
		if d < bestDist {
			bestDist = d
			best = a.Pos
			found = true
		}
	}
	return best, found
}

// RandomNeighbor picks a uniformly-random unblocked cardinal neighbor.
// Returns wm.Pos if all 4 are blocked.
func RandomNeighbor(w *world.World, wm *world.Wumpus) world.Pos {
	candidates := make([]world.Pos, 0, 4)
	for _, d := range world.Cardinals {
		np := world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
		if !blocked(w, np, wm) {
			candidates = append(candidates, np)
		}
	}
	if len(candidates) == 0 {
		return wm.Pos
	}
	return candidates[w.Rng.Intn(len(candidates))]
}

// bfsTo: shortest path from `wm.Pos` to `to` over walkable non-pit
// non-other-wumpus cells.
func bfsTo(w *world.World, wm *world.Wumpus, to world.Pos) []world.Pos {
	from := wm.Pos
	if from == to {
		return nil
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
			if np != to && blocked(w, np, wm) {
				continue
			}
			if _, seen := visited[np]; seen {
				continue
			}
			visited[np] = len(nodes)
			nodes = append(nodes, node{np, head})
		}
	}
	return nil
}

// dfsTo: first DFS path between from and to under the same blocking
// rules as bfsTo.
func dfsTo(w *world.World, wm *world.Wumpus, to world.Pos) []world.Pos {
	from := wm.Pos
	if from == to {
		return nil
	}
	visited := map[world.Pos]bool{from: true}
	var path []world.Pos
	var helper func(cur world.Pos) bool
	helper = func(cur world.Pos) bool {
		if cur == to {
			return true
		}
		for _, d := range world.Cardinals {
			np := world.Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if np != to && blocked(w, np, wm) {
				continue
			}
			if visited[np] {
				continue
			}
			visited[np] = true
			path = append(path, np)
			if helper(np) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}
	if helper(from) {
		return path
	}
	return nil
}

// ScentStrategy (A-style): hill-climb toward agent scent.
func ScentStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	candidates := make([]world.Pos, 0, 4)
	for _, d := range world.Cardinals {
		np := world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
		if blocked(w, np, wm) {
			continue
		}
		if w.ScentOwner[np.Y][np.X] != 0 {
			candidates = append(candidates, np)
		}
	}
	if len(candidates) > 0 {
		return candidates[w.Rng.Intn(len(candidates))]
	}
	return RandomNeighbor(w, wm)
}

// BfsStrategy (B-style): BFS straight at the nearest live agent.
func BfsStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	target, ok := nearestLiveAgent(w, wm.Pos)
	if !ok {
		return RandomNeighbor(w, wm)
	}
	path := bfsTo(w, wm, target)
	if len(path) == 0 {
		return RandomNeighbor(w, wm)
	}
	return path[0]
}

// DfsStrategy (C-style): DFS to nearest agent.
func DfsStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	target, ok := nearestLiveAgent(w, wm.Pos)
	if !ok {
		return RandomNeighbor(w, wm)
	}
	path := dfsTo(w, wm, target)
	if len(path) == 0 {
		return RandomNeighbor(w, wm)
	}
	return path[0]
}

// QLStrategy (D-style): tabular Q-learning with reward = -1 per step
// plus +100 if the wumpus ends the tick adjacent to a live agent.
func QLStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	if wm.QL == nil {
		wm.QL = world.NewQLearning()
	}
	q := wm.QL
	if q.HasPending {
		reward := -1.0
		if w.HasAdjacentLiveAgent(wm) {
			reward += 100
		}
		maxNext := q.MaxQ(wm.Pos)
		old := q.GetQ(q.PrevState, q.PrevAction)
		updated := old + qLearnAlpha*(reward+qLearnGamma*maxNext-old)
		q.SetQ(q.PrevState, q.PrevAction, updated)
		q.HasPending = false
	}
	var action int
	if w.Rng.Float64() < qLearnEpsilon {
		action = w.Rng.Intn(world.QActionCount)
	} else {
		action = q.ArgMaxQ(wm.Pos)
		if action < 0 {
			action = w.Rng.Intn(world.QActionCount)
		}
	}
	q.PrevState = wm.Pos
	q.PrevAction = action
	q.HasPending = true
	d := world.Cardinals[action]
	return world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
}

// DqnFeatures: 6-d feature vector for E-style wumpus. Goal target =
// nearest live agent (vs world.AgentDqnFeatures which targets the
// maze goal cell).
func DqnFeatures(w *world.World, wm *world.Wumpus) []float64 {
	in := make([]float64, world.DqnInput)
	in[0] = float64(wm.Pos.X) / float64(world.BoardWidth)
	in[1] = float64(wm.Pos.Y) / float64(world.BoardHeight)
	if target, ok := nearestLiveAgent(w, wm.Pos); ok {
		in[2] = float64(target.X-wm.Pos.X) / float64(world.BoardWidth)
		in[3] = float64(target.Y-wm.Pos.Y) / float64(world.BoardHeight)
	}
	if w.HeatAt(wm.Pos.X, wm.Pos.Y) {
		in[4] = 1
	}
	if w.StenchAt(wm.Pos.X, wm.Pos.Y) {
		in[5] = 1
	}
	return in
}

// DqnStrategy (E-style): neural-net policy with the same reward as
// QLStrategy.
func DqnStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	if wm.DQN == nil {
		wm.DQN = world.NewDQN(w.Rng)
	}
	dnn := wm.DQN
	if dnn.HasPending {
		reward := -1.0
		if w.HasAdjacentLiveAgent(wm) {
			reward += 100
		}
		_, out := dnn.Forward(DqnFeatures(w, wm))
		target := reward + dqnGamma*world.MaxFloat(out)
		dnn.Update(dnn.PrevFeatures, dnn.PrevAction, target, dqnLearnRate)
		dnn.HasPending = false
	}
	in := DqnFeatures(w, wm)
	var action int
	if w.Rng.Float64() < dqnEpsilon {
		action = w.Rng.Intn(world.DqnOutput)
	} else {
		_, out := dnn.Forward(in)
		action = world.ArgMaxFloat(out)
	}
	copy(dnn.PrevFeatures, in)
	dnn.PrevAction = action
	dnn.HasPending = true
	d := world.Cardinals[action]
	return world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
}
