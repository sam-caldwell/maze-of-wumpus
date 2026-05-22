// scent_follower.go — agent 6: strict-PO social learner that
// follows the freshest scent trail left by agents 1..5 (the
// "leader" agents: Bayesian, BFS, DFS, Q-learning, DQN).
//
// Conceptual role: imitation. Agent 6 doesn't plan its own path;
// it senses scent intensity at its cardinal neighbors and walks
// toward the freshest leader-trail, weighted by its Bayesian
// belief about cell safety. When no useful scent is in range it
// falls back to outward exploration (distance-from-start bias) —
// the same canonical PO-respecting heuristic agent 7 uses.
//
// Strict PO: never reads w.Maze.GoalPos or any global distance map.
// Only senses scent at cells in a.KnownCells (perceived terrain).
package strategy

import (
	"maze-of-wumpus/src/world"
)

// ScentFollowerStrategy is the scent-follower's strict-PO decision rule.
//
// Each call:
//  1. Apply the per-agent graph prune to a.KnownCells so cardinal
//     neighbors leading into perceived dead-end chains drop out.
//  2. Use a.CurrentTrustee — the attract label picked once per map
//     (uniform on the first map, weighted by TrustScores on later
//     maps). This is the agent's "who do I follow this map?" answer.
//  3. Score every walkable known cardinal neighbor:
//     trustee scent       → +safety × freshness
//     negative-trust scent → skip (dynamic repel)
//     anything else        → 0
//  4. Return argmax. **If no neighbor carries the trustee's scent**
//     (the trail's been lost), revert to a non-scent-following mode:
//     run the Bayesian planning core (bayesianStrategyPlan) — which
//     navigates to the goal if perceived, otherwise expands the
//     perception frontier. Both outcomes help reacquire the trail
//     (more terrain explored) or finish the run (goal located).
//
// Strict PO: only senses scent at cells in `a.KnownCells`. Never
// references `w.Maze.GoalPos` directly — the Bayesian fallback
// gates GoalPos reads on a.KnownCells[GoalPos].
func ScentFollowerStrategy(w *world.World, a *world.Agent) world.Pos {
	restore := applyAgentPrunedView(w, a)
	defer restore()
	return scentFollowerStrategyPlan(w, a)
}

// scentFollowerStrategyPlan is the inner decision rule. Assumes
// a.KnownCells has been set to the view the caller wants the
// planner to see (raw or solo-pruned).
func scentFollowerStrategyPlan(w *world.World, a *world.Agent) world.Pos {
	if step, ok := w.CachedStepFor(a); ok {
		return step
	}
	UpdateAgentBeliefs(w, a) // maintain the Bayesian safety layer
	// Non-follower or no trustee → there's nothing to follow; defer
	// directly to the Bayesian planner (find the goal / explore).
	if !world.IsScentFollower(a.Label) || a.CurrentTrustee == 0 {
		return bayesianStrategyPlan(w, a)
	}
	pick := a.CurrentTrustee
	best := a.Pos
	bestVal := 0.0
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !knownWalkable(w, a, np) {
			continue
		}
		owner := w.ScentOwner[np.Y][np.X]
		freshness := w.ScentFreshness(np.X, np.Y)
		if freshness <= 0 {
			continue
		}
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		switch {
		case owner == pick:
			if v := safety * freshness; v > bestVal {
				bestVal = v
				best = np
			}
		case a.TrustScores != nil && a.TrustScores[owner] < 0:
			// Dynamic repel: leader/peer whose trust has gone
			// negative on prior journeys — skip as a target.
			continue
		}
	}
	if best != a.Pos {
		return best
	}
	// Lost the trail. Switch to non-scent-following mode for this
	// tick: run the Bayesian planner. It'll route toward goal if
	// perceived, or to the nearest safe perception-boundary cell
	// otherwise — both of which give us a real shot at finding the
	// gold OR uncovering fresh trustee scent.
	return bayesianStrategyPlan(w, a)
}

// outwardBiasNeighbor picks the walkable cardinal neighbor with the
// highest DistFromStart. Used as the fallback when no scent rule
// applies or no useful scent is in range.
func outwardBiasNeighbor(w *world.World, a *world.Agent) world.Pos {
	best := a.Pos
	bestDist := -1
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !knownWalkable(w, a, np) {
			continue
		}
		if df := a.DistFromStart[np.Y][np.X]; df > bestDist {
			bestDist = df
			best = np
		}
	}
	return best
}
