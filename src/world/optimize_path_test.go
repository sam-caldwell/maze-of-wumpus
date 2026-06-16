package world

import "testing"

// TestOptimizeKnownPath_AvoidsCornerClipDiagonals: the cached entrance→goal
// route must be legally walkable under CanMoveTo. Cardinals is 8-connected,
// so a BFS that ignores corner-clipping can cache a diagonal squeeze
// between two walls — a move the agent physically can't make. On replay
// CanMoveTo rejects it, the agent derails off-path into a dead end, and
// under the tight post-solve TTL it never recovers ("1 win then stuck").
func TestOptimizeKnownPath_AvoidsCornerClipDiagonals(t *testing.T) {
	w := NewWorld(5)
	// Region (P=path, #=wall), goal at (3,1):
	//   y=1:  (1,1)P (2,1)#  (3,1)P
	//   y=2:  (1,2)P (2,2)P  (3,2)P
	//   y=3:  (1,3)P (2,3)#  (3,3)P
	// The diagonal (1,1)->(2,2) is a corner-clip (orthogonal (2,1) is a
	// wall), but a legal orthogonal detour exists. A correct optimizer
	// must route around the squeeze.
	walk := []Pos{{1, 1}, {3, 1}, {1, 2}, {2, 2}, {3, 2}, {1, 3}, {3, 3}}
	walls := []Pos{{2, 1}, {2, 3}}
	for _, p := range walk {
		w.Maze.Cells[p.Y][p.X] = CellPath
	}
	for _, p := range walls {
		w.Maze.Cells[p.Y][p.X] = CellWall
	}
	w.Maze.EntrancePos = Pos{1, 1}
	w.Maze.GoalPos = Pos{3, 1}

	a := w.AgentByLabel('1') // non-swarm letter → no peer pooling
	a.KnownCells = map[Pos]bool{}
	for _, p := range walk {
		a.KnownCells[p] = true
	}
	a.KnownShortestPath = nil

	w.optimizeKnownPath(a)

	p := a.KnownShortestPath
	if len(p) < 2 || p[len(p)-1] != w.Maze.GoalPos {
		t.Fatalf("no goal-terminating path cached: %v", p)
	}
	for i := 0; i+1 < len(p); i++ {
		from, to := p[i], p[i+1]
		if dx, dy := to.X-from.X, to.Y-from.Y; dx < -1 || dx > 1 || dy < -1 || dy > 1 {
			t.Errorf("step %v->%v exceeds a single Moore move", from, to)
		}
		if w.Maze.IsCornerClipped(from, to) {
			t.Errorf("cached path routes through a corner-clip the agent can't walk: %v->%v",
				from, to)
		}
	}
}
