// strategies.go — wumpus hunting strategies.
//
// Each wumpus picks ONE HuntMode for its entire life (set at spawn):
//
//	WumpusHuntBayesian — inductive Bayesian + smell detection. The
//	    wumpus scores each cardinal neighbor by the strongest agent
//	    scent within smelling range and moves toward the inferred
//	    agent direction. Aggressiveness scales the commit ratio.
//
//	WumpusHuntWander   — random walk lightly biased by agent scent.
//	    Even at max Aggressiveness this stays exploratory (≤50%
//	    scent-driven).
//
//	WumpusHuntCrowd    — swarm hunting. Every alive crowd-hunt
//	    wumpus shares its detections (agents within
//	    WumpusDetectionRadius); each wumpus BFS-routes to the
//	    nearest shared sighting.
//
// All three honor Aggressiveness ∈ [0, WumpusAggressionMax]: 0 →
// fully random / opportunistic (only kills if an agent walks
// adjacent); MAX → commits to its strategy every tick.
//
// VengeanceStrategy is kept as a legacy entry point for pack
// vengeance after a wumpus sibling is killed (it points at the
// agent-scent gradient irrespective of HuntMode).
package wumpus

import (
	"math/rand"

	"maze-of-wumpus/src/world"
)

// PickStrategy returns the unified HuntStrategy entry point.
// All wumpus dispatch through this single function and branch on
// their own HuntMode. The rng parameter is unused but kept for
// the world.Config.WumpusStrategy signature.
func PickStrategy(rng *rand.Rand) world.WumpusStrategy {
	_ = rng
	return HuntStrategy
}

// HuntStrategy is the top-level entry point. Dispatches to the
// per-mode helper based on wm.HuntMode.
func HuntStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	switch wm.HuntMode {
	case world.WumpusHuntBayesian:
		return bayesianHunt(w, wm)
	case world.WumpusHuntWander:
		return wanderHunt(w, wm)
	case world.WumpusHuntCrowd:
		return crowdHunt(w, wm)
	}
	return RandomNeighbor(w, wm)
}

// VengeanceStrategy is the temporary mode active for wm.VengeanceCycles
// after a pack-mate dies. Equivalent to bayesianHunt at full
// aggressiveness — the wumpus chases the freshest agent scent.
func VengeanceStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	return strongestAgentScentMove(w, wm)
}

// ScentStrategy is the legacy hill-climb-toward-agent-scent
// strategy. Kept as a public entry point for tests and any caller
// that wants the bare scent behavior; also used as a fallback by
// the new strategies when their primary heuristic returns the
// wumpus's own cell.
func ScentStrategy(w *world.World, wm *world.Wumpus) world.Pos {
	return strongestAgentScentMove(w, wm)
}

// commitsToHunt returns true if the wumpus should pursue its
// strategy this tick, based on Aggressiveness. Aggressiveness 0
// always rolls false (opportunistic only); Aggressiveness 15
// always rolls true.
func commitsToHunt(w *world.World, wm *world.Wumpus) bool {
	if wm.Aggressiveness <= 0 {
		return false
	}
	if wm.Aggressiveness >= world.WumpusAggressionMax {
		return true
	}
	return w.Rng.Float64() < float64(wm.Aggressiveness)/float64(world.WumpusAggressionMax)
}

// WumpusGoalPatrolRadius bounds the Manhattan distance a wumpus may
// drift from the goal cell when it's not actively pursuing an agent.
// Wumpus that find themselves farther than this from the goal will
// Dijkstra-plan a step BACK toward gold; wumpus already within the
// radius do a random local wander to ambush approaching agents.
const WumpusGoalPatrolRadius = 20

// seekOrPatrol is the fallback movement used by every hunt mode
// when the wumpus is NOT actively pursuing an agent. Implements
// "inductive bayesian reasoning to seek out gold" via a shortest-
// path step toward `w.Maze.GoalPos` (Dijkstra over walkable, non-
// blocked cells), AND the "remain within 20 steps of gold after
// finding it" rule via the Manhattan-distance gate. Once the
// wumpus is inside the patrol radius, it switches to random local
// wander — staying in the gold's neighborhood without leaving.
func seekOrPatrol(w *world.World, wm *world.Wumpus) world.Pos {
	d := world.AbsInt(wm.Pos.X-w.Maze.GoalPos.X) +
		world.AbsInt(wm.Pos.Y-w.Maze.GoalPos.Y)
	if d > WumpusGoalPatrolRadius {
		if step := goalStep(w, wm); step != wm.Pos {
			return step
		}
	}
	return RandomNeighbor(w, wm)
}

// goalStep returns the first cell on a Dijkstra-shortest path from
// the wumpus to the goal, gating intermediate cells through
// blocked() so we don't route through walls, fire pits, or other
// wumpus. Returns wm.Pos when no path exists (caller falls back to
// random wander).
func goalStep(w *world.World, wm *world.Wumpus) world.Pos {
	path := w.DijkstraPath(wm.Pos, w.Maze.GoalPos, func(p world.Pos) bool {
		if p == w.Maze.GoalPos {
			return true
		}
		return !blocked(w, p, wm)
	})
	if len(path) == 0 {
		return wm.Pos
	}
	return path[0]
}

// bayesianHunt: per-tick, with probability Aggressiveness/MAX, move
// toward the strongest local agent scent. When not pursuing, fall
// back to seek-or-patrol — actively head toward gold (or wander
// within 20 cells of it).
func bayesianHunt(w *world.World, wm *world.Wumpus) world.Pos {
	if commitsToHunt(w, wm) {
		if p := strongestAgentScentMove(w, wm); p != wm.Pos {
			return p
		}
	}
	return seekOrPatrol(w, wm)
}

// wanderHunt: random-leaning. Even at full aggressiveness, only
// half of the commits actually follow scent. When not following
// scent, fall back to seek-or-patrol so even the wanderers
// gravitate toward gold and ambush there.
func wanderHunt(w *world.World, wm *world.Wumpus) world.Pos {
	if commitsToHunt(w, wm) && w.Rng.Float64() < 0.5 {
		if p := strongestAgentScentMove(w, wm); p != wm.Pos {
			return p
		}
	}
	return seekOrPatrol(w, wm)
}

// crowdHunt: aggregate detections across all alive crowd-hunt
// wumpus, then BFS-route this wumpus toward the nearest detection.
// When no agent is in the shared sighting pool, OR aggressiveness
// gates the commit off, fall back to seek-or-patrol toward gold.
func crowdHunt(w *world.World, wm *world.Wumpus) world.Pos {
	sights := crowdSightings(w)
	if len(sights) == 0 {
		return seekOrPatrol(w, wm)
	}
	if !commitsToHunt(w, wm) {
		return seekOrPatrol(w, wm)
	}
	nearest := sights[0]
	bestDist := world.AbsInt(nearest.X-wm.Pos.X) + world.AbsInt(nearest.Y-wm.Pos.Y)
	for _, s := range sights[1:] {
		d := world.AbsInt(s.X-wm.Pos.X) + world.AbsInt(s.Y-wm.Pos.Y)
		if d < bestDist {
			bestDist = d
			nearest = s
		}
	}
	path := bfsTo(w, wm, nearest)
	if len(path) == 0 {
		return seekOrPatrol(w, wm)
	}
	return path[0]
}

// crowdSightings returns the union of all agent positions that any
// alive crowd-hunt wumpus has within WumpusDetectionRadius — the
// shared knowledge state the swarm uses to converge.
func crowdSightings(w *world.World) []world.Pos {
	var crowd []*world.Wumpus
	for _, x := range w.Wumpus {
		if x.Alive && x.HuntMode == world.WumpusHuntCrowd {
			crowd = append(crowd, x)
		}
	}
	if len(crowd) == 0 {
		return nil
	}
	seen := map[world.Pos]bool{}
	out := make([]world.Pos, 0, len(w.Agents))
	for _, a := range w.Agents {
		if !a.Alive {
			continue
		}
		for _, c := range crowd {
			d := world.AbsInt(c.Pos.X-a.Pos.X) + world.AbsInt(c.Pos.Y-a.Pos.Y)
			if d <= world.WumpusDetectionRadius {
				if !seen[a.Pos] {
					seen[a.Pos] = true
					out = append(out, a.Pos)
				}
				break
			}
		}
	}
	return out
}

// strongestAgentScentMove picks the unblocked cardinal neighbor
// whose cell carries the strongest AGENT scent (any agent label,
// not wumpus scent). Returns wm.Pos when no neighbor has agent
// scent — caller falls back to random.
func strongestAgentScentMove(w *world.World, wm *world.Wumpus) world.Pos {
	bestFreshness := 0.0
	best := wm.Pos
	for _, d := range world.Cardinals {
		np := world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
		if blocked(w, np, wm) {
			continue
		}
		owner := w.ScentOwner[np.Y][np.X]
		if owner == 0 || !isAgentLabel(owner) {
			continue
		}
		f := w.ScentFreshness(np.X, np.Y)
		if f > bestFreshness {
			bestFreshness = f
			best = np
		}
	}
	return best
}

// isAgentLabel reports whether `r` is one of the agent labels
// 1..9, A..C (i.e. NOT a wumpus or unset).
func isAgentLabel(r rune) bool {
	return (r >= '1' && r <= '9') || r == 'A' || r == 'B' || r == 'C'
}

// blocked: a cell that this wumpus cannot legally enter. Walls /
// fire pits / other wumpus all block; agents do NOT block.
func blocked(w *world.World, p world.Pos, ignore *world.Wumpus) bool {
	if !world.InBounds(p.X, p.Y) {
		return true
	}
	if !w.Maze.IsWalkable(p) {
		return true
	}
	if w.Maze.Cells[p.Y][p.X] == world.CellFirePit {
		return true
	}
	if other := w.WumpusAt[p.Y][p.X]; other != nil && other != ignore {
		return true
	}
	return false
}

// RandomNeighbor picks a uniformly-random unblocked cardinal
// neighbor. Returns wm.Pos if all 4 are blocked.
func RandomNeighbor(w *world.World, wm *world.Wumpus) world.Pos {
	candidates := make([]world.Pos, 0, 4)
	for _, d := range world.Cardinals {
		np := world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
		if !blocked(w, np, wm) {
			candidates = append(candidates, np)
		}
	}
	if len(candidates) == 0 {
		return wm.Pos
	}
	return candidates[w.Rng.Intn(len(candidates))]
}

// bfsTo: min-cost path from wm.Pos to `to` over walkable non-pit
// non-other-wumpus cells. Used by crowdHunt to route toward shared
// sightings. Backed by World.DijkstraPath (weighted 8-conn,
// corner-clipping enforced).
func bfsTo(w *world.World, wm *world.Wumpus, to world.Pos) []world.Pos {
	from := wm.Pos
	if from == to {
		return nil
	}
	return w.DijkstraPath(from, to, func(p world.Pos) bool {
		if p == to {
			return true
		}
		return !blocked(w, p, wm)
	})
}
