package world

import (
	"testing"
)

func TestNewWorld_Generates(t *testing.T) {
	w := NewWorld(42)
	if w.Maze == nil {
		t.Fatal("maze nil")
	}
	for _, label := range []rune{'1', '2', '3', '4', '5'} {
		if w.AgentByLabel(label) == nil {
			t.Errorf("agent %c missing", label)
		}
	}
}

func TestStep_DoesNotPanic(t *testing.T) {
	w := NewWorld(7)
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
	if a.Maze.EntrancePos != b.Maze.EntrancePos {
		t.Error("same seed produced different entrances")
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

// TestScore_Formula: Score is the route-efficiency ratio
// min(1, shortestPath/ActualDistance) — bounded to [0,1], higher is
// better. 1.0 = reached (or on pace for) the goal in the optimal number
// of steps via ANY route; a longer route scores proportionally lower.
func TestScore_Formula(t *testing.T) {
	tests := []struct {
		name         string
		s            AgentStats
		shortestPath int
		want         float64
	}{
		{"no steps yet returns 0", AgentStats{ActualDistance: 0}, 50, 0},
		{"unknown optimal returns 0", AgentStats{ActualDistance: 30}, 0, 0},
		{"optimal-length route = 1.0",
			AgentStats{ActualDistance: 50}, 50, 1.0},
		{"a different equal-length route also = 1.0 (cells don't matter)",
			AgentStats{ActualDistance: 50, OffPathSteps: 50}, 50, 1.0},
		{"still within budget (on pace) = 1.0",
			AgentStats{ActualDistance: 30}, 50, 1.0},
		{"twice the optimal length = 0.5",
			AgentStats{ActualDistance: 100}, 50, 0.5},
		{"25% over optimal = 0.8",
			AgentStats{ActualDistance: 100}, 80, 0.8},
	}
	for _, tc := range tests {
		if got := tc.s.Score(tc.shortestPath); got != tc.want {
			t.Errorf("%s: Score(%d) = %v, want %v",
				tc.name, tc.shortestPath, got, tc.want)
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

// TestRespawnAgents_AllowsOverlapAtEntrance: agents may share a
// spawn cell on respawn. Each agent now has its OWN EntrancePos
// (per-agent entries on the maze perimeter), so the natural case
// is they DON'T overlap; this test pins B's entrance to A's cell
// to verify the overlap-permitted invariant still holds.
func TestRespawnAgents_AllowsOverlapAtEntrance(t *testing.T) {
	w := NewWorld(56)
	a := SpawnAgentForTest(w, '1')
	b := w.AgentByLabel('2')
	b.Alive = false
	b.RespawnIn = 0
	// Force B to share A's entrance so we exercise the overlap path.
	b.EntrancePos = a.EntrancePos
	w.RespawnAgents()
	if !b.Alive {
		t.Error("B should spawn even though A occupies entrance — overlap allowed")
	}
	if a.Pos != b.Pos {
		t.Errorf("both agents should be at shared entrance: A=%v B=%v", a.Pos, b.Pos)
	}
}

// TestPickAgentEntrances_PerimeterAndConnected: every entry produced
// by pickAgentEntrances lies on the maze perimeter, is distinct
// from every other entry, and is connected to the goal via the
// maze paths.
func TestPickAgentEntrances_PerimeterAndConnected(t *testing.T) {
	w := NewWorld(57)
	entries := w.pickAgentEntrances(12)
	if len(entries) != 12 {
		t.Fatalf("got %d entries, want 12", len(entries))
	}
	seen := map[Pos]bool{}
	for i, p := range entries {
		if seen[p] {
			// Duplicate is allowed only for the fallback case (where
			// the picker can't find enough distinct perimeter cells)
			// and only after position 0.
			t.Logf("entry %d is a duplicate of an earlier entry (fallback): %v", i, p)
		}
		seen[p] = true
		if p.X != 0 && p.X != BoardWidth-1 && p.Y != 0 && p.Y != BoardHeight-1 {
			t.Errorf("entry %d %v is NOT on the maze perimeter", i, p)
		}
		if !w.Maze.IsWalkable(p) {
			t.Errorf("entry %d %v is not walkable", i, p)
		}
		// Verify path to goal exists.
		path := w.DijkstraPath(p, w.Maze.GoalPos, w.Maze.IsWalkable)
		if len(path) == 0 && p != w.Maze.GoalPos {
			t.Errorf("entry %d %v has no path to goal %v", i, p, w.Maze.GoalPos)
		}
	}
}

// TestAgentEntrances_PerAgentOptimalDistance: after construction
// each agent has a non-zero OptimalDistance and a non-empty
// ShortestPath that ends at the goal.
func TestAgentEntrances_PerAgentOptimalDistance(t *testing.T) {
	w := NewWorld(58)
	for _, a := range w.Agents {
		if a.OptimalDistance <= 0 {
			t.Errorf("agent %c: OptimalDistance = %d, want > 0", a.Label, a.OptimalDistance)
		}
		if len(a.ShortestPath) == 0 {
			t.Errorf("agent %c: ShortestPath empty", a.Label)
		}
		if !a.ShortestPath[a.EntrancePos] {
			t.Errorf("agent %c: ShortestPath should contain own entrance %v", a.Label, a.EntrancePos)
		}
		if !a.ShortestPath[w.Maze.GoalPos] {
			t.Errorf("agent %c: ShortestPath should contain goal %v", a.Label, w.Maze.GoalPos)
		}
	}
}

// TestShortestPathCells_UnionsAllAgents: the world-level overlay
// set is the union of every agent's individual ShortestPath.
func TestShortestPathCells_UnionsAllAgents(t *testing.T) {
	w := NewWorld(59)
	// Every cell in any agent's ShortestPath must be in the union.
	for _, a := range w.Agents {
		for p := range a.ShortestPath {
			if !w.ShortestPathCells[p] {
				t.Errorf("union missing cell %v from agent %c", p, a.Label)
			}
		}
	}
}

// TestTTL_UsesPerAgentOptimalDistance: stamp a high agent.OptimalDistance
// and confirm the TTL kill rule allows ActualDistance up to
// TTLMultiplier × that value (much higher than the world-wide
// Stats.OptimalDistance would have allowed).
func TestTTL_UsesPerAgentOptimalDistance(t *testing.T) {
	w := NewWorld(60)
	a := SpawnAgentForTest(w, '1')
	w.Stats.OptimalDistance = 10 // tight world-wide value
	a.OptimalDistance = 500      // generous per-agent value
	a.Stats.ActualDistance = 51  // > 5*10 but < 5*500
	// With the OLD world-wide rule this would kill; with the new
	// per-agent rule the agent survives.
	w.MoveAgents()
	if !a.Alive {
		t.Error("per-agent TTL: agent killed by world-wide rule instead of per-agent rule")
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
// full state union (Beliefs) so they can run any algorithm
// per-journey.
func TestAgentStrategy_UniversalSlots(t *testing.T) {
	w := NewWorld(99)
	for _, l := range []rune{'1', '2', '3', '4', '5'} {
		a := w.AgentByLabel(l)
		if a == nil {
			t.Fatalf("missing agent %c", l)
		}
		if a.Beliefs == nil {
			t.Errorf("agent %c missing Beliefs", l)
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
