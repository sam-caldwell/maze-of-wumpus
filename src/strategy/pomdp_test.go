package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestScentFollower_PrefersFreshLeaderTrail: agent 6 walks toward
// the neighbor with the strongest leader-scent freshness signal.
// Forces CurrentTrustee = '2' since the test plants agent 2's scent
// at south — without this the per-map trustee picked by NewWorld
// could be '5' (agent 6's other attract candidate).
func TestScentFollower_PrefersFreshLeaderTrail(t *testing.T) {
	w := newConfiguredWorld(500)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentTrustee = '2'
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	// Carve a 3×3 open block so the per-agent prune (leaf-trim +
	// articulation) keeps every cardinal neighbor alive. A bare +
	// shape would have every arm trimmed as a degree-1 leaf.
	for y := 39; y <= 41; y++ {
		for x := 39; x <= 41; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	w.MarkAgentSensed(a)
	// Plant scent: east neighbor is a STALE deposit from agent 1
	// (long ago); south neighbor is a FRESH deposit from agent 2.
	// Agent 4 (scent-follower) should pick south.
	w.Cycle = 200
	east := world.Pos{X: 41, Y: 40}
	south := world.Pos{X: 40, Y: 41}
	w.ScentOwner[east.Y][east.X] = '1'
	w.ScentCycle[east.Y][east.X] = 50 // age 150 → freshness 0.85
	w.ScentOwner[south.Y][south.X] = '2'
	w.ScentCycle[south.Y][south.X] = 195 // age 5 → freshness ~0.995 (fresher)
	got := ScentFollowerStrategy(w, a)
	if got != south {
		t.Errorf("scent-follower picked %v, want south %v (fresher scent)", got, south)
	}
}

// TestScentFollower_IgnoresNonLeaderScent: scent from other
// followers (5,6,7) is not "leader" scent (leaders are {1,2,3}),
// so it should be ignored.
func TestScentFollower_IgnoresNonLeaderScent(t *testing.T) {
	w := newConfiguredWorld(501)
	a := world.SpawnAgentForTest(w, '4')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	// 3×3 open block (same reason as the test above).
	for y := 39; y <= 41; y++ {
		for x := 39; x <= 41; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	w.MarkAgentSensed(a)
	w.Cycle = 10
	// Both neighbors have FRESH scent from non-leaders.
	w.ScentOwner[40][41] = '6'
	w.ScentCycle[40][41] = 9
	w.ScentOwner[41][40] = '7'
	w.ScentCycle[41][40] = 9
	got := ScentFollowerStrategy(w, a)
	if got == a.Pos {
		t.Error("scent-follower froze when only non-leader scent was available")
	}
}

// TestScentFollower_ColdStartMovesOutward: at game start with no
// scent anywhere, scent-follower picks an outward-bias neighbor.
func TestScentFollower_ColdStartMovesOutward(t *testing.T) {
	w := newConfiguredWorld(502)
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '4')
	got := ScentFollowerStrategy(w, a)
	if got == a.Pos {
		t.Errorf("scent-follower froze at game start: %v", got)
	}
	dx, dy := got.X-a.Pos.X, got.Y-a.Pos.Y
	if world.AbsInt(dx)+world.AbsInt(dy) != 1 {
		t.Errorf("returned non-cardinal step %v from %v", got, a.Pos)
	}
}

// TestPOMCPStrategy_PicksAMove: POMCP (agent 6) returns a valid
// cardinal neighbor (or a.Pos if boxed in).
func TestPOMCPStrategy_PicksAMove(t *testing.T) {
	w := newConfiguredWorld(503)
	a := world.SpawnAgentForTest(w, '6')
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
	got := POMCPStrategy(w, a)
	if got == a.Pos {
		t.Skip("agent boxed in at this seed")
	}
	dx, dy := got.X-a.Pos.X, got.Y-a.Pos.Y
	if world.AbsInt(dx)+world.AbsInt(dy) != 1 {
		t.Errorf("POMCP returned non-cardinal step %v from %v", got, a.Pos)
	}
}

// TestPOMCPStrategy_ColdStartFallbackMoves: at game start, agent 7
// must still pick a cardinal neighbor via outward-bias rollouts.
func TestPOMCPStrategy_ColdStartFallbackMoves(t *testing.T) {
	w := newConfiguredWorld(602)
	killAllWumpus(w)
	a := world.SpawnAgentForTest(w, '6')
	got := POMCPStrategy(w, a)
	if got == a.Pos {
		t.Errorf("POMCPStrategy froze at game start: %v", got)
	}
	dx, dy := got.X-a.Pos.X, got.Y-a.Pos.Y
	if world.AbsInt(dx)+world.AbsInt(dy) != 1 {
		t.Errorf("POMCPStrategy returned non-cardinal step %v from %v", got, a.Pos)
	}
}

// TestForLabel_ScentFollowerAndPlanners: scent-follower (4), POMCP
// (6) and QMDP (7) are wired with current names.
func TestForLabel_ScentFollowerAndPlanners(t *testing.T) {
	cases := map[rune]string{
		'4': "scent-follower",
		'6': "pomcp",
		'7': "qmdp",
	}
	for label, want := range cases {
		if ForLabel(label) == nil {
			t.Errorf("ForLabel(%c) = nil", label)
		}
		if got := Name(label); got != want {
			t.Errorf("Name(%c) = %q, want %q", label, got, want)
		}
	}
}
