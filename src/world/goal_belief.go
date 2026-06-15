// goal_belief.go — Bayesian goal-location belief (hazard-free).
//
// Strategy S (swarm-Bayesian) doesn't know where the goal is — under
// strict partial observability it only learns the goal cell once it
// physically perceives it. Until then it reasons about WHERE the goal
// is likely to be and steers exploration toward that region instead of
// just grabbing the nearest frontier.
//
// The model is a posterior over the single (unknown) goal cell:
//
//	prior(c)     ∝ Manhattan distance from the entrance, but only for
//	               cells at least MinGoalDistanceCells away — exactly the
//	               region the maze generator draws the goal from (see
//	               pickRandomGoal). Cells nearer than the floor get zero
//	               mass: the goal provably can't be there.
//	posterior(c) = prior(c) for every cell NOT yet observed; 0 for every
//	               observed cell (the agent perceived it and it wasn't
//	               the goal). Renormalized implicitly.
//
// We never need the full normalized distribution — only its centroid,
// the expected goal location. That's a weighted mean over unobserved
// cells, which decomposes into (board total − observed total) for each
// of the running sums W, W·x, W·y. The board total is memoized per maze;
// each agent accumulates its observed total incrementally as cells are
// perceived (MarkObserved). So the expected goal is O(1) to read and the
// whole scheme costs one grid sweep per maze plus O(1) per perceived cell.
package world

// goalPriorWeight is the prior mass for the goal sitting on cell (x, y),
// given the entrance at (ex, ey) and the generator's distance floor. It
// mirrors pickRandomGoal's candidacy test: Manhattan distance, hard zero
// below the floor, otherwise proportional to distance (farther = more
// likely, matching where the generator concentrates goals).
func goalPriorWeight(ex, ey, x, y, minD int) float64 {
	d := absInt(x-ex) + absInt(y-ey)
	if d < minD {
		return 0
	}
	return float64(d)
}

// GoalPrior returns the board-wide prior totals (Σw, Σw·x, Σw·y) over
// every interior cell, memoized on first use. Walls are included as
// candidates: under partial observability the agent can't rule a cell
// out as a wall until it perceives it, and perceiving it removes the
// cell from the belief via MarkObserved regardless of its terrain.
func (m *Maze) GoalPrior() (totW, totWX, totWY float64) {
	m.goalPriorOnce.Do(m.computeGoalPrior)
	return m.goalPriorW, m.goalPriorWX, m.goalPriorWY
}

func (m *Maze) computeGoalPrior() {
	ex, ey := m.EntrancePos.X, m.EntrancePos.Y
	minD := MinGoalDistanceCells
	for y := 1; y < BoardHeight-1; y++ {
		for x := 1; x < BoardWidth-1; x++ {
			w := goalPriorWeight(ex, ey, x, y, minD)
			if w == 0 {
				continue
			}
			m.goalPriorW += w
			m.goalPriorWX += w * float64(x)
			m.goalPriorWY += w * float64(y)
		}
	}
}

// GoalPriorWeight is the per-cell prior for this maze's entrance + floor.
func (m *Maze) GoalPriorWeight(p Pos) float64 {
	return goalPriorWeight(m.EntrancePos.X, m.EntrancePos.Y, p.X, p.Y, MinGoalDistanceCells)
}

// MarkObserved records that the agent has perceived cell p, folding p's
// prior mass into the observed totals exactly once. It returns true the
// first time p is seen. This is the SOLE writer of Observed for the goal
// belief — UpdateAgentBeliefs and the swarm merge both route through it
// so ObsW/ObsWX/ObsWY can never drift out of sync with the Observed set.
func (b *AgentBeliefs) MarkObserved(m *Maze, p Pos) bool {
	if b.Observed[p] {
		return false
	}
	b.Observed[p] = true
	w := m.GoalPriorWeight(p)
	if w != 0 {
		b.ObsW += w
		b.ObsWX += w * float64(p.X)
		b.ObsWY += w * float64(p.Y)
	}
	return true
}

// ExpectedGoalLocation returns the posterior-weighted centroid of the
// cells where the goal might still be — the agent's best single guess at
// the goal's location given everything it has perceived. The bool is
// false when essentially all prior mass has been observed away (the goal
// region is exhausted; by then the goal has almost certainly been seen
// and the planner heads straight for it instead).
func (w *World) ExpectedGoalLocation(a *Agent) (Pos, bool) {
	if a.Beliefs == nil {
		return Pos{}, false
	}
	totW, totWX, totWY := w.Maze.GoalPrior()
	remW := totW - a.Beliefs.ObsW
	if remW < 1 {
		return Pos{}, false
	}
	x := int(((totWX - a.Beliefs.ObsWX) / remW) + 0.5)
	y := int(((totWY - a.Beliefs.ObsWY) / remW) + 0.5)
	return Pos{X: x, Y: y}, true
}
