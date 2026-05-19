// pomdp.go — agent 6: a QMDP-style POMDP agent.
//
// "True" POMDP value iteration over the full hazard state space is
// intractable for our 120×80 board. The standard pragmatic approach
// is **QMDP** (Littman, Cassandra, Kaelbling 1995): treat the
// problem as fully observable from the *next* step onward, compute
// the underlying-MDP optimal value V*(s), and then take actions by
//
//	argmax_a  E_b[ V*(s_a) ]
//
// where b is the current belief over hidden state and s_a is the
// next state reached by action a.
//
// In our maze the "hidden state" is wumpus / pit locations. The
// agent's belief about those lives on a.Beliefs (shared with
// agent 1's Bayesian KB). The MDP value function we use is the
// shortest-path distance to the goal:
//
//	V*(s) = goalReward × γ^DistToGoal(s) − 1
//
// expectation over hazards reduces to a "safety probability" factor
// per candidate cell:
//
//	safety(s) = (1 − PitProb[s]) × (1 − WumpusProb[s])
//
// Final score: pick the cardinal neighbor that maximizes
// safety(s) × V*(s). Walls and out-of-bounds neighbors are skipped.
// If no neighbor has positive expected value, return a.Pos (let
// FallbackMove handle it).
package strategy

import (
	"math"

	"maze-of-wumpus/src/world"
)

const (
	pomdpGoalReward = 10000.0
	pomdpGamma      = 0.99
)

// POMDPStrategy is the QMDP decision rule for agent 6. Each call
// runs one BFS-from-candidate-cell to estimate distance-to-goal —
// the world does NOT pre-cache that distance (partially observable
// environment; agents do their own search).
func POMDPStrategy(w *world.World, a *world.Agent) world.Pos {
	UpdateAgentBeliefs(w, a)
	best := a.Pos
	bestVal := math.Inf(-1)
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !world.InBounds(np.X, np.Y) {
			continue
		}
		if !w.Maze.IsWalkable(np) {
			continue
		}
		dist := bfsDistToGoal(w, np)
		if dist < 0 {
			continue // unreachable from this cell
		}
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		value := pomdpGoalReward*math.Pow(pomdpGamma, float64(dist)) - 1
		expected := safety * value
		if expected > bestVal {
			bestVal = expected
			best = np
		}
	}
	return best
}
