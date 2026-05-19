// pomcp.go — agent 7: a POMCP-lite "flat Monte Carlo" planner.
//
// Full POMCP (Silver & Veness, 2010) couples a particle-filter
// belief with UCT tree search over depth-limited rollouts. A
// production implementation is several hundred lines of careful
// code (particle deprivation, double-counted observations, UCB1
// exploration constants...). This file implements the spirit of the
// idea in ~80 LOC:
//
//   - Belief: reuse agent 1's AgentBeliefs (PitProb / WumpusProb).
//     No particle filter — we use the analytic posterior directly.
//
//   - Action evaluation: for each of the 4 cardinal moves, run
//     PomcpRollouts random-walk rollouts of depth PomcpRolloutDepth,
//     scoring each rollout against the agent's beliefs + the cached
//     DistToGoal heuristic. The action with the highest mean return
//     wins.
//
//   - Rollout policy: weighted random walk biased toward neighbors
//     that look safer AND closer to the goal. This corresponds to
//     POMCP's "rollout policy" π_roll.
//
// The flat-MC variant skips the UCT tree but keeps the core insight:
// instead of picking actions by an analytic value (agent 6), you
// pick them by *averaging the return of sampled trajectories*.
// On the open maze with mostly-known hazards the two end up close;
// on a more uncertain board POMCP's stochastic samples handle the
// long tails better.
package strategy

import (
	"math"

	"maze-of-wumpus/src/world"
)

const (
	PomcpRollouts        = 12      // rollouts per candidate action
	PomcpRolloutDepth    = 25      // simulated steps per rollout
	pomcpStepCost        = 1.0     // per-step penalty
	pomcpDeathPenalty    = 100.0   // implicit cost for sampling a hazardous cell
	pomcpGoalReward      = 10000.0 // matches the real D/E goal reward
	pomcpGamma           = 0.99
	pomcpUnreachableBias = -1e6 // any candidate without a path to goal scores last
)

// POMCPStrategy returns the next cell by Monte-Carlo evaluation of
// each candidate action against the agent's belief state.
func POMCPStrategy(w *world.World, a *world.Agent) world.Pos {
	UpdateAgentBeliefs(w, a)
	best := a.Pos
	bestMean := math.Inf(-1)
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !world.InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
			continue
		}
		mean := meanRolloutReturn(w, a, np)
		if mean > bestMean {
			bestMean = mean
			best = np
		}
	}
	return best
}

// meanRolloutReturn averages a fixed number of rollouts starting
// from `start`. The first "step" is the move into `start` itself
// (so its hazard cost / goal reward both apply on rollout step 0).
func meanRolloutReturn(w *world.World, a *world.Agent, start world.Pos) float64 {
	total := 0.0
	for i := 0; i < PomcpRollouts; i++ {
		total += pomcpRollout(w, a, start)
	}
	return total / float64(PomcpRollouts)
}

// pomcpRollout simulates a single trajectory of up to
// PomcpRolloutDepth steps starting at `from`. Each step:
//
//	1. Charge step cost.
//	2. If on goal cell, add goal reward and break.
//	3. If belief says cell is hazardous (PitProb≥0.5 or any
//	   WumpusProb), subtract death penalty and break.
//	4. Otherwise weighted-sample the next cell from walkable
//	   cardinal neighbors, biased by closer-to-goal + safer.
//
// Rewards are discounted by γ at each step to match the QMDP
// value used by agent 6, keeping the two agents' utility scales
// comparable for side-by-side observation.
func pomcpRollout(w *world.World, a *world.Agent, from world.Pos) float64 {
	pos := from
	reward := 0.0
	discount := 1.0
	for step := 0; step < PomcpRolloutDepth; step++ {
		reward -= discount * pomcpStepCost
		if pos == w.Maze.GoalPos {
			reward += discount * pomcpGoalReward
			return reward
		}
		if a.Beliefs != nil {
			if a.Beliefs.PitProb[pos] >= 0.5 || a.Beliefs.WumpusProb[pos] > 0 {
				reward -= discount * pomcpDeathPenalty
				return reward
			}
		}
		pos = pomcpSampleNext(w, a, pos)
		discount *= pomcpGamma
	}
	// Hit depth limit: bias the trailing reward by how close we got.
	// One BFS-to-goal per terminating rollout — not free, but the
	// agent must compute its own goal distance (the world doesn't
	// expose one).
	dist := bfsDistToGoal(w, pos)
	if dist < 0 {
		reward += pomcpUnreachableBias
	} else {
		reward += discount * pomcpGoalReward * math.Pow(pomcpGamma, float64(dist))
	}
	return reward
}

// pomcpSampleNext picks a next cell from `pos`'s walkable cardinal
// neighbors via softmax over closer-to-goal × safety. Falls back
// to uniform random if no neighbor scores positively.
func pomcpSampleNext(w *world.World, a *world.Agent, pos world.Pos) world.Pos {
	type cand struct {
		p      world.Pos
		weight float64
	}
	var cands []cand
	total := 0.0
	for _, d := range world.Cardinals {
		np := world.Pos{X: pos.X + d.X, Y: pos.Y + d.Y}
		if !world.InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
			continue
		}
		dist := bfsDistToGoal(w, np)
		if dist < 0 {
			continue
		}
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		// Closer = bigger weight. Add 1 to dist so the goal itself
		// (dist=0) gets the highest weight, not infinity.
		wt := safety * (1.0 / float64(dist+1))
		if wt <= 0 {
			continue
		}
		cands = append(cands, cand{np, wt})
		total += wt
	}
	if len(cands) == 0 {
		return pos
	}
	r := w.Rng.Float64() * total
	acc := 0.0
	for _, c := range cands {
		acc += c.weight
		if r <= acc {
			return c.p
		}
	}
	return cands[len(cands)-1].p
}
