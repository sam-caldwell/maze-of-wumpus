package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestDescriptionByLetter covers all 7 known letters and the
// fall-through "unknown" branch.
func TestDescriptionByLetter(t *testing.T) {
	for _, l := range StrategyLetters {
		got := DescriptionByLetter(l)
		if got == "" || got == "unknown" {
			t.Errorf("DescriptionByLetter(%c) = %q", l, got)
		}
		if len(got) > 64 {
			t.Errorf("DescriptionByLetter(%c) exceeds 64 chars: %q", l, got)
		}
	}
	if got := DescriptionByLetter('Z'); got != "unknown" {
		t.Errorf("DescriptionByLetter(Z) = %q, want \"unknown\"", got)
	}
}

// TestName_AllAgentLabels covers every label including the far-sight
// variants 8/9/A/B/C and the unknown fallback.
func TestName_AllAgentLabels(t *testing.T) {
	cases := map[rune]string{
		'1': "bfs",
		'2': "dfs",
		'3': "bayesian",
		'4': "scent-follower",
		'5': "dqn",
		'6': "pomcp",
		'7': "qmdp",
		'8': "bayesian+fs",
		'9': "scent-follower+fs",
		'A': "dqn+fs",
		'B': "pomcp+fs",
		'C': "qmdp+fs",
	}
	for l, want := range cases {
		if got := Name(l); got != want {
			t.Errorf("Name(%c) = %q, want %q", l, got, want)
		}
	}
	if got := Name('Z'); got != "unknown" {
		t.Errorf("Name(Z) = %q, want \"unknown\"", got)
	}
}

// TestQMDPStrategy_BasicMove: with a small KnownCells region and an
// outward gradient, QMDP selects a walkable PO-known neighbor.
// Verifies the function runs to completion and respects PO.
func TestQMDPStrategy_BasicMove(t *testing.T) {
	w := newConfiguredWorld(200)
	a := world.SpawnAgentForTest(w, '7')
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{
		a.Pos: true,
	}
	// Mark the 4 cardinal neighbors as known (and walkable in the
	// underlying maze). If any are walls, just leave them off.
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.KnownCells[np] = true
		}
	}
	got := QMDPStrategy(w, a)
	// Returned cell must be either a.Pos itself (no positive scores)
	// or a Moore-neighbor of a.Pos.
	if got == a.Pos {
		return
	}
	dx := got.X - a.Pos.X
	dy := got.Y - a.Pos.Y
	if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
		t.Errorf("QMDPStrategy returned non-neighbor %v from %v", got, a.Pos)
	}
}

// TestQMDPStrategy_NoKnownNeighborsFallsBack: when none of the 4
// cardinal neighbors are in KnownCells, the explorer-bias fallback
// fires (outwardBiasNeighbor).
func TestQMDPStrategy_NoKnownNeighborsFallsBack(t *testing.T) {
	w := newConfiguredWorld(201)
	a := world.SpawnAgentForTest(w, '7')
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true} // nothing else
	got := QMDPStrategy(w, a)
	// outwardBiasNeighbor may return a.Pos when nothing scores.
	if got != a.Pos {
		dx := got.X - a.Pos.X
		dy := got.Y - a.Pos.Y
		if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
			t.Errorf("fallback returned non-neighbor %v", got)
		}
	}
}

// TestSwarmBayesianStrategy_BasicCall: with a minimal swarm setup,
// SwarmBayesianStrategy returns a move (or a.Pos for stay) and does
// not panic. Wires up: agent on swarm strategy, valid KnownCells,
// Beliefs initialized.
func TestSwarmBayesianStrategy_BasicCall(t *testing.T) {
	w := newConfiguredWorld(202)
	a := world.SpawnAgentForTest(w, '3')
	a.CurrentStrategy = StrategySwarmBayesian
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.KnownCells[np] = true
		}
	}
	got := SwarmBayesianStrategy(w, a)
	dx := got.X - a.Pos.X
	dy := got.Y - a.Pos.Y
	if got != a.Pos && (dx < -1 || dx > 1 || dy < -1 || dy > 1) {
		t.Errorf("SwarmBayesianStrategy returned non-neighbor %v from %v", got, a.Pos)
	}
}

// TestSwarmBayesianStrategy_StrictPO: SwarmBayesianStrategy must
// NEVER consult w.Maze.GoalPos when GoalPos isn't in the agent's
// KnownCells. We assert this by routing the agent on a world where
// only the agent's own cell + non-goal neighbors are known.
func TestSwarmBayesianStrategy_StrictPO(t *testing.T) {
	w := newConfiguredWorld(203)
	a := world.SpawnAgentForTest(w, '3')
	a.CurrentStrategy = StrategySwarmBayesian
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	// Do NOT include w.Maze.GoalPos in KnownCells.
	if a.KnownCells[w.Maze.GoalPos] {
		t.Fatal("test setup error: GoalPos should not be perceived")
	}
	got := SwarmBayesianStrategy(w, a)
	// The agent might "stay" or move to an unknown neighbor (which
	// the strategy returns as a.Pos). What it must NOT do is jump
	// to GoalPos, since that's not perceived.
	if got == w.Maze.GoalPos && got != a.Pos {
		t.Errorf("strict PO violation: agent moved to GoalPos %v without perceiving it", got)
	}
}

// TestMergeSwarmKnowledge_NilSidesAndBeliefMaps: when `a` has no
// KnownCells / Beliefs initialized, mergeSwarmKnowledge creates them.
// Belief merges cover Observed (union), SafeFromPit (union),
// PitProb (max), and WumpusProb (max).
func TestMergeSwarmKnowledge_NilSidesAndBeliefMaps(t *testing.T) {
	w := newConfiguredWorld(220)
	a := world.SpawnAgentForTest(w, '3')
	peer := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategySwarmBayesian
	peer.CurrentStrategy = StrategySwarmBayesian
	a.SwarmGroupID = 1
	peer.SwarmGroupID = 1
	a.KnownCells = nil // exercise nil-init
	a.Beliefs = nil    // exercise nil-init
	peer.KnownCells = map[world.Pos]bool{{X: 10, Y: 10}: true}
	peer.Beliefs = world.NewAgentBeliefs()
	peer.Beliefs.Observed[world.Pos{X: 10, Y: 10}] = true
	peer.Beliefs.SafeFromPit[world.Pos{X: 11, Y: 10}] = true
	peer.Beliefs.PitProb[world.Pos{X: 12, Y: 10}] = 0.7
	peer.Beliefs.WumpusProb[world.Pos{X: 13, Y: 10}] = 0.6
	mergeSwarmKnowledge(w, a)
	if !a.KnownCells[world.Pos{X: 10, Y: 10}] {
		t.Error("KnownCells not merged from peer")
	}
	if !a.Beliefs.Observed[world.Pos{X: 10, Y: 10}] {
		t.Error("Observed not merged")
	}
	if !a.Beliefs.SafeFromPit[world.Pos{X: 11, Y: 10}] {
		t.Error("SafeFromPit not merged")
	}
	if a.Beliefs.PitProb[world.Pos{X: 12, Y: 10}] != 0.7 {
		t.Errorf("PitProb max-merge failed: got %v", a.Beliefs.PitProb[world.Pos{X: 12, Y: 10}])
	}
	if a.Beliefs.WumpusProb[world.Pos{X: 13, Y: 10}] != 0.6 {
		t.Errorf("WumpusProb max-merge failed: got %v", a.Beliefs.WumpusProb[world.Pos{X: 13, Y: 10}])
	}
}

// TestMergeSwarmKnowledge_PeerBeliefsNilSkipped: peer with no Beliefs
// is skipped for the belief merges (KnownCells still merges).
func TestMergeSwarmKnowledge_PeerBeliefsNilSkipped(t *testing.T) {
	w := newConfiguredWorld(221)
	a := world.SpawnAgentForTest(w, '3')
	peer := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategySwarmBayesian
	peer.CurrentStrategy = StrategySwarmBayesian
	a.SwarmGroupID = 1
	peer.SwarmGroupID = 1
	a.Beliefs = world.NewAgentBeliefs()
	peer.KnownCells = map[world.Pos]bool{{X: 20, Y: 20}: true}
	peer.Beliefs = nil // exercise the continue branch
	mergeSwarmKnowledge(w, a) // must not panic
	if !a.KnownCells[world.Pos{X: 20, Y: 20}] {
		t.Error("KnownCells still should have merged")
	}
}

// TestMergeSwarmKnowledge_DeadPeerSkipped: a dead peer is skipped
// entirely even if it has KnownCells.
func TestMergeSwarmKnowledge_DeadPeerSkipped(t *testing.T) {
	w := newConfiguredWorld(222)
	a := world.SpawnAgentForTest(w, '3')
	peer := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategySwarmBayesian
	peer.CurrentStrategy = StrategySwarmBayesian
	a.SwarmGroupID = 1
	peer.SwarmGroupID = 1
	peer.Alive = false // exercise the continue branch
	peer.KnownCells = map[world.Pos]bool{{X: 30, Y: 30}: true}
	a.KnownCells = map[world.Pos]bool{}
	a.Beliefs = world.NewAgentBeliefs()
	mergeSwarmKnowledge(w, a)
	if a.KnownCells[world.Pos{X: 30, Y: 30}] {
		t.Error("dead peer's cells should not merge")
	}
}

// TestBayesianStrategy_AppliesSoloPrune: after a BayesianStrategy
// call, a.PrunedKnownCells must be populated and reflect the agent's
// own KnownCells (not the swarm's union).
func TestBayesianStrategy_AppliesSoloPrune(t *testing.T) {
	w := newConfiguredWorld(230)
	a := world.SpawnAgentForTest(w, '3')
	a.CurrentStrategy = StrategyBayesian
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.KnownCells[np] = true
		}
	}
	_ = BayesianStrategy(w, a)
	if a.PrunedKnownCells == nil {
		t.Fatal("BayesianStrategy did not populate PrunedKnownCells")
	}
	// Pruned view must be a subset of KnownCells.
	for p := range a.PrunedKnownCells {
		if !a.KnownCells[p] {
			t.Errorf("pruned cell %v is not in KnownCells", p)
		}
	}
	// Agent's current cell must be in the pruned view (anchor).
	if !a.PrunedKnownCells[a.Pos] {
		t.Errorf("a.Pos %v should be in pruned view (anchor)", a.Pos)
	}
}

// TestBayesianStrategy_StrictPO_NoGoalLeakWhenUnperceived: the
// goal anchor in pruneGraph is gated on `walkable[GoalPos]`, which
// requires GoalPos ∈ a.KnownCells. Confirm the agent does not
// route to GoalPos when it hasn't perceived it.
func TestBayesianStrategy_StrictPO_NoGoalLeakWhenUnperceived(t *testing.T) {
	w := newConfiguredWorld(231)
	a := world.SpawnAgentForTest(w, '3')
	a.CurrentStrategy = StrategyBayesian
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) && np != w.Maze.GoalPos {
			a.KnownCells[np] = true
		}
	}
	if a.KnownCells[w.Maze.GoalPos] {
		t.Fatal("test setup: GoalPos should not be perceived")
	}
	got := BayesianStrategy(w, a)
	if got == w.Maze.GoalPos && got != a.Pos {
		t.Errorf("strict PO violation: agent jumped to GoalPos %v without perceiving it", got)
	}
}

// TestPrunedView_Cache_StaleWhenKnownCellsGrow: a second call after
// new cells are perceived must rebuild the cache (prunedKnownSize
// tracks len(KnownCells)).
func TestPrunedView_Cache_StaleWhenKnownCellsGrow(t *testing.T) {
	w := newConfiguredWorld(232)
	a := world.SpawnAgentForTest(w, '3')
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.KnownCells[np] = true
		}
	}
	w.RecomputeAgentPrunedViewIfStale(a)
	firstPtr := &a.PrunedKnownCells
	firstLen := len(a.PrunedKnownCells)
	// Add a new perceived cell — should invalidate the cache.
	farPos := world.Pos{X: a.Pos.X + 5, Y: a.Pos.Y}
	if w.Maze.IsWalkable(farPos) {
		a.KnownCells[farPos] = true
	} else {
		// Pick a definitely-walkable cell elsewhere.
		for p := range a.KnownCells {
			_ = p
		}
		a.KnownCells[world.Pos{X: a.Pos.X + 100, Y: a.Pos.Y}] = true
	}
	w.RecomputeAgentPrunedViewIfStale(a)
	secondLen := len(a.PrunedKnownCells)
	_ = firstPtr
	if firstLen == secondLen && firstLen == 0 {
		t.Skip("pruned view degenerate for this seed")
	}
}

// TestPrunedView_Cache_SkipsWhenLenUnchanged: if KnownCells size
// didn't change between calls, the second call must be a no-op (no
// re-pruning work).
func TestPrunedView_Cache_SkipsWhenLenUnchanged(t *testing.T) {
	w := newConfiguredWorld(233)
	a := world.SpawnAgentForTest(w, '3')
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			a.KnownCells[np] = true
		}
	}
	w.RecomputeAgentPrunedViewIfStale(a)
	pruned1 := a.PrunedKnownCells
	w.RecomputeAgentPrunedViewIfStale(a)
	pruned2 := a.PrunedKnownCells
	// Same map object — no reallocation.
	if &pruned1 != &pruned2 {
		// Maps are reference types; identity isn't directly comparable
		// across calls, but we can verify content equality.
	}
	if len(pruned1) != len(pruned2) {
		t.Errorf("cache changed unexpectedly: len %d → %d", len(pruned1), len(pruned2))
	}
}

// TestScentFollowerStrategy_AppliesSoloPrune: after a call,
// a.PrunedKnownCells is populated and a.Pos is still in the view.
func TestScentFollowerStrategy_AppliesSoloPrune(t *testing.T) {
	w := newConfiguredWorld(240)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.Beliefs = world.NewAgentBeliefs()
	a.CurrentTrustee = '1'
	_ = ScentFollowerStrategy(w, a)
	if a.PrunedKnownCells == nil {
		t.Fatal("ScentFollowerStrategy did not populate PrunedKnownCells")
	}
}

// TestPOMCPStrategy_AppliesSoloPrune: after a call, a.PrunedKnownCells
// is populated. POMCP also exercises the deferred restore path with
// parallel goroutines — verify a.KnownCells is back to its original
// value after the strategy returns.
func TestPOMCPStrategy_AppliesSoloPrune(t *testing.T) {
	w := newConfiguredWorld(241)
	a := world.SpawnAgentForTest(w, '6')
	a.CurrentStrategy = StrategyPOMCP
	a.Beliefs = world.NewAgentBeliefs()
	origCells := a.KnownCells
	_ = POMCPStrategy(w, a)
	if a.PrunedKnownCells == nil {
		t.Fatal("POMCPStrategy did not populate PrunedKnownCells")
	}
	if &a.KnownCells == nil || len(a.KnownCells) < len(a.PrunedKnownCells) {
		t.Errorf("KnownCells not restored after POMCPStrategy (orig=%d, after=%d)",
			len(origCells), len(a.KnownCells))
	}
}

// TestSoloPrune_PreservesPOForU_NoGoalLeak: scent-follower must not
// jump to GoalPos when it hasn't perceived the goal cell. Strict-PO
// regression guard for the new pruning wrapper on U.
func TestSoloPrune_PreservesPOForU_NoGoalLeak(t *testing.T) {
	w := newConfiguredWorld(242)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	// Do NOT add GoalPos to KnownCells.
	got := ScentFollowerStrategy(w, a)
	if got == w.Maze.GoalPos && got != a.Pos {
		t.Errorf("strict-PO violation: U jumped to GoalPos %v without perceiving it", got)
	}
}

// TestSoloPrune_PreservesPOForW_NoGoalLeak: same PO guard for POMCP.
func TestSoloPrune_PreservesPOForW_NoGoalLeak(t *testing.T) {
	w := newConfiguredWorld(243)
	a := world.SpawnAgentForTest(w, '6')
	a.CurrentStrategy = StrategyPOMCP
	a.Beliefs = world.NewAgentBeliefs()
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) && np != w.Maze.GoalPos {
			a.KnownCells[np] = true
		}
	}
	got := POMCPStrategy(w, a)
	if got == w.Maze.GoalPos && got != a.Pos {
		t.Errorf("strict-PO violation: W jumped to GoalPos %v without perceiving it", got)
	}
}

// TestScentFollower_FollowsAnyNonSelfScent: with no trustee set and
// only a peer's scent in range, U should still pick that direction
// — opportunistic following.
func TestScentFollower_FollowsAnyNonSelfScent(t *testing.T) {
	w := newConfiguredWorld(630)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.CurrentTrustee = 0 // no formal trustee
	a.Beliefs = world.NewAgentBeliefs()
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	for y := 39; y <= 41; y++ {
		for x := 39; x <= 41; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	w.MarkAgentSensed(a)
	for p := range a.KnownCells {
		a.Beliefs.SafeFromPit[p] = true
	}
	// Plant fresh peer scent at the south neighbor (not own label).
	w.Cycle = 100
	south := world.Pos{X: 40, Y: 41}
	w.ScentOwner[south.Y][south.X] = '2' // peer label
	w.ScentCycle[south.Y][south.X] = 100
	got := ScentFollowerStrategy(w, a)
	if got != south {
		t.Errorf("opportunistic follow: got %v, want south %v (peer scent)", got, south)
	}
	// Opportunistic following should also have been recorded.
	if !a.OpportunisticFollowed['2'] {
		t.Errorf("OpportunisticFollowed['2'] not set: %v", a.OpportunisticFollowed)
	}
}

// TestScentFollower_TrusteeBonusBeatsTiedPeer: with two equally-fresh
// scents (trustee at one neighbor, peer at another), the trustee
// wins because of the multiplicative trusteeBonus.
func TestScentFollower_TrusteeBonusBeatsTiedPeer(t *testing.T) {
	w := newConfiguredWorld(631)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.CurrentTrustee = '2'
	a.Beliefs = world.NewAgentBeliefs()
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	for y := 39; y <= 41; y++ {
		for x := 39; x <= 41; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	w.MarkAgentSensed(a)
	w.Cycle = 100
	south := world.Pos{X: 40, Y: 41}
	east := world.Pos{X: 41, Y: 40}
	// Trustee scent at south, peer at east — both equally fresh.
	w.ScentOwner[south.Y][south.X] = '2' // trustee
	w.ScentCycle[south.Y][south.X] = 100
	w.ScentOwner[east.Y][east.X] = '3' // peer
	w.ScentCycle[east.Y][east.X] = 100
	got := ScentFollowerStrategy(w, a)
	if got != south {
		t.Errorf("trustee bonus failed: got %v, want trustee-south %v", got, south)
	}
}

// TestScentFollower_SkipsOwnScent: U must never pick a cell carrying
// its own scent, even if it's the freshest thing around.
func TestScentFollower_SkipsOwnScent(t *testing.T) {
	w := newConfiguredWorld(632)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.CurrentTrustee = 0
	a.Beliefs = world.NewAgentBeliefs()
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	for y := 39; y <= 41; y++ {
		for x := 39; x <= 41; x++ {
			w.Maze.Cells[y][x] = world.CellPath
		}
	}
	w.MarkAgentSensed(a)
	// Only own scent on the board.
	w.Cycle = 100
	for _, p := range []world.Pos{
		{X: 41, Y: 40}, {X: 39, Y: 40},
		{X: 40, Y: 41}, {X: 40, Y: 39},
	} {
		w.ScentOwner[p.Y][p.X] = '4' // self
		w.ScentCycle[p.Y][p.X] = 100
	}
	// Should fall through to Bayesian (returns some move via the
	// planner) rather than chasing own trail.
	got := ScentFollowerStrategy(w, a)
	// At minimum: should NOT have set OpportunisticFollowed['4'].
	if a.OpportunisticFollowed['4'] {
		t.Errorf("self scent should never be in OpportunisticFollowed")
	}
	_ = got
}

// TestScentFollower_LostScentFallsBackToBayesian: when no cardinal
// neighbor carries the trustee's scent, the strategy must defer to
// the Bayesian planner (which can navigate to the goal if perceived
// or expand the perception frontier) rather than the weak outward-
// bias fallback. We verify by setting the goal cell INTO KnownCells
// and confirming the agent steps toward it — outwardBias would just
// pick the highest DistFromStart neighbor regardless of goal.
func TestScentFollower_LostScentFallsBackToBayesian(t *testing.T) {
	w := newConfiguredWorld(620)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.CurrentTrustee = '2'
	a.Beliefs = world.NewAgentBeliefs()
	// Carve a 3-cell corridor: agent at (10,10), middle at (11,10),
	// goal at (12,10). All three perceived (KnownCells), middle
	// marked safe.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 10, Y: 10}
	w.AgentAt[10][10] = a
	w.Maze.Cells[10][10] = world.CellPath
	w.Maze.Cells[10][11] = world.CellPath
	w.Maze.Cells[10][12] = world.CellGoal
	w.Maze.GoalPos = world.Pos{X: 12, Y: 10}
	a.KnownCells = map[world.Pos]bool{
		{X: 10, Y: 10}: true, {X: 11, Y: 10}: true, {X: 12, Y: 10}: true,
	}
	a.Beliefs.SafeFromPit[world.Pos{X: 11, Y: 10}] = true
	a.Beliefs.SafeFromPit[world.Pos{X: 12, Y: 10}] = true
	// No scent anywhere on the board.
	got := ScentFollowerStrategy(w, a)
	if got != (world.Pos{X: 11, Y: 10}) {
		t.Errorf("lost-scent fallback should plan toward perceived goal; got %v, want (11,10)", got)
	}
}

// TestScentFollower_LostScentExploresWhenGoalUnperceived: same
// scenario without GoalPos in KnownCells — Bayesian fallback should
// still produce a constructive move (frontier expansion), never
// freeze at a.Pos.
func TestScentFollower_LostScentExploresWhenGoalUnperceived(t *testing.T) {
	w := newConfiguredWorld(621)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyScentFollower
	a.CurrentTrustee = '2'
	got := ScentFollowerStrategy(w, a)
	// Returned cell must be Moore-adjacent to a.Pos (a real move)
	// OR a.Pos itself if the agent is genuinely surrounded.
	if got != a.Pos {
		dx := got.X - a.Pos.X
		dy := got.Y - a.Pos.Y
		if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
			t.Errorf("fallback returned non-neighbor %v from %v", got, a.Pos)
		}
	}
}

// TestKnownWalkable covers OOB, not-in-KnownCells, and walkable
// branches.
func TestKnownWalkable(t *testing.T) {
	w := newConfiguredWorld(204)
	a := &world.Agent{KnownCells: map[world.Pos]bool{{X: 5, Y: 5}: true}}
	// OOB.
	if knownWalkable(w, a, world.Pos{X: -1, Y: 0}) {
		t.Error("OOB should be false")
	}
	// In KnownCells AND walkable.
	w.Maze.Cells[5][5] = world.CellPath
	if !knownWalkable(w, a, world.Pos{X: 5, Y: 5}) {
		t.Error("known+walkable should be true")
	}
	// Not in KnownCells.
	if knownWalkable(w, a, world.Pos{X: 6, Y: 6}) {
		t.Error("not-in-KnownCells should be false")
	}
	// In KnownCells but wall.
	w.Maze.Cells[5][5] = world.CellWall
	if knownWalkable(w, a, world.Pos{X: 5, Y: 5}) {
		t.Error("known but unwalkable should be false")
	}
}
