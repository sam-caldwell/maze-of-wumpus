package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// branchTestWorld carves a 4-way junction at (40, 40) with walkable
// neighbors on every cardinal side, and parks the agent at the
// junction.
func branchTestWorld(t *testing.T, seed int64, label rune) (*world.World, *world.Agent) {
	t.Helper()
	w := newConfiguredWorld(seed)
	a := world.SpawnAgentForTest(w, label)
	// Build a 4-way junction so branchCandidates returns multiple.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		w.Maze.Cells[np.Y][np.X] = world.CellPath
	}
	// Force the goal to a cardinal neighbor so BFSStrategy can
	// always produce a plan from this synthetic junction — without
	// this, the test depends on the random maze actually connecting
	// (40,40) to w.Maze.GoalPos, which broke whenever upstream
	// changes shifted the RNG state.
	w.Maze.GoalPos = world.Pos{X: 41, Y: 40}
	a.HasLastFrom = false // no "back direction" — all 4 branches count
	a.Plan = []world.Pos{{X: 41, Y: 40}}
	return w, a
}

// TestBranchAnim_InitOnFirstCall: a branch-point detect on the first
// strategy invocation creates SearchAnim and freezes the agent.
func TestBranchAnim_InitOnFirstCall(t *testing.T) {
	w, a := branchTestWorld(t, 400, '2')
	got := BFSStrategy(w, a)
	if got != a.Pos {
		t.Errorf("first call at branch returned %v, want a.Pos %v", got, a.Pos)
	}
	if a.SearchAnim == nil {
		t.Fatal("SearchAnim was not initialized")
	}
	if a.SearchAnim.Phase != 1 || a.SearchAnim.Depth != 1 {
		t.Errorf("anim state = phase %d depth %d, want phase 1 depth 1",
			a.SearchAnim.Phase, a.SearchAnim.Depth)
	}
	if len(a.SearchAnim.BranchDirs) < 2 {
		t.Errorf("BranchDirs = %d, want >= 2", len(a.SearchAnim.BranchDirs))
	}
}

// TestBranchAnim_ExpandThenRetractThenCommit drives the full cycle:
// SearchAnimMaxDepth=3 → 3 expand ticks + 3 retract ticks + commit.
func TestBranchAnim_ExpandThenRetractThenCommit(t *testing.T) {
	w, a := branchTestWorld(t, 401, '2')
	// Tick 1: init at depth 1, phase 1.
	BFSStrategy(w, a)
	if a.SearchAnim.Phase != 1 || a.SearchAnim.Depth != 1 {
		t.Fatalf("tick 1 = phase %d depth %d", a.SearchAnim.Phase, a.SearchAnim.Depth)
	}
	// Tick 2: depth 2 (still expanding).
	BFSStrategy(w, a)
	if a.SearchAnim.Phase != 1 || a.SearchAnim.Depth != 2 {
		t.Fatalf("tick 2 = phase %d depth %d", a.SearchAnim.Phase, a.SearchAnim.Depth)
	}
	// Tick 3: depth 3, switch to retracting.
	BFSStrategy(w, a)
	if a.SearchAnim.Phase != 2 || a.SearchAnim.Depth != 3 {
		t.Fatalf("tick 3 = phase %d depth %d (want phase 2 depth 3)",
			a.SearchAnim.Phase, a.SearchAnim.Depth)
	}
	// Ticks 4-5: retracting.
	BFSStrategy(w, a)
	BFSStrategy(w, a)
	if a.SearchAnim == nil || a.SearchAnim.Depth != 1 {
		t.Fatalf("tick 5 expected depth 1 retracting, got %+v", a.SearchAnim)
	}
	// Tick 6: depth → 0, anim cleared, commit chosen step.
	chosen := a.SearchAnim.ChosenStep
	got := BFSStrategy(w, a)
	if a.SearchAnim != nil {
		t.Errorf("SearchAnim should be nil after final retract")
	}
	if got != chosen {
		t.Errorf("final call returned %v, want chosen %v", got, chosen)
	}
}

// TestBranchAnim_NoAnimInCorridor: agent in a 1-way corridor (only
// the forward cell is walkable and non-backwards) does NOT animate.
func TestBranchAnim_NoAnimInCorridor(t *testing.T) {
	w := newConfiguredWorld(402)
	a := world.SpawnAgentForTest(w, '2')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = world.CellPath
	// Only one walkable neighbor (east).
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if d.X == 1 && d.Y == 0 {
			w.Maze.Cells[np.Y][np.X] = world.CellPath
		} else {
			w.Maze.Cells[np.Y][np.X] = world.CellWall
		}
	}
	a.Plan = []world.Pos{{X: 41, Y: 40}}
	a.HasLastFrom = false
	BFSStrategy(w, a)
	if a.SearchAnim != nil {
		t.Errorf("agent in 1-way corridor should not animate, got %+v", a.SearchAnim)
	}
}

// TestBranchAnim_DFSToo: agent 3 (DFS) animates at branch points too.
func TestBranchAnim_DFSToo(t *testing.T) {
	w, a := branchTestWorld(t, 403, '3')
	got := DFSStrategy(w, a)
	if got != a.Pos {
		t.Errorf("DFS first call returned %v, want a.Pos %v", got, a.Pos)
	}
	if a.SearchAnim == nil {
		t.Fatal("DFS did not initialize SearchAnim at branch point")
	}
}

// TestBranchCandidates_ExcludesLastFromCell: when the agent has just
// come from one direction, that direction is not a branch candidate.
func TestBranchCandidates_ExcludesLastFromCell(t *testing.T) {
	w, a := branchTestWorld(t, 404, '2')
	a.HasLastFrom = true
	a.LastFromCell = world.Pos{X: 40, Y: 39} // came from north
	got := branchCandidates(w, a)
	for _, d := range got {
		if d.X == 0 && d.Y == -1 {
			t.Errorf("branch candidates includes back direction: %+v", got)
		}
	}
}
