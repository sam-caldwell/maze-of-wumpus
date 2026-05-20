// learning.go — data types for tabular Q-learning (agent D) and a
// tiny pure-Go Deep Q-Network (agent E). The structs live here so
// the world package can hold them as fields on Agent / Wumpus without
// importing the strategy package; the algorithms that consume them
// live in the strategy and wumpus packages.
package world

import (
	"math"
	"math/rand"
)

// QActionCount: one Q-value per Moore-direction action (8: N, S,
// W, E + 4 diagonals).
const QActionCount = 8

// QLearning is the persistent Q-table + per-step transition snapshot
// used by the tabular Q-learning agent and wumpus.
type QLearning struct {
	Q                map[Pos][QActionCount]float64
	PrevState        Pos
	PrevAction       int
	PrevDeaths       int
	PrevWumpusKilled int
	PrevGoals        int
	PrevWater        int
	HasPending       bool
}

// NewQLearning returns an empty Q table.
func NewQLearning() *QLearning {
	return &QLearning{Q: map[Pos][QActionCount]float64{}}
}

// MaxQ returns the best Q-value across all actions at s. Unknown
// states return 0.
func (q *QLearning) MaxQ(s Pos) float64 {
	row, ok := q.Q[s]
	if !ok {
		return 0
	}
	best := row[0]
	for i := 1; i < QActionCount; i++ {
		if row[i] > best {
			best = row[i]
		}
	}
	return best
}

// ArgMaxQ returns the action index with the highest Q-value at s, or
// -1 if s is unseen.
func (q *QLearning) ArgMaxQ(s Pos) int {
	row, ok := q.Q[s]
	if !ok {
		return -1
	}
	best := 0
	for i := 1; i < QActionCount; i++ {
		if row[i] > row[best] {
			best = i
		}
	}
	return best
}

// HasState reports whether s has any recorded Q-value row. Used by
// the scent-biased argmax to decide whether to fall back to a
// uniform random action (no Q history AND no scent perception) vs
// pick the highest Q+scent score.
func (q *QLearning) HasState(s Pos) bool {
	_, ok := q.Q[s]
	return ok
}

// SetQ writes one cell of the Q table.
func (q *QLearning) SetQ(s Pos, action int, v float64) {
	row := q.Q[s]
	row[action] = v
	q.Q[s] = row
}

// GetQ reads one cell of the Q table.
func (q *QLearning) GetQ(s Pos, action int) float64 {
	return q.Q[s][action]
}

// DQN sizes.
//
// DqnInput = 6 local-topology features + 8 Moore-neighbor scent
// features (see AgentDqnFeatures slot map). The scent slots let
// the DQN PERCEIVE trusted-scent gradient at decision time rather
// than only learning about it through PendingBonus reward shaping.
//
// DqnOutput = 8 — one Q-value per Moore direction so the network
// can pick diagonals as well as cardinals.
const (
	DqnInput  = 14
	DqnHidden = 16
	DqnOutput = 8
)

// DQN holds the model parameters plus the per-step Bellman-update
// snapshot. Two-layer MLP: DqnInput → DqnHidden ReLU → DqnOutput linear.
type DQN struct {
	W1 []float64 // DqnInput × DqnHidden, row-major (h, i)
	B1 []float64 // DqnHidden
	W2 []float64 // DqnHidden × DqnOutput, row-major (o, h)
	B2 []float64 // DqnOutput

	PrevFeatures     []float64
	PrevAction       int
	PrevDeaths       int
	PrevWumpusKilled int
	PrevGoals        int
	PrevWater        int
	HasPending       bool
}

// NewDQN constructs a network with He-initialized weights.
func NewDQN(rng *rand.Rand) *DQN {
	d := &DQN{
		W1:           make([]float64, DqnInput*DqnHidden),
		B1:           make([]float64, DqnHidden),
		W2:           make([]float64, DqnHidden*DqnOutput),
		B2:           make([]float64, DqnOutput),
		PrevFeatures: make([]float64, DqnInput),
	}
	s1 := math.Sqrt(2.0 / float64(DqnInput))
	s2 := math.Sqrt(2.0 / float64(DqnHidden))
	for i := range d.W1 {
		d.W1[i] = (rng.Float64()*2 - 1) * s1
	}
	for i := range d.W2 {
		d.W2[i] = (rng.Float64()*2 - 1) * s2
	}
	return d
}

// Forward computes hidden activations and Q-values for in.
func (d *DQN) Forward(in []float64) (hidden, out []float64) {
	hidden = make([]float64, DqnHidden)
	out = make([]float64, DqnOutput)
	for h := 0; h < DqnHidden; h++ {
		sum := d.B1[h]
		for i := 0; i < DqnInput; i++ {
			sum += d.W1[h*DqnInput+i] * in[i]
		}
		if sum > 0 {
			hidden[h] = sum
		}
	}
	for o := 0; o < DqnOutput; o++ {
		sum := d.B2[o]
		for h := 0; h < DqnHidden; h++ {
			sum += d.W2[o*DqnHidden+h] * hidden[h]
		}
		out[o] = sum
	}
	return hidden, out
}

// Update applies one SGD step on the chosen action's Q-value (single-
// step TD; other outputs unaffected).
func (d *DQN) Update(in []float64, action int, target float64, learnRate float64) {
	hidden, out := d.Forward(in)
	dOutA := out[action] - target
	if dOutA == 0 {
		return
	}
	dHidden := make([]float64, DqnHidden)
	for h := 0; h < DqnHidden; h++ {
		dHidden[h] = dOutA * d.W2[action*DqnHidden+h]
		if hidden[h] == 0 {
			dHidden[h] = 0
		}
	}
	for h := 0; h < DqnHidden; h++ {
		d.W2[action*DqnHidden+h] -= learnRate * dOutA * hidden[h]
	}
	d.B2[action] -= learnRate * dOutA
	for h := 0; h < DqnHidden; h++ {
		if dHidden[h] == 0 {
			continue
		}
		for i := 0; i < DqnInput; i++ {
			d.W1[h*DqnInput+i] -= learnRate * dHidden[h] * in[i]
		}
		d.B1[h] -= learnRate * dHidden[h]
	}
}

// AgentDqnFeatures: 14-dimensional input vector for the DQN agent.
// Strict-PO friendly: no goal-relative slot. Slots 6..13 are scent
// signed-freshness at each Moore neighbor (positive when the
// neighbor carries the agent's trustee scent; negative when it
// carries a label with negative TrustScores — dynamic repel).
//
//	0   normalized X position
//	1   normalized Y position
//	2   east neighbor walkable (0/1)
//	3   south neighbor walkable (0/1)
//	4   heat at current cell
//	5   stench at current cell
//	6   scent signed freshness at Cardinals[0] (N)
//	7   scent signed freshness at Cardinals[1] (S)
//	8   scent signed freshness at Cardinals[2] (W)
//	9   scent signed freshness at Cardinals[3] (E)
//	10  scent signed freshness at Cardinals[4] (NW)
//	11  scent signed freshness at Cardinals[5] (NE)
//	12  scent signed freshness at Cardinals[6] (SW)
//	13  scent signed freshness at Cardinals[7] (SE)
func AgentDqnFeatures(w *World, a *Agent) []float64 {
	in := make([]float64, DqnInput)
	in[0] = float64(a.Pos.X) / float64(BoardWidth)
	in[1] = float64(a.Pos.Y) / float64(BoardHeight)
	if w.Maze.IsWalkable(Pos{X: a.Pos.X + 1, Y: a.Pos.Y}) {
		in[2] = 1
	}
	if w.Maze.IsWalkable(Pos{X: a.Pos.X, Y: a.Pos.Y + 1}) {
		in[3] = 1
	}
	if w.HeatAt(a.Pos.X, a.Pos.Y) {
		in[4] = 1
	}
	if w.StenchAt(a.Pos.X, a.Pos.Y) {
		in[5] = 1
	}
	for i, d := range Cardinals {
		in[6+i] = w.ScentSignedFreshness(a, a.Pos.X+d.X, a.Pos.Y+d.Y)
	}
	return in
}

// ArgMaxFloat: index of the largest element.
func ArgMaxFloat(v []float64) int {
	best := 0
	for i := 1; i < len(v); i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

// MaxFloat: largest element.
func MaxFloat(v []float64) float64 {
	best := v[0]
	for i := 1; i < len(v); i++ {
		if v[i] > best {
			best = v[i]
		}
	}
	return best
}
