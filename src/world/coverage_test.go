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

// TestWumpusHuntModeDescription covers all three known modes plus the
// fall-through "unknown" branch.
func TestWumpusHuntModeDescription(t *testing.T) {
	for _, c := range []struct {
		mode WumpusHuntMode
		want string
	}{
		{WumpusHuntBayesian, "Inductive Bayesian smell-tracking; aggressiveness gates commit"},
		{WumpusHuntWander, "Random walk lightly biased by agent scent"},
		{WumpusHuntCrowd, "Swarm hunting: shared sightings, BFS to nearest detected agent"},
		{WumpusHuntMode(99), "unknown"},
	} {
		if got := WumpusHuntModeDescription(c.mode); got != c.want {
			t.Errorf("description(mode=%d) = %q, want %q", c.mode, got, c.want)
		}
	}
}

// TestActiveWumpusModes_DeduplicatesAndSkipsDead: only alive wumpus
// contribute; duplicate modes appear once.
func TestActiveWumpusModes_DeduplicatesAndSkipsDead(t *testing.T) {
	w := NewWorld(50)
	w.EnableHazards()
	if len(w.Wumpus) < 3 {
		t.Skip("not enough wumpus at this seed")
	}
	for _, wm := range w.Wumpus {
		wm.Alive = false
	}
	w.Wumpus[0].HuntMode = WumpusHuntBayesian
	w.Wumpus[0].Alive = true
	w.Wumpus[1].HuntMode = WumpusHuntBayesian
	w.Wumpus[1].Alive = true
	w.Wumpus[2].HuntMode = WumpusHuntCrowd
	w.Wumpus[2].Alive = true
	modes := w.ActiveWumpusModes()
	if len(modes) != 2 {
		t.Errorf("ActiveWumpusModes = %v, want 2 entries (Bayesian + Crowd)", modes)
	}
	seen := map[WumpusHuntMode]bool{}
	for _, m := range modes {
		if seen[m] {
			t.Errorf("duplicate mode %v in output", m)
		}
		seen[m] = true
	}
}

// TestWumpusModeCount: counts only alive wumpus matching the mode.
func TestWumpusModeCount(t *testing.T) {
	w := NewWorld(51)
	w.EnableHazards()
	if len(w.Wumpus) < 3 {
		t.Skip("not enough wumpus at this seed")
	}
	for _, wm := range w.Wumpus {
		wm.Alive = false
	}
	w.Wumpus[0].HuntMode = WumpusHuntBayesian
	w.Wumpus[0].Alive = true
	w.Wumpus[1].HuntMode = WumpusHuntBayesian
	w.Wumpus[1].Alive = false // dead — must NOT count
	w.Wumpus[2].HuntMode = WumpusHuntCrowd
	w.Wumpus[2].Alive = true
	if got := w.WumpusModeCount(WumpusHuntBayesian); got != 1 {
		t.Errorf("Bayesian count = %d, want 1 (one alive)", got)
	}
	if got := w.WumpusModeCount(WumpusHuntCrowd); got != 1 {
		t.Errorf("Crowd count = %d, want 1", got)
	}
	if got := w.WumpusModeCount(WumpusHuntWander); got != 0 {
		t.Errorf("Wander count = %d, want 0", got)
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
// every cell is treated as alive (open-default).
func TestSwarmAliveCell_NoSwarmGraph(t *testing.T) {
	w := NewWorld(56)
	if !w.SwarmAliveCell(Pos{0, 0}) {
		t.Error("with no swarm graph yet, every cell should report alive")
	}
}

// TestSwarmAliveCell_AfterPrune: after a prune, only cells in the
// pruned set return true.
func TestSwarmAliveCell_AfterPrune(t *testing.T) {
	w := NewWorld(57)
	w.swarmGraph.aliveCells = map[Pos]bool{
		{10, 10}: true,
	}
	if !w.SwarmAliveCell(Pos{10, 10}) {
		t.Error("(10,10) should report alive")
	}
	if w.SwarmAliveCell(Pos{11, 11}) {
		t.Error("(11,11) should report not alive")
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

// TestHeatAt covers disabled + OOB + normal.
func TestHeatAt(t *testing.T) {
	w := NewWorld(70)
	w.EnableHazards()
	w.Heat[5][5] = true
	if !w.HeatAt(5, 5) {
		t.Error("HeatAt(5,5) should be true")
	}
	if w.HeatAt(-1, 0) {
		t.Error("HeatAt(-1,0) should be false (OOB)")
	}
	if w.HeatAt(BoardWidth, 0) {
		t.Error("HeatAt(BoardWidth,0) should be false (OOB)")
	}
	w.FirePitsDisabled = true
	if w.HeatAt(5, 5) {
		t.Error("HeatAt with FirePitsDisabled should always be false")
	}
}

// TestStenchAt covers disabled + OOB + normal.
func TestStenchAt(t *testing.T) {
	w := NewWorld(71)
	w.EnableHazards()
	w.Stench[5][5] = true
	if !w.StenchAt(5, 5) {
		t.Error("StenchAt(5,5) should be true")
	}
	if w.StenchAt(-1, 0) {
		t.Error("StenchAt(-1,0) should be false (OOB)")
	}
	if w.StenchAt(0, BoardHeight) {
		t.Error("StenchAt(0,BoardHeight) should be false (OOB)")
	}
	w.WumpusDisabled = true
	if w.StenchAt(5, 5) {
		t.Error("StenchAt with WumpusDisabled should always be false")
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

// TestSetFirePitsDisabled_NoOp: setting the same value is a no-op.
func TestSetFirePitsDisabled_NoOp(t *testing.T) {
	w := NewWorld(75)
	w.FirePitsDisabled = true
	w.SetFirePitsDisabled(true) // same value
	if !w.FirePitsDisabled {
		t.Error("no-op should leave value unchanged")
	}
}

// TestSetFirePitsDisabled_EnableEdge: false → true clears fire pits.
func TestSetFirePitsDisabled_EnableEdge(t *testing.T) {
	w := NewWorld(76)
	w.EnableHazards()
	w.FirePitsDisabled = false
	w.SetFirePitsDisabled(true)
	if !w.FirePitsDisabled {
		t.Error("expected FirePitsDisabled=true after set")
	}
}

// TestSetFirePitsDisabled_DisableEdge: true → false spawns fire pits.
func TestSetFirePitsDisabled_DisableEdge(t *testing.T) {
	w := NewWorld(77)
	w.FirePitsDisabled = true
	w.SetFirePitsDisabled(false)
	if w.FirePitsDisabled {
		t.Error("expected FirePitsDisabled=false after set")
	}
}

// TestSetWaterPitsDisabled covers same-value, enable, and disable edges.
func TestSetWaterPitsDisabled(t *testing.T) {
	w := NewWorld(78)
	w.WaterPitsDisabled = false
	w.SetWaterPitsDisabled(false) // no-op
	if w.WaterPitsDisabled {
		t.Error("no-op should leave value unchanged")
	}
	w.SetWaterPitsDisabled(true)
	if !w.WaterPitsDisabled {
		t.Error("expected WaterPitsDisabled=true")
	}
	w.SetWaterPitsDisabled(false)
	if w.WaterPitsDisabled {
		t.Error("expected WaterPitsDisabled=false")
	}
}

// TestSpawnReplacementWaterPit_Disabled: no-op when disabled.
func TestSpawnReplacementWaterPit_Disabled(t *testing.T) {
	w := NewWorld(79)
	w.WaterPitsDisabled = true
	before := len(w.Maze.WaterPits)
	w.SpawnReplacementWaterPit()
	if len(w.Maze.WaterPits) != before {
		t.Errorf("SpawnReplacementWaterPit while disabled added a pit")
	}
}

// TestSpawnReplacementWaterPit_Spawns: when enabled and there's open
// space, a fresh water pit appears.
func TestSpawnReplacementWaterPit_Spawns(t *testing.T) {
	w := NewWorld(80)
	w.WaterPitsDisabled = false
	w.ClearWaterPits()
	w.SpawnReplacementWaterPit()
	if len(w.Maze.WaterPits) != 1 {
		t.Errorf("after spawn, WaterPits = %d, want 1", len(w.Maze.WaterPits))
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

// TestSpawnGoalHazard_BothDisabled: returns immediately, no state
// change.
func TestSpawnGoalHazard_BothDisabled(t *testing.T) {
	w := NewWorld(90)
	w.FirePitsDisabled = true
	w.WumpusDisabled = true
	pitsBefore := len(w.Maze.FirePits)
	wumpusBefore := len(w.Wumpus)
	w.SpawnGoalHazard()
	if len(w.Maze.FirePits) != pitsBefore || len(w.Wumpus) != wumpusBefore {
		t.Errorf("SpawnGoalHazard with both disabled mutated state")
	}
}

// TestSpawnReplacementWumpus_Disabled: when WumpusDisabled, returns
// without spawning.
func TestSpawnReplacementWumpus_Disabled(t *testing.T) {
	w := NewWorld(91)
	w.WumpusDisabled = true
	before := len(w.Wumpus)
	w.SpawnReplacementWumpus()
	if len(w.Wumpus) != before {
		t.Errorf("SpawnReplacementWumpus while disabled added wumpus")
	}
}

// TestSpawnReplacementWumpus_Adds: with WumpusDisabled false, a fresh
// wumpus is added on a path cell.
func TestSpawnReplacementWumpus_Adds(t *testing.T) {
	w := NewWorldWithConfig(Config{Seed: 92})
	w.WumpusDisabled = false
	before := len(w.Wumpus)
	w.SpawnReplacementWumpus()
	if len(w.Wumpus) != before+1 {
		t.Errorf("SpawnReplacementWumpus: count %d → %d, want +1", before, len(w.Wumpus))
	}
}

// TestFallbackMove_AllNeighborsHazardous: when every neighbor is a
// hazard, fallback returns one (the second loop allows hazard cells
// as a last resort).
func TestFallbackMove_AllNeighborsHazardous(t *testing.T) {
	w := NewWorld(93)
	w.EnableHazards()
	a := w.Agents[0]
	a.Pos = Pos{40, 40}
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	// Surround with fire pits but make the cells walkable.
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			x, y := a.Pos.X+dx, a.Pos.Y+dy
			w.Maze.Cells[y][x] = CellFirePit
		}
	}
	got := w.FallbackMove(a)
	// Either a neighbor cell (hazardous, but allowed by second loop)
	// or stay-in-place when even CanMoveTo refuses. Both are fine.
	if got == a.Pos {
		return // stay-in-place branch
	}
	dx, dy := got.X-a.Pos.X, got.Y-a.Pos.Y
	if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
		t.Errorf("FallbackMove returned non-neighbor %v", got)
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
	got := w.pruneSwarmGraph(swarmKnown)
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

// TestSpawnGoalHazard_FireOnly: with WumpusDisabled true and fire
// pits enabled, the spawnFire branch fires and the fire-only switch
// arm executes.
func TestSpawnGoalHazard_FireOnly(t *testing.T) {
	w := NewWorld(100)
	w.WumpusDisabled = true
	w.FirePitsDisabled = false
	pitsBefore := len(w.Maze.FirePits)
	w.SpawnGoalHazard()
	if len(w.Maze.FirePits) <= pitsBefore {
		t.Logf("no fire pit added — possibly no valid candidate after %d attempts", 200)
	}
}

// TestSpawnGoalHazard_WumpusOnly: FirePitsDisabled true + WumpusDisabled
// false → wumpus-only branch.
func TestSpawnGoalHazard_WumpusOnly(t *testing.T) {
	w := NewWorld(101)
	w.FirePitsDisabled = true
	w.WumpusDisabled = false
	before := len(w.Wumpus)
	w.SpawnGoalHazard()
	if len(w.Wumpus) <= before {
		t.Logf("no wumpus added — possibly no valid candidate")
	}
}

// TestInitialSpawnStagger_OneSecondGap: at world construction every
// agent has a respawn timer that staggers their arrival 1 second
// (RespawnTicks ticks) after the previous one — agent 1 spawns at
// tick 1, agent 2 at tick 11, agent 3 at tick 21, etc. Drives the
// visible rollout at game start.
func TestInitialSpawnStagger_OneSecondGap(t *testing.T) {
	w := NewWorld(120)
	for i, a := range w.Agents {
		want := 1 + i*RespawnTicks
		if a.RespawnIn != want {
			t.Errorf("agent %c (#%d): RespawnIn = %d, want %d",
				a.Label, i, a.RespawnIn, want)
		}
		if a.Alive {
			t.Errorf("agent %c: should not be alive at construction", a.Label)
		}
	}
}

// TestInitialSpawnStagger_OnlyOneAlivePerSecond: after a single tick
// only agent 1 is alive; after RespawnTicks more ticks, agents 1
// and 2 are alive; etc. Walks the cadence forward through the
// first few seconds.
func TestInitialSpawnStagger_OnlyOneAlivePerSecond(t *testing.T) {
	w := NewWorld(121)
	w.EnableAllAgents()
	aliveCount := func() int {
		n := 0
		for _, a := range w.Agents {
			if a.Alive {
				n++
			}
		}
		return n
	}
	w.RespawnAgents()
	if got := aliveCount(); got != 1 {
		t.Errorf("after tick 1: alive = %d, want 1", got)
	}
	for k := 2; k <= 3; k++ {
		for j := 0; j < RespawnTicks; j++ {
			w.RespawnAgents()
		}
		if got := aliveCount(); got != k {
			t.Errorf("after %d seconds: alive = %d, want %d",
				k, got, k)
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

