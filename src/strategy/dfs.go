// dfs.go — agent C: omniscient depth-first search to goal. Finds a
// path (not necessarily shortest) by going deep before wide.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// DFSStrategy: cached DFS plan; re-plans on empty plan or when the
// plan no longer ends at the target.
func DFSStrategy(w *world.World, a *world.Agent) world.Pos {
	target := TargetFor(w, a)
	planEndsAtTarget := len(a.Plan) > 0 && a.Plan[len(a.Plan)-1] == target
	if len(a.Plan) == 0 || !planEndsAtTarget {
		a.Plan = DFSToward(w, a.Pos, target)
	}
	if len(a.Plan) == 0 {
		return a.Pos
	}
	next := a.Plan[0]

	if pos, animating := runBranchAnim(w, a, next); animating {
		if a.SearchAnim == nil {
			a.Plan = a.Plan[1:]
		}
		return pos
	}

	a.Plan = a.Plan[1:]
	return next
}

// DFSToGoal: recursive depth-first search from `from` to the world's
// goal cell. Backtracks on dead ends. Returns nil if unreachable.
// Kept as a stable public entry point; equivalent to DFSToward(w,
// from, w.Maze.GoalPos).
func DFSToGoal(w *world.World, from world.Pos) []world.Pos {
	return DFSToward(w, from, w.Maze.GoalPos)
}

// DFSToward: DFS from `from` to `to` over walkable cells. Returns nil
// if unreachable.
func DFSToward(w *world.World, from, to world.Pos) []world.Pos {
	if from == to {
		return nil
	}
	visited := map[world.Pos]bool{from: true}
	var path []world.Pos
	if dfsHelper(w, from, to, visited, &path) {
		return path
	}
	return nil
}

func dfsHelper(w *world.World, cur, goal world.Pos, visited map[world.Pos]bool, path *[]world.Pos) bool {
	if cur == goal {
		return true
	}
	for _, d := range world.Cardinals {
		np := world.Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
		if !w.Maze.IsWalkable(np) {
			continue
		}
		if w.Maze.IsCornerClipped(cur, np) {
			continue
		}
		if visited[np] {
			continue
		}
		visited[np] = true
		*path = append(*path, np)
		if dfsHelper(w, np, goal, visited, path) {
			return true
		}
		*path = (*path)[:len(*path)-1]
	}
	return false
}
