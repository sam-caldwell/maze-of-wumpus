// water.go — shared helpers for treating water pits as a secondary
// goal. When an agent has zero water charges AND the maze has at
// least one water pit, the agent's chosen target switches from the
// goal cell to the nearest water pit. Once a water charge is held
// (a.Water > 0), the agent reverts to goal-seeking.
//
// The rationale: water is a "free life" against fire pits. Picking
// one up costs essentially nothing (a small detour) and may save a
// later death. Agents without water also can't survive a goal-hazard
// fire pit; with this override, they will pre-emptively grab a charge.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// NeedsWater reports whether the agent should currently treat water
// as its primary objective: it has no charges, the world still has
// at least one water pit on the map, AND water pits are enabled.
func NeedsWater(w *world.World, a *world.Agent) bool {
	if w.WaterPitsDisabled {
		return false
	}
	return a.Water == 0 && len(w.Maze.WaterPits) > 0
}

// NearestWaterPit returns the water pit closest to `from` by Manhattan
// distance. Returns (Pos{}, false) when no water pits remain.
func NearestWaterPit(w *world.World, from world.Pos) (world.Pos, bool) {
	if len(w.Maze.WaterPits) == 0 {
		return world.Pos{}, false
	}
	best := w.Maze.WaterPits[0]
	bestD := world.AbsInt(best.X-from.X) + world.AbsInt(best.Y-from.Y)
	for _, p := range w.Maze.WaterPits[1:] {
		d := world.AbsInt(p.X-from.X) + world.AbsInt(p.Y-from.Y)
		if d < bestD {
			bestD = d
			best = p
		}
	}
	return best, true
}

// TargetFor returns the current planning target for `a`: a nearby
// water pit when the agent needs water, otherwise the maze goal.
// Used by B (BFS) and C (DFS) to swap targets dynamically.
func TargetFor(w *world.World, a *world.Agent) world.Pos {
	if NeedsWater(w, a) {
		if p, ok := NearestWaterPit(w, a.Pos); ok {
			return p
		}
	}
	return w.Maze.GoalPos
}

// BFSToward returns a hazard-avoiding BFS path from `from` to `to`,
// same semantics as BFSToGoal but with an arbitrary destination.
// Used by D (Q-learning) and E (DQN) to compute a one-step override
// toward water when the agent needs a charge.
func BFSToward(w *world.World, from, to world.Pos) []world.Pos {
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
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if _, seen := visited[np]; seen {
				continue
			}
			if np != to && w.IsHazard(np) {
				continue
			}
			visited[np] = len(nodes)
			nodes = append(nodes, node{np, head})
		}
	}
	return nil
}

// WaterOverride returns the first step of a BFS path toward the
// nearest water pit, or (Pos{}, false) when not applicable. Used by
// D and E to bypass their RL policy when fetching water is the
// strictly safer move.
func WaterOverride(w *world.World, a *world.Agent) (world.Pos, bool) {
	if !NeedsWater(w, a) {
		return world.Pos{}, false
	}
	pit, ok := NearestWaterPit(w, a.Pos)
	if !ok {
		return world.Pos{}, false
	}
	path := BFSToward(w, a.Pos, pit)
	if len(path) == 0 {
		return world.Pos{}, false
	}
	return path[0], true
}
