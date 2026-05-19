package strategy

import (
	"math/rand"
	"testing"

	"maze-of-wumpus/src/world"
)

// newConfiguredWorld constructs a world with the full agent strategy
// wiring (no wumpus strategies — most tests don't need them).
func newConfiguredWorld(seed int64) *world.World {
	return world.NewWorldWithConfig(world.Config{
		Seed:        seed,
		StrategyFor: ForLabel,
	})
}

func TestForLabel_All(t *testing.T) {
	for _, label := range []rune{'1', '2', '3', '4', '5'} {
		if ForLabel(label) == nil {
			t.Errorf("ForLabel(%c) = nil", label)
		}
	}
	if ForLabel('Z') != nil {
		t.Error("ForLabel('Z') should return nil")
	}
}

func TestName_All(t *testing.T) {
	want := map[rune]string{
		'1': "bayesian",
		'2': "bfs",
		'3': "dfs",
		'4': "q-learning",
		'5': "dqn",
		'Z': "unknown",
	}
	for label, expected := range want {
		if got := Name(label); got != expected {
			t.Errorf("Name(%c) = %q, want %q", label, got, expected)
		}
	}
}

func TestBFS_SameCell(t *testing.T) {
	w := newConfiguredWorld(8)
	if path := BFSToGoal(w, w.Maze.GoalPos); path != nil {
		t.Errorf("BFS goal->goal returned %v, want nil", path)
	}
}

// killAllWumpus removes all wumpus so BFS/DFS pathfinding tests have
// a guaranteed entrance-to-goal route.
func killAllWumpus(w *world.World) {
	for _, wm := range w.Wumpus {
		if wm.Alive {
			w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
			wm.Alive = false
		}
	}
}

// TestBFSStrategy_AllBranches exercises:
//   - initial plan empty → plans and returns first step
//   - cached plan → returns next step without replanning
//   - plan empty after BFS returns nil (agent already at goal) → returns a.Pos
//   - hazard on cached step → replans
func TestBFSStrategy_AllBranches(t *testing.T) {
	w := newConfiguredWorld(170)
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '2')
	a.Water = 1 // suppress the water-secondary-goal override
	first := BFSStrategy(w, a)
	if first == a.Pos {
		t.Fatal("expected BFS to find a path with no wumpus blocking")
	}
	if len(a.Plan) > 0 {
		_ = BFSStrategy(w, a)
	}
	a.Pos = w.Maze.GoalPos
	a.Plan = nil
	if got := BFSStrategy(w, a); got != a.Pos {
		t.Errorf("at-goal BFS = %v, want %v", got, a.Pos)
	}
	a.Pos = w.Maze.EntrancePos
	hazard := world.Pos{X: 70, Y: 40}
	w.Maze.Cells[hazard.Y][hazard.X] = world.CellFirePit
	a.Plan = []world.Pos{hazard}
	_ = BFSStrategy(w, a)
}

// TestDFSStrategy_AllBranches mirrors TestBFSStrategy_AllBranches.
func TestDFSStrategy_AllBranches(t *testing.T) {
	w := newConfiguredWorld(171)
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '3')
	a.Water = 1
	first := DFSStrategy(w, a)
	if first == a.Pos {
		t.Fatal("expected DFS to find a path with no wumpus blocking")
	}
	if len(a.Plan) > 0 {
		_ = DFSStrategy(w, a)
	}
	a.Pos = w.Maze.GoalPos
	a.Plan = nil
	if got := DFSStrategy(w, a); got != a.Pos {
		t.Errorf("at-goal DFS = %v, want %v", got, a.Pos)
	}
	a.Pos = w.Maze.EntrancePos
	hazard := world.Pos{X: 70, Y: 40}
	w.Maze.Cells[hazard.Y][hazard.X] = world.CellFirePit
	a.Plan = []world.Pos{hazard}
	_ = DFSStrategy(w, a)
}

// TestBFSToGoal_Unreachable: box off the start cell so BFS exhausts
// without finding goal.
func TestBFSToGoal_Unreachable(t *testing.T) {
	w := newConfiguredWorld(172)
	start := world.Pos{X: 40, Y: 40}
	w.Maze.Cells[start.Y][start.X] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[start.Y+d.Y][start.X+d.X] = world.CellWall
	}
	if path := BFSToGoal(w, start); path != nil {
		t.Errorf("unreachable BFS returned %v, want nil", path)
	}
}

func TestDFSToGoal_NoPath(t *testing.T) {
	w := newConfiguredWorld(50)
	start := world.Pos{X: 40, Y: 40}
	w.Maze.Cells[start.Y][start.X] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[start.Y+d.Y][start.X+d.X] = world.CellWall
	}
	if path := DFSToGoal(w, start); path != nil {
		t.Errorf("DFS from boxed-in cell returned %v, want nil", path)
	}
	if path := DFSToGoal(w, w.Maze.GoalPos); path != nil {
		t.Errorf("DFS goal->goal returned %v, want nil", path)
	}
}

func TestWWCellOK_StrictAndLoose(t *testing.T) {
	w := newConfiguredWorld(51)
	a := w.AgentByLabel('1')

	if wwCellOK(w, a, world.Pos{X: -1, Y: 0}) {
		t.Error("OOB cell must not be OK")
	}
	if wwCellOKLoose(w, a, world.Pos{X: -1, Y: 0}) {
		t.Error("OOB cell must not be OK loose either")
	}

	pit := world.Pos{X: 5, Y: 5}
	a.Beliefs.PitProb[pit] = 1.0
	if wwCellOK(w, a, pit) {
		t.Error("known pit must not be OK")
	}
	if wwCellOKLoose(w, a, pit) {
		t.Error("known pit must not be OK loose")
	}

	v := world.Pos{X: 10, Y: 10}
	a.Beliefs.Observed[v] = true
	w.Maze.Cells[v.Y][v.X] = world.CellPath
	if !wwCellOK(w, a, v) {
		t.Error("visited cell must be OK")
	}

	st := world.Pos{X: 20, Y: 20}
	w.Maze.Cells[st.Y][st.X] = world.CellPath
	a.Beliefs.WumpusProb[st] = 1.0
	if wwCellOK(w, a, st) {
		t.Error("current-stench cell must not be OK")
	}
	if wwCellOKLoose(w, a, st) {
		t.Error("current-stench cell must not be OK loose")
	}

	u := world.Pos{X: 30, Y: 30}
	w.Maze.Cells[u.Y][u.X] = world.CellPath
	if wwCellOK(w, a, u) {
		t.Error("unobserved+unsafe cell must not be strictly OK")
	}
	if !wwCellOKLoose(w, a, u) {
		t.Error("unobserved cell must be loosely OK")
	}

	b := w.AgentByLabel('2')
	if wwCellOK(w, b, world.Pos{X: 0, Y: 0}) {
		t.Error("nil-beliefs strict must be false")
	}
	if !wwCellOKLoose(w, b, world.Pos{X: 0, Y: 0}) {
		t.Error("nil-beliefs loose must be true for a walkable cell")
	}
}

func TestWWNearestSafeFrontier_FindsAndFailsCorrectly(t *testing.T) {
	w := newConfiguredWorld(58)
	a := w.AgentByLabel('1')
	a.Pos = w.Maze.EntrancePos
	a.Beliefs.Observed[a.Pos] = true
	a.Beliefs.SafeFromPit[a.Pos] = true
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.Beliefs.SafeFromPit[np] = true
			break
		}
	}
	if _, ok := wwNearestSafeFrontier(w, a); !ok {
		t.Error("expected a safe frontier")
	}
	if _, ok := wwNearestSafeFrontier(w, w.AgentByLabel('2')); ok {
		t.Error("nil-beliefs agent should have no frontier")
	}
}

// TestWWCellOK_OOBAndWall hits the "not InBounds" and "not walkable"
// branches of both predicates.
func TestWWCellOK_OOBAndWall(t *testing.T) {
	w := newConfiguredWorld(95)
	a := w.AgentByLabel('1')
	// Find a wall cell.
	var wall world.Pos
	for y := 0; y < world.BoardHeight && wall == (world.Pos{}); y++ {
		for x := 0; x < world.BoardWidth; x++ {
			if w.Maze.Cells[y][x] == world.CellWall {
				wall = world.Pos{X: x, Y: y}
				break
			}
		}
	}
	if wwCellOK(w, a, wall) {
		t.Error("wall must not be strictly OK")
	}
	if wwCellOKLoose(w, a, wall) {
		t.Error("wall must not be loosely OK")
	}
}

func TestUpdateBeliefs_NilNoop(t *testing.T) {
	w := newConfiguredWorld(53)
	b := w.AgentByLabel('2')
	b.Pos = w.Maze.EntrancePos
	UpdateAgentBeliefs(w, b)
}

func TestWWBFS_SameCell(t *testing.T) {
	w := newConfiguredWorld(54)
	a := w.AgentByLabel('1')
	if p := wwBFS(w, a, w.Maze.GoalPos, w.Maze.GoalPos, true); p != nil {
		t.Errorf("same-cell wwBFS returned %v", p)
	}
}

func TestBayesianStrategy_RunsThroughFullPipeline(t *testing.T) {
	for seed := int64(0); seed < 5; seed++ {
		w := newConfiguredWorld(85 + seed)
		a := world.SpawnAgentForTest(w, '1')
		for i := 0; i < 100; i++ {
			_ = BayesianStrategy(w, a)
		}
	}
}

func TestWWPlanPath_AllStages(t *testing.T) {
	w := newConfiguredWorld(87)
	a := w.AgentByLabel('1')
	a.Pos = w.Maze.EntrancePos
	_ = wwPlanPath(w, a)
	a.Beliefs.Observed[a.Pos] = true
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.Beliefs.SafeFromPit[np] = true
		}
	}
	_ = wwPlanPath(w, a)
}

func TestUpdateBeliefs_OOBSkip(t *testing.T) {
	w := newConfiguredWorld(96)
	a := world.SpawnAgentForTest(w, '1')
	UpdateAgentBeliefs(w, a)
}

func TestUpdateBeliefs_HeatBranches(t *testing.T) {
	w := newConfiguredWorld(86)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Heat[40][40] = true
	w.Stench[40][40] = true
	UpdateAgentBeliefs(w, a)
	if len(a.Beliefs.PitProb) == 0 {
		t.Error("multi-candidate heat branch should populate PitProb")
	}
	if len(a.Beliefs.WumpusProb) == 0 {
		t.Error("stench branch should populate WumpusProb")
	}
	for _, d := range []world.Pos{{X: 0, Y: -1}, {X: 0, Y: 1}, {X: -1, Y: 0}, {X: 1, Y: 0}, {X: -1, Y: -1}, {X: 1, Y: -1}, {X: -1, Y: 1}} {
		a.Beliefs.SafeFromPit[world.Pos{X: 40 + d.X, Y: 40 + d.Y}] = true
	}
	UpdateAgentBeliefs(w, a)
	if a.Beliefs.PitProb[world.Pos{X: 41, Y: 41}] != 1.0 {
		t.Errorf("single-candidate pit = %v, want 1.0", a.Beliefs.PitProb[world.Pos{X: 41, Y: 41}])
	}
}

func TestBayesianStrategy_NilBeliefsInitializes(t *testing.T) {
	w := newConfiguredWorld(93)
	a := world.SpawnAgentForTest(w, '1')
	a.Beliefs = nil
	_ = BayesianStrategy(w, a)
}

// TestBayesianStrategy_NoPath fires the return-a.Pos branch when
// wwPlanPath finds no path at all.
func TestBayesianStrategy_NoPath(t *testing.T) {
	w := newConfiguredWorld(174)
	a := world.SpawnAgentForTest(w, '1')
	// Wall off every cardinal neighbor of the agent.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if world.InBounds(np.X, np.Y) {
			w.Maze.Cells[np.Y][np.X] = world.CellWall
		}
	}
	a.Plan = nil
	if got := BayesianStrategy(w, a); got != a.Pos {
		t.Errorf("walled-in bayesian = %v, want %v", got, a.Pos)
	}
}

func TestQLearning_NilQLInitializes(t *testing.T) {
	w := newConfiguredWorld(91)
	a := world.SpawnAgentForTest(w, '4')
	a.QL = nil
	_ = QLearningStrategy(w, a)
	if a.QL == nil {
		t.Error("QLearningStrategy should allocate a QL on nil")
	}
}

func TestQLearning_PersistsAcrossDeaths(t *testing.T) {
	w := newConfiguredWorld(73)
	d := w.AgentByLabel('4')
	d.QL.SetQ(world.Pos{X: 5, Y: 5}, 0, 1.234)
	a := world.SpawnAgentForTest(w, '4')
	w.KillAgent(a, "wumpus")
	for i := 0; i < 30; i++ {
		w.Step()
		if a.Alive {
			break
		}
	}
	if d.QL.GetQ(world.Pos{X: 5, Y: 5}, 0) != 1.234 {
		t.Errorf("Q value did not persist across respawn: %v", d.QL.GetQ(world.Pos{X: 5, Y: 5}, 0))
	}
}

func TestQLearning_AppliesUpdate(t *testing.T) {
	w := newConfiguredWorld(74)
	a := world.SpawnAgentForTest(w, '4')
	_ = QLearningStrategy(w, a)
	a.Stats.Deaths++
	_ = QLearningStrategy(w, a)
	if len(a.QL.Q) == 0 {
		t.Error("Q table should be non-empty after two strategy calls")
	}
}

func TestQLearning_GoalReward(t *testing.T) {
	w := newConfiguredWorld(75)
	a := world.SpawnAgentForTest(w, '4')
	_ = QLearningStrategy(w, a)
	a.Stats.GoalsReached++
	_ = QLearningStrategy(w, a)
	if len(a.QL.Q) == 0 {
		t.Error("Q table should be populated after a goal-reward update")
	}
}

func TestQLearning_WaterAndKillRewards(t *testing.T) {
	w := newConfiguredWorld(76)
	a := world.SpawnAgentForTest(w, '4')
	_ = QLearningStrategy(w, a)
	a.Water++
	a.Stats.WumpusKilled++
	_ = QLearningStrategy(w, a)
	if len(a.QL.Q) == 0 {
		t.Error("Q table should be non-empty after kill/water reward")
	}
}

func TestDQN_NilDQNInitializes(t *testing.T) {
	w := newConfiguredWorld(92)
	a := world.SpawnAgentForTest(w, '5')
	a.DQN = nil
	_ = DQNStrategy(w, a)
	if a.DQN == nil {
		t.Error("DQNStrategy should allocate a DQN on nil")
	}
}

func TestDQN_PersistsAcrossDeaths(t *testing.T) {
	w := newConfiguredWorld(78)
	e := w.AgentByLabel('5')
	e.DQN.W1[0] = 12345.0
	a := world.SpawnAgentForTest(w, '5')
	w.KillAgent(a, "wumpus")
	for i := 0; i < 30; i++ {
		w.Step()
		if a.Alive {
			break
		}
	}
	if e.DQN.W1[0] != 12345.0 {
		t.Errorf("DQN weight did not persist across respawn: %v", e.DQN.W1[0])
	}
}

func TestDQN_AppliesUpdate(t *testing.T) {
	w := newConfiguredWorld(79)
	a := world.SpawnAgentForTest(w, '5')
	before := make([]float64, len(a.DQN.W2))
	copy(before, a.DQN.W2)
	_ = DQNStrategy(w, a)
	a.Stats.Deaths++
	_ = DQNStrategy(w, a)
	changed := false
	for i, v := range a.DQN.W2 {
		if v != before[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("DQN W2 weights did not change after a Bellman update")
	}
}

func TestDQN_FeaturesIncludeHazards(t *testing.T) {
	w := newConfiguredWorld(81)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '5')
	w.Heat[a.Pos.Y][a.Pos.X] = true
	w.Stench[a.Pos.Y][a.Pos.X] = true
	f := world.AgentDqnFeatures(w, a)
	if f[4] != 1 || f[5] != 1 {
		t.Errorf("heat/stench features = %v %v, want 1/1", f[4], f[5])
	}
}

func TestDQN_GoalReward(t *testing.T) {
	w := newConfiguredWorld(82)
	a := world.SpawnAgentForTest(w, '5')
	_ = DQNStrategy(w, a)
	a.Stats.GoalsReached++
	_ = DQNStrategy(w, a)
}

func TestDQN_WaterAndKillRewards(t *testing.T) {
	w := newConfiguredWorld(83)
	a := world.SpawnAgentForTest(w, '5')
	_ = DQNStrategy(w, a)
	a.Water++
	a.Stats.WumpusKilled++
	_ = DQNStrategy(w, a)
}

func TestDQN_UpdateNoOpWhenTargetMatches(t *testing.T) {
	d := world.NewDQN(rand.New(rand.NewSource(1)))
	in := []float64{0, 0, 0, 0, 0, 0}
	_, out := d.Forward(in)
	before := make([]float64, len(d.W1))
	copy(before, d.W1)
	d.Update(in, 0, out[0], DqnLearnRate)
	for i, v := range d.W1 {
		if v != before[i] {
			t.Errorf("W1[%d] changed despite zero-delta update", i)
		}
	}
}

func TestDQN_ForwardSanity(t *testing.T) {
	d := world.NewDQN(rand.New(rand.NewSource(1)))
	_, out := d.Forward([]float64{0.1, 0.2, 0.3, 0.4, 0, 1})
	if len(out) != world.DqnOutput {
		t.Errorf("forward output dim = %d, want %d", len(out), world.DqnOutput)
	}
	for _, v := range out {
		if v != v {
			t.Error("forward produced NaN")
		}
	}
}
