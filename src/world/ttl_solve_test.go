package world

import "testing"

// TestCheckGoal_PromotedCloneWinDoesNotPoisonBestSolveDistance is a
// regression guard for the TTL-strangulation bug: a swarm win can arrive
// on a promoted clone whose ActualDistance was reset to the clone's tiny
// local travel (KillAgent's body-swap). Recording that raw distance set
// BestSolveDistance BELOW the true entrance→goal shortest path, so
// TTLCeiling tightened the budget below anything a fresh life (walking
// from the entrance) could achieve — and every future attempt died of
// TTL before reaching the goal.
//
// A recorded solve must never be shorter than the omniscient optimum.
func TestCheckGoal_PromotedCloneWinDoesNotPoisonBestSolveDistance(t *testing.T) {
	w := NewWorld(99)
	a := w.AgentByLabel('2') // strategy S
	a.Alive = true
	a.Disabled = false
	a.Pos = w.Maze.GoalPos
	a.OptimalDistance = 335
	// Simulate the promoted-clone win: ActualDistance far below the true
	// entrance→goal shortest path, and no known route (so the optimum
	// floor is what protects us).
	a.Stats.ActualDistance = 180
	a.Stats.BestSolveDistance = 0
	a.KnownCells = map[Pos]bool{a.Pos: true}
	a.KnownShortestPath = nil

	w.CheckGoal()

	if a.Stats.GoalsReached != 1 {
		t.Fatalf("win not recorded: GoalsReached=%d", a.Stats.GoalsReached)
	}
	if a.Stats.BestSolveDistance < a.OptimalDistance {
		t.Errorf("BestSolveDistance=%d recorded below the true optimum %d — "+
			"TTLCeiling would strangle every future life",
			a.Stats.BestSolveDistance, a.OptimalDistance)
	}
	// And the TTL ceiling it drives must be at least the true optimum.
	if ttl := w.TTLCeiling(a); ttl < a.OptimalDistance {
		t.Errorf("TTLCeiling=%d below optimum %d — agent can't reach the goal",
			ttl, a.OptimalDistance)
	}
}
