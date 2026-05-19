package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestPOMDPStrategy_PrefersClosestSafeNeighbor: with all neighbors
// equally safe, agent 6 picks the one closest to the goal.
func TestPOMDPStrategy_PrefersClosestSafeNeighbor(t *testing.T) {
	w := newConfiguredWorld(500)
	a := world.SpawnAgentForTest(w, '6')
	// Park the agent at an interior cell with known neighbors.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[40+d.Y][40+d.X] = world.CellPath
	}
	// Pick whichever neighbor has the lowest (best) BFS distance to
	// goal — agent 6 computes the same distance via its own BFS.
	bestDist := 1 << 30
	var want world.Pos
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !w.Maze.IsWalkable(np) {
			continue
		}
		dist := bfsDistToGoal(w, np)
		if dist >= 0 && dist < bestDist {
			bestDist = dist
			want = np
		}
	}
	if bestDist == 1<<30 {
		t.Skip("no walkable neighbor reaches goal at this seed")
	}
	got := POMDPStrategy(w, a)
	if got != want {
		t.Errorf("POMDPStrategy = %v, want %v (best BFS dist %d)",
			got, want, bestDist)
	}
}

// TestPOMCPStrategy_PicksAMove: agent 7 returns a valid cardinal
// neighbor (or a.Pos if boxed in).
func TestPOMCPStrategy_PicksAMove(t *testing.T) {
	w := newConfiguredWorld(502)
	a := world.SpawnAgentForTest(w, '7')
	got := POMCPStrategy(w, a)
	if got == a.Pos {
		t.Skip("agent boxed in at this seed")
	}
	dx, dy := got.X-a.Pos.X, got.Y-a.Pos.Y
	if world.AbsInt(dx)+world.AbsInt(dy) != 1 {
		t.Errorf("POMCP returned non-cardinal step %v from %v", got, a.Pos)
	}
}

// TestForLabel_6and7: the new strategy labels are wired.
func TestForLabel_6and7(t *testing.T) {
	if ForLabel('6') == nil {
		t.Error("ForLabel('6') = nil")
	}
	if ForLabel('7') == nil {
		t.Error("ForLabel('7') = nil")
	}
	if Name('6') != "pomdp" {
		t.Errorf("Name('6') = %q, want pomdp", Name('6'))
	}
	if Name('7') != "pomcp" {
		t.Errorf("Name('7') = %q, want pomcp", Name('7'))
	}
}
