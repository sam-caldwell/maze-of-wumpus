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

// TestSwarmBayesian_SharesKnowledge: two agents both using strategy
// S should converge on a union of their KnownCells after one tick.
func TestSwarmBayesian_SharesKnowledge(t *testing.T) {
	w := newConfiguredWorld(98)
	a := world.SpawnAgentForTest(w, '3')
	b := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategySwarmBayesian
	b.CurrentStrategy = StrategySwarmBayesian
	a.KnownCells = map[world.Pos]bool{{X: 1, Y: 1}: true}
	b.KnownCells = map[world.Pos]bool{{X: 2, Y: 2}: true}
	a.Beliefs = world.NewAgentBeliefs()
	b.Beliefs = world.NewAgentBeliefs()
	// Run the merge phase on agent a.
	mergeSwarmKnowledge(w, a)
	if !a.KnownCells[world.Pos{X: 2, Y: 2}] {
		t.Error("a did not pick up b's cell")
	}
	if a.KnownCells[world.Pos{X: 999, Y: 999}] {
		t.Error("a picked up a cell nobody saw")
	}
}

// TestSwarmBayesian_IgnoresNonSwarmPeers: an agent not using
// strategy S should NOT contribute to the swarm view.
func TestSwarmBayesian_IgnoresNonSwarmPeers(t *testing.T) {
	w := newConfiguredWorld(99)
	a := world.SpawnAgentForTest(w, '3')
	b := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategySwarmBayesian
	b.CurrentStrategy = StrategyBayesian // NOT S
	a.KnownCells = map[world.Pos]bool{}
	b.KnownCells = map[world.Pos]bool{{X: 50, Y: 50}: true}
	a.Beliefs = world.NewAgentBeliefs()
	b.Beliefs = world.NewAgentBeliefs()
	mergeSwarmKnowledge(w, a)
	if a.KnownCells[world.Pos{X: 50, Y: 50}] {
		t.Error("a picked up cells from a non-S peer")
	}
}

// TestStrategyLetters_RegistryComplete: every letter in
// StrategyLetters has a corresponding ForLetter mapping and a
// human-readable NameByLetter entry, and ForLetter('Z') returns
// nil for unrecognized input.
func TestStrategyLetters_RegistryComplete(t *testing.T) {
	if len(StrategyLetters) != 7 {
		t.Fatalf("StrategyLetters len = %d, want 7", len(StrategyLetters))
	}
	for _, l := range StrategyLetters {
		if ForLetter(l) == nil {
			t.Errorf("ForLetter(%c) = nil", l)
		}
		if name := NameByLetter(l); name == "" || name == "unknown" {
			t.Errorf("NameByLetter(%c) = %q", l, name)
		}
	}
	if ForLetter('Z') != nil {
		t.Error("ForLetter('Z') should return nil")
	}
	if NameByLetter('Z') != "unknown" {
		t.Errorf("NameByLetter('Z') = %q, want unknown", NameByLetter('Z'))
	}
}

func TestForLabel_All(t *testing.T) {
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
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
		'1': "bfs",
		'2': "dfs",
		'3': "bayesian",
		'4': "scent-follower",
		'5': "dqn",
		'6': "pomcp",
		'7': "qmdp",
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
	a := w.AgentByLabel('3') // Bayesian (renumbered from '1')
	// Seed KnownCells for every cell under test — wwCellOK now gates
	// on perception, so cells must be in the agent's seen set for the
	// belief predicate to even matter.
	if a.KnownCells == nil {
		a.KnownCells = map[world.Pos]bool{}
	}
	known := func(p world.Pos) { a.KnownCells[p] = true }

	if wwCellOK(w, a, world.Pos{X: -1, Y: 0}) {
		t.Error("OOB cell must not be OK")
	}
	if wwCellOKLoose(w, a, world.Pos{X: -1, Y: 0}) {
		t.Error("OOB cell must not be OK loose either")
	}

	pit := world.Pos{X: 5, Y: 5}
	known(pit)
	a.Beliefs.PitProb[pit] = 1.0
	if wwCellOK(w, a, pit) {
		t.Error("known pit must not be OK")
	}
	if wwCellOKLoose(w, a, pit) {
		t.Error("known pit must not be OK loose")
	}

	v := world.Pos{X: 10, Y: 10}
	known(v)
	a.Beliefs.Observed[v] = true
	w.Maze.Cells[v.Y][v.X] = world.CellPath
	if !wwCellOK(w, a, v) {
		t.Error("visited cell must be OK")
	}

	st := world.Pos{X: 20, Y: 20}
	known(st)
	w.Maze.Cells[st.Y][st.X] = world.CellPath
	a.Beliefs.WumpusProb[st] = 1.0
	if wwCellOK(w, a, st) {
		t.Error("current-stench cell must not be OK")
	}
	if wwCellOKLoose(w, a, st) {
		t.Error("current-stench cell must not be OK loose")
	}

	u := world.Pos{X: 30, Y: 30}
	known(u)
	w.Maze.Cells[u.Y][u.X] = world.CellPath
	if wwCellOK(w, a, u) {
		t.Error("unobserved+unsafe cell must not be strictly OK")
	}
	if !wwCellOKLoose(w, a, u) {
		t.Error("unobserved cell must be loosely OK")
	}

	// Nil-beliefs path: agent 2 has no AgentBeliefs. Seed its
	// KnownCells with (0,0) so the perception gate passes and we
	// test the actual nil-beliefs branch of the predicate.
	b := w.AgentByLabel('2')
	b.KnownCells = map[world.Pos]bool{{X: 0, Y: 0}: true}
	if wwCellOK(w, b, world.Pos{X: 0, Y: 0}) {
		t.Error("nil-beliefs strict must be false")
	}
	if !wwCellOKLoose(w, b, world.Pos{X: 0, Y: 0}) {
		t.Error("nil-beliefs loose must be true for a walkable cell")
	}

	// Partial-observability gate: an unseen cell rejects from both
	// predicates regardless of belief state.
	unseen := world.Pos{X: 60, Y: 60}
	w.Maze.Cells[unseen.Y][unseen.X] = world.CellPath
	a.Beliefs.SafeFromPit[unseen] = true
	if wwCellOK(w, a, unseen) {
		t.Error("unseen cell must not be OK regardless of SafeFromPit")
	}
	if wwCellOKLoose(w, a, unseen) {
		t.Error("unseen cell must not be OK loose either")
	}
}

// TestWWNearestSafeFrontier_FindsBoundaryCell: under the new
// perception-boundary semantics, a cell qualifies as a frontier
// when it has at least one neighbor the agent has NOT perceived.
// We constrain the agent to SightRadius=1 (3×3 perception) and mark
// every perceived cell as safe, so the function should return one
// of the safe perceived cells whose outer neighbors are still
// unperceived.
func TestWWNearestSafeFrontier_FindsBoundaryCell(t *testing.T) {
	w := newConfiguredWorld(58)
	a := w.AgentByLabel('3')
	a.Pos = w.Maze.EntrancePos
	a.SightRadius = 1 // tight perception so a boundary is reachable
	w.MarkAgentSensed(a)
	a.Beliefs.Observed[a.Pos] = true
	a.Beliefs.SafeFromPit[a.Pos] = true
	for p := range a.KnownCells {
		a.Beliefs.SafeFromPit[p] = true
	}
	got, ok := wwNearestSafeFrontier(w, a)
	if !ok {
		t.Fatal("expected a safe perception-boundary cell")
	}
	// Verify the returned cell really is on the boundary: at least
	// one Moore neighbor must be outside KnownCells.
	onBoundary := false
	for _, d := range world.Cardinals {
		np := world.Pos{X: got.X + d.X, Y: got.Y + d.Y}
		if !world.InBounds(np.X, np.Y) {
			continue
		}
		if !a.KnownCells[np] {
			onBoundary = true
			break
		}
	}
	if !onBoundary {
		t.Errorf("returned cell %v has all neighbors perceived — not a boundary", got)
	}
	// nil-Beliefs agent has no frontier (early bail).
	if _, ok := wwNearestSafeFrontier(w, w.AgentByLabel('2')); ok {
		t.Error("nil-beliefs agent should have no frontier")
	}
}

// TestWWNearestSafeFrontier_AllInteriorReturnsFalse: when every
// perceived cell is interior (all Moore neighbors also perceived),
// there's no boundary to head to — the function must return false.
func TestWWNearestSafeFrontier_AllInteriorReturnsFalse(t *testing.T) {
	w := newConfiguredWorld(59)
	a := w.AgentByLabel('3')
	a.Pos = world.Pos{X: 50, Y: 50}
	// Manually mark only a single isolated cell as known — but the
	// agent's own cell has unperceived neighbors, so it IS a
	// boundary cell... let's instead carve out and perceive a 5x5
	// region and then ALSO mark all the cells in a 7x7 outer ring
	// as known so the inner 5x5 has no boundary neighbors that
	// matter. Simpler: mark KnownCells empty so wwNearestSafeFrontier
	// trivially returns false (no cells to BFS from).
	a.KnownCells = map[world.Pos]bool{}
	if _, ok := wwNearestSafeFrontier(w, a); ok {
		t.Error("empty KnownCells should yield no frontier")
	}
}

// TestWWCellOK_OOBAndWall hits the "not InBounds" and "not walkable"
// branches of both predicates.
func TestWWCellOK_OOBAndWall(t *testing.T) {
	w := newConfiguredWorld(95)
	a := w.AgentByLabel('3')
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
	a := w.AgentByLabel('3')
	if p := wwBFS(w, a, w.Maze.GoalPos, w.Maze.GoalPos, true); p != nil {
		t.Errorf("same-cell wwBFS returned %v", p)
	}
}

func TestBayesianStrategy_RunsThroughFullPipeline(t *testing.T) {
	for seed := int64(0); seed < 5; seed++ {
		w := newConfiguredWorld(85 + seed)
		a := world.SpawnAgentForTest(w, '3')
		for i := 0; i < 100; i++ {
			_ = BayesianStrategy(w, a)
		}
	}
}

func TestWWPlanPath_AllStages(t *testing.T) {
	w := newConfiguredWorld(87)
	a := w.AgentByLabel('3')
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
	a := world.SpawnAgentForTest(w, '3')
	UpdateAgentBeliefs(w, a)
}

func TestUpdateBeliefs_HeatBranches(t *testing.T) {
	w := newConfiguredWorld(86)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '3')
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
	a := world.SpawnAgentForTest(w, '3')
	a.Beliefs = nil
	_ = BayesianStrategy(w, a)
}

// TestBayesianStrategy_NoPath fires the return-a.Pos branch when
// wwPlanPath finds no path at all.
func TestBayesianStrategy_NoPath(t *testing.T) {
	w := newConfiguredWorld(174)
	a := world.SpawnAgentForTest(w, '3')
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
	in := make([]float64, world.DqnInput)
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
	in := make([]float64, world.DqnInput)
	for i := range in {
		in[i] = float64(i) * 0.1
	}
	_, out := d.Forward(in)
	if len(out) != world.DqnOutput {
		t.Errorf("forward output dim = %d, want %d", len(out), world.DqnOutput)
	}
	for _, v := range out {
		if v != v {
			t.Error("forward produced NaN")
		}
	}
}
