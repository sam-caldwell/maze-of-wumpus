// frontier.go — exploration-frontier detection for swarm forking. The
// frontier is the set of perceived, walkable cells that border unseen
// territory. To decide where to deploy clones, the frontier is bucketed
// into 8 DIRECTIONAL sectors around a reference point (the leader): an
// open room's frontier ring lands in many sectors (so the swarm fans
// out and saturates toward the cap), while a corridor's frontier sits
// in just one or two (so the swarm only splits where the maze branches).
//
// Detection runs over the swarm's full pooled KnownCells (line-of-sight
// perception shared across all members), so the fork decision reflects
// the whole swarm's partial observability.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// isFrontierCell reports whether `p` is a perceived, walkable cell with
// at least one in-bounds neighbor that has NOT been perceived — i.e.,
// stepping onward from it could reveal new territory. Perceived walls
// live in KnownCells, so an unperceived neighbor is genuinely unseen.
func isFrontierCell(w *world.World, known map[world.Pos]bool, p world.Pos) bool {
	if !known[p] || !w.Maze.IsWalkable(p) {
		return false
	}
	for _, d := range world.Cardinals {
		np := world.Pos{X: p.X + d.X, Y: p.Y + d.Y}
		if !world.InBounds(np.X, np.Y) {
			continue
		}
		if !known[np] {
			return true
		}
	}
	return false
}

// sectorOf returns a directional bucket in [0,8] for `cell` relative to
// `from`, keyed by the sign of each axis delta: key = (sgnX+1)*3 +
// (sgnY+1). The center bucket 4 (same cell) returns -1. The other 8
// keys are the 8 compass directions.
func sectorOf(from, cell world.Pos) int {
	sx := sign(cell.X - from.X)
	sy := sign(cell.Y - from.Y)
	if sx == 0 && sy == 0 {
		return -1
	}
	return (sx+1)*3 + (sy + 1)
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

// chebyshev is the 8-connected (Chebyshev) distance between two cells —
// the number of diagonal-capable steps between them.
func chebyshev(a, b world.Pos) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// frontierSectorReps buckets the frontier of `fullKnown` into 8
// directional sectors around `from` and returns, for each non-empty
// sector, the frontier cell NEAREST to `from` (the soonest-reachable
// gateway into that direction's unexplored space). Keyed by sector id.
func frontierSectorReps(w *world.World, fullKnown map[world.Pos]bool, from world.Pos) map[int]world.Pos {
	reps := map[int]world.Pos{}
	bestDist := map[int]int{}
	for p := range fullKnown {
		if !isFrontierCell(w, fullKnown, p) {
			continue
		}
		s := sectorOf(from, p)
		if s < 0 {
			continue
		}
		d := chebyshev(from, p)
		if cur, ok := bestDist[s]; !ok || d < cur {
			bestDist[s] = d
			reps[s] = p
		}
	}
	return reps
}
