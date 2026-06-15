package world

import (
	"math/rand"
	"testing"
)

// TestPickRandomGoal_NeverPicksUnreachablePocket: a maze with a far-away
// ISOLATED walkable pocket (the exact unsolvable-maze condition) must
// never have its goal placed in that pocket — pickRandomGoal only chooses
// from cells reachable from the entrance.
func TestPickRandomGoal_NeverPicksUnreachablePocket(t *testing.T) {
	m := &Maze{Cells: newGrid[CellType]()} // all walls
	entrance := Pos{X: 1, Y: 0}
	m.Cells[0][1] = CellEntrance
	// A small connected corridor from the entrance (all < MinGoalDistance).
	for y := 1; y <= 10; y++ {
		m.Cells[y][1] = CellPath
	}
	// An isolated pocket FAR away (≥ MinGoalDistanceCells) — walkable but
	// not connected to the entrance corridor.
	pocket := []Pos{{X: 1000, Y: 1000}, {X: 1001, Y: 1000}}
	for _, p := range pocket {
		m.Cells[p.Y][p.X] = CellPath
	}

	reachable := reachableFrom(m, entrance)
	for _, p := range pocket {
		if reachable[p] {
			t.Fatalf("setup: pocket cell %v should be unreachable", p)
		}
	}
	for seed := int64(0); seed < 25; seed++ {
		goal := pickRandomGoal(m, entrance, rand.New(rand.NewSource(seed)))
		if !reachable[goal] {
			t.Errorf("seed %d: goal %v is NOT reachable from the entrance", seed, goal)
		}
		for _, p := range pocket {
			if goal == p {
				t.Errorf("seed %d: goal landed in the isolated pocket %v", seed, p)
			}
		}
	}
}

// TestGenerateMaze_GoalAlwaysReachable: every generated maze has its goal
// reachable from the entrance — the maze is always solvable.
func TestGenerateMaze_GoalAlwaysReachable(t *testing.T) {
	for seed := int64(0); seed < 8; seed++ {
		m := GenerateMaze(rand.New(rand.NewSource(seed)))
		if !reachableFrom(m, m.EntrancePos)[m.GoalPos] {
			t.Errorf("seed %d: goal %v unreachable from entrance %v",
				seed, m.GoalPos, m.EntrancePos)
		}
	}
}
