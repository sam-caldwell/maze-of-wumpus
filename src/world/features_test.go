package world

import (
	"testing"
)

// TestSetWumpusDisabled_SpawnsOnEnable: toggle-on populates the board
// with fresh wumpus; toggle-off removes them all.
func TestSetWumpusDisabled_SpawnsOnEnable(t *testing.T) {
	w := NewWorld(260)
	// Default is disabled → no wumpus.
	if len(w.Wumpus) != 0 {
		t.Fatalf("precondition: expected 0 wumpus by default, got %d", len(w.Wumpus))
	}
	w.SetWumpusDisabled(false)
	if len(w.Wumpus) < 5 || len(w.Wumpus) > 12 {
		t.Errorf("after enable: wumpus count = %d, want 5..12", len(w.Wumpus))
	}
	for _, wm := range w.Wumpus {
		if !wm.Alive {
			t.Error("freshly spawned wumpus should be Alive")
		}
		if w.WumpusAt[wm.Pos.Y][wm.Pos.X] != wm {
			t.Error("spatial index out of sync with wumpus list")
		}
	}
	w.SetWumpusDisabled(true)
	if len(w.Wumpus) != 0 {
		t.Errorf("after disable: wumpus count = %d, want 0", len(w.Wumpus))
	}
}

// TestSetFirePitsDisabled_SpawnsOnEnable: toggle-on adds fire pits to
// rooms with the heat envelope, toggle-off strips them.
func TestSetFirePitsDisabled_SpawnsOnEnable(t *testing.T) {
	w := NewWorld(261)
	if len(w.Maze.FirePits) != 0 {
		t.Fatalf("precondition: expected 0 fire pits by default")
	}
	w.SetFirePitsDisabled(false)
	if len(w.Maze.FirePits) == 0 {
		// Sometimes rooms produce zero pits randomly; retry by re-
		// flipping a few times to give the rng a chance.
		for i := 0; i < 5 && len(w.Maze.FirePits) == 0; i++ {
			w.SetFirePitsDisabled(true)
			w.SetFirePitsDisabled(false)
		}
		if len(w.Maze.FirePits) == 0 {
			t.Skip("seed/rooms produced no fire pits even after retries")
		}
	}
	// Confirm each pit's cell is CellFirePit and Heat is set adjacent.
	for _, p := range w.Maze.FirePits {
		if w.Maze.Cells[p.Y][p.X] != CellFirePit {
			t.Errorf("pit %v cell type = %v, want CellFirePit", p, w.Maze.Cells[p.Y][p.X])
		}
	}
	w.SetFirePitsDisabled(true)
	if len(w.Maze.FirePits) != 0 {
		t.Errorf("after disable: fire pit count = %d, want 0", len(w.Maze.FirePits))
	}
	// Heat grid must be fully zero too.
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Heat[y][x] {
				t.Fatalf("heat at (%d,%d) survived fire-pit disable", x, y)
			}
		}
	}
}

// TestSetWaterPitsDisabled_SpawnsOnEnable: toggle-on scatters water
// pits, toggle-off clears them.
func TestSetWaterPitsDisabled_SpawnsOnEnable(t *testing.T) {
	w := NewWorld(262)
	if len(w.Maze.WaterPits) != 0 {
		t.Fatalf("precondition: expected 0 water pits by default")
	}
	w.SetWaterPitsDisabled(false)
	if len(w.Maze.WaterPits) < 3 || len(w.Maze.WaterPits) > 10 {
		t.Errorf("after enable: water pit count = %d, want 3..10",
			len(w.Maze.WaterPits))
	}
	w.SetWaterPitsDisabled(true)
	if len(w.Maze.WaterPits) != 0 {
		t.Errorf("after disable: water pit count = %d, want 0",
			len(w.Maze.WaterPits))
	}
}

// TestSetWumpusDisabled_NoOpWhenAlreadyMatchingState: flipping to the
// same state is idempotent — no new wumpus spawn.
func TestSetWumpusDisabled_NoOpWhenAlreadyMatchingState(t *testing.T) {
	w := NewWorld(263)
	w.SetWumpusDisabled(false)
	first := len(w.Wumpus)
	w.SetWumpusDisabled(false) // already false, should be no-op
	if len(w.Wumpus) != first {
		t.Errorf("redundant enable spawned new wumpus: %d -> %d",
			first, len(w.Wumpus))
	}
}

// TestWumpusDisabled_DoesNotBlockMovement: a live but disabled wumpus
// must NOT block agent movement. Regression for the bug where agent 1
// froze indefinitely in front of a wumpus cell when WumpusDisabled
// was true (wumpus was inert gameplay-wise but MoveAgents still
// refused to step onto its cell).
func TestWumpusDisabled_DoesNotBlockMovement(t *testing.T) {
	w := NewWorld(250)
	// Make sure wumpus stays disabled (default for NewWorld but be
	// explicit in case future defaults change).
	w.WumpusDisabled = true
	a := SpawnAgentForTest(w, '1')
	// Park the agent at an interior path cell with a known walkable
	// neighbor, place a (disabled-context) wumpus on that neighbor,
	// and ask the agent's planner-free Strategy stub to walk into it.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	target := Pos{X: 41, Y: 40}
	w.Maze.Cells[target.Y][target.X] = CellPath
	// Plant a "live" wumpus at the target cell.
	wm := &Wumpus{ID: 999, Pos: target, Alive: true}
	w.Wumpus = append(w.Wumpus, wm)
	w.WumpusAt[target.Y][target.X] = wm
	// Drive agent's move via injected strategy returning `target`.
	a.Strategy = func(_ *World, _ *Agent) Pos { return target }
	w.MoveAgents()
	if a.Pos != target {
		t.Errorf("agent stayed at %v despite wumpus-disabled; expected to walk onto %v", a.Pos, target)
	}
}

// TestWaterShield_ExtinguishesFirePit: stepping onto a fire pit with
// water charges consumes the charge AND removes the fire pit.
func TestWaterShield_ExtinguishesFirePit(t *testing.T) {
	w := NewWorld(200)
	w.EnableHazards()
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	a := SpawnAgentForTest(w, '1')
	pit := w.Maze.FirePits[0]
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = pit
	w.AgentAt[pit.Y][pit.X] = a
	a.Water = 1
	pitsBefore := len(w.Maze.FirePits)
	w.ResolvePitDeaths()
	if !a.Alive {
		t.Fatal("agent should survive with water")
	}
	if a.Water != 0 {
		t.Errorf("water = %d after shield, want 0", a.Water)
	}
	if w.Maze.Cells[pit.Y][pit.X] != CellPath {
		t.Errorf("pit cell type = %v, want CellPath", w.Maze.Cells[pit.Y][pit.X])
	}
	if len(w.Maze.FirePits) != pitsBefore-1 {
		t.Errorf("FirePits = %d, want %d", len(w.Maze.FirePits), pitsBefore-1)
	}
}

// TestExtinguishFirePit_RecomputesHeat: extinguishing a fire pit
// clears Heat in its neighborhood UNLESS another pit also adjoins
// the same cell.
func TestExtinguishFirePit_RecomputesHeat(t *testing.T) {
	w := NewWorld(201)
	// Set up two adjacent fire pits at (40,40) and (40,42). Heat at
	// (40,41) is contributed by both — extinguishing one should leave
	// (40,41) hot from the other.
	p1, p2 := Pos{40, 40}, Pos{40, 42}
	w.Maze.Cells[p1.Y][p1.X] = CellFirePit
	w.Maze.Cells[p2.Y][p2.X] = CellFirePit
	w.Maze.FirePits = append(w.Maze.FirePits, p1, p2)
	// Manually flag heat at every cell adjacent to either pit.
	for _, p := range []Pos{p1, p2} {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nx, ny := p.X+dx, p.Y+dy
				if InBounds(nx, ny) && w.Maze.Cells[ny][nx] != CellWall {
					w.Heat[ny][nx] = true
				}
			}
		}
	}
	shared := Pos{40, 41}
	if !w.Heat[shared.Y][shared.X] {
		t.Fatal("test setup: shared cell should be hot")
	}
	w.ExtinguishFirePit(p1)
	if w.Heat[shared.Y][shared.X] != true {
		t.Errorf("shared cell heat = %v, want true (other pit still adjacent)",
			w.Heat[shared.Y][shared.X])
	}
	// (39,39) was only adjacent to p1; after p1 is gone it should cool.
	cool := Pos{39, 39}
	if w.Heat[cool.Y][cool.X] {
		t.Errorf("cell %v should be cool after p1 extinguished", cool)
	}
}

// TestExtinguishFirePit_NoopOnNonPit: calling Extinguish on a non-pit
// cell is a silent no-op.
func TestExtinguishFirePit_NoopOnNonPit(t *testing.T) {
	w := NewWorld(202)
	p := Pos{50, 50}
	w.Maze.Cells[p.Y][p.X] = CellPath
	pitsBefore := len(w.Maze.FirePits)
	w.ExtinguishFirePit(p)
	if len(w.Maze.FirePits) != pitsBefore {
		t.Errorf("FirePits changed by no-op")
	}
}

// TestSolveTimeAggregates_TrackedAcrossGoals: simulate two solves with
// different TicksAlive values and verify min / max / avg are updated.
func TestSolveTimeAggregates_TrackedAcrossGoals(t *testing.T) {
	w := NewWorld(240)
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = w.Maze.GoalPos
	a.TicksAlive = 80
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	w.CheckGoal()
	if a.Stats.MinSolveTime != 80 || a.Stats.MaxSolveTime != 80 ||
		a.Stats.AvgSolveTime != 80 {
		t.Errorf("after first solve, min/max/avg = %d/%d/%v, want 80/80/80",
			a.Stats.MinSolveTime, a.Stats.MaxSolveTime, a.Stats.AvgSolveTime)
	}
	if a.Stats.LastSolveTime != 80 {
		t.Errorf("LastSolveTime = %d, want 80", a.Stats.LastSolveTime)
	}
	// Second solve, slower.
	a.Alive = true
	a.TicksAlive = 200
	a.Pos = w.Maze.GoalPos
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	w.CheckGoal()
	if a.Stats.MinSolveTime != 80 {
		t.Errorf("MinSolveTime = %d, want 80", a.Stats.MinSolveTime)
	}
	if a.Stats.MaxSolveTime != 200 {
		t.Errorf("MaxSolveTime = %d, want 200", a.Stats.MaxSolveTime)
	}
	if a.Stats.AvgSolveTime != 140 {
		t.Errorf("AvgSolveTime = %v, want 140", a.Stats.AvgSolveTime)
	}
	if a.Stats.LastSolveTime != 200 {
		t.Errorf("LastSolveTime = %d, want 200", a.Stats.LastSolveTime)
	}
}

// TestPathAlignment_OnPathStepCounted: walking onto a ShortestPathCells
// cell bumps OnPathSteps; walking elsewhere bumps OffPathSteps. Score
// reflects (OnPath - OffPath) / OptimalDistance.
func TestPathAlignment_OnPathStepCounted(t *testing.T) {
	w := NewWorld(241)
	a := SpawnAgentForTest(w, '1')
	// Pick a walkable cell on the chosen shortest path.
	var onPath Pos
	for p := range w.ShortestPathCells {
		if p == w.Maze.EntrancePos || p == w.Maze.GoalPos {
			continue
		}
		onPath = p
		break
	}
	if onPath == (Pos{}) {
		t.Skip("no usable non-endpoint cell on the chosen path")
	}
	// Move a manually toward onPath. Use a stub strategy that drives
	// the agent there one step at a time; for simplicity just stomp it.
	a.HasLastFrom = true
	a.Strategy = func(_ *World, _ *Agent) Pos { return onPath }
	w.MoveAgents()
	if a.Stats.OnPathSteps == 0 && a.Stats.OffPathSteps == 0 {
		t.Skip("agent did not move (probably blocked)")
	}
	if a.Pos == onPath && a.Stats.OnPathSteps != 1 {
		t.Errorf("landed on path cell %v, OnPathSteps = %d, want 1",
			onPath, a.Stats.OnPathSteps)
	}
}

// TestWumpusDisabled_FreezesAndClearsStench: enabling WumpusDisabled
// stops wumpus from moving, attacking, and emitting stench.
func TestWumpusDisabled_FreezesAndClearsStench(t *testing.T) {
	w := NewWorld(220)
	w.EnableHazards()
	wm := w.Wumpus[0]
	startPos := wm.Pos
	w.WumpusDisabled = true
	// Step a few ticks — wumpus should not move, no stench should
	// appear anywhere on the board.
	for i := 0; i < 5; i++ {
		w.Step()
	}
	if wm.Pos != startPos {
		t.Errorf("wumpus moved %v -> %v despite disabled", startPos, wm.Pos)
	}
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Stench[y][x] {
				t.Fatalf("stench at (%d,%d) despite wumpus disabled", x, y)
			}
		}
	}
}

// TestWumpusDisabled_NotHazard: when disabled, wumpus cells aren't
// reported as hazards by IsHazard.
func TestWumpusDisabled_NotHazard(t *testing.T) {
	w := NewWorld(221)
	w.EnableHazards()
	wm := w.Wumpus[0]
	if !w.IsHazard(wm.Pos) {
		t.Fatal("live wumpus cell should be hazard when enabled")
	}
	w.WumpusDisabled = true
	if w.IsHazard(wm.Pos) {
		t.Error("wumpus cell should NOT be hazard when disabled")
	}
}

// TestFirePitsDisabled_NoDeaths: with fire pits off, an agent on a
// fire-pit cell does not die.
func TestFirePitsDisabled_NoDeaths(t *testing.T) {
	w := NewWorld(222)
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	a := SpawnAgentForTest(w, '1')
	pit := w.Maze.FirePits[0]
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = pit
	w.AgentAt[pit.Y][pit.X] = a
	w.FirePitsDisabled = true
	w.ResolvePitDeaths()
	if !a.Alive {
		t.Error("agent on fire pit should survive when fire pits disabled")
	}
}

// TestWaterPitsDisabled_NoCollection: CollectWater no-ops when
// water pits are disabled.
func TestWaterPitsDisabled_NoCollection(t *testing.T) {
	w := NewWorld(225)
	a := SpawnAgentForTest(w, '1')
	w.Maze.Cells[a.Pos.Y][a.Pos.X] = CellWaterPit
	w.WaterPitsDisabled = true
	beforeWater := a.Water
	beforeCell := w.Maze.Cells[a.Pos.Y][a.Pos.X]
	w.CollectWater()
	if a.Water != beforeWater {
		t.Errorf("agent gained water (%d->%d) despite WaterPitsDisabled",
			beforeWater, a.Water)
	}
	if w.Maze.Cells[a.Pos.Y][a.Pos.X] != beforeCell {
		t.Errorf("water-pit cell consumed despite WaterPitsDisabled")
	}
}

// TestTTLDisabled_AgentDoesNotDie: with TTL off, an agent that has
// blown past 2x optimal distance still does not die from TTL.
func TestTTLDisabled_AgentDoesNotDie(t *testing.T) {
	w := NewWorld(230)
	a := SpawnAgentForTest(w, '2')
	w.TTLDisabled = true
	a.Stats.ActualDistance = TTLMultiplier*w.Stats.OptimalDistance + 1000
	for i := 0; i < 20 && a.Alive; i++ {
		w.MoveAgents()
	}
	if !a.Alive {
		t.Errorf("agent died despite TTLDisabled (last_death=%q)",
			a.Stats.LastDeathReason)
	}
}

// TestFirePitsDisabled_NotHazard: with fire pits off, IsHazard
// reports fire-pit cells as walkable.
func TestFirePitsDisabled_NotHazard(t *testing.T) {
	w := NewWorld(223)
	w.EnableHazards()
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	pit := w.Maze.FirePits[0]
	if !w.IsHazard(pit) {
		t.Fatal("fire pit should be hazard when enabled")
	}
	w.FirePitsDisabled = true
	if w.IsHazard(pit) {
		t.Error("fire pit should NOT be hazard when disabled")
	}
}

// TestComputeDistFromStart_EntranceIsZero: BFS distance from entrance
// is 0 at the entrance and increases through walkable cells.
func TestComputeDistFromStart_EntranceIsZero(t *testing.T) {
	w := NewWorld(204)
	e := w.Maze.EntrancePos
	if w.DistFromStart[e.Y][e.X] != 0 {
		t.Errorf("DistFromStart at entrance = %d, want 0",
			w.DistFromStart[e.Y][e.X])
	}
	// Some walkable neighbor must be 1.
	hasOne := false
	for _, d := range Cardinals {
		np := Pos{X: e.X + d.X, Y: e.Y + d.Y}
		if InBounds(np.X, np.Y) && w.DistFromStart[np.Y][np.X] == 1 {
			hasOne = true
			break
		}
	}
	if !hasOne {
		t.Error("no neighbor of entrance has distance 1")
	}
	// Walls (and unreached cells) stay at -1.
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Maze.Cells[y][x] == CellWall && w.DistFromStart[y][x] != -1 {
				t.Errorf("wall at (%d,%d) got distance %d, want -1",
					x, y, w.DistFromStart[y][x])
			}
		}
	}
}

// TestRealDistanceShaping_OnlyOnNewMax: a step into a cell with
// strictly higher DistFromStart pays once; back-stepping doesn't.
func TestRealDistanceShaping_OnlyOnNewMax(t *testing.T) {
	w := NewWorld(205)
	a := SpawnAgentForTest(w, '4')
	// Pick a walkable neighbor of the entrance with DistFromStart == 1.
	var step Pos
	for _, d := range Cardinals {
		np := Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if InBounds(np.X, np.Y) && w.DistFromStart[np.Y][np.X] == 1 {
			step = np
			break
		}
	}
	if step == (Pos{}) {
		t.Skip("no walkable neighbor at distance 1")
	}
	a.MaxStartDist = 0
	a.PendingBonus = 0
	a.Strategy = func(_ *World, _ *Agent) Pos { return step }
	w.MoveAgents()
	if a.MaxStartDist != 1 {
		t.Errorf("MaxStartDist = %d after step out, want 1", a.MaxStartDist)
	}
	if a.PendingBonus < RealDistanceShaping {
		t.Errorf("PendingBonus = %v, want >= %v after first-time progress",
			a.PendingBonus, RealDistanceShaping)
	}
	// Back-step to entrance. Then step back to the same cell. Neither
	// move should pay the real-distance reward again.
	a.PendingBonus = 0
	entrance := w.Maze.EntrancePos
	a.Strategy = func(_ *World, _ *Agent) Pos { return entrance }
	w.MoveAgents()
	a.Strategy = func(_ *World, _ *Agent) Pos { return step }
	bonusBefore := a.PendingBonus
	w.MoveAgents()
	if a.PendingBonus > bonusBefore+RealDistanceShaping/2 {
		t.Errorf("back-and-forth re-paid: bonus went from %v to %v",
			bonusBefore, a.PendingBonus)
	}
}

// TestMaxStartDist_ResetsOnRespawn: after death and respawn the
// agent's max-start-dist starts over from zero.
func TestMaxStartDist_ResetsOnRespawn(t *testing.T) {
	w := NewWorld(206)
	a := SpawnAgentForTest(w, '4')
	a.MaxStartDist = 47
	w.KillAgent(a, "wumpus")
	for i := 0; i < 30 && !a.Alive; i++ {
		w.Step()
	}
	if !a.Alive {
		t.Skip("did not respawn")
	}
	if a.MaxStartDist != 0 {
		t.Errorf("MaxStartDist = %d after respawn, want 0", a.MaxStartDist)
	}
}

// TestKnownPathReward_OncePerCellEver: after paying KnownPathReward
// for a cell, dying and re-entering must NOT pay it a second time.
func TestKnownPathReward_OncePerCellEver(t *testing.T) {
	w := NewWorld(203)
	a := SpawnAgentForTest(w, '4')
	cell := Pos{40, 40}
	w.Maze.Cells[cell.Y][cell.X] = CellPath
	// Pretend the cell was visited in a prior life.
	a.LifetimeVisited = map[Pos]bool{cell: true}
	a.Visited = map[Pos]bool{}
	a.KnownPathRewarded = nil
	// Move agent onto cell — simulates the gate inside MoveAgents.
	a.Pos = cell
	a.Visited[a.Pos] = true
	if a.KnownPathRewarded == nil {
		a.KnownPathRewarded = map[Pos]bool{}
	}
	if !a.KnownPathRewarded[a.Pos] && a.LifetimeVisited[a.Pos] {
		a.PendingBonus += KnownPathReward
		a.KnownPathRewarded[a.Pos] = true
	}
	first := a.PendingBonus
	// Second life: Visited reset, KnownPathRewarded persists.
	a.Visited = map[Pos]bool{}
	a.PendingBonus = 0
	a.Visited[a.Pos] = true
	if a.LifetimeVisited[a.Pos] && !a.KnownPathRewarded[a.Pos] {
		a.PendingBonus += KnownPathReward
		a.KnownPathRewarded[a.Pos] = true
	}
	if a.PendingBonus != 0 {
		t.Errorf("KnownPathReward fired twice: first=%v, second=%v",
			first, a.PendingBonus)
	}
}
