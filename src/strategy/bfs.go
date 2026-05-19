// bfs.go — agent B: omniscient breadth-first search to goal,
// treating fire pits and live wumpus as hazards.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// BFSStrategy: cached BFS plan, re-planned when the next cached step
// becomes a hazard. Targets the nearest water pit when the agent has
// zero water charges and pits exist (see water.go); reverts to goal-
// seeking once a charge is picked up.
func BFSStrategy(w *world.World, a *world.Agent) world.Pos {
	target := TargetFor(w, a)
	planEndsAtTarget := len(a.Plan) > 0 && a.Plan[len(a.Plan)-1] == target
	if len(a.Plan) == 0 || w.IsHazard(a.Plan[0]) || !planEndsAtTarget {
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

// BFSToGoal returns a hazard-avoiding BFS path from `from` to the
// world's goal cell. Empty slice if no path exists. Kept as a stable
// public entry point; equivalent to BFSToward(w, from, w.Maze.GoalPos).
func BFSToGoal(w *world.World, from world.Pos) []world.Pos {
	return BFSToward(w, from, w.Maze.GoalPos)
}
