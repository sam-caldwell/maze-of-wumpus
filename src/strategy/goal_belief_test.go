package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestBestFrontierForGoalBelief_SteersTowardGoal: given a set of
// frontier candidates, a solo S agent picks the one nearest its
// expected-goal centroid (maximum expected progress), not merely the
// first/nearest in BFS order.
func TestBestFrontierForGoalBelief_SteersTowardGoal(t *testing.T) {
	w := newConfiguredWorld(7)
	a := world.SpawnAgentForTest(w, '2') // strategy S
	a.Beliefs = world.NewAgentBeliefs()
	a.SwarmPeers = nil // solo: dispersion term off

	goal, ok := w.ExpectedGoalLocation(a)
	if !ok {
		t.Fatal("expected a goal belief for a fresh agent")
	}

	// Candidates: one sitting exactly on the believed goal, plus two
	// decoys far away. The first in slice order is a decoy, so a naive
	// "nearest in BFS order" picker would choose wrong.
	onGoal := goal
	decoyA := world.Pos{X: goal.X + 300, Y: goal.Y + 300}
	decoyB := world.Pos{X: goal.X - 250, Y: goal.Y + 50}
	candidates := []world.Pos{decoyA, decoyB, onGoal}

	got := bestFrontierForGoalBelief(w, a, candidates)
	if got != onGoal {
		t.Errorf("picked %v, want the on-belief candidate %v", got, onGoal)
	}
}

// TestBestFrontierForGoalBelief_DispersesAmongEqualGoalCandidates: with
// swarm peers present, two candidates equally close to the believed goal
// must be split by the dispersion term — the swarm picks the one farther
// from its peers, so members fan out instead of stacking on one path.
// (Regression guard: the goal-pull must not swamp dispersion.)
func TestBestFrontierForGoalBelief_DispersesAmongEqualGoalCandidates(t *testing.T) {
	w := newConfiguredWorld(7)
	a := world.SpawnAgentForTest(w, '2')
	a.Beliefs = world.NewAgentBeliefs()

	g, ok := w.ExpectedGoalLocation(a)
	if !ok {
		t.Fatal("expected a goal belief for a fresh agent")
	}
	// A peer sits well to the +Y side of the goal.
	a.SwarmPeers = []world.Pos{{X: g.X, Y: g.Y + 50}}

	near := world.Pos{X: g.X, Y: g.Y + 2}          // equal goal dist, NEAR the peer
	farFromPeer := world.Pos{X: g.X, Y: g.Y - 2}   // equal goal dist, AWAY from peer
	farFromGoal := world.Pos{X: g.X + 200, Y: g.Y} // sets the normalization range
	candidates := []world.Pos{near, farFromPeer, farFromGoal}

	got := bestFrontierForGoalBelief(w, a, candidates)
	if got != farFromPeer {
		t.Errorf("picked %v, want the equally-goal-near but peer-distant %v "+
			"(dispersion not competing)", got, farFromPeer)
	}
}

// TestBestFrontierForGoalBelief_NoBeliefFallsBackToNearest: when the
// goal belief is exhausted (everything observed) the picker reduces to
// the prior nearest-first behavior, returning candidates[0].
func TestBestFrontierForGoalBelief_NoBeliefFallsBackToNearest(t *testing.T) {
	w := newConfiguredWorld(7)
	a := world.SpawnAgentForTest(w, '2')
	a.Beliefs = world.NewAgentBeliefs()
	a.SwarmPeers = nil
	// Drain all prior mass so ExpectedGoalLocation reports false.
	totW, _, _ := w.Maze.GoalPrior()
	a.Beliefs.ObsW = totW
	if _, ok := w.ExpectedGoalLocation(a); ok {
		t.Fatal("test setup: belief should be exhausted")
	}
	first := world.Pos{X: 10, Y: 10}
	candidates := []world.Pos{first, {X: 500, Y: 500}}
	if got := bestFrontierForGoalBelief(w, a, candidates); got != first {
		t.Errorf("fallback picked %v, want nearest %v", got, first)
	}
}
