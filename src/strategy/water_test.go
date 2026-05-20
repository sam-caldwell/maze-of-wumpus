package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

func TestNeedsWater(t *testing.T) {
	w := newConfiguredWorld(300)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '2')
	a.Water = 0
	if !NeedsWater(w, a) {
		t.Error("Water=0 and pits exist should NeedsWater")
	}
	a.Water = 1
	if NeedsWater(w, a) {
		t.Error("Water>0 should NOT NeedsWater")
	}
	a.Water = 0
	w.Maze.WaterPits = nil
	if NeedsWater(w, a) {
		t.Error("no pits should NOT NeedsWater")
	}
}

func TestNearestWaterPit(t *testing.T) {
	w := newConfiguredWorld(301)
	w.Maze.WaterPits = []world.Pos{{X: 10, Y: 10}, {X: 50, Y: 50}, {X: 80, Y: 70}}
	got, ok := NearestWaterPit(w, world.Pos{X: 12, Y: 11})
	if !ok || got != (world.Pos{X: 10, Y: 10}) {
		t.Errorf("nearest from (12,11) = %v ok=%v, want (10,10)", got, ok)
	}
	w.Maze.WaterPits = nil
	if _, ok := NearestWaterPit(w, world.Pos{X: 0, Y: 0}); ok {
		t.Error("empty WaterPits should return false")
	}
}

func TestBFSToward_SameCell(t *testing.T) {
	w := newConfiguredWorld(302)
	if p := BFSToward(w, w.Maze.GoalPos, w.Maze.GoalPos); p != nil {
		t.Errorf("same-cell BFSToward = %v, want nil", p)
	}
}

func TestBFSToward_Unreachable(t *testing.T) {
	w := newConfiguredWorld(303)
	start := world.Pos{X: 40, Y: 40}
	target := world.Pos{X: 60, Y: 60}
	w.Maze.Cells[start.Y][start.X] = world.CellPath
	w.Maze.Cells[target.Y][target.X] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[start.Y+d.Y][start.X+d.X] = world.CellWall
		w.Maze.Cells[target.Y+d.Y][target.X+d.X] = world.CellWall
	}
	if p := BFSToward(w, start, target); p != nil {
		t.Errorf("unreachable BFSToward returned %v, want nil", p)
	}
}

func TestWaterOverride_TriggersWhenAppropriate(t *testing.T) {
	// Try a handful of seeds: each yields different fire-pit / water
	// layout. The override must fire on at least one. Partial-
	// observability now applies: the agent must have "seen" a water
	// pit AND the corridor between it and itself. We hand-seed both.
	for seed := int64(304); seed < 320; seed++ {
		w := newConfiguredWorld(seed)
		w.EnableHazards()
		killAllWumpus(w)
		a := world.SpawnAgentForTest(w, '4')
		a.Water = 0
		if len(w.Maze.WaterPits) == 0 {
			continue
		}
		// Pretend agent has perceived every walkable cell so the
		// water-override BFS can route through them. This isolates
		// the test from the perception model — we're verifying
		// override mechanics, not exploration coverage.
		if a.KnownCells == nil {
			a.KnownCells = map[world.Pos]bool{}
		}
		for y := 0; y < world.BoardHeight; y++ {
			for x := 0; x < world.BoardWidth; x++ {
				if w.Maze.IsWalkable(world.Pos{X: x, Y: y}) {
					a.KnownCells[world.Pos{X: x, Y: y}] = true
				}
			}
		}
		step, ok := WaterOverride(w, a)
		if !ok {
			continue
		}
		dx, dy := step.X-a.Pos.X, step.Y-a.Pos.Y
		if world.AbsInt(dx)+world.AbsInt(dy) != 1 {
			t.Errorf("override step %v is not a cardinal neighbor of %v", step, a.Pos)
		}
		return
	}
	t.Fatal("no seed produced a reachable water pit")
}

func TestWaterOverride_SkipsWhenWatered(t *testing.T) {
	w := newConfiguredWorld(305)
	a := world.SpawnAgentForTest(w, '4')
	a.Water = 1
	if _, ok := WaterOverride(w, a); ok {
		t.Error("override should not fire when agent has water")
	}
}

func TestWaterOverride_NoPits(t *testing.T) {
	w := newConfiguredWorld(306)
	a := world.SpawnAgentForTest(w, '4')
	a.Water = 0
	w.Maze.WaterPits = nil
	if _, ok := WaterOverride(w, a); ok {
		t.Error("override should not fire when no water pits exist")
	}
}

// TestNeedsWater_RespectsDisabledFlag: when WaterPitsDisabled is
// set, NeedsWater returns false even with low water + pits present.
func TestNeedsWater_RespectsDisabledFlag(t *testing.T) {
	w := newConfiguredWorld(313)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '2')
	a.Water = 0
	if !NeedsWater(w, a) {
		t.Fatal("precondition: NeedsWater should be true")
	}
	w.WaterPitsDisabled = true
	if NeedsWater(w, a) {
		t.Error("NeedsWater should be false when WaterPitsDisabled is set")
	}
}

func TestWaterOverride_NoPathToWater(t *testing.T) {
	w := newConfiguredWorld(307)
	a := world.SpawnAgentForTest(w, '4')
	a.Water = 0
	// Place the only water pit unreachable (boxed off).
	target := world.Pos{X: 40, Y: 40}
	w.Maze.Cells[target.Y][target.X] = world.CellWaterPit
	for _, d := range world.Cardinals {
		w.Maze.Cells[target.Y+d.Y][target.X+d.X] = world.CellWall
	}
	w.Maze.WaterPits = []world.Pos{target}
	if _, ok := WaterOverride(w, a); ok {
		t.Error("override should not fire when path to water doesn't exist")
	}
}

func TestDirectionAction(t *testing.T) {
	from := world.Pos{X: 5, Y: 5}
	tests := []struct {
		to   world.Pos
		want int
	}{
		{world.Pos{X: 5, Y: 4}, 0}, // up
		{world.Pos{X: 5, Y: 6}, 1}, // down
		{world.Pos{X: 4, Y: 5}, 2}, // left
		{world.Pos{X: 6, Y: 5}, 3}, // right
		{world.Pos{X: 9, Y: 9}, 0}, // not adjacent — falls back to 0
	}
	for _, tc := range tests {
		got := directionAction(from, tc.to)
		if got != tc.want {
			t.Errorf("directionAction(%v, %v) = %d, want %d", from, tc.to, got, tc.want)
		}
	}
}

// TestBFSStrategy_TargetsWaterWhenLow: B with Water=0 should plan
// toward a water pit instead of the goal.
func TestBFSStrategy_TargetsWaterWhenLow(t *testing.T) {
	w := newConfiguredWorld(308)
	w.EnableHazards()
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '2')
	a.Water = 0
	a.Plan = nil
	_ = BFSStrategy(w, a)
	if len(a.Plan) == 0 {
		t.Skip("no plan produced (no water pits?)")
	}
	dest := a.Plan[len(a.Plan)-1]
	// Plan should end at a water pit, not the goal.
	if dest == w.Maze.GoalPos {
		t.Errorf("plan ended at goal; expected a water pit")
	}
}

// TestDFSStrategy_TargetsWaterWhenLow mirrors the BFS test for C.
func TestDFSStrategy_TargetsWaterWhenLow(t *testing.T) {
	w := newConfiguredWorld(309)
	w.EnableHazards()
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '3')
	a.Water = 0
	a.Plan = nil
	_ = DFSStrategy(w, a)
	if len(a.Plan) == 0 {
		t.Skip("no plan produced")
	}
	dest := a.Plan[len(a.Plan)-1]
	if dest == w.Maze.GoalPos {
		t.Errorf("plan ended at goal; expected a water pit")
	}
}

// TestQLearningStrategy_WaterOverride: D's chosen target should be
// the first step of a BFS toward water when low.
// TestDQNStrategy_WaterOverride: water-out + pit-exists → DQN must
// step toward the nearest water pit instead of its network choice.
func TestDQNStrategy_WaterOverride(t *testing.T) {
	w := newConfiguredWorld(311)
	w.EnableHazards()
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '5')
	a.Water = 0
	if len(w.Maze.WaterPits) == 0 {
		t.Skip("no water pits")
	}
	step, ok := WaterOverride(w, a)
	if !ok {
		t.Skip("no override available")
	}
	got := DQNStrategy(w, a)
	if got != step {
		t.Errorf("DQN step = %v, want override step %v", got, step)
	}
}

// TestBayesianStrategy_TargetsWaterWhenLow: A's wwPlanPath should
// produce a water-targeted plan when (a) the agent is out of water,
// (b) the path to a water pit is STRICT-safe, and (c) such a path
// exists. We grant A full belief over a chain of cells to a water
// pit so the strict predicate succeeds.
func TestBayesianStrategy_TargetsWaterWhenLow(t *testing.T) {
	w := newConfiguredWorld(312)
	w.EnableHazards()
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '3')
	a.Water = 0
	// Pretend every walkable cell is SafeFromPit and Observed, so
	// strict-safe BFS succeeds.
	for y := 0; y < world.BoardHeight; y++ {
		for x := 0; x < world.BoardWidth; x++ {
			p := world.Pos{X: x, Y: y}
			if w.Maze.IsWalkable(p) {
				a.Beliefs.SafeFromPit[p] = true
				a.Beliefs.Observed[p] = true
			}
		}
	}
	a.Plan = wwPlanPath(w, a)
	if len(a.Plan) == 0 {
		t.Skip("no plan produced")
	}
	dest := a.Plan[len(a.Plan)-1]
	if dest == w.Maze.GoalPos {
		t.Errorf("A's plan ended at goal; expected a water pit")
	}
}
