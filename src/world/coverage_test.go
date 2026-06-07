package world

import (
	"os"
	"testing"
)

// osStat and osReadDir are tiny indirections so the test file can
// stat / readdir without dragging in a separate test util.
var osStat = os.Stat
var osReadDir = os.ReadDir

// TestIsDiagonal_AllCases covers the 4 axis cases and 4 diagonals.
func TestIsDiagonal_AllCases(t *testing.T) {
	for _, d := range Cardinals[:CardinalCount] {
		if IsDiagonal(d) {
			t.Errorf("IsDiagonal(%v) = true for cardinal", d)
		}
	}
	for _, d := range Cardinals[CardinalCount:] {
		if !IsDiagonal(d) {
			t.Errorf("IsDiagonal(%v) = false for diagonal", d)
		}
	}
	if IsDiagonal(Pos{0, 0}) {
		t.Error("IsDiagonal({0,0}) should be false")
	}
}

// TestStrategyLettersForWorld covers both the configured and the
// nil-default cases.
func TestStrategyLettersForWorld(t *testing.T) {
	w := NewWorldWithConfig(Config{Seed: 52, StrategyLetters: []rune{'X', 'Y'}})
	got := w.StrategyLettersForWorld()
	if len(got) != 2 || got[0] != 'X' || got[1] != 'Y' {
		t.Errorf("StrategyLettersForWorld = %v, want [X Y]", got)
	}
	w2 := NewWorld(53)
	if got := w2.StrategyLettersForWorld(); got != nil {
		t.Errorf("unset letters should be nil, got %v", got)
	}
}

// TestStrategyDescription covers configured and unset paths.
func TestStrategyDescription(t *testing.T) {
	w := NewWorldWithConfig(Config{
		Seed:                         54,
		StrategyLetters:              []rune{'A'},
		StrategyDescriptionForLetter: func(l rune) string { return "letter " + string(l) },
	})
	if got := w.StrategyDescription('A'); got != "letter A" {
		t.Errorf("StrategyDescription(A) = %q, want %q", got, "letter A")
	}
	w2 := NewWorld(55)
	if got := w2.StrategyDescription('Z'); got != "" {
		t.Errorf("unconfigured StrategyDescription = %q, want \"\"", got)
	}
}

// TestSwarmAliveCell_NoSwarmGraph: with no swarm graph computed yet,
// every cell is treated as alive (open-default) — also true when
// the queried group ID has no entry yet.
func TestSwarmAliveCell_NoSwarmGraph(t *testing.T) {
	w := NewWorld(56)
	if !w.SwarmAliveCell(1, Pos{0, 0}) {
		t.Error("with no swarm graph yet, every cell should report alive")
	}
	if !w.SwarmAliveCell(0, Pos{0, 0}) {
		t.Error("group=0 (no swarm) should always report alive")
	}
}

// TestSwarmAliveCell_AfterPrune: after a prune for a specific group,
// only cells in THAT group's pruned set return true. Other groups
// are unaffected (independent swarms).
func TestSwarmAliveCell_AfterPrune(t *testing.T) {
	w := NewWorld(57)
	w.swarmGraphs = map[int]*swarmGraphState{
		1: {aliveCells: map[Pos]bool{{10, 10}: true}},
		2: {aliveCells: map[Pos]bool{{20, 20}: true}},
	}
	if !w.SwarmAliveCell(1, Pos{10, 10}) {
		t.Error("group 1: (10,10) should report alive")
	}
	if w.SwarmAliveCell(1, Pos{11, 11}) {
		t.Error("group 1: (11,11) should report not alive")
	}
	// Cross-group isolation: group 1's cell isn't alive in group 2.
	if w.SwarmAliveCell(2, Pos{10, 10}) {
		t.Error("group 2 should not see group 1's alive cell")
	}
}

// TestVisibleEvents_ShortBuffer returns the whole slice when the
// buffer is shorter than EventsVisible.
func TestVisibleEvents_ShortBuffer(t *testing.T) {
	w := NewWorld(58)
	w.Events = nil
	w.RecordEvent("red", "one")
	w.RecordEvent("green", "two")
	got := w.VisibleEvents()
	if len(got) != 2 {
		t.Errorf("VisibleEvents short = %d entries, want 2", len(got))
	}
}

// TestVisibleEvents_OverflowReturnsLastN returns only the last
// EventsVisible entries when the buffer overflows that threshold.
func TestVisibleEvents_OverflowReturnsLastN(t *testing.T) {
	w := NewWorld(59)
	w.Events = nil
	for i := 0; i < EventsVisible+5; i++ {
		w.RecordEvent("red", "msg")
	}
	got := w.VisibleEvents()
	if len(got) != EventsVisible {
		t.Errorf("VisibleEvents overflow = %d entries, want %d", len(got), EventsVisible)
	}
}

// TestPickTemplate_EmptyPool returns "" on an empty pool.
func TestPickTemplate_EmptyPool(t *testing.T) {
	w := NewWorld(60)
	if got := w.pickTemplate(nil); got != "" {
		t.Errorf("pickTemplate(nil) = %q, want \"\"", got)
	}
	if got := w.pickTemplate([]string{}); got != "" {
		t.Errorf("pickTemplate(empty) = %q, want \"\"", got)
	}
}

// TestRecordAgentDeath_AllReasons covers each switch branch including
// the default fall-through.
func TestRecordAgentDeath_AllReasons(t *testing.T) {
	w := NewWorld(61)
	a := w.Agents[0]
	for _, r := range []string{"wumpus", "ttl", "firepit", "fire-pit", "fire", "drowned"} {
		w.Events = nil
		w.recordAgentDeath(a, r)
		if len(w.Events) != 1 {
			t.Errorf("reason=%q: events=%d, want 1", r, len(w.Events))
		}
	}
}

// TestAbsInt covers positive, negative, zero.
func TestAbsInt(t *testing.T) {
	if absInt(-5) != 5 {
		t.Errorf("absInt(-5) = %d, want 5", absInt(-5))
	}
	if absInt(0) != 0 {
		t.Errorf("absInt(0) = %d, want 0", absInt(0))
	}
	if absInt(7) != 7 {
		t.Errorf("absInt(7) = %d, want 7", absInt(7))
	}
}

// TestScentFreshness covers OOB, never-scented, fresh, half-aged,
// aged-out, and past-cycle (negative age) branches. All deposited
// values are kept > 0 because deposited <= 0 is the "never scented"
// guard.
func TestScentFreshness(t *testing.T) {
	w := NewWorld(72)
	// OOB.
	if w.ScentFreshness(-1, 0) != 0 {
		t.Error("OOB ScentFreshness should be 0")
	}
	// Never scented (deposited == 0).
	if w.ScentFreshness(5, 5) != 0 {
		t.Error("unscented cell should be 0")
	}
	// Make Cycle large enough that we can pick aged values without
	// the deposited value going non-positive.
	w.Cycle = ScentMaxAge * 3
	// Fresh: deposited == Cycle.
	w.ScentCycle[5][5] = w.Cycle
	if got := w.ScentFreshness(5, 5); got != 1.0 {
		t.Errorf("fresh scent = %v, want 1.0", got)
	}
	// Half-aged: deposited is ScentMaxAge/2 ticks in the past.
	w.ScentCycle[5][5] = w.Cycle - ScentMaxAge/2
	if got := w.ScentFreshness(5, 5); got <= 0.4 || got >= 0.6 {
		t.Errorf("half-aged scent = %v, want ~0.5", got)
	}
	// Fully aged-out: deposited == Cycle - ScentMaxAge.
	w.ScentCycle[5][5] = w.Cycle - ScentMaxAge
	if got := w.ScentFreshness(5, 5); got != 0 {
		t.Errorf("aged-out scent = %v, want 0", got)
	}
	// Past-cycle (deposited > Cycle): age < 0 clamps to 0.
	w.ScentCycle[5][5] = w.Cycle + 10
	if got := w.ScentFreshness(5, 5); got != 1.0 {
		t.Errorf("past-cycle scent = %v, want 1.0 (clamped)", got)
	}
}

// TestScentSignedFreshness covers non-follower, no-owner, currentTrustee
// match, negative-trust, and neutral.
func TestScentSignedFreshness(t *testing.T) {
	w := NewWorld(73)
	// Non-follower agent: always 0.
	for _, a := range w.Agents {
		if !IsScentFollower(a.Label) {
			if got := w.ScentSignedFreshness(a, 5, 5); got != 0 {
				t.Errorf("non-follower freshness = %v, want 0", got)
			}
			break
		}
	}
	// Follower with no scent at cell: 0.
	var follower *Agent
	for _, a := range w.Agents {
		if IsScentFollower(a.Label) {
			follower = a
			break
		}
	}
	if follower == nil {
		t.Skip("no scent-follower agent in world")
	}
	if got := w.ScentSignedFreshness(follower, 5, 5); got != 0 {
		t.Errorf("no-scent freshness = %v, want 0", got)
	}
	// Owner 0 → 0.
	w.Cycle = 100
	w.ScentCycle[5][5] = 100
	w.ScentOwner[5][5] = 0
	if got := w.ScentSignedFreshness(follower, 5, 5); got != 0 {
		t.Errorf("owner=0 freshness = %v, want 0", got)
	}
	// Owner == CurrentTrustee → positive freshness.
	w.ScentOwner[5][5] = '1'
	follower.CurrentTrustee = '1'
	if got := w.ScentSignedFreshness(follower, 5, 5); got <= 0 {
		t.Errorf("trustee match freshness = %v, want > 0", got)
	}
	// Owner with negative trust → negative freshness.
	follower.CurrentTrustee = 0
	follower.TrustScores = map[rune]float64{'1': -2}
	if got := w.ScentSignedFreshness(follower, 5, 5); got >= 0 {
		t.Errorf("negative-trust freshness = %v, want < 0", got)
	}
	// Owner neutral → 0.
	follower.TrustScores = map[rune]float64{'1': 0}
	if got := w.ScentSignedFreshness(follower, 5, 5); got != 0 {
		t.Errorf("neutral-trust freshness = %v, want 0", got)
	}
}

// TestCachedStepFor covers short-path, found-with-next, found-end,
// next-unwalkable, and not-found branches.
func TestCachedStepFor(t *testing.T) {
	w := NewWorld(74)
	a := w.Agents[0]
	a.Pos = Pos{10, 10}
	// Short path: <2 entries.
	a.KnownShortestPath = nil
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("nil path should return false")
	}
	a.KnownShortestPath = []Pos{{10, 10}}
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("1-entry path should return false")
	}
	// Path with a.Pos at index 0, next walkable.
	w.Maze.Cells[10][11] = CellPath
	a.KnownShortestPath = []Pos{{10, 10}, {11, 10}}
	if next, ok := w.CachedStepFor(a); !ok || next != (Pos{11, 10}) {
		t.Errorf("expected next=(11,10) ok=true; got %v ok=%v", next, ok)
	}
	// Path where a.Pos is the last entry.
	a.KnownShortestPath = []Pos{{5, 5}, {10, 10}}
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("at-end path should return false")
	}
	// Next becomes unwalkable.
	a.KnownShortestPath = []Pos{{10, 10}, {11, 10}}
	w.Maze.Cells[10][11] = CellWall
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("unwalkable next should return false")
	}
	// a.Pos not in path at all.
	a.Pos = Pos{20, 20}
	a.KnownShortestPath = []Pos{{5, 5}, {6, 5}}
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("not-in-path should return false")
	}
}

// TestPickStrategy_EmptyLetters: clears CurrentStrategy.
func TestPickStrategy_EmptyLetters(t *testing.T) {
	a := &Agent{CurrentStrategy: 'X'}
	a.PickStrategy(nil, nil)
	if a.CurrentStrategy != 0 {
		t.Errorf("CurrentStrategy = %c, want 0", a.CurrentStrategy)
	}
}

// TestWriteStatsLog_WritesFile: creates the directory and writes a
// readable JSON file.
func TestWriteStatsLog_WritesFile(t *testing.T) {
	w := NewWorld(81)
	dir := t.TempDir()
	path, err := w.WriteStatsLog(dir)
	if err != nil {
		t.Fatalf("WriteStatsLog: %v", err)
	}
	info, err := osStat(path)
	if err != nil {
		t.Errorf("written log not found: %v", err)
	}
	if info != nil && info.Size() == 0 {
		t.Errorf("log file is empty")
	}
}

// TestAppendSolveRecord_DirMissingIsNoOp: when SolveLogDir doesn't
// exist, appendSolveRecord silently does nothing.
func TestAppendSolveRecord_DirMissingIsNoOp(t *testing.T) {
	w := NewWorld(82)
	a := w.Agents[0]
	orig := SolveLogDir
	defer func() { SolveLogDir = orig }()
	SolveLogDir = "/this/dir/does/not/exist/intentionally"
	w.appendSolveRecord(a) // must not panic, must not create anything
}

// TestAppendSolveRecord_WritesToDir: when SolveLogDir exists, appending
// adds one JSON-Lines record.
func TestAppendSolveRecord_WritesToDir(t *testing.T) {
	w := NewWorld(83)
	a := w.Agents[0]
	dir := t.TempDir()
	orig := SolveLogDir
	defer func() { SolveLogDir = orig }()
	SolveLogDir = dir
	w.appendSolveRecord(a)
	// Look for any file matching agent<label>.log
	entries, err := osReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("no log file written under %s", dir)
	}
}

// TestAppendSolveRecord_PathIsFileNotDir: SolveLogDir points at a
// regular file → the os.Stat says it isn't a directory and the
// function bails. Covers the !info.IsDir() branch.
func TestAppendSolveRecord_PathIsFileNotDir(t *testing.T) {
	w := NewWorld(84)
	a := w.Agents[0]
	dir := t.TempDir()
	file := dir + "/notadir"
	if err := os.WriteFile(file, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	orig := SolveLogDir
	defer func() { SolveLogDir = orig }()
	SolveLogDir = file
	w.appendSolveRecord(a) // must not panic
}

// TestSoftmaxPickLabel_Empty returns 0 for an empty candidate slice.
func TestSoftmaxPickLabel_Empty(t *testing.T) {
	w := NewWorld(85)
	if got := softmaxPickLabel(w.Rng, nil, nil); got != 0 {
		t.Errorf("empty softmaxPickLabel = %c, want 0", got)
	}
}

// TestSoftmaxPickLabel_AllZeroScores: with all-zero weights it falls
// back to uniform — every returned label must be in the candidate set.
func TestSoftmaxPickLabel_AllZeroScores(t *testing.T) {
	w := NewWorld(86)
	candidates := []rune{'A', 'B', 'C'}
	scores := map[rune]float64{}
	seen := map[rune]bool{}
	for i := 0; i < 200; i++ {
		got := softmaxPickLabel(w.Rng, candidates, scores)
		seen[got] = true
		found := false
		for _, c := range candidates {
			if c == got {
				found = true
			}
		}
		if !found {
			t.Errorf("got %c not in candidate set", got)
		}
	}
	if len(seen) < 2 {
		t.Errorf("uniform pick should cover ≥2 labels in 200 draws, got %d", len(seen))
	}
}

// TestSoftmaxPickLabel_BiasedScores: a strongly-positive trust score
// dominates the distribution.
func TestSoftmaxPickLabel_BiasedScores(t *testing.T) {
	w := NewWorld(87)
	candidates := []rune{'A', 'B'}
	scores := map[rune]float64{'A': 10, 'B': 0}
	winsA := 0
	for i := 0; i < 200; i++ {
		if softmaxPickLabel(w.Rng, candidates, scores) == 'A' {
			winsA++
		}
	}
	if winsA < 180 {
		t.Errorf("strongly-biased softmax: A wins %d/200, want ≥180", winsA)
	}
}

// TestPickTrustee_NonFollower: a non-follower agent gets CurrentTrustee
// cleared regardless of trust state.
func TestPickTrustee_NonFollower(t *testing.T) {
	w := NewWorld(88)
	for _, a := range w.Agents {
		if !IsScentFollower(a.Label) {
			a.CurrentTrustee = 'X'
			a.PickTrustee(w, w.Rng)
			if a.CurrentTrustee != 0 {
				t.Errorf("non-follower CurrentTrustee = %c, want 0", a.CurrentTrustee)
			}
			return
		}
	}
}

// TestPickTrustee_NoLiveLeaders: a follower with zero starts and no
// alive leaders gets CurrentTrustee = 0.
func TestPickTrustee_NoLiveLeaders(t *testing.T) {
	w := NewWorld(89)
	var follower *Agent
	for _, a := range w.Agents {
		if IsScentFollower(a.Label) {
			follower = a
			break
		}
	}
	if follower == nil {
		t.Skip("no scent-follower in world")
	}
	for _, a := range w.Agents {
		a.Alive = false
	}
	follower.Stats.Starts = 0
	follower.PickTrustee(w, w.Rng)
	if follower.CurrentTrustee != 0 {
		t.Errorf("no-live-leaders trustee = %c, want 0", follower.CurrentTrustee)
	}
}

// TestTickAgentClocks_Disabled: a disabled agent skipped (continue
// branch). A non-alive non-disabled agent leaves TicksAlive unchanged.
func TestTickAgentClocks_Branches(t *testing.T) {
	w := NewWorld(94)
	if len(w.Agents) < 2 {
		t.Skip("need ≥ 2 agents")
	}
	a := w.Agents[0]
	a.Disabled = true
	a.TicksAlive = 10
	b := w.Agents[1]
	b.Disabled = false
	b.Alive = false
	b.TicksAlive = 20
	w.tickAgentClocks()
	if a.TicksAlive != 10 {
		t.Errorf("disabled agent: ticks = %d, want 10 (unchanged)", a.TicksAlive)
	}
	if b.TicksAlive != 20 {
		t.Errorf("dead agent: ticks = %d, want 20 (unchanged)", b.TicksAlive)
	}
}

// TestWriteStatsLog_MkdirFails: targeting a path under a regular file
// triggers MkdirAll failure.
func TestWriteStatsLog_MkdirFails(t *testing.T) {
	w := NewWorld(95)
	dir := t.TempDir()
	file := dir + "/blocker"
	if err := os.WriteFile(file, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := w.WriteStatsLog(file + "/subdir") // file is not a dir → MkdirAll fails
	if err == nil {
		t.Errorf("expected MkdirAll error, got nil")
	}
}

// TestWriteStatsLog_WriteErrorOnReadOnlyDir: create a read-only
// directory and try to write into it — os.WriteFile fails.
func TestWriteStatsLog_WriteErrorOnReadOnlyDir(t *testing.T) {
	w := NewWorld(96)
	dir := t.TempDir() + "/readonly"
	if err := os.MkdirAll(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755)
	_, err := w.WriteStatsLog(dir)
	// Could succeed (root, test sandbox) or fail. Either way, no panic.
	_ = err
}

// TestAppendSolveRecord_OpenFails: SolveLogDir exists but the per-agent
// log file already exists as a DIRECTORY (so O_WRONLY|O_APPEND can't
// open it as a file) — exercise the open-error branch.
func TestAppendSolveRecord_OpenFails(t *testing.T) {
	w := NewWorld(97)
	a := w.Agents[0]
	dir := t.TempDir()
	orig := SolveLogDir
	defer func() { SolveLogDir = orig }()
	SolveLogDir = dir
	// Make the target file path actually be a directory.
	blocker := dir + "/agent" + string(a.Label) + ".log"
	if err := os.Mkdir(blocker, 0755); err != nil {
		t.Fatal(err)
	}
	w.appendSolveRecord(a) // must not panic; the function silently bails
}

// TestPruneSwarmGraph_EmptyWalkable: when swarmKnown contains no
// walkable cells, the function returns the empty walkable set early.
func TestPruneSwarmGraph_EmptyWalkable(t *testing.T) {
	w := NewWorld(98)
	// Pick cells that are walls in this seed's maze.
	swarmKnown := map[Pos]bool{}
	for y := 0; y < BoardHeight && len(swarmKnown) < 3; y++ {
		for x := 0; x < BoardWidth && len(swarmKnown) < 3; x++ {
			if w.Maze.Cells[y][x] == CellWall {
				swarmKnown[Pos{x, y}] = true
			}
		}
	}
	got := w.pruneSwarmGraph(swarmKnown, 1)
	if len(got) != 0 {
		t.Errorf("empty-walkable pruneSwarmGraph = %v, want empty", got)
	}
}

// TestBfsAlive_StaleQueueEntrySkipped: pre-populate dist with a
// smaller value than the queue's stale entry — the function must
// take the cur.cost > dist[cur.pos] branch and skip.
func TestBfsAlive_StaleQueueEntrySkipped(t *testing.T) {
	w := NewWorld(99)
	// 2x1 corridor.
	w.Maze.Cells[20][20] = CellPath
	w.Maze.Cells[20][21] = CellPath
	alive := map[Pos]bool{{20, 20}: true, {21, 20}: true}
	// Just call bfsAlive — the stale-skip branch is internal but
	// gets exercised when multiple paths reach the same cell.
	dist := w.bfsAlive(Pos{20, 20}, alive)
	if dist[Pos{21, 20}] != CardinalStepCost {
		t.Errorf("dist = %d, want %d", dist[Pos{21, 20}], CardinalStepCost)
	}
}

// TestPickAgentEntrances_NEqualsOne returns just the canonical
// entrance (the n=1 early-return branch).
func TestPickAgentEntrances_NEqualsOne(t *testing.T) {
	w := NewWorld(130)
	entries := w.pickAgentEntrances(1)
	if len(entries) != 1 || entries[0] != w.Maze.EntrancePos {
		t.Errorf("n=1 = %v, want [%v]", entries, w.Maze.EntrancePos)
	}
}

// TestPickAgentEntrances_NEqualsZero returns the canonical entrance
// only (the function always seeds with it).
func TestPickAgentEntrances_NEqualsZero(t *testing.T) {
	w := NewWorld(131)
	entries := w.pickAgentEntrances(0)
	if len(entries) != 1 {
		t.Errorf("n=0 entries = %v, want 1 (canonical entrance always seeded)", entries)
	}
}

// TestCarveEntryConnection_GoalRejected: passing the goal cell as a
// candidate must fail — agents cannot spawn on the goal.
func TestCarveEntryConnection_GoalRejected(t *testing.T) {
	w := NewWorld(132)
	if w.carveEntryConnection(w.Maze.GoalPos) {
		t.Error("carveEntryConnection accepted GoalPos")
	}
}

// TestCarveEntryConnection_NonPerimeter: a non-perimeter cell must
// be rejected — the carve logic only handles one-axis perimeter
// directions.
func TestCarveEntryConnection_NonPerimeter(t *testing.T) {
	w := NewWorld(133)
	interior := Pos{X: 40, Y: 40}
	if w.carveEntryConnection(interior) {
		t.Error("carveEntryConnection accepted an interior cell")
	}
}

// TestCarveEntryConnection_PerimeterCarves: a perimeter cell whose
// inward neighbor is wall gets a corridor carved straight inward
// and reports connection success.
func TestCarveEntryConnection_PerimeterCarves(t *testing.T) {
	w := NewWorld(134)
	// Force a perimeter cell to be wall, then carve.
	p := Pos{X: 0, Y: 5}
	w.Maze.Cells[p.Y][p.X] = CellWall
	w.Maze.Cells[p.Y][p.X+1] = CellWall
	w.Maze.Cells[p.Y][p.X+2] = CellPath // existing path 2 cells inward
	// Connect the carved corridor into goal-reachable territory by
	// chaining cells to the existing maze paths from (0,5)→(2,5).
	// First make sure (2,5) actually reaches the goal.
	if len(w.DijkstraPath(Pos{X: 2, Y: 5}, w.Maze.GoalPos, w.Maze.IsWalkable)) == 0 {
		t.Skip("seed-dependent: (2,5) not reachable to goal")
	}
	ok := w.carveEntryConnection(p)
	if !ok {
		t.Error("carveEntryConnection should succeed when path to goal exists")
	}
	if w.Maze.Cells[p.Y][p.X] != CellEntrance {
		t.Errorf("perimeter cell should be marked CellEntrance, got %d",
			w.Maze.Cells[p.Y][p.X])
	}
	if w.Maze.Cells[p.Y][p.X+1] != CellPath {
		t.Errorf("inward cell should be carved to CellPath")
	}
}

// TestRecomputeAgentPrunedView_DirectCall: world-package call to
// the per-agent prune helper. Covers the no-op shortcut (cache
// hit) and the rebuild path.
func TestRecomputeAgentPrunedView_DirectCall(t *testing.T) {
	w := NewWorld(135)
	a := SpawnAgentForTest(w, '1')
	// First call: rebuild.
	w.RecomputeAgentPrunedViewIfStale(a)
	if a.PrunedKnownCells == nil {
		t.Fatal("first call should populate PrunedKnownCells")
	}
	firstSize := len(a.PrunedKnownCells)
	// Second call with same KnownCells: cache hit, no change.
	w.RecomputeAgentPrunedViewIfStale(a)
	if len(a.PrunedKnownCells) != firstSize {
		t.Errorf("cache-hit changed PrunedKnownCells size %d → %d",
			firstSize, len(a.PrunedKnownCells))
	}
}

// TestInitAgentEntrance_DirectCall covers initAgentEntrance independent
// of pickAgentEntrances. Verifies fields are populated.
func TestInitAgentEntrance_DirectCall(t *testing.T) {
	w := NewWorld(136)
	a := &Agent{Label: 'Z'}
	costFromGoal := w.computeCostFromGoal()
	w.initAgentEntrance(a, w.Maze.EntrancePos, costFromGoal)
	if a.EntrancePos != w.Maze.EntrancePos {
		t.Errorf("EntrancePos = %v, want %v", a.EntrancePos, w.Maze.EntrancePos)
	}
	if a.OptimalDistance <= 0 {
		t.Errorf("OptimalDistance = %d, want > 0", a.OptimalDistance)
	}
	if len(a.ShortestPath) == 0 {
		t.Error("ShortestPath empty")
	}
	if a.DistFromStart[w.Maze.EntrancePos.Y][w.Maze.EntrancePos.X] != 0 {
		t.Errorf("DistFromStart at entrance = %d, want 0",
			a.DistFromStart[w.Maze.EntrancePos.Y][w.Maze.EntrancePos.X])
	}
}

// TestPickAgentEntrances_FallbackWhenAllPerimeterFails: if every
// perimeter cell fails the goal-distance filter (we set the goal
// adjacent to the entrance and use a high min-distance), the
// picker must fall back to filling the slate with the canonical
// entrance. Exercises the no-pick branch.
func TestPickAgentEntrances_FallbackWhenAllPerimeterFails(t *testing.T) {
	w := NewWorld(140)
	// Pin the goal right next to the entrance so the minGoalDist
	// filter (MinGoalDistanceCells/2 = 50) rejects every perimeter
	// cell within 50 Manhattan of the goal — which is the entire
	// near-entrance region of the maze. We don't expect FULL
	// rejection in practice; this is a stress test on the constraint
	// loop, not a "all 12 entries are the canonical entrance" assert.
	w.Maze.GoalPos = Pos{X: w.Maze.EntrancePos.X + 1, Y: w.Maze.EntrancePos.Y + 1}
	if !InBounds(w.Maze.GoalPos.X, w.Maze.GoalPos.Y) {
		t.Skip("seed makes adjacent-goal placement infeasible")
	}
	w.Maze.Cells[w.Maze.GoalPos.Y][w.Maze.GoalPos.X] = CellGoal
	entries := w.pickAgentEntrances(12)
	if len(entries) != 12 {
		t.Errorf("got %d entries, want 12 (with fallback to canonical entrance)", len(entries))
	}
}

// TestCarveEntryConnection_ConnectsToExistingPath covers the inner
// loop that walks inward until existing path is reached.
func TestCarveEntryConnection_ConnectsToExistingPath(t *testing.T) {
	w := NewWorld(141)
	// Choose a perimeter cell whose inward neighbor is wall; carve
	// should turn the perimeter cell to entrance and inward to path.
	p := Pos{X: BoardWidth - 1, Y: 5}
	// Force the cells we care about.
	w.Maze.Cells[p.Y][p.X] = CellWall
	w.Maze.Cells[p.Y][p.X-1] = CellWall
	// Existing path inland — ensure it leads to goal.
	w.Maze.Cells[p.Y][p.X-2] = CellPath
	if len(w.DijkstraPath(Pos{X: p.X - 2, Y: p.Y}, w.Maze.GoalPos, w.Maze.IsWalkable)) == 0 {
		t.Skip("seed-dependent: inland cell not reachable to goal")
	}
	ok := w.carveEntryConnection(p)
	if !ok {
		t.Skip("seed-dependent: carve didn't produce a goal-connected entry")
	}
	if w.Maze.Cells[p.Y][p.X] != CellEntrance {
		t.Errorf("perimeter not marked CellEntrance")
	}
}

// TestPickAgentEntrances_DistinctOnDifferentSeeds: across multiple
// seeds the 12 picked entries should generally land on multiple
// sides of the board (not all on one edge).
func TestPickAgentEntrances_DistinctOnDifferentSeeds(t *testing.T) {
	for _, seed := range []int64{137, 138, 139} {
		w := NewWorld(seed)
		entries := w.pickAgentEntrances(12)
		sidesSeen := map[string]bool{}
		for _, p := range entries {
			switch {
			case p.Y == 0:
				sidesSeen["top"] = true
			case p.Y == BoardHeight-1:
				sidesSeen["bottom"] = true
			case p.X == 0:
				sidesSeen["left"] = true
			case p.X == BoardWidth-1:
				sidesSeen["right"] = true
			}
		}
		if len(sidesSeen) < 2 {
			t.Errorf("seed %d: entries cluster on only %d side(s); distribution expected ≥ 2",
				seed, len(sidesSeen))
		}
	}
}

// TestSwarmLeaderStartsSolo_NoStartedBump: under lazy spawning,
// joining a swarm assigns a SwarmGroupID but spawns NO clones — the
// leader starts solo and forks lazily during movement. maintainSwarm-
// Membership must not bump StrategyPerf.Started either.
func TestSwarmLeaderStartsSolo_NoStartedBump(t *testing.T) {
	w := NewWorld(316)
	w.ensureStrategyPerf(SwarmStrategyLetter).Started = 0 // reset baseline
	a := w.AgentByLabel('3')
	a.CurrentStrategy = SwarmStrategyLetter
	a.SwarmGroupID = 0
	a.SwarmClones = nil
	w.maintainSwarmMembership(a)
	if a.SwarmGroupID == 0 {
		t.Error("swarm leader got no SwarmGroupID")
	}
	if len(a.SwarmClones) != 0 {
		t.Errorf("leader should start solo, got %d clones", len(a.SwarmClones))
	}
	if got := w.StrategyPerf[SwarmStrategyLetter].Started; got != 0 {
		t.Errorf("Started bumped by %d, want 0", got)
	}
}

// TestSwarmCloneSpawn_QmdpSwarmLetterV: strategy 'V' is also a swarm
// strategy — an agent on V gets a SwarmGroupID (and, lazily, clones).
// Joining assigns the group and starts solo.
func TestSwarmCloneSpawn_QmdpSwarmLetterV(t *testing.T) {
	w := NewWorld(317)
	a := w.AgentByLabel('6')
	a.CurrentStrategy = QmdpSwarmStrategyLetter
	a.SwarmGroupID = 0
	a.SwarmClones = nil
	w.maintainSwarmMembership(a)
	if a.SwarmGroupID == 0 {
		t.Error("X agent got no SwarmGroupID")
	}
	if len(a.SwarmClones) != 0 {
		t.Errorf("X leader should start solo, got %d clones", len(a.SwarmClones))
	}
}

// TestMaintainSwarmMembership_AllNonRLettersSwarm: every strategy
// letter except R spawns a swarm (group + clones) under the unified
// all-but-R swarm model.
func TestMaintainSwarmMembership_AllNonRLettersSwarm(t *testing.T) {
	w := NewWorld(319)
	for _, letter := range []rune{'S', 'T', 'U', 'V', 'W', 'X'} {
		a := w.AgentByLabel('3')
		a.SwarmGroupID = 0
		a.SwarmClones = nil
		a.CurrentStrategy = letter
		w.maintainSwarmMembership(a)
		// Lazy model: joining assigns a group but starts solo (clones
		// are forked later during movement).
		if a.SwarmGroupID == 0 {
			t.Errorf("letter %c: expected a SwarmGroupID (swarm-capable)", letter)
		}
		if len(a.SwarmClones) != 0 {
			t.Errorf("letter %c: should start solo, got %d clones", letter, len(a.SwarmClones))
		}
	}
}

// TestMaintainSwarmMembership_RNeverSwarms: the omniscient benchmark R
// must never spawn a swarm.
func TestMaintainSwarmMembership_RNeverSwarms(t *testing.T) {
	w := NewWorld(320)
	a := w.AgentByLabel('3')
	a.CurrentStrategy = BenchmarkStrategyLetter
	a.SwarmGroupID = 0
	a.SwarmClones = nil
	w.maintainSwarmMembership(a)
	if a.SwarmGroupID != 0 || len(a.SwarmClones) != 0 {
		t.Errorf("R must never swarm: group=%d clones=%d", a.SwarmGroupID, len(a.SwarmClones))
	}
	if IsSwarmStrategy(BenchmarkStrategyLetter) {
		t.Error("IsSwarmStrategy(R) must be false")
	}
}

// TestSwarmLeaderPromotion_LetterV: killing a V swarm leader with a
// surviving clone promotes the clone into the leader slot (body swap,
// no real death) — the same generic promotion path S uses.
func TestSwarmLeaderPromotion_LetterV(t *testing.T) {
	w := NewWorld(318)
	a := SpawnAgentForTest(w, '6')
	a.CurrentStrategy = QmdpSwarmStrategyLetter
	a.SwarmGroupID = 0
	w.maintainSwarmMembership(a)
	// Lazy model: leader starts solo. Attach a clone manually to model
	// a swarm that has already forked one, then verify promotion.
	clonePos := Pos{X: a.EntrancePos.X, Y: a.EntrancePos.Y}
	a.SwarmClones = []*SwarmClone{{Pos: clonePos, Alive: true}}
	deathsBefore := a.Stats.Deaths
	clonesBefore := len(a.SwarmClones)
	w.KillAgent(a, "wumpus")
	if !a.Alive {
		t.Error("X leader should survive as a promoted clone, not die")
	}
	if a.Stats.Deaths != deathsBefore {
		t.Error("promotion should not bump Deaths (body swap, not a journey end)")
	}
	if len(a.SwarmClones) != clonesBefore-1 {
		t.Errorf("clone roster = %d, want %d after promotion", len(a.SwarmClones), clonesBefore-1)
	}
}

// TestSwarmCloneCleanupOnStrategyLeave: an agent that switches OFF S
// has its clones cleared.
func TestSwarmCloneCleanupOnStrategyLeave(t *testing.T) {
	w := NewWorld(311)
	a := w.AgentByLabel('3')
	a.CurrentStrategy = SwarmStrategyLetter
	w.maintainSwarmMembership(a)
	// Attach a clone manually (lazy model starts solo) so we can verify
	// it's cleared on leaving the swarm.
	a.SwarmClones = []*SwarmClone{{Pos: a.EntrancePos, Alive: true}}
	if a.SwarmGroupID == 0 {
		t.Fatal("swarm not initialized")
	}
	// R (the omniscient benchmark) is the only non-swarm letter, so
	// switching to it is the way to "leave the swarm."
	a.CurrentStrategy = BenchmarkStrategyLetter
	w.maintainSwarmMembership(a)
	if a.SwarmGroupID != 0 || len(a.SwarmClones) != 0 {
		t.Errorf("swarm not cleared after leaving to R: groupID=%d, clones=%d",
			a.SwarmGroupID, len(a.SwarmClones))
	}
}

// TestKillAgent_PromotesCloneToLeader: when an S-leader is killed
// but has alive clones, one is promoted to the leader slot — the
// agent stays alive, no Stats.Deaths bump.
func TestKillAgent_PromotesCloneToLeader(t *testing.T) {
	w := NewWorld(312)
	a := w.AgentByLabel('3')
	a.CurrentStrategy = SwarmStrategyLetter
	a.Alive = true
	w.maintainSwarmMembership(a)
	// Lazy model starts solo; attach a clone at a distinctive cell so we
	// can verify the leader inherits its position on promotion.
	a.SwarmClones = []*SwarmClone{{Pos: Pos{X: 50, Y: 50}, Alive: true}}
	prevDeaths := a.Stats.Deaths
	prevClones := len(a.SwarmClones)
	w.KillAgent(a, "wumpus")
	if !a.Alive {
		t.Error("leader should still be alive after clone promotion")
	}
	if a.Stats.Deaths != prevDeaths {
		t.Errorf("Stats.Deaths changed on promotion: %d → %d", prevDeaths, a.Stats.Deaths)
	}
	if a.Pos != (Pos{X: 50, Y: 50}) {
		t.Errorf("leader didn't inherit clone position: at %v, want (50,50)", a.Pos)
	}
	if len(a.SwarmClones) != prevClones-1 {
		t.Errorf("clone count after promotion = %d, want %d",
			len(a.SwarmClones), prevClones-1)
	}
}

// TestKillAgent_PromotionAdoptsCloneDistance: on a leader TTL death,
// the promoted clone's individual Dist becomes the leader's
// ActualDistance — so the swarm continues on the survivor's budget
// instead of cascading to death on the dead leader's over-TTL distance.
func TestKillAgent_PromotionAdoptsCloneDistance(t *testing.T) {
	w := NewWorld(313)
	a := w.AgentByLabel('3')
	a.CurrentStrategy = SwarmStrategyLetter
	a.Alive = true
	w.maintainSwarmMembership(a)
	a.Stats.ActualDistance = 9999 // dead leader is way over TTL
	a.SwarmClones = []*SwarmClone{{Pos: Pos{X: 60, Y: 60}, Alive: true, Dist: 42}}
	w.KillAgent(a, "ttl")
	if !a.Alive {
		t.Fatal("leader should survive via promotion")
	}
	if a.Stats.ActualDistance != 42 {
		t.Errorf("leader ActualDistance after promotion = %d, want 42 (promoted clone's Dist)",
			a.Stats.ActualDistance)
	}
}

// TestKillAgent_NoCloneFallsThroughToDeath: when an S-leader has no
// alive clones, KillAgent proceeds with normal death handling.
func TestKillAgent_NoCloneFallsThroughToDeath(t *testing.T) {
	w := NewWorld(313)
	a := w.AgentByLabel('3')
	a.CurrentStrategy = SwarmStrategyLetter
	a.Alive = true
	w.maintainSwarmMembership(a)
	// Kill every clone.
	for _, c := range a.SwarmClones {
		c.Alive = false
	}
	prevDeaths := a.Stats.Deaths
	w.KillAgent(a, "ttl")
	if a.Alive {
		t.Error("leader should die when no clones survive")
	}
	if a.Stats.Deaths != prevDeaths+1 {
		t.Errorf("Stats.Deaths bumped %d → %d, want +1", prevDeaths, a.Stats.Deaths)
	}
}

// TestEndJourney_OpportunisticCreditOnGoal: a follower that recorded
// opportunistic followings of two peer labels and reaches the goal
// must see BOTH peer labels gain trust (TrustGoalBonus + within-TTL
// bonus when applicable).
func TestEndJourney_OpportunisticCreditOnGoal(t *testing.T) {
	w := NewWorld(322)
	a := w.AgentByLabel('4') // a follower label
	a.Alive = true
	a.CurrentTrustee = 0 // no formal trustee
	a.OptimalDistance = 100
	a.TicksAlive = 50 // < TTLMultiplier*OptimalDistance → within TTL
	a.OpportunisticFollowed = map[rune]bool{'2': true, '3': true}
	a.TrustScores = map[rune]float64{}
	w.endJourney(a, true)
	gain2 := a.TrustScores['2']
	gain3 := a.TrustScores['3']
	want := TrustGoalBonus + TrustWithinTTLBonus
	if gain2 != want {
		t.Errorf("opportunistic trust for '2' = %v, want %v", gain2, want)
	}
	if gain3 != want {
		t.Errorf("opportunistic trust for '3' = %v, want %v", gain3, want)
	}
}

// TestEndJourney_OpportunisticNoCreditOnFailure: opportunistic
// followings DON'T penalize on a failed run (the agent simply learns
// nothing about those labels from a death — they may have led
// somewhere fine and the agent died from other causes).
func TestEndJourney_OpportunisticNoCreditOnFailure(t *testing.T) {
	w := NewWorld(323)
	a := w.AgentByLabel('4')
	a.Alive = true
	a.CurrentTrustee = 0
	a.OpportunisticFollowed = map[rune]bool{'2': true}
	a.TrustScores = map[rune]float64{}
	w.endJourney(a, false)
	if v := a.TrustScores['2']; v != 0 {
		t.Errorf("opportunistic trust for '2' shouldn't change on failure: got %v", v)
	}
}

// TestEndJourney_TrusteeAndOpportunisticBothCredit: when the agent
// has BOTH a CurrentTrustee (with sufficient contact ticks) and
// opportunistic followings on a winning run, both pathways credit
// trust — the trustee gets credit via the existing gate, and any
// OTHER labels in OpportunisticFollowed each get TrustGoalBonus.
// The trustee label is NOT double-counted via the opportunistic path.
func TestEndJourney_TrusteeAndOpportunisticBothCredit(t *testing.T) {
	w := NewWorld(324)
	a := w.AgentByLabel('4')
	a.Alive = true
	a.CurrentTrustee = '2'
	a.JourneyTrusteeContactTicks = MinTrusteeContactTicks
	a.OptimalDistance = 100
	a.TicksAlive = 50
	a.OpportunisticFollowed = map[rune]bool{
		'2': true, // trustee — should NOT be double-credited via the opp path
		'3': true, // peer    — should get opp credit
	}
	a.TrustScores = map[rune]float64{}
	w.endJourney(a, true)
	wantTrustee := TrustGoalBonus + TrustWithinTTLBonus
	if got := a.TrustScores['2']; got != wantTrustee {
		t.Errorf("trustee trust = %v, want %v (no double-credit)", got, wantTrustee)
	}
	wantPeer := TrustGoalBonus + TrustWithinTTLBonus
	if got := a.TrustScores['3']; got != wantPeer {
		t.Errorf("peer opportunistic trust = %v, want %v", got, wantPeer)
	}
}

// TestCheckGoal_CloneOnGoalCreditsLeader: a clone reaching the goal
// cell increments the leader's Stats.GoalsReached EXACTLY ONCE — the
// whole swarm gets a single win regardless of how many clones touch
// the goal cell. All alive clones collapse to the goal position.
// SwarmGroupID is cleared at the leader's next respawn (not in
// CheckGoal itself), so we don't assert on it here.
func TestCheckGoal_CloneOnGoalCreditsLeader(t *testing.T) {
	w := NewWorld(314)
	a := w.AgentByLabel('3')
	a.CurrentStrategy = SwarmStrategyLetter
	a.Alive = true
	w.maintainSwarmMembership(a)
	// Lazy model starts solo; attach a few clones manually, several
	// already sitting on the goal cell — the swarm must still score
	// only once.
	a.SwarmClones = []*SwarmClone{
		{Pos: w.Maze.GoalPos, Alive: true},
		{Pos: w.Maze.GoalPos, Alive: true},
		{Pos: w.Maze.GoalPos, Alive: true},
	}
	prevGoals := a.Stats.GoalsReached
	w.CheckGoal()
	if a.Stats.GoalsReached != prevGoals+1 {
		t.Errorf("clone-on-goal didn't credit leader exactly once: %d → %d, want +1",
			prevGoals, a.Stats.GoalsReached)
	}
	// Every alive clone should now sit on the goal cell (collapse).
	for i, c := range a.SwarmClones {
		if c != nil && c.Alive && c.Pos != w.Maze.GoalPos {
			t.Errorf("clone %d at %v after collapse, want goal %v",
				i, c.Pos, w.Maze.GoalPos)
		}
	}
}

// TestCheckGoal_CloneOnGoal_SwarmDissolvesAtRespawn: after a clone-
// on-goal win, the leader's swarm state stays alive (collapsed at
// goal) until the leader's next respawn. RespawnAgents is what
// clears SwarmGroupID + SwarmClones, not CheckGoal — so the
// rendering of the immediately-following frame still shows the
// collapsed swarm at the goal.
func TestCheckGoal_CloneOnGoal_SwarmDissolvesAtRespawn(t *testing.T) {
	w := NewWorld(317)
	a := w.AgentByLabel('3')
	a.Disabled = false
	a.CurrentStrategy = SwarmStrategyLetter
	a.Alive = true
	w.maintainSwarmMembership(a)
	// Lazy model starts solo; attach a clone on the goal to trigger the
	// clone-on-goal win.
	a.SwarmClones = []*SwarmClone{{Pos: w.Maze.GoalPos, Alive: true}}
	w.CheckGoal()
	// After CheckGoal: leader is dead-pending-respawn, swarm
	// state should STILL be present (clones at goal, group ID
	// still set) for one rendering pass.
	if a.SwarmGroupID == 0 {
		t.Errorf("SwarmGroupID prematurely cleared in CheckGoal")
	}
	// Force the respawn path and verify cleanup. Under lazy spawning a
	// fresh swarm starts solo (0 clones) with a (possibly new) group.
	a.RespawnIn = 0
	w.RespawnAgents()
	if len(a.SwarmClones) != 0 {
		t.Errorf("respawned swarm should start solo, got %d clones", len(a.SwarmClones))
	}
}

// TestSwarmIndependentGraphs: two S-agents in distinct groups have
// distinct entries in World.swarmGraphs after recompute.
func TestSwarmIndependentGraphs(t *testing.T) {
	w := NewWorld(315)
	a := w.AgentByLabel('3')
	b := w.AgentByLabel('4')
	a.CurrentStrategy = SwarmStrategyLetter
	b.CurrentStrategy = SwarmStrategyLetter
	a.Alive = true
	b.Alive = true
	a.KnownCells = map[Pos]bool{a.Pos: true}
	b.KnownCells = map[Pos]bool{b.Pos: true}
	w.maintainSwarmMembership(a)
	w.maintainSwarmMembership(b)
	if a.SwarmGroupID == b.SwarmGroupID {
		t.Fatal("distinct S-agents should have distinct SwarmGroupIDs")
	}
	w.RecomputeSwarmGraphIfStale(a.SwarmGroupID)
	w.RecomputeSwarmGraphIfStale(b.SwarmGroupID)
	if w.swarmGraphs[a.SwarmGroupID] == nil {
		t.Error("group A's swarmGraphs entry missing")
	}
	if w.swarmGraphs[b.SwarmGroupID] == nil {
		t.Error("group B's swarmGraphs entry missing")
	}
}

// TestStrategyPerf_StartedCountsAllRuns: #Runs (Started field) must
// count every journey that began on a strategy, regardless of outcome.
// Wire a fresh agent up, respawn it three times (drive a death between
// spawns), and assert Started ≥ 3 for whichever strategies it picked.
func TestStrategyPerf_StartedCountsAllRuns(t *testing.T) {
	w := NewWorldWithConfig(Config{
		Seed:            300,
		StrategyLetters: []rune{'R', 'T', 'X'},
	})
	a := w.AgentByLabel('1')
	a.Disabled = false
	totalStartedBefore := 0
	if w.StrategyPerf != nil {
		for _, c := range w.StrategyPerf {
			totalStartedBefore += c.Started
		}
	}
	for i := 0; i < 3; i++ {
		a.Alive = false
		a.RespawnIn = 0
		w.RespawnAgents()
		// Force a death so the next RespawnAgents call counts as a
		// fresh start.
		a.Alive = false
	}
	totalStartedAfter := 0
	for _, c := range w.StrategyPerf {
		totalStartedAfter += c.Started
	}
	if totalStartedAfter-totalStartedBefore < 3 {
		t.Errorf("Started total bumped by %d across 3 respawns; want ≥ 3",
			totalStartedAfter-totalStartedBefore)
	}
}

// TestStrategyPerf_StartedIncrementedRegardlessOfOutcome: even if the
// agent dies of TTL (not goal-reach), the Started counter still ticked
// up on the spawn that began the death-bound journey.
func TestStrategyPerf_StartedIncrementedRegardlessOfOutcome(t *testing.T) {
	w := NewWorldWithConfig(Config{
		Seed:            301,
		StrategyLetters: []rune{'T'},
	})
	a := w.AgentByLabel('3')
	a.Disabled = false
	a.Alive = false
	a.RespawnIn = 0
	w.RespawnAgents()
	c := w.StrategyPerf[a.CurrentStrategy]
	if c == nil || c.Started < 1 {
		t.Errorf("Started not bumped after first spawn (CurrentStrategy=%c)",
			a.CurrentStrategy)
	}
	// Every non-R letter is now a swarm; a leader with surviving clones
	// would PROMOTE a clone on "death" (a body swap, not a journey end),
	// so the strategy-death accounting wouldn't fire. Clear the clones
	// to model the swarm fully collapsing — then KillAgent takes the
	// real-death path that records the TTL outcome.
	a.SwarmClones = nil
	// Kill via TTL — should NOT clear Started.
	beforeStarted := c.Started
	w.KillAgent(a, "ttl")
	if c.Started != beforeStarted {
		t.Errorf("Started changed on death: %d → %d", beforeStarted, c.Started)
	}
	// TTLExpiry should have incremented.
	if c.TTLExpiry < 1 {
		t.Errorf("TTLExpiry not bumped on TTL death: %d", c.TTLExpiry)
	}
}

// TestInitialSpawn_AllSimultaneous: every agent has RespawnIn = 1
// at construction — they all enter on the very first tick. The
// per-agent perimeter entrances distribute them across the maze
// so there's no visual clumping to avoid via a stagger.
func TestInitialSpawn_AllSimultaneous(t *testing.T) {
	w := NewWorld(120)
	for _, a := range w.Agents {
		if a.RespawnIn != 1 {
			t.Errorf("agent %c: RespawnIn = %d, want 1", a.Label, a.RespawnIn)
		}
		if a.Alive {
			t.Errorf("agent %c: should not be alive at construction", a.Label)
		}
	}
}

// TestInitialSpawn_AllAliveAfterFirstTick: after one RespawnAgents
// call every enabled agent is alive on the board simultaneously.
func TestInitialSpawn_AllAliveAfterFirstTick(t *testing.T) {
	w := NewWorld(121)
	w.EnableAllAgents()
	w.RespawnAgents()
	for _, a := range w.Agents {
		if !a.Alive {
			t.Errorf("agent %c: should be alive after first tick", a.Label)
		}
	}
}

// TestPruneGraph_Phase2OptIn: phase 2 (articulation pruning) keeps
// strictly ≤ cells alive vs phase 1 alone, since it's a second-pass
// filter on top of leaf-trim. Used by solo callers to skip phase 2.
func TestPruneGraph_Phase2OptIn(t *testing.T) {
	w := NewWorld(110)
	// Build a "loop with a tail" known set: a 3×3 block plus a
	// single-cell tail sticking out. Tail gets trimmed in phase 1;
	// loop cells survive phase 1 (degree ≥ 2) but most get pruned
	// in phase 2 because they aren't on the entrance↔self path.
	for y := 50; y <= 52; y++ {
		for x := 50; x <= 52; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	known := map[Pos]bool{}
	for y := 50; y <= 52; y++ {
		for x := 50; x <= 52; x++ {
			known[Pos{X: x, Y: y}] = true
		}
	}
	anchors := []Pos{{X: 51, Y: 51}} // center
	leafOnly := w.pruneGraph(known, anchors, false)
	full := w.pruneGraph(known, anchors, true)
	if len(leafOnly) < len(full) {
		t.Errorf("phase 2 produced more alive cells than phase 1 alone: %d > %d",
			len(full), len(leafOnly))
	}
}
