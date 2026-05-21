// goal_distance.go — the partial-observability traversal predicate
// every PO-respecting strategy uses. A cell is "walkable" for
// planning only if the agent has perceived it AND the underlying
// maze cell isn't a wall.
//
// Historical note: this file previously hosted bfsDistToGoal /
// manhattanToGoal helpers that read w.Maze.GoalPos. Those leaked
// goal coordinates to partial-observability agents — a violation of
// the immutable PO requirement — so they were removed.
package strategy

import (
	"maze-of-wumpus/src/world"
)

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
