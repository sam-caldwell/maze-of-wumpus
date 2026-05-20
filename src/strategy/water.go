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
// This is the **omniscient** variant used by agents 2 / 3 which have
// global map access.
func NeedsWater(w *world.World, a *world.Agent) bool {
	if w.WaterPitsDisabled {
		return false
	}
	return a.Water == 0 && len(w.Maze.WaterPits) > 0
}

// NeedsKnownWater is the partial-observability variant of NeedsWater
// for agents 1 / 4 / 5 / 6 / 7. The agent only treats water as a
// goal when it has actually perceived at least one water-pit cell.
// Falls back to false otherwise even if the world has water pits.
func NeedsKnownWater(w *world.World, a *world.Agent) bool {
	if w.WaterPitsDisabled || a.Water != 0 {
		return false
	}
	for _, p := range w.Maze.WaterPits {
		if a.KnownCells[p] {
			return true
		}
	}
	return false
}

// NearestKnownWaterPit returns the closest water pit (by Manhattan
// distance) that the agent has actually perceived (i.e., it's in
// `a.KnownCells`). Returns (Pos{}, false) when the agent hasn't seen
// any water pit yet.
func NearestKnownWaterPit(w *world.World, a *world.Agent, from world.Pos) (world.Pos, bool) {
	best := world.Pos{}
	bestD := 1 << 30
	found := false
	for _, p := range w.Maze.WaterPits {
		if !a.KnownCells[p] {
			continue
		}
		d := world.AbsInt(p.X-from.X) + world.AbsInt(p.Y-from.Y)
		if d < bestD {
			bestD = d
			best = p
			found = true
		}
	}
	return best, found
}

// NearestWaterPit returns the water pit closest to `from` by Manhattan
// distance. Returns (Pos{}, false) when no water pits remain.
// Omniscient: used by agents 2 / 3 only.
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

// bfsTowardKnown is the partial-observability variant of BFSToward:
// only traverses cells in `a.KnownCells`. Used by PO agents for the
// water override. Now uses Dijkstra under the hood so the path
// respects 8-conn weighting and corner-clipping; the function name
// is kept for callers.
func bfsTowardKnown(w *world.World, a *world.Agent, from, to world.Pos) []world.Pos {
	if from == to {
		return nil
	}
	return w.DijkstraPath(from, to, func(p world.Pos) bool {
		if !knownWalkable(w, a, p) {
			return false
		}
		if p != to && w.IsHazard(p) {
			return false
		}
		return true
	})
}

// BFSToward returns a hazard-avoiding shortest path from `from` to
// `to` over the full (omniscient) walkable graph. Used by R (BFS)
// for goal/water routing. Backed by Dijkstra with 8-conn weighting
// and corner-clipping; "BFS" is kept in the name for historical
// continuity.
func BFSToward(w *world.World, from, to world.Pos) []world.Pos {
	if from == to {
		return nil
	}
	return w.DijkstraPath(from, to, func(p world.Pos) bool {
		if !w.Maze.IsWalkable(p) {
			return false
		}
		if p != to && w.IsHazard(p) {
			return false
		}
		return true
	})
}

// WaterOverride returns the first step of a BFS path toward the
// nearest KNOWN water pit, or (Pos{}, false) when not applicable.
// Partial-observability-respecting: only considers water pits the
// agent has seen, and only routes through cells in a.KnownCells.
// Used by agents 4 and 5 to bypass their RL policy when fetching
// a known water charge is the strictly safer move.
func WaterOverride(w *world.World, a *world.Agent) (world.Pos, bool) {
	if !NeedsKnownWater(w, a) {
		return world.Pos{}, false
	}
	pit, ok := NearestKnownWaterPit(w, a, a.Pos)
	if !ok {
		return world.Pos{}, false
	}
	path := bfsTowardKnown(w, a, a.Pos, pit)
	if len(path) == 0 {
		return world.Pos{}, false
	}
	return path[0], true
}
