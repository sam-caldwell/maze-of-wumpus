package world

import "testing"

// TestGoalPriorWeight_FloorAndDistance: cells within MinGoalDistanceCells
// of the entrance carry zero prior mass (the generator never seats the
// goal there); beyond the floor the weight equals the Manhattan distance.
func TestGoalPriorWeight_FloorAndDistance(t *testing.T) {
	ex, ey := 1, 0
	minD := 1024
	if got := goalPriorWeight(ex, ey, 2, 2, minD); got != 0 {
		t.Errorf("near-entrance weight = %v, want 0", got)
	}
	// Manhattan dist of (600,600) from (1,0) = 599 + 600 = 1199 (≥ floor).
	if got := goalPriorWeight(ex, ey, 600, 600, minD); got != 1199 {
		t.Errorf("far-cell weight = %v, want 1199", got)
	}
	// Farther cell carries strictly more mass.
	near := goalPriorWeight(ex, ey, 600, 600, minD)
	far := goalPriorWeight(ex, ey, 1000, 1000, minD)
	if !(far > near) {
		t.Errorf("expected farther cell to weigh more: near=%v far=%v", near, far)
	}
}

// TestMarkObserved_AccumulatesOnce: the first sighting of a cell folds
// its prior mass into the observed totals; repeat sightings are no-ops,
// keeping ObsW/ObsWX/ObsWY exactly in sync with the Observed set.
func TestMarkObserved_AccumulatesOnce(t *testing.T) {
	w := NewWorld(1)
	b := NewAgentBeliefs()
	// A cell guaranteed beyond the distance floor (so it has mass).
	p := Pos{X: BoardWidth - 2, Y: BoardHeight - 2}
	wt := w.Maze.GoalPriorWeight(p)
	if wt <= 0 {
		t.Fatalf("test setup: far corner should have positive weight, got %v", wt)
	}
	if !b.MarkObserved(w.Maze, p) {
		t.Fatal("first MarkObserved should report a new cell")
	}
	if b.ObsW != wt || b.ObsWX != wt*float64(p.X) || b.ObsWY != wt*float64(p.Y) {
		t.Errorf("totals after first observe = (%v,%v,%v), want (%v,%v,%v)",
			b.ObsW, b.ObsWX, b.ObsWY, wt, wt*float64(p.X), wt*float64(p.Y))
	}
	if b.MarkObserved(w.Maze, p) {
		t.Error("second MarkObserved of same cell should report false")
	}
	if b.ObsW != wt {
		t.Errorf("ObsW double-counted: %v, want %v", b.ObsW, wt)
	}
}

// TestExpectedGoalLocation_FarFromEntrance: with nothing observed, the
// belief centroid sits in the far region the goal is drawn from — at
// least the distance floor away from the entrance.
func TestExpectedGoalLocation_FarFromEntrance(t *testing.T) {
	w := NewWorld(7)
	a := w.AgentByLabel('2') // strategy S
	a.Beliefs = NewAgentBeliefs()
	got, ok := w.ExpectedGoalLocation(a)
	if !ok {
		t.Fatal("expected a goal belief with an unobserved board")
	}
	e := w.Maze.EntrancePos
	d := absInt(got.X-e.X) + absInt(got.Y-e.Y)
	if d < MinGoalDistanceCells {
		t.Errorf("expected-goal centroid %v is only %d from entrance %v, want ≥ %d",
			got, d, e, MinGoalDistanceCells)
	}
}

// TestExpectedGoalLocation_ShiftsAwayFromObserved: perceiving the far
// corner (where prior mass concentrates) without finding the goal pulls
// the centroid back toward the rest of the candidate region.
func TestExpectedGoalLocation_ShiftsAwayFromObserved(t *testing.T) {
	w := NewWorld(7)
	a := w.AgentByLabel('2')
	a.Beliefs = NewAgentBeliefs()
	base, ok := w.ExpectedGoalLocation(a)
	if !ok {
		t.Fatal("baseline belief unavailable")
	}
	// Observe the entire high-coordinate corner away (no goal there).
	for y := BoardHeight * 3 / 4; y < BoardHeight-1; y++ {
		for x := BoardWidth * 3 / 4; x < BoardWidth-1; x++ {
			a.Beliefs.MarkObserved(w.Maze, Pos{X: x, Y: y})
		}
	}
	shifted, ok := w.ExpectedGoalLocation(a)
	if !ok {
		t.Fatal("post-observation belief unavailable")
	}
	if !(shifted.X < base.X || shifted.Y < base.Y) {
		t.Errorf("centroid did not shift away from observed corner: base=%v shifted=%v",
			base, shifted)
	}
}
