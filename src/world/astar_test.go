package world

import (
	"testing"
)

// TestAStarPath_SameCell: from==to returns nil — same contract as
// DijkstraPath (returned path excludes `from`, so a zero-step trip
// is nil rather than a single-element slice).
func TestAStarPath_SameCell(t *testing.T) {
	w := NewWorld(1)
	p := w.AStarPath(w.Maze.EntrancePos, w.Maze.EntrancePos, w.Maze.IsWalkable)
	if p != nil {
		t.Errorf("AStarPath(x,x) = %v, want nil", p)
	}
}

// TestAStarPath_Unreachable: walling off a target with no openings
// must return nil rather than an empty slice.
func TestAStarPath_Unreachable(t *testing.T) {
	w := NewWorld(2)
	start := Pos{40, 40}
	w.Maze.Cells[start.Y][start.X] = CellPath
	for _, d := range Cardinals {
		w.Maze.Cells[start.Y+d.Y][start.X+d.X] = CellWall
	}
	p := w.AStarPath(start, w.Maze.GoalPos, w.Maze.IsWalkable)
	if p != nil {
		t.Errorf("unreachable AStarPath = %v, want nil", p)
	}
}

// TestAStarPath_ReturnsValidPath: every step is walkable, every
// consecutive pair is Moore-adjacent (Chebyshev distance 1), no
// duplicates, and the last cell is the target.
func TestAStarPath_ReturnsValidPath(t *testing.T) {
	w := NewWorld(3)
	from := w.Maze.EntrancePos
	to := w.Maze.GoalPos
	p := w.AStarPath(from, to, w.Maze.IsWalkable)
	if len(p) == 0 {
		t.Fatalf("expected non-empty path from %v to %v", from, to)
	}
	if p[len(p)-1] != to {
		t.Errorf("path doesn't terminate at goal: last=%v, want=%v", p[len(p)-1], to)
	}
	seen := map[Pos]bool{from: true}
	prev := from
	for _, step := range p {
		if !w.Maze.IsWalkable(step) {
			t.Errorf("non-walkable step %v", step)
		}
		dx, dy := step.X-prev.X, step.Y-prev.Y
		if dx < -1 || dx > 1 || dy < -1 || dy > 1 || (dx == 0 && dy == 0) {
			t.Errorf("non-Moore-adjacent step from %v to %v", prev, step)
		}
		if seen[step] {
			t.Errorf("path revisits %v", step)
		}
		seen[step] = true
		prev = step
	}
}

// TestAStarPath_DiagonalShortcut: in an open square, A* must pick the
// diagonal (cost 14) over two cardinals (cost 20). Asserts both the
// weighted nature of the cost function and the heuristic's correct
// scaling against those weights.
func TestAStarPath_DiagonalShortcut(t *testing.T) {
	w := NewWorld(4)
	for y := 30; y <= 32; y++ {
		for x := 30; x <= 32; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	from := Pos{30, 30}
	to := Pos{32, 32}
	p := w.AStarPath(from, to, w.Maze.IsWalkable)
	if len(p) != 2 {
		t.Errorf("diagonal shortcut: len(path) = %d, want 2 (two diagonals)", len(p))
	}
	if p[len(p)-1] != to {
		t.Errorf("path didn't reach %v", to)
	}
}

// TestAStarPath_HonorsCornerClip: returned path must never include a
// step that violates IsCornerClipped.
func TestAStarPath_HonorsCornerClip(t *testing.T) {
	w := NewWorld(5)
	for y := 30; y <= 32; y++ {
		for x := 30; x <= 32; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	w.Maze.Cells[30][32] = CellWall
	w.Maze.Cells[31][31] = CellWall
	from := Pos{30, 30}
	to := Pos{32, 32}
	p := w.AStarPath(from, to, w.Maze.IsWalkable)
	if len(p) == 0 {
		return // unreachable is an acceptable outcome here
	}
	prev := from
	for _, step := range p {
		if w.Maze.IsCornerClipped(prev, step) {
			t.Errorf("path contains corner-clipped step %v -> %v", prev, step)
		}
		prev = step
	}
}

// TestAStarPath_MatchesDijkstraCost: A* with an admissible heuristic
// must return a path whose summed cost equals Dijkstra's optimum.
// Path shape may differ when multiple equal-cost paths exist; this
// test only locks in cost equivalence.
func TestAStarPath_MatchesDijkstraCost(t *testing.T) {
	for _, seed := range []int64{11, 23, 47, 89, 137} {
		w := NewWorld(seed)
		from := w.Maze.EntrancePos
		to := w.Maze.GoalPos
		ap := w.AStarPath(from, to, w.Maze.IsWalkable)
		dp := w.DijkstraPath(from, to, w.Maze.IsWalkable)
		if (ap == nil) != (dp == nil) {
			t.Errorf("seed=%d: reachability differs (A*=%v, Dijkstra=%v)", seed, ap == nil, dp == nil)
			continue
		}
		if ap == nil {
			continue
		}
		aCost, dCost := pathCost(from, ap), pathCost(from, dp)
		if aCost != dCost {
			t.Errorf("seed=%d: A* cost %d != Dijkstra cost %d", seed, aCost, dCost)
		}
	}
}

// TestOctile_Admissible: the heuristic must never exceed the true
// optimal cost — sampling on an open grid where the true cost is
// exactly octile, the heuristic should equal it (tight), and any
// detour must only increase the actual cost (verified by comparing
// to DijkstraPath in TestAStarPath_MatchesDijkstraCost).
func TestOctile_Admissible(t *testing.T) {
	cases := []struct {
		a, b Pos
		want int
	}{
		{Pos{0, 0}, Pos{0, 0}, 0},
		{Pos{0, 0}, Pos{5, 0}, 5 * CardinalStepCost},     // pure cardinal
		{Pos{0, 0}, Pos{5, 5}, 5 * DiagonalStepCost},     // pure diagonal
		{Pos{0, 0}, Pos{5, 3}, 3*DiagonalStepCost + 2*CardinalStepCost},
		{Pos{3, 7}, Pos{1, 2}, 2*DiagonalStepCost + 3*CardinalStepCost},
	}
	for _, c := range cases {
		if got := octile(c.a, c.b); got != c.want {
			t.Errorf("octile(%v,%v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// pathCost sums StepCost over each consecutive pair in (from, path...).
func pathCost(from Pos, path []Pos) int {
	cost := 0
	prev := from
	for _, step := range path {
		cost += StepCost(Pos{X: step.X - prev.X, Y: step.Y - prev.Y})
		prev = step
	}
	return cost
}
