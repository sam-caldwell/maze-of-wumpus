package wumpus

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestScentStrategy_DelegatesToScentMove: ScentStrategy is the legacy
// public entry point that wraps strongestAgentScentMove. With agent
// scent on a neighbor cell, it picks that neighbor.
func TestScentStrategy_DelegatesToScentMove(t *testing.T) {
	w := newConfiguredWorld(300)
	w.EnableHazards()
	_ = world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	// Plant agent scent on a known neighbor.
	wm.Pos = world.Pos{X: 40, Y: 40}
	w.WumpusAt[40][40] = wm
	w.Maze.Cells[40][40] = world.CellPath
	w.Maze.Cells[40][41] = world.CellPath
	w.Cycle = 100
	w.ScentCycle[40][41] = 100
	w.ScentOwner[40][41] = '1'
	got := ScentStrategy(w, wm)
	if got != (world.Pos{X: 41, Y: 40}) {
		t.Errorf("ScentStrategy = %v, want (41,40)", got)
	}
}

// TestVengeanceStrategy_FollowsScent: vengeance is bayesian-style
// pursuit at full aggressiveness — pick the neighbor with the
// strongest agent scent.
func TestVengeanceStrategy_FollowsScent(t *testing.T) {
	w := newConfiguredWorld(301)
	w.EnableHazards()
	_ = world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	wm.Pos = world.Pos{X: 30, Y: 30}
	w.WumpusAt[30][30] = wm
	w.Maze.Cells[30][30] = world.CellPath
	w.Maze.Cells[30][31] = world.CellPath
	w.Cycle = 50
	w.ScentCycle[30][31] = 50
	w.ScentOwner[30][31] = '1'
	got := VengeanceStrategy(w, wm)
	if got != (world.Pos{X: 31, Y: 30}) {
		t.Errorf("VengeanceStrategy = %v, want (31,30)", got)
	}
}

// TestBfsTo_SameCellReturnsNil covers the from==to early return.
func TestBfsTo_SameCellReturnsNil(t *testing.T) {
	w := newConfiguredWorld(302)
	wm := &world.Wumpus{Pos: world.Pos{X: 5, Y: 5}, Alive: true}
	if p := bfsTo(w, wm, world.Pos{X: 5, Y: 5}); p != nil {
		t.Errorf("bfsTo(self) = %v, want nil", p)
	}
}

// TestBfsTo_ReturnsPath: routes through walkable cells.
func TestBfsTo_ReturnsPath(t *testing.T) {
	w := newConfiguredWorld(303)
	wm := &world.Wumpus{Pos: world.Pos{X: 30, Y: 30}, Alive: true}
	// Carve a small open block.
	for y := 30; y <= 32; y++ {
		for x := 30; x <= 32; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	p := bfsTo(w, wm, world.Pos{X: 32, Y: 32})
	if len(p) == 0 {
		t.Error("expected non-empty path through open block")
	}
}

// TestCrowdHunt_NoSightingsFallsBackToWander: with no agents in the
// detection radius, crowdHunt falls through to wanderHunt (random
// neighbor).
func TestCrowdHunt_NoSightingsFallsBackToWander(t *testing.T) {
	w := newConfiguredWorld(304)
	w.EnableHazards()
	wm := w.Wumpus[0]
	wm.HuntMode = world.WumpusHuntCrowd
	wm.Aggressiveness = world.WumpusAggressionMax
	// No agents alive → no sightings.
	for _, a := range w.Agents {
		a.Alive = false
	}
	got := crowdHunt(w, wm)
	dx := got.X - wm.Pos.X
	dy := got.Y - wm.Pos.Y
	if got != wm.Pos && (dx < -1 || dx > 1 || dy < -1 || dy > 1) {
		t.Errorf("crowdHunt fallback returned non-neighbor %v", got)
	}
}

// TestCrowdHunt_RoutesToNearestSighting: with a single agent within
// detection radius, the wumpus moves in the direction of that agent.
func TestCrowdHunt_RoutesToNearestSighting(t *testing.T) {
	w := newConfiguredWorld(305)
	w.EnableHazards()
	if len(w.Wumpus) == 0 {
		t.Skip("no wumpus")
	}
	wm := w.Wumpus[0]
	wm.HuntMode = world.WumpusHuntCrowd
	wm.Aggressiveness = world.WumpusAggressionMax
	// Place wumpus and agent in known cells.
	wm.Pos = world.Pos{X: 50, Y: 50}
	w.WumpusAt[50][50] = wm
	w.Maze.Cells[50][50] = world.CellPath
	w.Maze.Cells[51][50] = world.CellPath
	w.Maze.Cells[52][50] = world.CellPath
	a := world.SpawnAgentForTest(w, '1')
	a.Pos = world.Pos{X: 52, Y: 50}
	a.Alive = true
	// Need the agent to be within detection radius (≤5 Manhattan).
	if got := crowdHunt(w, wm); got == wm.Pos {
		t.Error("crowdHunt didn't move toward known sighting")
	}
}

// TestCrowdSightings_DeduplicatesAgents covers the seen-map skip path:
// two crowd wumpus see the same agent — output has one entry.
func TestCrowdSightings_DeduplicatesAgents(t *testing.T) {
	w := newConfiguredWorld(306)
	w.EnableHazards()
	if len(w.Wumpus) < 2 {
		t.Skip("need ≥ 2 wumpus")
	}
	a := world.SpawnAgentForTest(w, '1')
	a.Alive = true
	a.Pos = world.Pos{X: 30, Y: 30}
	for i := 0; i < 2; i++ {
		w.Wumpus[i].HuntMode = world.WumpusHuntCrowd
		w.Wumpus[i].Alive = true
		w.Wumpus[i].Pos = world.Pos{X: 30 + i, Y: 30}
	}
	for i := 2; i < len(w.Wumpus); i++ {
		w.Wumpus[i].Alive = false
	}
	sights := crowdSightings(w)
	count := 0
	for _, p := range sights {
		if p == a.Pos {
			count++
		}
	}
	if count != 1 {
		t.Errorf("agent counted %d times, want 1", count)
	}
}

// TestCrowdSightings_NoCrowdWumpus: with no crowd-mode wumpus alive,
// returns nil regardless of agent positions.
func TestCrowdSightings_NoCrowdWumpus(t *testing.T) {
	w := newConfiguredWorld(307)
	w.EnableHazards()
	for _, wm := range w.Wumpus {
		wm.HuntMode = world.WumpusHuntBayesian
	}
	if got := crowdSightings(w); got != nil {
		t.Errorf("crowdSightings with no crowd wumpus = %v, want nil", got)
	}
}

// TestCommitsToHunt_AggZero: with aggressiveness 0, never commits.
func TestCommitsToHunt_AggZero(t *testing.T) {
	w := newConfiguredWorld(308)
	wm := &world.Wumpus{Aggressiveness: 0}
	for i := 0; i < 20; i++ {
		if commitsToHunt(w, wm) {
			t.Error("agg=0 should never commit")
			break
		}
	}
}

// TestCommitsToHunt_AggMax: with aggressiveness MAX, always commits.
func TestCommitsToHunt_AggMax(t *testing.T) {
	w := newConfiguredWorld(309)
	wm := &world.Wumpus{Aggressiveness: world.WumpusAggressionMax}
	for i := 0; i < 20; i++ {
		if !commitsToHunt(w, wm) {
			t.Error("agg=MAX should always commit")
			break
		}
	}
}

// TestBlocked_AllBranches: OOB, wall, fire pit, other wumpus, open.
func TestBlocked_AllBranches(t *testing.T) {
	w := newConfiguredWorld(310)
	wm := &world.Wumpus{Pos: world.Pos{X: 50, Y: 50}}
	// OOB.
	if !blocked(w, world.Pos{X: -1, Y: 0}, wm) {
		t.Error("OOB should be blocked")
	}
	// Wall.
	w.Maze.Cells[60][60] = world.CellWall
	if !blocked(w, world.Pos{X: 60, Y: 60}, wm) {
		t.Error("wall should be blocked")
	}
	// Fire pit.
	w.Maze.Cells[60][60] = world.CellFirePit
	if !blocked(w, world.Pos{X: 60, Y: 60}, wm) {
		t.Error("fire pit should be blocked")
	}
	// Other wumpus.
	w.Maze.Cells[60][60] = world.CellPath
	other := &world.Wumpus{Pos: world.Pos{X: 60, Y: 60}}
	w.WumpusAt[60][60] = other
	if !blocked(w, world.Pos{X: 60, Y: 60}, wm) {
		t.Error("other wumpus should be blocked")
	}
	// Ignored wumpus (self).
	if blocked(w, world.Pos{X: 60, Y: 60}, other) {
		t.Error("self should not block self")
	}
	// Open path.
	w.WumpusAt[60][60] = nil
	if blocked(w, world.Pos{X: 60, Y: 60}, wm) {
		t.Error("open path should not be blocked")
	}
}

// TestRandomNeighbor_AllBlockedReturnsSelf: when every neighbor is
// blocked, return wm.Pos.
func TestRandomNeighbor_AllBlockedReturnsSelf(t *testing.T) {
	w := newConfiguredWorld(311)
	wm := &world.Wumpus{Pos: world.Pos{X: 30, Y: 30}}
	// Wall all 8 neighbors.
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			w.Maze.Cells[30+dy][30+dx] = world.CellWall
		}
	}
	if got := RandomNeighbor(w, wm); got != wm.Pos {
		t.Errorf("all-blocked RandomNeighbor = %v, want %v", got, wm.Pos)
	}
}

// TestSeekOrPatrol_OutsideRadiusHeadsTowardGoal: a wumpus far from
// the goal must take a step that REDUCES its Manhattan distance to
// the goal — it should be planning toward gold, not wandering.
func TestSeekOrPatrol_OutsideRadiusHeadsTowardGoal(t *testing.T) {
	w := newConfiguredWorld(400)
	wm := &world.Wumpus{Pos: world.Pos{X: 10, Y: 10}, Alive: true}
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = wm
	// Carve a clear path from (10,10) toward (40,40) so Dijkstra
	// has somewhere to plan.
	w.Maze.GoalPos = world.Pos{X: 40, Y: 40}
	for y := 10; y <= 40; y++ {
		for x := 10; x <= 40; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	w.Maze.Cells[w.Maze.GoalPos.Y][w.Maze.GoalPos.X] = world.CellGoal
	before := world.AbsInt(wm.Pos.X-w.Maze.GoalPos.X) +
		world.AbsInt(wm.Pos.Y-w.Maze.GoalPos.Y)
	if before <= WumpusGoalPatrolRadius {
		t.Fatalf("test setup: wumpus already inside patrol radius (d=%d)", before)
	}
	got := seekOrPatrol(w, wm)
	after := world.AbsInt(got.X-w.Maze.GoalPos.X) +
		world.AbsInt(got.Y-w.Maze.GoalPos.Y)
	if after >= before {
		t.Errorf("seekOrPatrol didn't head toward goal: d went %d → %d", before, after)
	}
}

// TestSeekOrPatrol_InsideRadiusWanders: when the wumpus is already
// within the patrol radius, seekOrPatrol should defer to
// RandomNeighbor (no forced goal-step). We test by asserting the
// returned cell is Moore-adjacent and the function doesn't crash.
func TestSeekOrPatrol_InsideRadiusWanders(t *testing.T) {
	w := newConfiguredWorld(401)
	w.Maze.GoalPos = world.Pos{X: 40, Y: 40}
	wm := &world.Wumpus{Pos: world.Pos{X: 42, Y: 42}, Alive: true} // d=4 inside radius
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = wm
	for y := 39; y <= 43; y++ {
		for x := 39; x <= 43; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	got := seekOrPatrol(w, wm)
	dx := got.X - wm.Pos.X
	dy := got.Y - wm.Pos.Y
	if got != wm.Pos && (dx < -1 || dx > 1 || dy < -1 || dy > 1) {
		t.Errorf("inside-radius patrol returned non-neighbor %v from %v", got, wm.Pos)
	}
}

// TestBayesianHunt_FallbackSeeksGoal: when commitsToHunt fires false
// (aggressiveness=0), bayesianHunt's fallback must seek-toward-goal
// rather than random-wander. Position the wumpus far from goal so
// the seek path is observable.
func TestBayesianHunt_FallbackSeeksGoal(t *testing.T) {
	w := newConfiguredWorld(402)
	w.Maze.GoalPos = world.Pos{X: 40, Y: 40}
	for y := 10; y <= 40; y++ {
		for x := 10; x <= 40; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	wm := &world.Wumpus{
		Pos:            world.Pos{X: 10, Y: 10},
		Alive:          true,
		Aggressiveness: 0, // never commits to hunt
		HuntMode:       world.WumpusHuntBayesian,
	}
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = wm
	got := bayesianHunt(w, wm)
	before := world.AbsInt(wm.Pos.X-w.Maze.GoalPos.X) +
		world.AbsInt(wm.Pos.Y-w.Maze.GoalPos.Y)
	after := world.AbsInt(got.X-w.Maze.GoalPos.X) +
		world.AbsInt(got.Y-w.Maze.GoalPos.Y)
	if after >= before {
		t.Errorf("non-pursuing bayesianHunt didn't seek goal: d went %d → %d",
			before, after)
	}
}

// TestIsAgentLabel_AllBranches covers each branch (renamed to avoid
// collision with the existing TestIsAgentLabel in wumpus_test.go).
func TestIsAgentLabel_AllBranches(t *testing.T) {
	for _, c := range []rune{'1', '5', '9', 'A', 'B', 'C'} {
		if !isAgentLabel(c) {
			t.Errorf("isAgentLabel(%c) = false, want true", c)
		}
	}
	for _, c := range []rune{'0', 'D', 'Z', 0, 'a'} {
		if isAgentLabel(c) {
			t.Errorf("isAgentLabel(%c) = true, want false", c)
		}
	}
}
