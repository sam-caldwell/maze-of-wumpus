// dqn.go — agent E: Deep Q-Network. Same Bellman-update + ε-greedy
// structure as agent D, but Q is approximated by a small neural net
// (defined in src/world/learning.go).
package strategy

import (
	"maze-of-wumpus/src/world"
)

const (
	DqnLearnRate = 0.01
	DqnGamma     = 0.95
	DqnEpsilon   = 0.05
)

// directionAction maps a from-to one-step transition back to the
// action index in world.Cardinals (or 0 if the step isn't a cardinal
// neighbor, which shouldn't happen in practice).
func directionAction(from, to world.Pos) int {
	dx, dy := to.X-from.X, to.Y-from.Y
	for i, d := range world.Cardinals {
		if d.X == dx && d.Y == dy {
			return i
		}
	}
	return 0
}

// DQNStrategy: the entry-point for the DQN agent.
//
// Applies the per-agent graph prune to a.KnownCells before the rest
// of the policy runs. Direct effect on action selection is small —
// DQN argmax over 8 cardinal actions doesn't filter by KnownCells —
// but the water-override path (WaterOverride → bfsTowardKnown)
// becomes consistent with T/U/W's pruned view: shorter PO paths to
// known water pits, and dead-end corridors no longer get traversed
// just because they happen to contain a perceived pit elsewhere.
// Perceived water pits are added as prune anchors at the world
// layer so leaf-trim cannot trim them away.
func DQNStrategy(w *world.World, a *world.Agent) world.Pos {
	restore := applyAgentPrunedView(w, a)
	defer restore()
	return dqnStrategyPlan(w, a)
}

// dqnStrategyPlan is the inner policy. Assumes a.KnownCells has been
// set to the view the caller wants the water-override BFS to see.
func dqnStrategyPlan(w *world.World, a *world.Agent) world.Pos {
	if step, ok := w.CachedStepFor(a); ok {
		return step
	}
	if a.DQN == nil {
		a.DQN = world.NewDQN(w.Rng)
	}
	dnn := a.DQN

	if dnn.HasPending {
		reward := -1.0
		died := a.Stats.Deaths > dnn.PrevDeaths
		atGoal := a.Stats.GoalsReached > dnn.PrevGoals
		if died {
			reward -= 100
		}
		if atGoal {
			reward += 10000
		}
		if a.Stats.WumpusKilled > dnn.PrevWumpusKilled {
			reward += 10
		}
		if a.Water > dnn.PrevWater {
			reward += 5
		}
		reward += a.PendingBonus
		a.PendingBonus = 0
		var maxNext float64
		if !died && !atGoal {
			_, out := dnn.Forward(world.AgentDqnFeatures(w, a))
			maxNext = world.MaxFloat(out)
		}
		target := reward + DqnGamma*maxNext
		dnn.Update(dnn.PrevFeatures, dnn.PrevAction, target, DqnLearnRate)
		dnn.HasPending = false
	}

	in := world.AgentDqnFeatures(w, a)
	var action int
	if w.Rng.Float64() < DqnEpsilon {
		action = w.Rng.Intn(world.DqnOutput)
	} else {
		_, out := dnn.Forward(in)
		action = world.ArgMaxFloat(out)
	}

	// Water override: out-of-water + pits-exist → step toward nearest
	// pit. Translate that step into an action so the network still
	// receives a coherent (state, action, reward) tuple.
	overrideTarget, override := WaterOverride(w, a)
	if override {
		action = directionAction(a.Pos, overrideTarget)
	}

	copy(dnn.PrevFeatures, in)
	dnn.PrevAction = action
	dnn.PrevDeaths = a.Stats.Deaths
	dnn.PrevWumpusKilled = a.Stats.WumpusKilled
	dnn.PrevGoals = a.Stats.GoalsReached
	dnn.PrevWater = a.Water
	dnn.HasPending = true

	if override {
		return overrideTarget
	}
	d := world.Cardinals[action]
	return world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
}
