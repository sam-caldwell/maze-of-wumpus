// qlearn.go — agent D: tabular Q-learning.
//
// State          = the agent's current grid cell.
// Action         = one of the 4 cardinal directions.
// Reward shaping = -1 per step, +100 on goal, -100 on death,
//
//	+10 per wumpus kill, +5 per water pickup, plus any
//	count-based exploration / back-step / dead-end
//	shaping accumulated in MoveAgents (PendingBonus).
package strategy

import (
	"maze-of-wumpus/src/world"
)

// directionAction maps a from-to one-step transition back to the
// action index in world.Cardinals (or the current best-guess if the
// step isn't a cardinal neighbor, which shouldn't happen in practice).
func directionAction(from, to world.Pos) int {
	dx, dy := to.X-from.X, to.Y-from.Y
	for i, d := range world.Cardinals {
		if d.X == dx && d.Y == dy {
			return i
		}
	}
	return 0
}

// Q-learning constants (ε-greedy).
const (
	QLearnAlpha   = 0.1
	QLearnGamma   = 0.95
	QLearnEpsilon = 0.05
)

// QLearningStrategy: each call applies the pending Bellman update
// for the previous (state, action), then chooses this tick's action.
func QLearningStrategy(w *world.World, a *world.Agent) world.Pos {
	if a.QL == nil {
		a.QL = world.NewQLearning()
	}
	q := a.QL

	if q.HasPending {
		died := a.Stats.Deaths > q.PrevDeaths
		atGoal := a.Stats.GoalsReached > q.PrevGoals
		reward := -1.0
		if died {
			reward -= 100
		}
		if atGoal {
			reward += 10000
		}
		if a.Stats.WumpusKilled > q.PrevWumpusKilled {
			reward += 10
		}
		if a.Water > q.PrevWater {
			reward += 5
		}
		reward += a.PendingBonus
		a.PendingBonus = 0
		var maxNext float64
		if !died && !atGoal {
			maxNext = q.MaxQ(a.Pos)
		}
		old := q.GetQ(q.PrevState, q.PrevAction)
		updated := old + QLearnAlpha*(reward+QLearnGamma*maxNext-old)
		q.SetQ(q.PrevState, q.PrevAction, updated)
		q.HasPending = false
	}

	var action int
	if w.Rng.Float64() < QLearnEpsilon {
		action = w.Rng.Intn(world.QActionCount)
	} else {
		action = q.ArgMaxQ(a.Pos)
		if action < 0 {
			action = w.Rng.Intn(world.QActionCount)
		}
	}
	d := world.Cardinals[action]
	target := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}

	// Water override: if out of water and pits exist, take the first
	// step of a BFS toward the nearest pit instead. Translate that
	// step back into an action index so the Q-table records the
	// chosen direction (the agent still learns the reward signal).
	if step, ok := WaterOverride(w, a); ok {
		target = step
		action = directionAction(a.Pos, step)
	}

	q.PrevState = a.Pos
	q.PrevAction = action
	q.PrevDeaths = a.Stats.Deaths
	q.PrevWumpusKilled = a.Stats.WumpusKilled
	q.PrevGoals = a.Stats.GoalsReached
	q.PrevWater = a.Water
	q.HasPending = true

	return target
}
