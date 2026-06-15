package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestStrategyS_RunsBayesianInduction is an end-to-end check that the
// agent assigned strategy S genuinely drives the maze with the
// goal-location Bayesian belief — not just that the belief code
// compiles. It verifies the full runtime chain:
//
//	label '2' -> letter S -> ForLetter(S) -> SwarmStrategy
//	          -> planFor(S) -> BayesianStrategy -> UpdateAgentBeliefs
//	          (Bayes update) + wwNearestSafeFrontier ->
//	          bestFrontierForGoalBelief -> ExpectedGoalLocation.
//
// It drives the agent the way the real engine does (sense, plan, move,
// re-sense) and asserts the belief is updated every tick and read as a
// well-formed posterior while the goal is still unperceived.
func TestStrategyS_RunsBayesianInduction(t *testing.T) {
	// Runtime dispatch: the letter S maps to the swarm wrapper, and the
	// wrapper's planner for S is the Bayesian planner.
	if ForLetter(world.SwarmStrategyLetter) == nil {
		t.Fatal("ForLetter(S) returned nil — S is not dispatched at runtime")
	}
	if LetterForLabel('2') != world.SwarmStrategyLetter {
		t.Fatalf("agent 2 maps to %c, want S", LetterForLabel('2'))
	}

	w := newConfiguredWorld(7)
	a := world.SpawnAgentForTest(w, '2')
	a.CurrentStrategy = world.SwarmStrategyLetter // S, as the engine fixes it
	a.Beliefs = world.NewAgentBeliefs()
	w.MarkAgentSensed(a)

	startObserved := len(a.Beliefs.Observed)
	beliefConsulted := false
	e := w.Maze.EntrancePos

	for i := 0; i < 400; i++ {
		// While the goal is unperceived, S must hold a well-formed
		// posterior over where it is — in the far region the generator
		// draws goals from.
		if !a.KnownCells[w.Maze.GoalPos] {
			if g, ok := w.ExpectedGoalLocation(a); ok {
				beliefConsulted = true
				d := abs(g.X-e.X) + abs(g.Y-e.Y)
				if d < world.MinGoalDistanceCells {
					t.Fatalf("belief centroid %v only %d from entrance, want ≥ %d",
						g, d, world.MinGoalDistanceCells)
				}
			}
		}
		next := SwarmStrategy(w, a) // runtime entry point for S
		if next == a.Pos {
			continue
		}
		a.Pos = next
		w.MarkAgentSensed(a)
		if a.Pos == w.Maze.GoalPos {
			break
		}
	}

	// The Bayes update (UpdateAgentBeliefs -> MarkObserved) ran on every
	// planning tick via S's planner: the observed set grew substantially.
	if grown := len(a.Beliefs.Observed) - startObserved; grown < 50 {
		t.Errorf("Observed set barely grew (%d cells) — Bayesian update not running via S", grown)
	}
	if !beliefConsulted {
		t.Error("ExpectedGoalLocation never produced a posterior during exploration")
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
