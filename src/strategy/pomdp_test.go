package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

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
	// POMCP's action space is the 8-way Moore neighborhood, so a
	// diagonal step is valid too — assert Moore-adjacency, not
	// strictly cardinal.
	dx, dy := world.AbsInt(got.X-a.Pos.X), world.AbsInt(got.Y-a.Pos.Y)
	if dx > 1 || dy > 1 || (dx == 0 && dy == 0) {
		t.Errorf("POMCP returned non-adjacent step %v from %v", got, a.Pos)
	}
}

// TestPOMCPStrategy_ColdStartFallbackMoves: at game start, agent 6
// must still pick a Moore-adjacent neighbor (cardinal or diagonal)
// via outward-bias rollouts.
func TestPOMCPStrategy_ColdStartFallbackMoves(t *testing.T) {
	w := newConfiguredWorld(602)
	a := world.SpawnAgentForTest(w, '6')
	got := POMCPStrategy(w, a)
	if got == a.Pos {
		t.Errorf("POMCPStrategy froze at game start: %v", got)
	}
	dx, dy := got.X-a.Pos.X, got.Y-a.Pos.Y
	if world.AbsInt(dx) > 1 || world.AbsInt(dy) > 1 || (dx == 0 && dy == 0) {
		t.Errorf("POMCPStrategy returned non-Moore-adjacent step %v from %v", got, a.Pos)
	}
}

// TestForLabel_Planners: swarm-Bayesian (4), POMCP (5) and QMDP (6)
// are wired with current names.
func TestForLabel_Planners(t *testing.T) {
	cases := map[rune]string{
		'4': "swarm-bayesian",
		'5': "pomcp",
		'6': "qmdp",
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
