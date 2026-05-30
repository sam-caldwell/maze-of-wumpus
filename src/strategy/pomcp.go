// pomcp.go — agent 7: a strict-PO POMCP-lite "flat Monte Carlo"
// planner.
//
// Full POMCP (Silver & Veness, 2010) couples a particle-filter
// belief with UCT tree search over depth-limited rollouts. This file
// implements the spirit in ~80 LOC, and crucially STRICT partial
// observability: the agent never reads `w.Maze.GoalPos` or any
// goal-distance estimate inside the rollout policy. It only knows
// where it started — `w.DistFromStart` is the legitimate signal,
// since the entrance is where every life begins.
//
//   - Belief: reuse agent 1's AgentBeliefs (PitProb / WumpusProb).
//     No particle filter — we use the analytic posterior directly.
//
//   - Action evaluation: for each of the 4 cardinal moves, run
//     PomcpRollouts random-walk rollouts of depth PomcpRolloutDepth.
//     Terminal goal reward (+pomcpGoalReward) fires ONLY when the
//     rollout simulates stepping onto the actual goal cell —
//     equivalent to the agent sensing glitter when it's there.
//
//   - Rollout policy: outward-biased weighted random walk — weight
//     by safety × (1 + DistFromStart). Rollouts naturally drift
//     away from the entrance into unexplored terrain.
//
// Tradeoff: without a goal-direction signal, rollouts in a large
// maze rarely stumble onto the goal randomly. Convergence is slower
// than goal-aware POMCP but it's a faithful POMDP-under-PO setup —
// agent 7 effectively becomes "explore outward, sometimes get
// lucky, learn from the lucky runs."
package strategy

import (
	"math"
	"math/rand"
	"sync"

	"maze-of-wumpus/src/world"
)

const (
	PomcpRollouts     = 12  // rollouts per candidate action
	PomcpRolloutDepth = 100 // deeper rollouts to let random-walk
	// stumble onto the goal without a
	// goal-direction heuristic
	pomcpStepCost     = 1.0     // per-step penalty
	pomcpDeathPenalty = 100.0   // implicit cost for sampling hazardous cell
	pomcpGoalReward   = 10000.0 // fires only on actual goal-cell step
	pomcpGamma        = 0.99
	pomcpExploreBonus = 10.0 // per-DistFromStart unit, depth-limit fallback
)

// POMCPStrategy returns the next cell by Monte-Carlo evaluation of
// each candidate action against the agent's belief state. Per-
// candidate rollout batches run in parallel goroutines; each
// goroutine gets its own *rand.Rand seeded deterministically from
// World.Rng so concurrent runs don't race on the shared RNG yet
// the same world seed still yields a fixed output mapping.
//
// Before any candidates are considered, a per-agent graph prune is
// applied to a.KnownCells so rollouts can't wander into perceived
// dead-end chains. wg.Wait() blocks the call until every rollout
// goroutine has finished, so the deferred restore can't race with
// in-flight reads of a.KnownCells.
func POMCPStrategy(w *world.World, a *world.Agent) world.Pos {
	restore := applyAgentPrunedView(w, a)
	defer restore()
	return pomcpStrategyPlan(w, a)
}

// pomcpStrategyPlan is the inner planning core. Assumes a.KnownCells
// has been set to whatever view the caller wants the rollouts to see.
func pomcpStrategyPlan(w *world.World, a *world.Agent) world.Pos {
	if step, ok := w.CachedStepFor(a); ok {
		return step
	}
	UpdateAgentBeliefs(w, a)
	var candidates []world.Pos
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !knownWalkable(w, a, np) {
			continue
		}
		candidates = append(candidates, np)
	}
	if len(candidates) == 0 {
		return a.Pos
	}
	// Consume one seed per candidate up front. This serializes RNG
	// usage on a single goroutine (the strategy caller) before any
	// goroutines start, so World.Rng advances by a predictable
	// number of values per strategy call regardless of how rollouts
	// interleave.
	seeds := make([]int64, len(candidates))
	for i := range seeds {
		seeds[i] = w.Rng.Int63()
	}
	means := make([]float64, len(candidates))
	var wg sync.WaitGroup
	for i, np := range candidates {
		i, np := i, np
		rng := rand.New(rand.NewSource(seeds[i]))
		wg.Add(1)
		go func() {
			defer wg.Done()
			means[i] = meanRolloutReturn(w, a, np, rng)
		}()
	}
	wg.Wait()
	best := a.Pos
	bestMean := math.Inf(-1)
	for i, m := range means {
		// Swarm dispersion: penalize candidates near swarm-mates so
		// clones spread (no-op solo / once goal perceived).
		m -= pomcpRepelWeight * swarmDispersionPenalty(w, a, candidates[i])
		if m > bestMean {
			bestMean = m
			best = candidates[i]
		}
	}
	return best
}

// meanRolloutReturn averages a fixed number of rollouts starting
// from `start`. The first "step" is the move into `start` itself
// (so its hazard cost / goal reward both apply on rollout step 0).
// `rng` is the per-candidate RNG passed in by the caller — never
// touches the shared World.Rng.
func meanRolloutReturn(w *world.World, a *world.Agent, start world.Pos, rng *rand.Rand) float64 {
	total := 0.0
	for i := 0; i < PomcpRollouts; i++ {
		total += pomcpRollout(w, a, start, rng)
	}
	return total / float64(PomcpRollouts)
}

// pomcpRollout simulates a single trajectory of up to
// PomcpRolloutDepth steps starting at `from`. Each step:
//
//  1. Charge step cost.
//  2. If on goal cell, add goal reward and break.
//  3. If belief says cell is hazardous (PitProb≥0.5 or any
//     WumpusProb), subtract death penalty and break.
//  4. Otherwise weighted-sample the next cell from walkable
//     cardinal neighbors, biased by closer-to-goal + safer.
//
// Rewards are discounted by γ at each step to match the QMDP
// value used by agent 6, keeping the two agents' utility scales
// comparable for side-by-side observation.
func pomcpRollout(w *world.World, a *world.Agent, from world.Pos, rng *rand.Rand) float64 {
	pos := from
	reward := 0.0
	discount := 1.0
	for step := 0; step < PomcpRolloutDepth; step++ {
		reward -= discount * pomcpStepCost
		// Strict PO: the rollout only awards goal reward when the
		// simulated position is the goal cell AND that cell is in
		// the agent's KnownCells (i.e. the agent has actually
		// perceived it via prior sensing). Before the agent has
		// ever seen the goal, no rollout can recognize it.
		if pos == w.Maze.GoalPos && a.KnownCells != nil && a.KnownCells[pos] {
			reward += discount * pomcpGoalReward
			return reward
		}
		if a.Beliefs != nil {
			if a.Beliefs.PitProb[pos] >= 0.5 || a.Beliefs.WumpusProb[pos] > 0 {
				reward -= discount * pomcpDeathPenalty
				return reward
			}
		}
		pos = pomcpSampleNext(w, a, pos, rng)
		discount *= pomcpGamma
	}
	// Hit depth limit: strict-PO bonus — reward proportional to how
	// far the rollout traveled from the entrance. Rollouts that
	// explored further (regardless of direction) score higher. No
	// goal-distance signal, because the agent doesn't know where
	// the goal is until it senses it.
	distFromStart := 0
	if world.InBounds(pos.X, pos.Y) {
		if d := a.DistFromStart[pos.Y][pos.X]; d > 0 {
			distFromStart = d
		}
	}
	reward += discount * pomcpExploreBonus * float64(distFromStart)
	return reward
}

// pomcpSampleNext picks a next cell from `pos`'s walkable cardinal
// neighbors via softmax over closer-to-goal × safety. Falls back
// to uniform random if no neighbor scores positively. The caller-
// supplied rng is used so concurrent rollouts don't race on a shared
// random source.
func pomcpSampleNext(w *world.World, a *world.Agent, pos world.Pos, rng *rand.Rand) world.Pos {
	type cand struct {
		p      world.Pos
		weight float64
	}
	var cands []cand
	total := 0.0
	for _, d := range world.Cardinals {
		np := world.Pos{X: pos.X + d.X, Y: pos.Y + d.Y}
		if !knownWalkable(w, a, np) {
			continue
		}
		// Strict-PO outward bias: weight grows with distance from the
		// entrance — rollouts drift into unexplored territory rather
		// than aiming at the (unknown) goal. DistFromStart is the
		// only spatial signal the agent legitimately holds.
		distFromStart := 0
		if d := a.DistFromStart[np.Y][np.X]; d > 0 {
			distFromStart = d
		}
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		// Outward bias: weight = safety × (1 + distFromStart). +1 so
		// neighbors at distance 0 (the entrance) still get nonzero
		// weight.
		wt := safety * float64(1+distFromStart)
		// Scent shaping: uses a.CurrentTrustee (the per-journey
		// picked attract label) — every rollout step inside the
		// same journey is consistent with the agent's "who do I
		// follow this run" decision. Labels with negative
		// TrustScores act as dynamic repel.
		if world.IsScentFollower(a.Label) {
			owner := w.ScentOwner[np.Y][np.X]
			freshness := w.ScentFreshness(np.X, np.Y)
			if freshness > 0 && owner != 0 {
				switch {
				case a.CurrentTrustee != 0 && owner == a.CurrentTrustee:
					wt *= 1 + freshness
				case a.TrustScores != nil && a.TrustScores[owner] < 0:
					wt *= 1 - freshness
					if wt < 0 {
						wt = 0
					}
				}
			}
		}
		if wt <= 0 {
			continue
		}
		cands = append(cands, cand{np, wt})
		total += wt
	}
	if len(cands) == 0 {
		return pos
	}
	r := rng.Float64() * total
	acc := 0.0
	for _, c := range cands {
		acc += c.weight
		if r <= acc {
			return c.p
		}
	}
	return cands[len(cands)-1].p
}
