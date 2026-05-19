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
// world's goal cell through walkable cells. Returns -1 if no path
// exists (or if `from` is itself unreachable, e.g., inside a wall).
func bfsDistToGoal(w *world.World, from world.Pos) int {
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
			if !w.Maze.IsWalkable(np) {
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
