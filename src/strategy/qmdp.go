// qmdp.go — agent 7: POMDP QMDP-style planner with strict partial
// observability.
//
// QMDP (Littman, Cassandra & Kaelbling, 1995) approximates the value
// of a belief state by treating it as a weighted sum over the
// underlying MDP's Q-values:
//
//	Q(b, a) ≈ Σ_s b(s) Q_MDP(s, a)
//
// In our setting the agent KNOWS its position (no positional
// uncertainty) but is uncertain about hazards. The belief is
// captured by AgentBeliefs.PitProb and WumpusProb. So
// b(s, hazards) collapses to a delta over position, with hazard
// uncertainty integrated out per-cell via safety = (1 − PitProb) ×
// (1 − WumpusProb).
//
// Per-action utility at the agent's current cell:
//
//	Q(a) = safety(s') × (
//	           qmdpExploreWeight × DistFromStart(s')
//	         + qmdpScentWeight   × ScentSignedFreshness(s')
//	       )
//
// where s' is the cell reached by action a. Safety folds in the
// hazard belief; the explore term is the strict-PO outward bias
// (the only spatial signal the agent legitimately holds); the
// scent term integrates the trustee gradient (positive for trustee,
// negative for negative-trust labels — the dynamic repel).
//
// This is QMDP-style in spirit rather than a full POMDP solve: we
// don't backpropagate value updates across belief transitions.
// The decision rule is the QMDP one-step expected-value argmax,
// which is the canonical fast approximation when belief decay
// across one step is small.
package strategy

import (
	"math"

	"maze-of-wumpus/src/world"
)

const (
	qmdpExploreWeight = 1.0
	qmdpScentWeight   = 50.0 // matches ScentShapingMagnitude scale
)

// QMDPStrategy returns the next cell by QMDP-style expected-value
// argmax over the 4 cardinal actions, using AgentBeliefs for hazard
// uncertainty and scent perception for trustee guidance.
//
// Applies the per-agent graph prune to a.KnownCells before scoring.
// QMDP is a one-step argmax — it doesn't multi-step plan — so the
// prune's direct effect is small (cardinal neighbors that lead into
// perceived dead-end corridors are no longer considered "walkable"
// from the agent's view, preventing the agent from stepping into
// perceived cul-de-sacs).
//
// Strict PO: only senses cells in a.KnownCells. Never reads
// w.Maze.GoalPos. Falls back to outward-bias exploration when no
// candidate scores positively.
func QMDPStrategy(w *world.World, a *world.Agent) world.Pos {
	restore := applyAgentPrunedView(w, a)
	defer restore()
	return qmdpStrategyPlan(w, a)
}

// qmdpStrategyPlan is the inner policy. Assumes a.KnownCells has
// been set to the view the caller wants scored.
func qmdpStrategyPlan(w *world.World, a *world.Agent) world.Pos {
	if step, ok := w.CachedStepFor(a); ok {
		return step
	}
	UpdateAgentBeliefs(w, a)
	best := a.Pos
	bestVal := math.Inf(-1)
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !knownWalkable(w, a, np) {
			continue
		}
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		explore := float64(0)
		if d := a.DistFromStart[np.Y][np.X]; d > 0 {
			explore = float64(d)
		}
		scent := w.ScentSignedFreshness(a, np.X, np.Y)
		score := safety * (qmdpExploreWeight*explore + qmdpScentWeight*scent)
		if score > bestVal {
			bestVal = score
			best = np
		}
	}
	if best == a.Pos {
		return outwardBiasNeighbor(w, a)
	}
	return best
}
