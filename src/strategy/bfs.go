// bfs.go — agent B: omniscient breadth-first search to goal.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// TargetFor returns the current planning target for `a`: the maze
// goal. Hazards and water pits no longer exist, so the target is
// always the goal. Kept for the B (BFS) and C (DFS) call sites.
func TargetFor(w *world.World, a *world.Agent) world.Pos {
	return w.Maze.GoalPos
}

// BFSStrategy: cached BFS plan, re-planned when the plan empties or
// no longer ends at the target.
func BFSStrategy(w *world.World, a *world.Agent) world.Pos {
	target := TargetFor(w, a)
	planEndsAtTarget := len(a.Plan) > 0 && a.Plan[len(a.Plan)-1] == target
	if len(a.Plan) == 0 || !planEndsAtTarget {
		a.Plan = BFSToward(w, a.Pos, target)
	}
	if len(a.Plan) == 0 {
		return a.Pos
	}
	next := a.Plan[0]

	// Branch-decision animation: pause at junctions and visualize the
	// search before committing the planned step.
	if pos, animating := runBranchAnim(w, a, next); animating {
		// Animation just finished iff SearchAnim was cleared inside
		// runBranchAnim; in that case the caller must also consume
		// the plan step so the agent advances.
		if a.SearchAnim == nil {
			a.Plan = a.Plan[1:]
		}
		return pos
	}

	a.Plan = a.Plan[1:]
	return next
}

// BFSToGoal returns a BFS path from `from` to the world's goal cell.
// Empty slice if no path exists. Kept as a stable public entry point;
// equivalent to BFSToward(w, from, w.Maze.GoalPos).
func BFSToGoal(w *world.World, from world.Pos) []world.Pos {
	return BFSToward(w, from, w.Maze.GoalPos)
}

// BFSToward returns a shortest path from `from` to `to` over the full
// (omniscient) walkable graph. Walls are the only blocker. Backed by
// A* with octile heuristic on the 10/14-weighted 8-conn grid. "BFS"
// is kept in the name for historical continuity at the call sites.
func BFSToward(w *world.World, from, to world.Pos) []world.Pos {
	if from == to {
		return nil
	}
	return w.AStarPath(from, to, w.Maze.IsWalkable)
}
