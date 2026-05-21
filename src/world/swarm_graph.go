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

import "container/heap"

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

// RecomputeAgentPrunedViewIfStale rebuilds the per-agent pruned view
// of a.KnownCells when the agent has perceived new cells since the
// last run. Cheap dirty-check: KnownCells is monotonic within a map
// life, so a size bump is the only signal needed.
//
// Solo prune runs Phase 1 only (leaf-trim) — phase 2 articulation
// pruning is too aggressive for a single agent's sparse anchor set,
// dropping side branches that the agent legitimately wants to
// explore (scent gradients, water pits).
//
// Extra anchors:
//   - a.Pos (so the planner can plan FROM the agent's current cell)
//   - Every perceived water pit (so the water override's PO BFS can
//     still reach it; without this, a known water pit at the end of
//     a degree-1 corridor would be leaf-trimmed and become invisible
//     to NearestKnownWaterPit / bfsTowardKnown).
//
// Result lands on a.PrunedKnownCells — a set of cells the agent's
// solo planner should treat as the effective walkable space. The
// pruner uses agent perception only (no shared knowledge), and the
// goal anchor is gated on a.KnownCells[GoalPos] — strict PO is
// preserved.
func (w *World) RecomputeAgentPrunedViewIfStale(a *Agent) {
	cur := len(a.KnownCells)
	if a.PrunedKnownCells != nil && cur == a.prunedKnownSize {
		return
	}
	extra := []Pos{a.Pos}
	for _, p := range w.Maze.WaterPits {
		if a.KnownCells[p] {
			extra = append(extra, p)
		}
	}
	a.PrunedKnownCells = w.pruneGraph(a.KnownCells, extra, false)
	a.prunedKnownSize = cur
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

// pruneSwarmGraph runs both phases of the pruner on the union of
// every swarm member's KnownCells. The swarm members' current
// positions are added as anchors so an agent currently standing in
// a dead-end can still plan its way out. Returns the alive set
// (cells the planner should treat as walkable). The input
// swarmKnown stays untouched.
func (w *World) pruneSwarmGraph(swarmKnown map[Pos]bool) map[Pos]bool {
	var memberPos []Pos
	for _, peer := range w.Agents {
		if peer.Alive && peer.CurrentStrategy == SwarmStrategyLetter {
			memberPos = append(memberPos, peer.Pos)
		}
	}
	return w.pruneGraph(swarmKnown, memberPos, true)
}

// pruneGraph is the core pruner used by both the swarm graph and
// the per-agent solo prune:
//
//  Phase 1: leaf-trim cells with ≤ 1 alive walkable neighbor that
//           aren't anchored.
//  Phase 2 (opt-in via `phase2`): keep only cells lying on SOME
//           shortest path from entrance to an anchor (drops closed
//           loops without anchors).
//
// Phase 2 makes sense when the anchor set is dense enough that the
// shortest-path skeleton between anchors covers most useful terrain
// — true for the swarm case (many member positions + frontier cells
// → meaningful skeleton). For solo callers the anchor set is sparse
// (entrance, maybe-goal, perception boundary, self) and phase 2
// drops side branches the agent legitimately wants to explore
// (scent gradients, water pits, alternative routes). Solo callers
// should pass phase2=false.
//
// Anchor set, gated on `walkable[p]` to preserve PO:
//   - Maze entrance (always, if perceived)
//   - Maze goal (only if perceived — KnownCells acts as the gate)
//   - Frontier cells (perceived cells with at least one unperceived
//     neighbor — places we still want to explore from)
//   - Every position in `extraAnchors` (e.g. an agent's current cell)
//
// PO invariant: callers pass perception-bounded `known` sets, so the
// goal anchor here is gated on the caller having perceived the goal.
func (w *World) pruneGraph(known map[Pos]bool, extraAnchors []Pos, phase2 bool) map[Pos]bool {
	walkable := map[Pos]bool{}
	for p := range known {
		if w.Maze.IsWalkable(p) {
			walkable[p] = true
		}
	}
	if len(walkable) == 0 {
		return walkable
	}

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
			if !known[np] {
				anchors[p] = true
				break
			}
		}
	}
	for _, p := range extraAnchors {
		if walkable[p] {
			anchors[p] = true
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

	if !phase2 {
		return alive
	}

	// Phase 2: shortest-path essential-cell labeling. Cells not on
	// SOME shortest path from entrance to an anchor get pruned.
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
	pq := &dijkstraPQ{{start, 0}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(dijkstraItem)
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
			heap.Push(pq, dijkstraItem{np, newCost})
		}
	}
	return dist
}
