// goal_distance.go — on-demand BFS that lets a strategy compute the
// shortest-path distance from any cell to the goal WITHOUT relying
// on a world-wide cache. The world intentionally does not expose
// distance-to-goal as a sensor — agents that need it must do the
// search themselves (the same way agents 1, 2, 3 implicitly do
// during their own planning).
//
// Cost: one BFS over the maze per call, ≤ BoardWidth × BoardHeight
// cell visits. For agent 6 that's once per tick; for agent 7 it's
// once per first-move candidate (≤ 4) per tick.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// bfsDistToGoal returns the BFS path length from `from` to the
// world's goal cell THROUGH CELLS THE AGENT HAS PERCEIVED (i.e.,
// `a.KnownCells`). Returns -1 if no such path exists.
//
// Partial-observability-respecting agents (1, 6, 7) call this
// instead of touching `w.Maze.Cells` directly so they can't route
// through walls / corridors they've never seen.
func bfsDistToGoal(w *world.World, a *world.Agent, from world.Pos) int {
	goal := w.Maze.GoalPos
	if from == goal {
		return 0
	}
	type node struct {
		world.Pos
		dist int
	}
	visited := map[world.Pos]bool{from: true}
	queue := []node{{from, 0}}
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
			if np == goal {
				return cur.dist + 1
			}
			visited[np] = true
			queue = append(queue, node{np, cur.dist + 1})
		}
	}
	return -1
}

// manhattanToGoal returns the Manhattan distance from `p` to the
// world's goal cell. Used by agents 6 and 7 as a heuristic fallback
// when bfsDistToGoal returns -1 — i.e., when the agent's KnownCells
// hasn't yet expanded to form a connected known path to the goal.
// Computable from `GoalPos` alone (which the agent knows) so it
// respects partial observability.
func manhattanToGoal(w *world.World, p world.Pos) int {
	return world.AbsInt(p.X-w.Maze.GoalPos.X) + world.AbsInt(p.Y-w.Maze.GoalPos.Y)
}

// knownWalkable: cell is in the agent's perceived terrain AND
// walkable. Used by every partial-observability-respecting helper in
// this package as the traversal predicate. Returns false for cells
// outside `a.KnownCells` — agents only plan through what they've
// seen.
func knownWalkable(w *world.World, a *world.Agent, p world.Pos) bool {
	if !world.InBounds(p.X, p.Y) {
		return false
	}
	if !a.KnownCells[p] {
		return false
	}
	return w.Maze.IsWalkable(p)
}
