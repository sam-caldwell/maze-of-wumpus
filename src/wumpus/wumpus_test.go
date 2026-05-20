package wumpus

import (
	"math/rand"
	"testing"

	"maze-of-wumpus/src/world"
)

func newConfiguredWorld(seed int64) *world.World {
	return world.NewWorldWithConfig(world.Config{
		Seed:              seed,
		WumpusStrategy:    PickStrategy,
		VengeanceStrategy: VengeanceStrategy,
	})
}

// TestPickStrategy_ReturnsHuntStrategy: PickStrategy now returns a
// single unified entry point; dispatch happens at call time via
// wm.HuntMode. Validate it's non-nil for the wumpus path to work.
func TestPickStrategy_ReturnsHuntStrategy(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	s := PickStrategy(rng)
	if s == nil {
		t.Fatal("PickStrategy returned nil")
	}
}

// TestHuntStrategy_AllModesDoNotPanic exercises every HuntMode
// path so the dispatch switch is fully covered.
func TestHuntStrategy_AllModesDoNotPanic(t *testing.T) {
	w := newConfiguredWorld(102)
	w.EnableHazards()
	_ = world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	wm.Aggressiveness = world.WumpusAggressionMax
	for _, mode := range []world.WumpusHuntMode{
		world.WumpusHuntBayesian,
		world.WumpusHuntWander,
		world.WumpusHuntCrowd,
	} {
		wm.HuntMode = mode
		_ = HuntStrategy(w, wm)
	}
}

// TestBayesianHunt_ChasesScent: with full aggressiveness and only
// one cardinal neighbor carrying agent scent, bayesianHunt should
// prefer that neighbor (high probability — 50 trials × p≈1 → ≥40).
func TestBayesianHunt_ChasesScent(t *testing.T) {
	w := newConfiguredWorld(110)
	w.EnableHazards()
	wm := w.Wumpus[0]
	wm.Aggressiveness = world.WumpusAggressionMax
	wm.HuntMode = world.WumpusHuntBayesian
	// Move wumpus to a known open cell with all cardinals walkable.
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = world.Pos{X: 40, Y: 40}
	w.WumpusAt[40][40] = wm
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		np := world.Pos{X: 40 + d.X, Y: 40 + d.Y}
		w.Maze.Cells[np.Y][np.X] = world.CellPath
		w.ScentOwner[np.Y][np.X] = 0
	}
	scentTarget := world.Pos{X: 41, Y: 40}
	w.ScentOwner[scentTarget.Y][scentTarget.X] = '1'
	w.ScentCycle[scentTarget.Y][scentTarget.X] = 1
	w.Cycle = 1
	hits := 0
	for i := 0; i < 50; i++ {
		if got := bayesianHunt(w, wm); got == scentTarget {
			hits++
		}
	}
	if hits < 40 {
		t.Errorf("bayesianHunt picked scent target %d/50, want ≥40", hits)
	}
}

// TestCommitsToHunt_AggressivenessExtremes: aggression=0 always
// rolls false; aggression=MAX always rolls true.
func TestCommitsToHunt_AggressivenessExtremes(t *testing.T) {
	w := newConfiguredWorld(111)
	wm := &world.Wumpus{Aggressiveness: 0}
	for i := 0; i < 50; i++ {
		if commitsToHunt(w, wm) {
			t.Errorf("aggression=0 should never commit; trial %d", i)
		}
	}
	wm.Aggressiveness = world.WumpusAggressionMax
	for i := 0; i < 50; i++ {
		if !commitsToHunt(w, wm) {
			t.Errorf("aggression=MAX should always commit; trial %d", i)
		}
	}
}

// TestCrowdSightings_OnlyCrowdHuntersDetect: a Bayesian-mode wumpus
// next to an agent does NOT contribute to the crowd's sightings;
// only WumpusHuntCrowd members do.
func TestCrowdSightings_OnlyCrowdHuntersDetect(t *testing.T) {
	w := newConfiguredWorld(112)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '1')
	// Force all wumpus to NON-crowd.
	for _, wm := range w.Wumpus {
		wm.HuntMode = world.WumpusHuntBayesian
		w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
		wm.Alive = false
	}
	// Spawn a single Crowd-mode wumpus right next to the agent.
	wm := w.Wumpus[0]
	wm.HuntMode = world.WumpusHuntCrowd
	wm.Alive = true
	wm.Pos = world.Pos{X: a.Pos.X + 1, Y: a.Pos.Y}
	if !world.InBounds(wm.Pos.X, wm.Pos.Y) {
		t.Skip("entrance at world edge")
	}
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = wm
	sights := crowdSightings(w)
	if len(sights) != 1 || sights[0] != a.Pos {
		t.Errorf("crowdSightings = %v, want [%v]", sights, a.Pos)
	}
	// Switch the wumpus back to Bayesian — no crowd members → no sightings.
	wm.HuntMode = world.WumpusHuntBayesian
	if got := crowdSightings(w); len(got) != 0 {
		t.Errorf("non-crowd: crowdSightings = %v, want empty", got)
	}
}

// TestRandomNeighbor_AllBlocked: a fully-walled wumpus has no move.
func TestRandomNeighbor_AllBlocked(t *testing.T) {
	w := newConfiguredWorld(103)
	w.EnableHazards()
	wm := w.Wumpus[0]
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = world.Pos{X: 40, Y: 40}
	w.WumpusAt[40][40] = wm
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[40+d.Y][40+d.X] = world.CellWall
	}
	if got := RandomNeighbor(w, wm); got != wm.Pos {
		t.Errorf("walled-in wumpus = %v, want %v", got, wm.Pos)
	}
}

// TestVengeanceStrategy_ChasesScent: VengeanceStrategy ignores
// HuntMode/Aggressiveness and goes straight at the freshest agent
// scent — equivalent to bayesianHunt at full commitment.
func TestVengeanceStrategy_ChasesScent(t *testing.T) {
	w := newConfiguredWorld(120)
	w.EnableHazards()
	wm := w.Wumpus[0]
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = world.Pos{X: 40, Y: 40}
	w.WumpusAt[40][40] = wm
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		np := world.Pos{X: 40 + d.X, Y: 40 + d.Y}
		w.Maze.Cells[np.Y][np.X] = world.CellPath
		w.ScentOwner[np.Y][np.X] = 0
	}
	scentTarget := world.Pos{X: 41, Y: 40}
	w.ScentOwner[scentTarget.Y][scentTarget.X] = '1'
	w.ScentCycle[scentTarget.Y][scentTarget.X] = 1
	w.Cycle = 1
	if got := VengeanceStrategy(w, wm); got != scentTarget {
		t.Errorf("vengeance: got %v, want %v", got, scentTarget)
	}
}

// TestIsAgentLabel: accepts 1..9, A..C; rejects wumpus letters
// and unset.
func TestIsAgentLabel(t *testing.T) {
	for _, r := range []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C'} {
		if !isAgentLabel(r) {
			t.Errorf("isAgentLabel(%c) = false, want true", r)
		}
	}
	for _, r := range []rune{0, 'Z', 'W'} {
		if isAgentLabel(r) {
			t.Errorf("isAgentLabel(%c) = true, want false", r)
		}
	}
}
