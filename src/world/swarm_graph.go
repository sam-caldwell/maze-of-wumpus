// swarm_graph.go — mid-flight graph pruning for the swarm strategy.
//
// Phase 1: Leaf-trim. Iteratively delete non-anchor cells with at
// most one walkable alive neighbor. Captures dead-end CHAINS of any
// length (corridors that wander into nothing). An "anchor" is any
// cell the swarm has a reason to revisit: entrance, goal, frontier
// (cell with an unperceived neighbor), or the current position of
// any alive swarm member.
//
// Phase 2: Articulation / loop pruning via shortest-path essential-
// cell labeling. After leaf-trim, the graph may still contain
// closed loops with no anchors. For each anchor A, BFS from
// entrance and from A yields the cells lying on SOME shortest path
// between them: c is essential iff
//
//	dist(entrance, c) + dist(c, A) == dist(entrance, A)
//
// The union over all anchors is the live set. Cells in loops not
// on any shortest path to an anchor get pruned.
//
// The resulting alive set is the swarm's "pruned graph" — strategy
// S uses it as the effective walkable space for planning, so the
// agent ignores rooms and corridors that lead nowhere.
package world

// swarmGraphState caches the most recent pruned-alive set so we
// don't recompute every tick. Stored on World; updated lazily when
// the swarm union grows.
type swarmGraphState struct {
	aliveCells    map[Pos]bool
	lastUnionSize int
}

// RecomputeSwarmGraphIfStale rebuilds the pruned alive-cell set
// when the swarm's union of KnownCells has grown since the last
// computation. Cheap dirty-check via size; for a 120×80 maze the
// recompute itself runs ~ O(anchors × V) and is amortized over
// many ticks where the union doesn't change.
func (w *World) RecomputeSwarmGraphIfStale() {
	union := w.unionSwarmKnownCells()
	if w.swarmGraph.aliveCells != nil && len(union) == w.swarmGraph.lastUnionSize {
		return
	}
	w.swarmGraph.aliveCells = w.pruneSwarmGraph(union)
	w.swarmGraph.lastUnionSize = len(union)
}

// SwarmAliveCell reports whether `p` survived the swarm's mid-flight
// pruning. Returns true when no pruning has run yet (no swarm, or
// first tick) so existing planners default to "treat everything as
// alive." Strategy S consults this to filter its planning view.
func (w *World) SwarmAliveCell(p Pos) bool {
	if w.swarmGraph.aliveCells == nil {
		return true
	}
	return w.swarmGraph.aliveCells[p]
}

// unionSwarmKnownCells gathers every cell perceived by any alive
// agent currently using SwarmStrategyLetter. Walls included (they
// were perceived but block onward routing) because the planner
// gates on IsWalkable separately.
func (w *World) unionSwarmKnownCells() map[Pos]bool {
	out := map[Pos]bool{}
	for _, a := range w.Agents {
		if !a.Alive || a.CurrentStrategy != SwarmStrategyLetter {
			continue
		}
		for p := range a.KnownCells {
			out[p] = true
		}
	}
	return out
}

// pruneSwarmGraph runs the two-phase pruner on swarmKnown and
// returns the alive set (cells the planner should treat as
// walkable). The input swarmKnown stays untouched.
func (w *World) pruneSwarmGraph(swarmKnown map[Pos]bool) map[Pos]bool {
	// Walkable subset of the swarm's perception.
	walkable := map[Pos]bool{}
	for p := range swarmKnown {
		if w.Maze.IsWalkable(p) {
			walkable[p] = true
		}
	}
	if len(walkable) == 0 {
		return walkable
	}

	// Anchors = cells we never prune. Includes entrance, goal,
	// frontier cells, and every alive swarm member's current cell
	// (so an agent that's currently in a dead-end can still plan
	// its way out).
	anchors := map[Pos]bool{}
	if walkable[w.Maze.EntrancePos] {
		anchors[w.Maze.EntrancePos] = true
	}
	if walkable[w.Maze.GoalPos] {
		anchors[w.Maze.GoalPos] = true
	}
	for p := range walkable {
		for _, d := range Cardinals {
			np := Pos{X: p.X + d.X, Y: p.Y + d.Y}
			if !InBounds(np.X, np.Y) {
				continue
			}
			if !swarmKnown[np] {
				anchors[p] = true
				break
			}
		}
	}
	for _, peer := range w.Agents {
		if peer.Alive && peer.CurrentStrategy == SwarmStrategyLetter && walkable[peer.Pos] {
			anchors[peer.Pos] = true
		}
	}

	// Phase 1: leaf-trim. Cells with ≤ 1 alive walkable neighbor
	// and not anchored get peeled away. Iterate until quiescent.
	alive := map[Pos]bool{}
	for p := range walkable {
		alive[p] = true
	}
	for {
		removed := false
		for p := range alive {
			if anchors[p] {
				continue
			}
			degree := 0
			for _, d := range Cardinals {
				np := Pos{X: p.X + d.X, Y: p.Y + d.Y}
				if alive[np] {
					degree++
				}
			}
			if degree <= 1 {
				delete(alive, p)
				removed = true
			}
		}
		if !removed {
			break
		}
	}

	// Phase 2: shortest-path essential-cell labeling. Cells not on
	// SOME shortest path from entrance to an anchor get pruned.
	// This eliminates closed loops with no anchors (their cells
	// have ≥ 2 alive neighbors so phase 1 missed them, but they're
	// not on any entrance↔anchor shortest path).
	if !alive[w.Maze.EntrancePos] {
		return alive
	}
	distFromEntrance := w.bfsAlive(w.Maze.EntrancePos, alive)
	essential := map[Pos]bool{}
	for a := range anchors {
		if !alive[a] || a == w.Maze.EntrancePos {
			continue
		}
		dEA, ok := distFromEntrance[a]
		if !ok {
			continue
		}
		distFromAnchor := w.bfsAlive(a, alive)
		for p := range alive {
			de, okE := distFromEntrance[p]
			da, okA := distFromAnchor[p]
			if !okE || !okA {
				continue
			}
			if de+da == dEA {
				essential[p] = true
			}
		}
	}
	// Entrance is always essential; anchors that couldn't be
	// reached from entrance (degenerate post-leaf-trim cases) stay
	// alive too — they may yet become useful next tick when the
	// swarm grows.
	essential[w.Maze.EntrancePos] = true
	for a := range anchors {
		if alive[a] {
			essential[a] = true
		}
	}
	return essential
}

// bfsAlive computes Dijkstra (weighted 8-conn) distances from
// `start` through cells in `alive` (walkable + non-pruned). Used
// by phase 2 to identify cells on shortest entrance↔anchor paths
// under the same movement model as the rest of the game.
func (w *World) bfsAlive(start Pos, alive map[Pos]bool) map[Pos]int {
	dist := map[Pos]int{start: 0}
	type item struct {
		pos  Pos
		cost int
	}
	pq := []item{{start, 0}}
	for len(pq) > 0 {
		mi := 0
		for i := 1; i < len(pq); i++ {
			if pq[i].cost < pq[mi].cost {
				mi = i
			}
		}
		cur := pq[mi]
		pq[mi] = pq[len(pq)-1]
		pq = pq[:len(pq)-1]
		if cur.cost > dist[cur.pos] {
			continue
		}
		for _, d := range Cardinals {
			np := Pos{X: cur.pos.X + d.X, Y: cur.pos.Y + d.Y}
			if !alive[np] {
				continue
			}
			if w.Maze.IsCornerClipped(cur.pos, np) {
				continue
			}
			newCost := cur.cost + StepCost(d)
			if cd, ok := dist[np]; ok && newCost >= cd {
				continue
			}
			dist[np] = newCost
			pq = append(pq, item{np, newCost})
		}
	}
	return dist
}
