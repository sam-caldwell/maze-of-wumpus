// maze.go — random maze + room generation for Maze of Wumpus.
//
// The board is a fixed BoardWidth x BoardHeight grid of CellType values.
// Generation pipeline:
//
//   1. Recursive backtracker carves a perfect maze on the grid (every cell
//      reachable, no cycles). Paths are 1-cell wide; walls are 1-cell wide
//      between paths.
//   2. A random number of rectangular ROOMS (up to 4x4) are carved out at
//      random positions. Rooms naturally inherit their entry/exit points
//      from any underlying maze paths they cover or border.
//   3. The entrance is placed in the top-left path cell, the goal in the
//      bottom-right.
//   4. Fire pits are sprinkled into rooms (a few per maze).
package world

import (
	"math/rand"
)

// CellType categorizes every grid cell. Walls block movement; paths are
// walkable; entrance/goal/firepit are walkable cells with special meaning.
type CellType uint8

const (
	CellWall CellType = iota
	CellPath
	CellEntrance
	CellGoal
	CellFirePit
	CellWaterPit
)

const (
	BoardWidth  = 120
	BoardHeight = 80
)

// Pos is an integer grid coordinate. (0,0) is top-left.
type Pos struct {
	X, Y int
}

// Maze holds the generated terrain. It's immutable after GenerateMaze
// returns; entity positions and dynamic state (heat/stench) live in
// World, not here.
type Maze struct {
	Cells       [BoardHeight][BoardWidth]CellType
	EntrancePos Pos
	GoalPos     Pos
	FirePits    []Pos
	WaterPits   []Pos
	Rooms       []Room
}

// Room is a rectangular open area carved into the maze. Recorded so the
// game can place fire pits inside them.
type Room struct {
	X, Y, W, H int
}

// GenerateMaze builds a complete Maze using the given seeded RNG.
// Deterministic: same seed -> same maze.
func GenerateMaze(rng *rand.Rand) *Maze {
	m := &Maze{}
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			m.Cells[y][x] = CellWall
		}
	}
	carved := make([][]bool, BoardHeight)
	for i := range carved {
		carved[i] = make([]bool, BoardWidth)
	}
	var carve func(x, y int)
	carve = func(x, y int) {
		carved[y][x] = true
		m.Cells[y][x] = CellPath
		dirs := []Pos{{2, 0}, {-2, 0}, {0, 2}, {0, -2}}
		rng.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
		for _, d := range dirs {
			nx, ny := x+d.X, y+d.Y
			if nx < 0 || nx >= BoardWidth || ny < 0 || ny >= BoardHeight {
				continue
			}
			if carved[ny][nx] {
				continue
			}
			m.Cells[(y+ny)/2][(x+nx)/2] = CellPath
			carve(nx, ny)
		}
	}
	carve(0, 0)

	numRooms := 4 + rng.Intn(7)
	for i := 0; i < numRooms; i++ {
		w := 2 + rng.Intn(3)
		h := 2 + rng.Intn(3)
		x := 1 + rng.Intn(BoardWidth-w-2)
		y := 1 + rng.Intn(BoardHeight-h-2)
		for dy := 0; dy < h; dy++ {
			for dx := 0; dx < w; dx++ {
				m.Cells[y+dy][x+dx] = CellPath
			}
		}
		m.Rooms = append(m.Rooms, Room{X: x, Y: y, W: w, H: h})
	}

	m.EntrancePos = Pos{0, 0}
	m.GoalPos = Pos{BoardWidth - 2, BoardHeight - 2}
	m.Cells[m.EntrancePos.Y][m.EntrancePos.X] = CellEntrance
	m.Cells[m.GoalPos.Y][m.GoalPos.X] = CellGoal

	for _, r := range m.Rooms {
		nPits := rng.Intn(3)
		for j := 0; j < nPits; j++ {
			x := r.X + rng.Intn(r.W)
			y := r.Y + rng.Intn(r.H)
			p := Pos{x, y}
			if p == m.EntrancePos || p == m.GoalPos {
				continue
			}
			if m.Cells[y][x] != CellPath {
				continue
			}
			m.Cells[y][x] = CellFirePit
			m.FirePits = append(m.FirePits, p)
		}
	}
	numWater := 3 + rng.Intn(8)
	for j := 0; j < numWater; j++ {
		for attempts := 0; attempts < 100; attempts++ {
			x := rng.Intn(BoardWidth)
			y := rng.Intn(BoardHeight)
			p := Pos{x, y}
			if m.Cells[y][x] != CellPath {
				continue
			}
			if p == m.EntrancePos || p == m.GoalPos {
				continue
			}
			m.Cells[y][x] = CellWaterPit
			m.WaterPits = append(m.WaterPits, p)
			break
		}
	}
	return m
}

// IsWalkable reports whether an entity may step onto (or through) the
// given cell. Walls block; fire pits don't block movement per se —
// stepping onto one is the death event resolved by World.Step.
func (m *Maze) IsWalkable(p Pos) bool {
	if p.X < 0 || p.X >= BoardWidth || p.Y < 0 || p.Y >= BoardHeight {
		return false
	}
	return m.Cells[p.Y][p.X] != CellWall
}
