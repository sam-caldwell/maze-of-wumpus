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
package world

import (
	"math/rand"
	"sync"
)

// CellType categorizes every grid cell. Walls block movement; paths are
// walkable; entrance/goal are walkable cells with special meaning.
type CellType uint8

const (
	CellWall CellType = iota
	CellPath
	CellEntrance
	CellGoal
)

// BoardWidth / BoardHeight are the maze dimensions in cells. They
// default to 1024×1024 but are runtime-configurable via SetBoardSize
// (wired to the --size CLI flag). They are package VARIABLES rather than
// constants because the grids they size are now slices, allocated at
// world/maze construction from whatever value is in effect then. Set the
// size ONCE at startup, before constructing any World or Maze.
var (
	BoardWidth  = 1024
	BoardHeight = 1024
)

// MinGoalDistanceCells is the minimum Manhattan distance from
// EntrancePos that a randomly-placed GoalPos must satisfy. Half
// the (W+H) bound makes the goal land in the far half of the map
// regardless of where the entrance sits. Recomputed by SetBoardSize.
var MinGoalDistanceCells = (BoardWidth + BoardHeight) / 2

// SetBoardSize sets the maze dimensions (columns × rows) and recomputes
// the derived bounds. Call it once at startup BEFORE any World or Maze is
// constructed — the grids are sized from these values at construction
// time. Non-positive dimensions are ignored.
func SetBoardSize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	BoardWidth = width
	BoardHeight = height
	MinGoalDistanceCells = (BoardWidth + BoardHeight) / 2
}

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
// returns; entity positions live in World, not here.
type Maze struct {
	Cells       [][]CellType // [BoardHeight][BoardWidth], allocated at GenerateMaze
	EntrancePos Pos
	GoalPos     Pos
	Rooms       []Room

	// Lazily-computed goal-location prior (see goal_belief.go). The
	// totals depend only on EntrancePos + board dims, both fixed once
	// the maze is generated, so they're memoized on first use.
	goalPriorOnce                        sync.Once
	goalPriorW, goalPriorWX, goalPriorWY float64
}

// Room is a rectangular open area carved into the maze.
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
// downstream room population runs.
func GenerateMaze(rng *rand.Rand) *Maze {
	m := &Maze{Cells: newGrid[CellType]()}
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

	// EntrancePos: a non-corner perimeter cell. (1, 0) is on the top
	// edge but not at the (0, 0) corner. Both variants carve a
	// doorway from this cell inward to ensure connectivity.
	m.EntrancePos = Pos{1, 0}
	// Make sure (1, 0) and its inward neighbor are walkable so the
	// entrance connects to the carved/open interior.
	m.Cells[0][1] = CellEntrance
	if m.Cells[1][1] == CellWall {
		m.Cells[1][1] = CellPath
	}
	// Goal placement: uniformly random walkable cell at Manhattan
	// distance ≥ MinGoalDistanceCells from the entrance. Falls back
	// to the diametric corner if (somehow) no candidate exists.
	m.GoalPos = pickRandomGoal(m, m.EntrancePos, rng)
	m.Cells[m.GoalPos.Y][m.GoalPos.X] = CellGoal

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
	// Flood-fill the cells an agent can actually reach from the entrance,
	// under the SAME movement model the rest of the engine uses (8-conn
	// Moore, corner-clipping enforced). The goal is chosen only from this
	// set, so it can never land in an isolated open pocket — every
	// generated maze stays solvable by construction.
	reachable := reachableFrom(m, entrance)

	// Candidates: reachable cells at least MinGoalDistanceCells away,
	// scanned in deterministic row-major order so the rng pick is
	// reproducible for a given seed.
	var far []Pos
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			p := Pos{X: x, Y: y}
			if p == entrance || !reachable[p] {
				continue
			}
			if absInt(x-entrance.X)+absInt(y-entrance.Y) < MinGoalDistanceCells {
				continue
			}
			far = append(far, p)
		}
	}
	if len(far) > 0 {
		return far[rng.Intn(len(far))]
	}

	// No reachable cell satisfies the distance floor (a small reachable
	// region): fall back to the reachable cell FARTHEST from the entrance.
	// It's still guaranteed reachable, so the maze stays solvable even if
	// the goal ends up closer than the preferred minimum.
	best := entrance
	bestD := -1
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			p := Pos{X: x, Y: y}
			if p == entrance || !reachable[p] {
				continue
			}
			if d := absInt(x-entrance.X) + absInt(y-entrance.Y); d > bestD {
				bestD = d
				best = p
			}
		}
	}
	return best // entrance itself if nothing else is reachable (trivially solvable)
}

// reachableFrom returns the set of cells reachable from `start` over
// walkable cells using the engine's 8-connected, corner-clipped movement
// model — the same connectivity CountShortestPaths and AStarPath use, so
// "reachable" here means "an agent can actually get there."
func reachableFrom(m *Maze, start Pos) map[Pos]bool {
	reachable := map[Pos]bool{}
	if !m.IsWalkable(start) {
		return reachable
	}
	reachable[start] = true
	queue := []Pos{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range Cardinals {
			np := Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if reachable[np] || !m.IsWalkable(np) {
				continue
			}
			if m.IsCornerClipped(cur, np) {
				continue
			}
			reachable[np] = true
			queue = append(queue, np)
		}
	}
	return reachable
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// IsWalkable reports whether an entity may step onto (or through) the
// given cell. Walls block; every other cell is walkable.
func (m *Maze) IsWalkable(p Pos) bool {
	if p.X < 0 || p.X >= BoardWidth || p.Y < 0 || p.Y >= BoardHeight {
		return false
	}
	return m.Cells[p.Y][p.X] != CellWall
}
