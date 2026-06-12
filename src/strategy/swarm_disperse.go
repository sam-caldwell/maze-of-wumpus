// swarm_disperse.go — dispersion (repel members apart while exploring)
// and convergence (route everyone to the goal once it's detected).
//
// Dispersion is a per-algorithm REPULSION TERM: each scoring planner
// subtracts swarmDispersionPenalty from a candidate move's score, so a
// move that keeps the member near its swarm-mates is penalized. The
// term is gated to zero when the agent has no swarm peers (solo agents
// are unaffected) and once the goal is perceived (members should then
// converge, not avoid each other).
//
// Convergence: when the goal is in the shared KnownCells, the swarm
// wrapper seeds each member a known-graph shortest path to it, which
// every planner replays via CachedStepFor — so the swarm collapses
// onto the goal only after detecting it.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// Per-algorithm dispersion weights. Tuned to each planner's score
// scale (QMDP's explore term can reach the hundreds; the rollout
// values are O(1-10)). Exposed as named constants so the strength is
// easy to retune from live runs.
const (
	qmdpRepelWeight  = 8.0
	pomcpRepelWeight = 2.0
)

// swarmDispersionPenalty returns a positive penalty for moving to `np`
// when other swarm members are nearby — larger the closer `np` is to
// them — incentivizing the swarm to spread. Zero when the agent has
// no swarm peers (solo / non-swarm tick) or once the goal is perceived
// (convergence takes over).
func swarmDispersionPenalty(w *world.World, a *world.Agent, np world.Pos) float64 {
	if len(a.SwarmPeers) == 0 {
		return 0
	}
	if a.KnownCells != nil && a.KnownCells[w.Maze.GoalPos] {
		return 0 // goal detected → converge, don't repel
	}
	pen := 0.0
	for _, p := range a.SwarmPeers {
		dx := p.X - np.X
		if dx < 0 {
			dx = -dx
		}
		dy := p.Y - np.Y
		if dy < 0 {
			dy = -dy
		}
		cheb := dx
		if dy > cheb {
			cheb = dy
		}
		pen += 1.0 / float64(1+cheb) // adjacent ⇒ ~0.5, same cell ⇒ 1.0
	}
	return pen
}

// swarmPeerPositions collects the positions of every alive member of
// a's swarm EXCEPT the one currently at `selfPos` (the mover). The
// leader's own position is included via leaderPos. Used by the swarm
// wrapper to populate a.SwarmPeers before each member plans.
func swarmPeerPositions(a *world.Agent, leaderPos, selfPos world.Pos) []world.Pos {
	peers := make([]world.Pos, 0, len(a.SwarmClones)+1)
	if leaderPos != selfPos {
		peers = append(peers, leaderPos)
	}
	for _, c := range a.SwarmClones {
		if c != nil && c.Alive && c.Pos != selfPos {
			peers = append(peers, c.Pos)
		}
	}
	return peers
}

// farthestFromPeers returns the candidate cell maximizing total
// distance from the agent's swarm peers (the most "spread out" pick).
// Falls back to the first candidate when there are no peers.
func farthestFromPeers(a *world.Agent, candidates []world.Pos) world.Pos {
	if len(candidates) == 0 {
		return a.Pos
	}
	if len(a.SwarmPeers) == 0 {
		return candidates[0]
	}
	best := candidates[0]
	bestD := -1
	for _, c := range candidates {
		sum := 0
		for _, p := range a.SwarmPeers {
			dx := p.X - c.X
			if dx < 0 {
				dx = -dx
			}
			dy := p.Y - c.Y
			if dy < 0 {
				dy = -dy
			}
			if dx > dy {
				sum += dx
			} else {
				sum += dy
			}
		}
		if sum > bestD {
			bestD = sum
			best = c
		}
	}
	return best
}

// seedGoalConvergencePath gives the member a known-graph shortest path
// to the goal when the goal has been perceived, so its planner's
// CachedStepFor replays it (convergence). No-op until the goal is in
// the shared KnownCells, or if a still-valid path is already cached.
func seedGoalConvergencePath(w *world.World, a *world.Agent) {
	if a.KnownCells == nil || !a.KnownCells[w.Maze.GoalPos] {
		return
	}
	if _, ok := w.CachedStepFor(a); ok {
		return // existing cached path still drives a step
	}
	path := w.DijkstraPath(a.Pos, w.Maze.GoalPos, func(p world.Pos) bool {
		return knownWalkable(w, a, p)
	})
	if len(path) == 0 {
		return
	}
	full := make([]world.Pos, 0, len(path)+1)
	full = append(full, a.Pos)
	full = append(full, path...)
	a.KnownShortestPath = full
	// Drop any stale exploration plan so the member fully abandons the
	// branch it was probing and commits to the goal route — without this,
	// the planner's frontier plan would resume the moment the convergence
	// path is briefly interrupted.
	a.Plan = nil
}
