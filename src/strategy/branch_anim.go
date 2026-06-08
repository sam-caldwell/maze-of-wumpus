// branch_anim.go — drives the per-tick state machine for the branch-
// decision animation that the omniscient BFS agent uses. When the
// agent reaches a cell with two or more candidate forward moves the
// strategy spawns "ghost replicas" that fan out along each branch,
// hold at max extent, retract back, and only then commit the planned
// next step.
//
// The strategy is the sole owner of the animation state; the world
// package just respects the returned target (a.Pos while animating).
package strategy

import (
	"maze-of-wumpus/src/world"
)

// SearchAnimMaxDepth caps how far ghosts extend from the agent. With
// a tick rate of 100ms, MaxDepth = 3 yields a ~600ms pause per branch
// point (3 expand + 3 retract ticks) — slow enough to read, not so
// slow that the agent crawls.
const SearchAnimMaxDepth = 3

// runBranchAnim handles one tick of the branch-decision animation
// for `a`. It returns the next target cell and a flag indicating
// whether the strategy should bypass its normal "consume plan and
// move" path.
//
// Behavior:
//   - If no animation is active and `a` is at a branch cell (≥2 walkable
//     non-backwards neighbors), initialize SearchAnim and return
//     (a.Pos, true) — the move is suppressed this tick.
//   - If an animation is active and not done, advance it one frame and
//     return (a.Pos, true).
//   - If an animation just finished, clear SearchAnim and return
//     (chosenStep, true) — the caller should commit the plan step
//     (i.e., consume a.Plan[0]) before returning.
//   - Otherwise return (Pos{}, false) — caller proceeds normally.
func runBranchAnim(w *world.World, a *world.Agent, plannedNext world.Pos) (world.Pos, bool) {
	if a.SearchAnim != nil {
		s := a.SearchAnim
		switch s.Phase {
		case 1: // expanding
			s.Depth++
			if s.Depth >= s.MaxDepth {
				s.Phase = 2
			}
			return a.Pos, true
		case 2: // retracting
			s.Depth--
			if s.Depth <= 0 {
				chosen := s.ChosenStep
				a.SearchAnim = nil
				return chosen, true
			}
			return a.Pos, true
		}
	}
	branches := branchCandidates(w, a)
	if len(branches) < 2 {
		return world.Pos{}, false
	}
	a.SearchAnim = &world.SearchAnim{
		Origin:     a.Pos,
		BranchDirs: branches,
		ChosenStep: plannedNext,
		Phase:      1,
		Depth:      1,
		MaxDepth:   SearchAnimMaxDepth,
	}
	return a.Pos, true
}

// branchCandidates returns unit-vector deltas to every walkable, non-
// hazard, cardinal neighbor of `a`, EXCLUDING the cell the agent
// just came from (so an in-corridor step doesn't read as a branch).
func branchCandidates(w *world.World, a *world.Agent) []world.Pos {
	out := make([]world.Pos, 0, world.CardinalCount)
	// Branch animation uses STRICT cardinals only — diagonal
	// neighbors would make almost every cell a "branch" and the
	// ghost-fanout effect would fire constantly. The first
	// CardinalCount entries of world.Cardinals are the 4
	// axis-aligned directions.
	for _, d := range world.Cardinals[:world.CardinalCount] {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if !w.Maze.IsWalkable(np) {
			continue
		}
		if a.HasLastFrom && np == a.LastFromCell {
			continue
		}
		out = append(out, d)
	}
	return out
}
