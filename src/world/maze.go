// maze.go — random maze + room generation for Maze of Wumpus.
//
// The board is a fixed BoardWidth x BoardHeight grid of CellType values.
// Generation pipeline:
//
//  1. Recursive backtracker carves a perfect maze on the grid (every cell
//     reachable, no cycles). Paths are 1-cell wide; walls are 1-cell wide
//     between paths.
//  2. A random number of IRREGULAR ROOMS are flood-carved at random
//     positions. Each room targets a random area in [4, MaxRoomArea]
//     (currently 2500 = 50×50 worth of cells), bounded inside a 50×50
//     window. Shape is the connected-blob result of a random-frontier
//     flood — never a perfect rectangle, often a meandering organic
//     pocket.
//  3. The entrance is placed in the top-left path cell, the goal in the
//     bottom-right.
//  4. Fire pits are sprinkled into rooms (a few per maze).
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

// MinGoalDistanceCells is the minimum Manhattan distance from
// EntrancePos that a randomly-placed GoalPos must satisfy. Half
// the (W+H) bound makes the goal land in the far half of the map
// regardless of where the entrance sits.
const MinGoalDistanceCells = (BoardWidth + BoardHeight) / 2

// Room size bounds for the irregular-blob room carver.
//
//	MaxRoomDim:  bounding-box side length for any room (W and H each
//	             ≤ this), so rooms fit in a 50×50 window.
//	MaxRoomArea: target carved-cell count is capped here. The carver
//	             rolls a uniform random target in [MinRoomArea, MaxRoomArea]
//	             and flood-carves until it hits the target (or runs out
//	             of frontier within the bounding box).
//	MinRoomArea: floor so rooms aren't degenerate.
const (
	MaxRoomDim  = 50
	MaxRoomArea = 2500
	MinRoomArea = 4
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

// OpenFieldProbability is the chance per GenerateMaze call that the
// returned maze skips the corridor carver entirely and produces an
// open arena bounded only by perimeter walls. Tuned to ~20% so most
// seeds still produce proper mazes but enough open-field maps appear
// to give scent-following and Bayesian frontier agents a meaningful
// stress test.
const OpenFieldProbability = 0.20

// GenerateMaze builds a complete Maze using the given seeded RNG.
// Deterministic: same seed -> same maze.
//
// With probability OpenFieldProbability the maze is an open arena
// (perimeter walls, fully-open interior, entrance at (1,1), goal at
// the diametrically opposite interior corner). Otherwise a standard
// recursive-backtracker maze is carved from (0,0). Either way the
// downstream room / fire-pit / water-pit population runs.
func GenerateMaze(rng *rand.Rand) *Maze {
	m := &Maze{}
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			m.Cells[y][x] = CellWall
		}
	}
	openField := rng.Float64() < OpenFieldProbability
	if openField {
		// Carve every interior cell — perimeter stays walled.
		for y := 1; y < BoardHeight-1; y++ {
			for x := 1; x < BoardWidth-1; x++ {
				m.Cells[y][x] = CellPath
			}
		}
	} else {
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
	}

	numRooms := 4 + rng.Intn(7)
	for i := 0; i < numRooms; i++ {
		r := carveIrregularRoom(m, rng)
		if r != nil {
			m.Rooms = append(m.Rooms, *r)
		}
	}

	if openField {
		m.EntrancePos = Pos{1, 1}
	} else {
		m.EntrancePos = Pos{0, 0}
	}
	m.Cells[m.EntrancePos.Y][m.EntrancePos.X] = CellEntrance
	// Goal placement: uniformly random walkable cell at Manhattan
	// distance ≥ MinGoalDistanceCells from the entrance. Falls back
	// to the diametric corner if (somehow) no candidate exists.
	m.GoalPos = pickRandomGoal(m, m.EntrancePos, rng)
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

// carveIrregularRoom flood-carves a single random-shape room inside
// the maze. The room fits within a MaxRoomDim × MaxRoomDim bounding
// box anchored at a random position, and its carved-cell count is
// a random target in [MinRoomArea, MaxRoomArea] (capped also by
// the bounding-box area). The shape is the connected blob produced
// by a random-frontier flood from a seed cell — never a perfect
// rectangle.
//
// Returns the Room (bounding box) describing the area, or nil if
// the carver couldn't seat a room (degenerate board).
func carveIrregularRoom(m *Maze, rng *rand.Rand) *Room {
	// Bounding-box dimensions in [4, MaxRoomDim], clamped so the
	// box fits inside the board with a 1-cell wall margin.
	maxW := MaxRoomDim
	if maxW > BoardWidth-2 {
		maxW = BoardWidth - 2
	}
	maxH := MaxRoomDim
	if maxH > BoardHeight-2 {
		maxH = BoardHeight - 2
	}
	if maxW < 4 || maxH < 4 {
		return nil
	}
	w := 4 + rng.Intn(maxW-3)
	h := 4 + rng.Intn(maxH-3)
	x := 1 + rng.Intn(BoardWidth-w-1)
	y := 1 + rng.Intn(BoardHeight-h-1)

	// Target carved-cell count.
	boxArea := w * h
	maxTarget := MaxRoomArea
	if maxTarget > boxArea {
		maxTarget = boxArea
	}
	target := MinRoomArea + rng.Intn(maxTarget-MinRoomArea+1)

	// Random-frontier flood from a random seed cell inside the box.
	inBox := func(p Pos) bool {
		return p.X >= x && p.X < x+w && p.Y >= y && p.Y < y+h
	}
	seed := Pos{x + rng.Intn(w), y + rng.Intn(h)}
	carved := map[Pos]bool{seed: true}
	m.Cells[seed.Y][seed.X] = CellPath
	frontier := []Pos{}
	pushFrontier := func(p Pos) {
		for _, d := range []Pos{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			np := Pos{p.X + d.X, p.Y + d.Y}
			if !inBox(np) || carved[np] {
				continue
			}
			frontier = append(frontier, np)
		}
	}
	pushFrontier(seed)
	for len(carved) < target && len(frontier) > 0 {
		i := rng.Intn(len(frontier))
		next := frontier[i]
		frontier[i] = frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		if carved[next] {
			continue
		}
		carved[next] = true
		m.Cells[next.Y][next.X] = CellPath
		pushFrontier(next)
	}
	return &Room{X: x, Y: y, W: w, H: h}
}

// pickRandomGoal selects a walkable cell with Manhattan distance
// ≥ MinGoalDistanceCells from `entrance`. Uniform random pick among
// qualifying candidates. Falls back to (BoardWidth-2, BoardHeight-2)
// (the legacy diametric corner) if no candidate qualifies.
func pickRandomGoal(m *Maze, entrance Pos, rng *rand.Rand) Pos {
	var candidates []Pos
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			p := Pos{X: x, Y: y}
			if p == entrance {
				continue
			}
			if !m.IsWalkable(p) {
				continue
			}
			if absInt(x-entrance.X)+absInt(y-entrance.Y) < MinGoalDistanceCells {
				continue
			}
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return Pos{X: BoardWidth - 2, Y: BoardHeight - 2}
	}
	return candidates[rng.Intn(len(candidates))]
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
