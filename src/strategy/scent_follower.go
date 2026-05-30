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

// scentFollowerTrusteeBonus is the multiplicative preference given
// to scent from the agent's CurrentTrustee over scent from any
// other (non-self, non-negative-trust) label. >1.0 means the
// trustee wins ties; if a fresher non-trustee scent is nearby,
// the higher freshness can still outweigh the bonus.
const scentFollowerTrusteeBonus = 1.5

// ScentFollowerStrategy is the scent-follower's opportunistic
// strict-PO decision rule.
//
// Each call:
//  1. Apply the per-agent graph prune to a.KnownCells so cardinal
//     neighbors leading into perceived dead-end chains drop out.
//  2. Score every walkable known cardinal neighbor by the strongest
//     OTHER-AGENT scent that landed there:
//
//     trustee scent       → +safety × freshness × trusteeBonus (1.5)
//     other-agent scent   → +safety × freshness  (opportunistic)
//     self scent          → skip (don't chase own trail)
//     negative-trust scent → skip (dynamic repel)
//
//     The agent now follows ANY fresh non-self trail it discovers,
//     not just its assigned trustee's. The trustee still wins ties
//     and small freshness gaps thanks to the bonus, but a strongly-
//     fresher peer trail will be chased opportunistically.
//
//  3. Return argmax. If no neighbor carries any followable scent,
//     defer to the Bayesian planning core (bayesianStrategyPlan) —
//     which navigates to the goal if perceived, otherwise expands
//     the perception frontier. Both outcomes give us a fresh shot
//     at finding the gold OR uncovering fresh scent to follow.
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
	// Non-followers run pure Bayesian — they don't sniff scent.
	if !world.IsScentFollower(a.Label) {
		return bayesianStrategyPlan(w, a)
	}
	pick := a.CurrentTrustee // 0 when no trustee was assigned
	best := a.Pos
	bestVal := 0.0
	bestOwner := rune(0)
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !knownWalkable(w, a, np) {
			continue
		}
		owner := w.ScentOwner[np.Y][np.X]
		freshness := w.ScentFreshness(np.X, np.Y)
		if freshness <= 0 || owner == 0 {
			continue
		}
		// Skip own trail — don't chase yourself.
		if owner == a.Label {
			continue
		}
		// Dynamic repel: a label whose trust has gone negative on
		// prior journeys is treated as a non-target.
		if a.TrustScores != nil && a.TrustScores[owner] < 0 {
			continue
		}
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		weight := 1.0
		if pick != 0 && owner == pick {
			weight = scentFollowerTrusteeBonus
		}
		v := safety*freshness*weight - scentRepelWeight*swarmDispersionPenalty(w, a, np)
		if v > bestVal {
			bestVal = v
			best = np
			bestOwner = owner
		}
	}
	if best != a.Pos {
		// Record the opportunistic following so endJourney can
		// credit this label's trust if the run ends in a goal-reach.
		// (CurrentTrustee credit is handled separately via the
		// existing JourneyTrusteeContactTicks gate.)
		if bestOwner != 0 && bestOwner != pick {
			if a.OpportunisticFollowed == nil {
				a.OpportunisticFollowed = map[rune]bool{}
			}
			a.OpportunisticFollowed[bestOwner] = true
		}
		return best
	}
	// No followable scent anywhere nearby. Switch to non-scent-
	// following mode for this tick: run the Bayesian planner. It'll
	// route toward goal if perceived, or to the nearest safe
	// perception-boundary cell otherwise — both of which give us a
	// real shot at finding the gold OR uncovering fresh scent.
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
