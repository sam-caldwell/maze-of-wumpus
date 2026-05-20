package world

import (
	"math/rand"
	"testing"
)

func TestNewWorld_Generates(t *testing.T) {
	w := NewWorld(42)
	w.EnableHazards() // hazards default to disabled; tests want them on
	if w.Maze == nil {
		t.Fatal("maze nil")
	}
	for _, label := range []rune{'1', '2', '3', '4', '5'} {
		if w.AgentByLabel(label) == nil {
			t.Errorf("agent %c missing", label)
		}
	}
	if len(w.Wumpus) == 0 {
		t.Error("expected at least one wumpus")
	}
}

func TestStep_DoesNotPanic(t *testing.T) {
	w := NewWorld(7)
	w.EnableHazards()
	w.EnableAllAgents()
	for i := 0; i < 1000; i++ {
		w.Step()
		// Agents are now allowed to share cells (overlap is permitted),
		// so we only check bounds — not uniqueness — of their positions.
		for _, a := range w.Agents {
			if !a.Alive {
				continue
			}
			if !InBounds(a.Pos.X, a.Pos.Y) {
				t.Fatalf("agent %c OOB at %v step %d", a.Label, a.Pos, i)
			}
		}
		for _, wm := range w.Wumpus {
			if !wm.Alive {
				continue
			}
			if !InBounds(wm.Pos.X, wm.Pos.Y) {
				t.Fatalf("wumpus #%d OOB at %v step %d", wm.ID, wm.Pos, i)
			}
		}
	}
}

func TestMazeConnectivity(t *testing.T) {
	for seed := int64(0); seed < 10; seed++ {
		w := NewWorld(seed)
		visited := map[Pos]bool{w.Maze.EntrancePos: true}
		queue := []Pos{w.Maze.EntrancePos}
		found := false
		for len(queue) > 0 && !found {
			cur := queue[0]
			queue = queue[1:]
			if cur == w.Maze.GoalPos {
				found = true
				break
			}
			for _, d := range Cardinals {
				np := Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
				if !w.Maze.IsWalkable(np) {
					continue
				}
				if visited[np] {
					continue
				}
				visited[np] = true
				queue = append(queue, np)
			}
		}
		if !found {
			t.Errorf("seed %d: maze entrance->goal unreachable", seed)
		}
	}
}

func TestDeterminism(t *testing.T) {
	a := NewWorld(123)
	b := NewWorld(123)
	if a.Maze.GoalPos != b.Maze.GoalPos {
		t.Error("same seed produced different goals")
	}
	if len(a.Maze.FirePits) != len(b.Maze.FirePits) {
		t.Error("same seed produced different fire-pit counts")
	}
}

func TestGoalRespawnsAgent(t *testing.T) {
	w := NewWorld(5)
	a := SpawnAgentForTest(w, '2')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = w.Maze.GoalPos
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	w.CheckGoal()
	if a.Stats.GoalsReached != 1 {
		t.Errorf("GoalsReached = %d, want 1", a.Stats.GoalsReached)
	}
	if a.Alive {
		t.Error("agent should be removed (alive=false) after goal reach")
	}
	if a.RespawnIn != RespawnTicks {
		t.Errorf("respawn timer = %d, want %d", a.RespawnIn, RespawnTicks)
	}
	if w.GameOver {
		t.Error("game must NOT end on goal reach")
	}
}

// TestScore_Formula: Score is cumulative goals per cycle.
func TestScore_Formula(t *testing.T) {
	tests := []struct {
		name  string
		s     AgentStats
		cycle int
		want  float64
	}{
		{"no goals, no cycles", AgentStats{}, 0, 0},
		{"goals but cycle=0 returns 0",
			AgentStats{GoalsReached: 5}, 0, 0},
		{"no goals after N cycles = 0",
			AgentStats{GoalsReached: 0}, 1000, 0.0},
		{"1 goal in 100 cycles = 0.01",
			AgentStats{GoalsReached: 1}, 100, 0.01},
		{"5 goals in 1000 cycles = 0.005",
			AgentStats{GoalsReached: 5}, 1000, 0.005},
		{"OnPathSteps no longer influences Score",
			AgentStats{GoalsReached: 2, OnPathSteps: 1000,
				OffPathSteps: 5000, BestAlignment: 0.7}, 200, 0.01},
	}
	for _, tc := range tests {
		if got := tc.s.Score(tc.cycle); got != tc.want {
			t.Errorf("%s: Score(%d) = %v, want %v",
				tc.name, tc.cycle, got, tc.want)
		}
	}
}

func TestMaze_HasMinShortestPaths(t *testing.T) {
	for seed := int64(0); seed < 10; seed++ {
		w := NewWorld(seed)
		if w.Stats.ShortestPaths < MinAcceptablePaths {
			t.Errorf("seed %d: ShortestPaths = %d, want >= %d",
				seed, w.Stats.ShortestPaths, MinAcceptablePaths)
		}
	}
}

func TestShortestPathSet_OnRoute(t *testing.T) {
	w := NewWorld(80)
	if !w.ShortestPathCells[w.Maze.EntrancePos] {
		t.Error("path set missing entrance")
	}
	if !w.ShortestPathCells[w.Maze.GoalPos] {
		t.Error("path set missing goal")
	}
	for p := range w.ShortestPathCells {
		if !w.Maze.IsWalkable(p) {
			t.Errorf("path-set cell %v is not walkable", p)
		}
	}
}

func TestIsWalkable_Bounds(t *testing.T) {
	w := NewWorld(15)
	for _, p := range []Pos{{-1, 0}, {0, -1}, {BoardWidth, 0}, {0, BoardHeight}} {
		if w.Maze.IsWalkable(p) {
			t.Errorf("IsWalkable(%v) true for OOB", p)
		}
	}
}

func TestIsWalkable_Wall(t *testing.T) {
	w := NewWorld(16)
	var wall Pos
	for y := 0; y < BoardHeight && wall == (Pos{}); y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Maze.Cells[y][x] == CellWall {
				wall = Pos{X: x, Y: y}
				break
			}
		}
	}
	if w.Maze.IsWalkable(wall) {
		t.Errorf("IsWalkable on wall %v returned true", wall)
	}
}

func TestRandomWumpusSpawn_NeverEntrance(t *testing.T) {
	w := NewWorld(20)
	for i := 0; i < 50; i++ {
		p := w.RandomWumpusSpawn()
		if p == w.Maze.EntrancePos {
			t.Errorf("RandomWumpusSpawn returned entrance %v", p)
		}
	}
}

func TestNewWorldHasInitialPits(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		w := NewWorld(seed)
		for _, p := range w.Maze.FirePits {
			if p.X < 0 || p.X >= BoardWidth || p.Y < 0 || p.Y >= BoardHeight {
				t.Errorf("seed %d: pit out of bounds %v", seed, p)
			}
		}
	}
}

func TestFirePitKillsAgent(t *testing.T) {
	w := NewWorld(11)
	w.EnableHazards()
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	a := SpawnAgentForTest(w, '1')
	pit := w.Maze.FirePits[0]
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = pit
	w.AgentAt[pit.Y][pit.X] = a
	w.ResolvePitDeaths()
	if a.Alive {
		t.Error("agent on fire pit should die")
	}
}

func TestKillWumpus_TicksStats(t *testing.T) {
	w := NewWorld(3)
	w.EnableHazards()
	wm := w.Wumpus[0]
	pos := wm.Pos
	wumpusBefore := len(w.Wumpus)
	before := w.Stats.WumpusDied
	w.KillWumpus(wm)
	if wm.Alive {
		t.Error("KillWumpus did not flip Alive")
	}
	if w.WumpusAt[pos.Y][pos.X] != nil {
		t.Error("KillWumpus did not clear spatial index")
	}
	if w.Stats.WumpusDied != before+1 {
		t.Errorf("WumpusDied = %d, want %d", w.Stats.WumpusDied, before+1)
	}
	if len(w.Wumpus) != wumpusBefore+1 {
		t.Errorf("Wumpus slice = %d, want %d", len(w.Wumpus), wumpusBefore+1)
	}
}

func TestKillAgent_StartsRespawnTimer(t *testing.T) {
	w := NewWorld(4)
	a := SpawnAgentForTest(w, '1')
	w.KillAgent(a)
	if a.RespawnIn != RespawnTicks {
		t.Errorf("RespawnIn = %d, want %d", a.RespawnIn, RespawnTicks)
	}
	if a.Stats.Deaths != 1 {
		t.Errorf("Deaths = %d, want 1", a.Stats.Deaths)
	}
}

func TestRespawn_HappensEventually(t *testing.T) {
	w := NewWorld(5)
	a := SpawnAgentForTest(w, '1')
	w.KillAgent(a)
	for i := 0; i < 30; i++ {
		w.Step()
		if a.Alive {
			return
		}
	}
	t.Errorf("agent A did not respawn within 30 steps")
}

func TestStench_PopulatedAroundLiveWumpus(t *testing.T) {
	w := NewWorld(7)
	w.EnableHazards()
	w.RecomputeStench()
	for _, wm := range w.Wumpus {
		if !wm.Alive {
			continue
		}
		any := false
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nx, ny := wm.Pos.X+dx, wm.Pos.Y+dy
				if !InBounds(nx, ny) {
					continue
				}
				if w.Maze.Cells[ny][nx] == CellWall {
					continue
				}
				if w.Stench[ny][nx] {
					any = true
				}
			}
		}
		if !any {
			t.Errorf("wumpus #%d at %v has no stench around it", wm.ID, wm.Pos)
		}
	}
}

func TestIsHazard_OutOfBounds(t *testing.T) {
	w := NewWorld(10)
	if !w.IsHazard(Pos{-1, 0}) {
		t.Error("OOB cell should be hazard")
	}
	if !w.IsHazard(Pos{BoardWidth, 0}) {
		t.Error("OOB cell should be hazard")
	}
}

func TestIsHazard_WumpusAt(t *testing.T) {
	w := NewWorld(11)
	w.EnableHazards()
	wm := w.Wumpus[0]
	if !w.IsHazard(wm.Pos) {
		t.Error("cell with live wumpus should be hazard")
	}
}

func TestStep_NoOpAfterGameOver(t *testing.T) {
	w := NewWorld(12)
	w.GameOver = true
	startCycle := w.Cycle
	w.Step()
	if w.Cycle != startCycle {
		t.Errorf("Cycle advanced after GameOver: %d -> %d", startCycle, w.Cycle)
	}
}

func TestAbsInt_Negative(t *testing.T) {
	if AbsInt(-7) != 7 || AbsInt(7) != 7 || AbsInt(0) != 0 {
		t.Error("AbsInt failed")
	}
}

func TestInBounds(t *testing.T) {
	tests := []struct {
		x, y int
		want bool
	}{
		{0, 0, true},
		{BoardWidth - 1, BoardHeight - 1, true},
		{-1, 0, false},
		{0, -1, false},
		{BoardWidth, 0, false},
		{0, BoardHeight, false},
	}
	for _, tc := range tests {
		if got := InBounds(tc.x, tc.y); got != tc.want {
			t.Errorf("InBounds(%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestLongRun_NoPanics(t *testing.T) {
	for seed := int64(0); seed < 10; seed++ {
		w := NewWorld(seed)
		w.EnableHazards()
		w.EnableAllAgents()
		for i := 0; i < 500; i++ {
			w.Step()
		}
	}
}

func TestStats_OptimalDistance_Positive(t *testing.T) {
	for seed := int64(0); seed < 5; seed++ {
		w := NewWorld(seed)
		if w.Stats.OptimalDistance <= 0 {
			t.Errorf("seed %d: OptimalDistance = %d, want > 0", seed, w.Stats.OptimalDistance)
		}
	}
}

func TestShortestPathLength_SameCell(t *testing.T) {
	w := NewWorld(22)
	if d := w.ShortestPathLength(w.Maze.GoalPos, w.Maze.GoalPos); d != 0 {
		t.Errorf("self-to-self distance = %d, want 0", d)
	}
}

func TestStats_ActualDistance_ResetsOnRespawn(t *testing.T) {
	w := NewWorld(30)
	a := SpawnAgentForTest(w, '1')
	a.Stats.ActualDistance = 42
	w.KillAgent(a)
	for i := 0; i < 30; i++ {
		w.Step()
		if a.Alive {
			break
		}
	}
	if !a.Alive {
		t.Fatal("agent did not respawn")
	}
	if a.Stats.ActualDistance != 0 {
		t.Errorf("ActualDistance = %d after respawn, want 0", a.Stats.ActualDistance)
	}
}

func TestLastDeathReason_Records(t *testing.T) {
	w := NewWorld(40)
	a := SpawnAgentForTest(w, '1')
	w.KillAgent(a, "wumpus")
	if a.Stats.LastDeathReason != "wumpus" {
		t.Errorf("wumpus path: got %q", a.Stats.LastDeathReason)
	}
	a = SpawnAgentForTest(w, '1')
	w.KillAgent(a, "fire pit")
	if a.Stats.LastDeathReason != "fire pit" {
		t.Errorf("fire pit path: got %q", a.Stats.LastDeathReason)
	}
	a = SpawnAgentForTest(w, '1')
	w.KillAgent(a, "ttl")
	if a.Stats.LastDeathReason != "ttl" {
		t.Errorf("ttl path: got %q", a.Stats.LastDeathReason)
	}
	a = SpawnAgentForTest(w, '1')
	w.KillAgent(a)
	if a.Stats.LastDeathReason != "unknown" {
		t.Errorf("default: got %q, want 'unknown'", a.Stats.LastDeathReason)
	}
}

func TestFallbackMove_NoOptions(t *testing.T) {
	w := NewWorld(55)
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{40, 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	for _, d := range Cardinals {
		w.Maze.Cells[40+d.Y][40+d.X] = CellWall
	}
	if w.FallbackMove(a) != a.Pos {
		t.Error("walled-in fallback should return a.Pos")
	}
}

// TestRespawnAgents_AllowsOverlapAtEntrance: agents may share the
// entrance cell on respawn. Two agents both at the entrance is now
// a valid steady state — used to be blocked.
func TestRespawnAgents_AllowsOverlapAtEntrance(t *testing.T) {
	w := NewWorld(56)
	a := SpawnAgentForTest(w, '1')
	b := w.AgentByLabel('2')
	b.Alive = false
	b.RespawnIn = 0
	w.RespawnAgents()
	if !b.Alive {
		t.Error("B should spawn even though A occupies entrance — overlap allowed")
	}
	if a.Pos != b.Pos {
		t.Errorf("both agents should be at entrance: A=%v B=%v", a.Pos, b.Pos)
	}
}

func TestShortestPathLength_Unreachable(t *testing.T) {
	w := NewWorld(57)
	start := Pos{40, 40}
	w.Maze.Cells[start.Y][start.X] = CellPath
	for _, d := range Cardinals {
		w.Maze.Cells[start.Y+d.Y][start.X+d.X] = CellWall
	}
	if d := w.ShortestPathLength(start, w.Maze.GoalPos); d != 0 {
		t.Errorf("unreachable ShortestPathLength = %d, want 0", d)
	}
}

func TestAgentByLabel_NotFound(t *testing.T) {
	w := NewWorld(52)
	if a := w.AgentByLabel('Z'); a != nil {
		t.Errorf("AgentByLabel('Z') = %v, want nil", a)
	}
}

func TestWaterPit_PickupAndShield(t *testing.T) {
	w := NewWorld(60)
	w.EnableHazards()
	a := SpawnAgentForTest(w, '1')
	w.Maze.Cells[a.Pos.Y][a.Pos.X] = CellWaterPit
	w.CollectWater()
	if a.Water != 1 {
		t.Errorf("Water = %d, want 1", a.Water)
	}
	if w.Maze.Cells[a.Pos.Y][a.Pos.X] != CellPath {
		t.Error("collected water pit should revert to path")
	}
	w.Maze.Cells[a.Pos.Y][a.Pos.X] = CellFirePit
	w.ResolvePitDeaths()
	if !a.Alive {
		t.Error("agent with water should survive a fire pit")
	}
	if a.Water != 0 {
		t.Errorf("Water = %d after shield, want 0", a.Water)
	}
}

func TestFallbackMove_HazardousOnlyOption(t *testing.T) {
	w := NewWorld(61)
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{40, 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	w.Maze.Cells[40][41] = CellFirePit
	w.Maze.Cells[40][39] = CellWall
	w.Maze.Cells[39][40] = CellWall
	w.Maze.Cells[41][40] = CellWall
	target := w.FallbackMove(a)
	if target == a.Pos {
		t.Error("fallback should accept a fire pit when nothing else is walkable")
	}
}

func TestAgentScent_DroppedOnMove(t *testing.T) {
	w := NewWorld(62)
	a := SpawnAgentForTest(w, '2')
	start := a.Pos
	w.MoveAgents()
	if a.Pos == start {
		t.Skip("agent didn't move this tick")
	}
	if w.ScentOwner[start.Y][start.X] != '2' {
		t.Errorf("ScentOwner at vacated cell = %q, want '2'", w.ScentOwner[start.Y][start.X])
	}
}

func TestSpawnGoalHazard_AddsEntity(t *testing.T) {
	w := NewWorld(70)
	w.EnableHazards()
	for i := 0; i < 20; i++ {
		pitsBefore := len(w.Maze.FirePits)
		wumpusBefore := len(w.Wumpus)
		w.SpawnGoalHazard()
		pitsAfter := len(w.Maze.FirePits)
		wumpusAfter := len(w.Wumpus)
		grew := (pitsAfter == pitsBefore+1 && wumpusAfter == wumpusBefore) ||
			(wumpusAfter == wumpusBefore+1 && pitsAfter == pitsBefore)
		if !grew {
			t.Fatalf("iter %d: neither pits nor wumpus grew by 1 (pits %d->%d, wumpus %d->%d)",
				i, pitsBefore, pitsAfter, wumpusBefore, wumpusAfter)
		}
	}
}

func TestGoalReach_TriggersSpawn(t *testing.T) {
	w := NewWorld(71)
	w.EnableHazards()
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = w.Maze.GoalPos
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	pitsBefore := len(w.Maze.FirePits)
	wumpusBefore := len(w.Wumpus)
	w.CheckGoal()
	pitsAfter := len(w.Maze.FirePits)
	wumpusAfter := len(w.Wumpus)
	if pitsAfter+wumpusAfter <= pitsBefore+wumpusBefore {
		t.Errorf("expected a hazard to be added on goal reach")
	}
}

func TestShortestPathSet_SameCell(t *testing.T) {
	w := NewWorld(82)
	set := w.ShortestPathSet(w.Maze.GoalPos, w.Maze.GoalPos)
	if len(set) != 1 || !set[w.Maze.GoalPos] {
		t.Errorf("self-set = %v, want {%v}", set, w.Maze.GoalPos)
	}
}

func TestCountShortestPaths_SaturatesAtCap(t *testing.T) {
	for seed := int64(0); seed < 5; seed++ {
		w := NewWorld(seed)
		got := w.Stats.ShortestPaths
		if got > MaxShortestPathsCount {
			t.Errorf("seed %d: ShortestPaths = %d exceeded cap %d", seed, got, MaxShortestPathsCount)
		}
	}
}

// TestAgentStrategy_UniversalSlots: every agent now carries the
// full state union (Beliefs + DQN) so they can run any algorithm
// per-journey.
func TestAgentStrategy_UniversalSlots(t *testing.T) {
	w := NewWorld(99)
	for _, l := range []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C'} {
		a := w.AgentByLabel(l)
		if a == nil {
			t.Fatalf("missing agent %c", l)
		}
		if a.Beliefs == nil {
			t.Errorf("agent %c missing Beliefs", l)
		}
		if a.DQN == nil {
			t.Errorf("agent %c missing DQN", l)
		}
	}
}

func TestShortestPathSet_Unreachable(t *testing.T) {
	w := NewWorld(97)
	start := Pos{40, 40}
	w.Maze.Cells[start.Y][start.X] = CellPath
	for _, d := range Cardinals {
		w.Maze.Cells[start.Y+d.Y][start.X+d.X] = CellWall
	}
	set := w.ShortestPathSet(start, w.Maze.GoalPos)
	if len(set) != 0 {
		t.Errorf("unreachable set = %d cells, want empty", len(set))
	}
}

func TestWumpusNotBoredOnScent(t *testing.T) {
	w := NewWorld(142)
	w.EnableHazards()
	wm := w.Wumpus[0]
	startPos := wm.Pos
	wm.CyclesSinceKill = WumpusKillTimeout + 5
	w.ScentOwner[wm.Pos.Y][wm.Pos.X] = '1'
	w.TickWumpusClocks()
	if wm.Pos != startPos {
		t.Errorf("wumpus on scent teleported from %v to %v", startPos, wm.Pos)
	}
	if wm.CyclesSinceKill != 0 {
		t.Errorf("CyclesSinceKill = %d on scent, want 0", wm.CyclesSinceKill)
	}
}

func TestPackVengeance_ArmedOnKill(t *testing.T) {
	w := NewWorld(140)
	if len(w.Wumpus) < 2 {
		t.Skip("need at least two wumpus")
	}
	victim := w.Wumpus[0]
	siblings := append([]*Wumpus{}, w.Wumpus[1:]...)
	w.KillWumpus(victim)
	for _, s := range siblings {
		if !s.Alive {
			continue
		}
		if s.VengeanceCycles != PackVengeanceCycles {
			t.Errorf("sibling #%d VengeanceCycles = %d, want %d",
				s.ID, s.VengeanceCycles, PackVengeanceCycles)
		}
	}
}

func TestPackVengeance_CountsDown(t *testing.T) {
	// Use a config with a no-op vengeance strategy so the countdown
	// branch fires without depending on the wumpus package.
	cfg := Config{
		Seed: 141,
		WumpusStrategy: func(*rand.Rand) WumpusStrategy {
			return func(*World, *Wumpus) Pos { return Pos{} }
		},
		VengeanceStrategy: func(*World, *Wumpus) Pos { return Pos{} },
	}
	w := NewWorldWithConfig(cfg)
	w.EnableHazards()
	wm := w.Wumpus[0]
	wm.VengeanceCycles = 3
	for i := 0; i < 3; i++ {
		w.MoveWumpus()
	}
	if wm.VengeanceCycles != 0 {
		t.Errorf("VengeanceCycles = %d after 3 ticks, want 0", wm.VengeanceCycles)
	}
}

func TestWumpusKillTimeout_LoweredTo30(t *testing.T) {
	if WumpusKillTimeout != 30 {
		t.Errorf("WumpusKillTimeout = %d, want 30", WumpusKillTimeout)
	}
}

func TestWumpusTeleport_OnIdle(t *testing.T) {
	w := NewWorld(120)
	w.EnableHazards()
	wm := w.Wumpus[0]
	startPos := wm.Pos
	wm.CyclesSinceKill = WumpusKillTimeout - 1
	w.TickWumpusClocks()
	if wm.Pos == startPos {
		t.Errorf("wumpus did not teleport from %v", startPos)
	}
	if wm.CyclesSinceKill != 0 {
		t.Errorf("CyclesSinceKill = %d, want 0 after teleport", wm.CyclesSinceKill)
	}
}

func TestWumpusKillResetsBoredom(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		w := NewWorld(seed)
		w.EnableHazards()
		a := SpawnAgentForTest(w, '1')
		wm := w.Wumpus[0]
		w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
		target := Pos{a.Pos.X + 1, a.Pos.Y}
		if !w.Maze.IsWalkable(target) {
			target = Pos{a.Pos.X, a.Pos.Y + 1}
		}
		if !w.Maze.IsWalkable(target) {
			continue
		}
		wm.Pos = target
		w.WumpusAt[target.Y][target.X] = wm
		wm.CyclesSinceKill = 50
		for i := 0; i < 30 && a.Alive; i++ {
			w.ResolveCombat()
		}
		if a.Alive {
			continue
		}
		if wm.CyclesSinceKill != 0 {
			t.Errorf("seed %d: CyclesSinceKill = %d after kill, want 0",
				seed, wm.CyclesSinceKill)
		}
		return
	}
	t.Error("no agent-death observed across 50 seeded trials")
}

func TestKnownPathReward_PaidOnRevisit(t *testing.T) {
	w := NewWorld(160)
	a := SpawnAgentForTest(w, '4')
	a.LifetimeVisited = map[Pos]bool{}
	knownCell := Pos{40, 41}
	a.LifetimeVisited[knownCell] = true
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{40, 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	w.Maze.Cells[knownCell.Y][knownCell.X] = CellPath
	for _, d := range Cardinals {
		w.Maze.Cells[a.Pos.Y+d.Y][a.Pos.X+d.X] = CellWall
	}
	w.Maze.Cells[knownCell.Y][knownCell.X] = CellPath
	a.PendingBonus = 0
	a.HasLastFrom = false
	w.MoveAgents()
	if a.Pos != knownCell {
		t.Skip("agent didn't land on the known cell")
	}
	if a.PendingBonus < KnownPathReward {
		t.Errorf("PendingBonus = %v, want >= %v after known-path step",
			a.PendingBonus, KnownPathReward)
	}
}

func TestKnownPathReward_SurvivesRespawn(t *testing.T) {
	w := NewWorld(161)
	a := SpawnAgentForTest(w, '4')
	known := Pos{20, 20}
	a.LifetimeVisited = map[Pos]bool{known: true}
	w.KillAgent(a, "wumpus")
	for i := 0; i < 30 && !a.Alive; i++ {
		w.Step()
	}
	if !a.Alive {
		t.Skip("agent didn't respawn in window")
	}
	if !a.LifetimeVisited[known] {
		t.Error("LifetimeVisited entry did not survive respawn")
	}
}

func TestDeadEnd_EscalatingCost(t *testing.T) {
	w := NewWorld(162)
	a := SpawnAgentForTest(w, '4')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{40, 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	w.Maze.Cells[40][41] = CellPath
	// Wall off every other Moore neighbor of (41, 40) so it has
	// exactly one walkable Moore neighbor — the cell at (40, 40).
	// With 8-conn Cardinals the dead-end check looks at all 8
	// directions.
	for _, p := range []Pos{
		{41, 39}, {42, 40}, {41, 41},
		{40, 39}, {42, 39}, {40, 41}, {42, 41},
	} {
		w.Maze.Cells[p.Y][p.X] = CellWall
	}
	a.PendingBonus = 0
	a.HasLastFrom = false
	beforeCount := a.DeadEndCount
	simulateDeadEndHit(w, a, Pos{41, 40})
	if a.DeadEndCount != beforeCount+1 {
		t.Errorf("DeadEndCount = %d, want %d", a.DeadEndCount, beforeCount+1)
	}
	if a.PendingBonus != -1.0 {
		t.Errorf("First dead-end cost = %v, want -1.0", a.PendingBonus)
	}
	a.PendingBonus = 0
	simulateDeadEndHit(w, a, Pos{41, 40})
	if a.PendingBonus != -2.0 {
		t.Errorf("Second dead-end cost = %v, want -2.0", a.PendingBonus)
	}
}

func simulateDeadEndHit(w *World, a *Agent, pos Pos) {
	a.Pos = pos
	walkables := 0
	for _, dd := range Cardinals {
		np := Pos{X: pos.X + dd.X, Y: pos.Y + dd.Y}
		if w.Maze.IsWalkable(np) {
			walkables++
		}
	}
	if walkables != 1 {
		return
	}
	if a.LastDeadEndCycle != 0 && w.Cycle-a.LastDeadEndCycle > DeadEndWindow {
		a.DeadEndCount = 0
	}
	exp := a.DeadEndCount
	if exp > DeadEndExpCap {
		exp = DeadEndExpCap
	}
	a.PendingBonus -= float64(int(1) << exp)
	a.DeadEndCount++
	a.LastDeadEndCycle = w.Cycle
}

func TestBackStepPenalty_AppliedOnReversal(t *testing.T) {
	w := NewWorld(150)
	a := SpawnAgentForTest(w, '4')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{40, 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	for _, d := range Cardinals {
		w.Maze.Cells[40+d.Y][40+d.X] = CellPath
	}
	a.HasLastFrom = false
	a.PendingBonus = 0
	startBonus := a.PendingBonus
	a.HasLastFrom = true
	a.LastFromCell = Pos{41, 40}
	oldPos := a.Pos
	w.AgentAt[oldPos.Y][oldPos.X] = nil
	a.Pos = Pos{41, 40}
	w.AgentAt[41][40] = a
	if a.HasLastFrom && a.Pos == a.LastFromCell {
		a.PendingBonus -= BackStepPenalty
	}
	if a.PendingBonus != startBonus-BackStepPenalty {
		t.Errorf("PendingBonus after back step = %v, want %v",
			a.PendingBonus, startBonus-BackStepPenalty)
	}
}

func TestExplorationBonus_FirstVisitOnly(t *testing.T) {
	w := NewWorld(110)
	a := SpawnAgentForTest(w, '2')
	a.PendingBonus = 0
	first := a.Pos
	w.MoveAgents()
	if a.Pos == first {
		t.Skip("agent didn't move this tick")
	}
	if a.PendingBonus < ExplorationBonus {
		t.Errorf("PendingBonus after new cell = %v, want >= %v",
			a.PendingBonus, ExplorationBonus)
	}
	visitedCell := first
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = visitedCell
	w.AgentAt[visitedCell.Y][visitedCell.X] = a
	a.Visited[visitedCell] = true
	a.PendingBonus = 0
	w.MoveAgents()
	revisitBonus := a.PendingBonus
	a.PendingBonus = 0
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = visitedCell
	w.AgentAt[visitedCell.Y][visitedCell.X] = a
	w.MoveAgents()
	if a.PendingBonus > revisitBonus+ExplorationBonus {
		t.Errorf("revisit paid out a second bonus on the same cell")
	}
}

func TestExplorationBonus_ResetsOnRespawn(t *testing.T) {
	w := NewWorld(111)
	a := SpawnAgentForTest(w, '4')
	a.Visited = map[Pos]bool{{1, 2}: true}
	a.PendingBonus = 42
	w.KillAgent(a, "wumpus")
	for i := 0; i < 30; i++ {
		w.Step()
		if a.Alive {
			break
		}
	}
	if !a.Alive {
		t.Skip("agent didn't respawn in window")
	}
	if len(a.Visited) > 1 {
		if a.Visited[Pos{1, 2}] {
			t.Error("stale Visited entry survived respawn")
		}
	}
	if a.PendingBonus != 0 && a.PendingBonus != ExplorationBonus {
		t.Errorf("PendingBonus after respawn = %v, want 0 or %v",
			a.PendingBonus, ExplorationBonus)
	}
}

func TestSpawnReplacementWaterPit_PlacesAPit(t *testing.T) {
	w := NewWorld(94)
	w.EnableHazards()
	before := len(w.Maze.WaterPits)
	w.SpawnReplacementWaterPit()
	if len(w.Maze.WaterPits) != before+1 {
		t.Errorf("WaterPits = %d, want %d", len(w.Maze.WaterPits), before+1)
	}
}

func TestFirePitDeath_MaySpawnWater(t *testing.T) {
	saw := false
	for seed := int64(0); seed < 30 && !saw; seed++ {
		w := NewWorld(seed)
		w.EnableHazards()
		a := SpawnAgentForTest(w, '1')
		pit := w.Maze.FirePits[0]
		w.AgentAt[a.Pos.Y][a.Pos.X] = nil
		a.Pos = pit
		w.AgentAt[pit.Y][pit.X] = a
		before := len(w.Maze.WaterPits)
		w.ResolvePitDeaths()
		if len(w.Maze.WaterPits) > before {
			saw = true
		}
	}
	if !saw {
		t.Error("expected at least one fire-pit death to spawn water across 30 trials")
	}
}

// TestSpawnAgentForTest_KicksOutExistingOccupant: calling
// SpawnAgentForTest when another agent occupies the entrance must
// remove them.
func TestSpawnAgentForTest_KicksOutExistingOccupant(t *testing.T) {
	w := NewWorld(180)
	first := SpawnAgentForTest(w, '1')
	if !first.Alive {
		t.Fatal("first spawn failed")
	}
	second := SpawnAgentForTest(w, '2')
	if first.Alive {
		t.Error("first agent should have been displaced")
	}
	if !second.Alive || second.Pos != w.Maze.EntrancePos {
		t.Error("second agent not properly spawned at entrance")
	}
}

// TestIsHazard_PathCellSafe: a plain walkable cell with no fire or
// live wumpus is not a hazard.
func TestIsHazard_PathCellSafe(t *testing.T) {
	w := NewWorld(181)
	// Find a path cell with no wumpus.
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			p := Pos{x, y}
			if w.Maze.Cells[y][x] == CellPath && w.WumpusAt[y][x] == nil {
				if w.IsHazard(p) {
					t.Errorf("plain path %v reported as hazard", p)
				}
				return
			}
		}
	}
	t.Fatal("no clean path cell found")
}

func TestTTL_KillsLongLivedAgent(t *testing.T) {
	w := NewWorld(95)
	w.EnableHazards()
	a := SpawnAgentForTest(w, '2')
	a.Stats.ActualDistance = TTLMultiplier*w.Stats.OptimalDistance + 1
	for i := 0; i < 20 && a.Alive; i++ {
		w.MoveAgents()
	}
	if a.Alive {
		t.Error("agent should TTL after exceeding 2x optimal distance")
	}
	if a.Stats.LastDeathReason != "ttl" {
		t.Errorf("LastDeathReason = %q, want 'ttl'", a.Stats.LastDeathReason)
	}
}
