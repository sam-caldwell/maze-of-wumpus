// swarm_spawn.go — lazy, per-algorithm clone forking. A swarm leader
// starts solo; as members move, each one may FORK a clone down an
// untaken, unexplored branch at a decision point. WHICH branches get
// a clone is decided per algorithm (its own signal), bounded by the
// SwarmClonesPerLeader cap on alive clones.
package strategy

import (
	"fmt"
	"math/rand"
	"sort"

	"maze-of-wumpus/src/world"
)

// spawnMarginFrac: for the score-based policies (QMDP/POMCP/DQN), a
// branch is forked when its score is within this fraction of the
// best→worst score spread below the best. 0 → only ties with the
// best; 1 → every candidate. 0.25 forks the clearly-competitive
// branches (the algorithm is "unsure" among them).
const spawnMarginFrac = 0.25

// untakenUnexploredBranches returns the onward cells adjacent to the
// member's current position (a.Pos) that it is NOT moving to (`taken`),
// are perceived + walkable, and open onto unperceived territory.
// `fullKnown` is the FULL perceived set (not the swarm-pruned view) so
// "unexplored" means genuinely-unseen, not merely pruned-out.
func untakenUnexploredBranches(w *world.World, a *world.Agent, fullKnown map[world.Pos]bool, taken world.Pos) []world.Pos {
	var out []world.Pos
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if np == taken {
			continue
		}
		if !world.InBounds(np.X, np.Y) || !fullKnown[np] || !w.Maze.IsWalkable(np) {
			continue
		}
		if w.Maze.IsCornerClipped(a.Pos, np) {
			continue
		}
		if opensToUnexplored(fullKnown, np) {
			out = append(out, np)
		}
	}
	return out
}

// opensToUnexplored reports whether `np` borders at least one in-bounds
// cell that hasn't been perceived — i.e., stepping there could reveal
// new territory.
func opensToUnexplored(fullKnown map[world.Pos]bool, np world.Pos) bool {
	for _, d := range world.Cardinals {
		nn := world.Pos{X: np.X + d.X, Y: np.Y + d.Y}
		if world.InBounds(nn.X, nn.Y) && !fullKnown[nn] {
			return true
		}
	}
	return false
}

// forkReq is one queued clone spawn. `at` is the spawn cell; `seed`, if
// set (len ≥ 2), is an initial KnownShortestPath the clone replays to
// travel toward a distant frontier region before exploring on its own.
type forkReq struct {
	at   world.Pos
	seed []world.Pos
}

// collectForks appends adjacent-branch forks the member's per-algorithm
// policy chooses, bounded by free clone slots (cap minus alive clones
// minus forks already queued this tick). a.Pos must be the member's
// current cell when called. These spawn AT the branch cell (already a
// frontier gateway), so they need no seed.
func collectForks(w *world.World, a *world.Agent, fullKnown map[world.Pos]bool, taken world.Pos, policy spawnPolicy, forks *[]forkReq) {
	freeSlots := world.SwarmClonesPerLeader - len(a.SwarmClones) - len(*forks)
	if freeSlots <= 0 {
		return
	}
	branches := untakenUnexploredBranches(w, a, fullKnown, taken)
	if len(branches) == 0 {
		return
	}
	for _, cell := range policy(w, a, branches, freeSlots) {
		*forks = append(*forks, forkReq{at: cell})
	}
}

// swarmRegionForks fills any remaining clone slots by forking toward
// distinct, UNCOVERED frontier directions — the open-space saturation
// path. The frontier (over the swarm's pooled KnownCells) is bucketed
// into directional sectors; sectors a live clone is already heading
// into are skipped; the per-algorithm policy picks among the rest. Each
// chosen sector's clone spawns at the leader and is seeded a known-graph
// path to that sector's gateway, so it heads out there and then explores
// with its own algorithm. This is a SWARM-level decision over the full
// pooled perception, run once per tick after members have moved.
func swarmRegionForks(w *world.World, a *world.Agent, fullKnown map[world.Pos]bool, leaderPos world.Pos, forks *[]forkReq) {
	freeSlots := world.SwarmClonesPerLeader - len(a.SwarmClones) - len(*forks)
	if freeSlots <= 0 {
		return
	}
	reps := frontierSectorReps(w, fullKnown, leaderPos)
	if len(reps) == 0 {
		return
	}
	occupied := occupiedSectors(a, leaderPos)
	candReps := make([]world.Pos, 0, len(reps))
	for s, rep := range reps {
		if occupied[s] {
			continue
		}
		candReps = append(candReps, rep)
	}
	if len(candReps) == 0 {
		return
	}
	sortRowMajor(candReps)
	for _, rep := range pickRegionTargets(w, a, candReps, freeSlots) {
		seed := seedPathToward(w, fullKnown, leaderPos, rep)
		if len(seed) < 2 {
			continue
		}
		*forks = append(*forks, forkReq{at: leaderPos, seed: seed})
	}
}

// pickRegionTargets chooses which frontier-direction gateways to fork
// toward. Unlike the adjacent-branch policy (which keeps only branches
// competitive WITH the best, for junction splitting), region forking
// SATURATES: it forks toward as many distinct uncovered directions as
// slots allow. The per-algorithm part is (a) a viability filter — never
// send a clone into a region its beliefs deem hazardous — and (b) the
// order in which directions are filled when slots are scarce, by the
// algorithm's own valuation (scent for the follower, expected utility
// for QMDP, rollout for POMCP, Q for DQN, outward distance otherwise).
func pickRegionTargets(w *world.World, a *world.Agent, reps []world.Pos, freeSlots int) []world.Pos {
	if freeSlots <= 0 {
		return nil
	}
	type sr struct {
		score float64
		pos   world.Pos
	}
	scored := make([]sr, 0, len(reps))
	for _, np := range reps {
		if !regionViable(a, np) {
			continue
		}
		scored = append(scored, sr{regionScore(w, a, np), np})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if len(scored) > freeSlots {
		scored = scored[:freeSlots]
	}
	out := make([]world.Pos, len(scored))
	for i, s := range scored {
		out[i] = s.pos
	}
	return out
}

// regionViable rejects a frontier direction whose gateway the agent's
// beliefs consider hazardous — no algorithm should deploy a clone into
// a known pit/wumpus cell.
func regionViable(a *world.Agent, np world.Pos) bool {
	if a.Beliefs == nil {
		return true
	}
	return a.Beliefs.PitProb[np] < 0.5 && a.Beliefs.WumpusProb[np] == 0
}

// regionScore ranks a frontier-direction gateway by the agent's own
// algorithm signal, for ordering when slots are scarce. Higher = filled
// first. (Saturation still forks toward every viable direction when
// slots allow; this only decides priority.)
func regionScore(w *world.World, a *world.Agent, np world.Pos) float64 {
	switch a.CurrentStrategy {
	case StrategyDQN:
		if a.DQN != nil {
			_, out := a.DQN.Forward(world.AgentDqnFeatures(w, a))
			if act := directionAction(a.Pos, np); act >= 0 && act < len(out) {
				return out[act]
			}
		}
	case StrategyPOMCP:
		// Outward distance is a cheap, allocation-free proxy here;
		// per-direction rollouts would be costly at fork time.
	}
	// Default (QMDP and fallbacks): prefer farther-from-entrance,
	// scent-positive gateways — the swarm's outward exploration bias.
	explore := 0.0
	if d := a.DistFromStart[np.Y][np.X]; d > 0 {
		explore = float64(d)
	}
	return explore + qmdpScentWeight*w.ScentSignedFreshness(a, np.X, np.Y)
}

// occupiedSectors marks the directional sectors (relative to leaderPos)
// that an alive clone is already heading into, so the region pass won't
// double-cover a direction.
func occupiedSectors(a *world.Agent, leaderPos world.Pos) map[int]bool {
	occ := map[int]bool{}
	for _, c := range a.SwarmClones {
		if c == nil || !c.Alive {
			continue
		}
		if s := sectorOf(leaderPos, c.Pos); s >= 0 {
			occ[s] = true
		}
	}
	return occ
}

// seedPathToward returns a known-graph path from `from` to `to`
// (prefixed with `from` so CachedStepFor can replay it), or nil if
// unreachable over the perceived walkable cells.
func seedPathToward(w *world.World, fullKnown map[world.Pos]bool, from, to world.Pos) []world.Pos {
	if from == to {
		return nil
	}
	path := w.DijkstraPath(from, to, func(p world.Pos) bool {
		return fullKnown[p] && w.Maze.IsWalkable(p)
	})
	if len(path) == 0 {
		return nil
	}
	out := make([]world.Pos, 0, len(path)+1)
	out = append(out, from)
	return append(out, path...)
}

// sortRowMajor orders cells deterministically (row-major) so the
// per-algorithm policy sees a stable candidate order.
func sortRowMajor(cells []world.Pos) {
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Y != cells[j].Y {
			return cells[i].Y < cells[j].Y
		}
		return cells[i].X < cells[j].X
	})
}

// applyForks materializes queued forks as fresh alive clones on the
// leader's roster, never exceeding the cap. A fork's seed path (if any)
// is installed as the clone's KnownShortestPath so it heads to its
// assigned frontier region.
func applyForks(w *world.World, a *world.Agent, forks []forkReq) {
	for _, req := range forks {
		if len(a.SwarmClones) >= world.SwarmClonesPerLeader {
			return
		}
		c := &world.SwarmClone{Pos: req.at, Alive: true}
		if len(req.seed) >= 2 {
			c.KnownShortestPath = req.seed
		}
		a.SwarmClones = append(a.SwarmClones, c)
		if w.DecisionLogEnabled {
			kind := "branch"
			if len(req.seed) >= 2 {
				kind = "region" // seeded toward a distant frontier
			}
			dest := req.at
			if len(req.seed) >= 2 {
				dest = req.seed[len(req.seed)-1]
			}
			w.LogDecision(fmt.Sprintf("t%d %c fork(%s)->(%d,%d)",
				w.Cycle, a.Label, kind, dest.X, dest.Y))
		}
	}
}

// spawnPolicy decides which of the candidate `branches` to fork a clone
// onto, returning at most `freeSlots` cells. a.Pos is the member's
// current cell.
type spawnPolicy func(w *world.World, a *world.Agent, branches []world.Pos, freeSlots int) []world.Pos

// spawnPolicyFor maps a swarm letter to its per-algorithm fork policy.
func spawnPolicyFor(letter rune) spawnPolicy {
	switch letter {
	case StrategySwarmBayesian, StrategyBayesian:
		return bayesianSpawnPolicy
	case StrategyScentFollower:
		return scentSpawnPolicy
	case StrategyDQN:
		return dqnSpawnPolicy
	case StrategyPOMCP:
		return pomcpSpawnPolicy
	case StrategyQMDP:
		return qmdpSpawnPolicy
	}
	return qmdpSpawnPolicy
}

// bayesianSpawnPolicy forks down branches the agent's beliefs deem
// safe (no likely pit, no possible wumpus) — Bayesian's purpose is
// hazard-avoiding inference, so it only commits a clone to a branch it
// believes survivable.
func bayesianSpawnPolicy(w *world.World, a *world.Agent, branches []world.Pos, freeSlots int) []world.Pos {
	var out []world.Pos
	for _, np := range branches {
		if len(out) >= freeSlots {
			break
		}
		safe := true
		if a.Beliefs != nil {
			if a.Beliefs.PitProb[np] >= 0.5 || a.Beliefs.WumpusProb[np] > 0 {
				safe = false
			}
		}
		if safe {
			out = append(out, np)
		}
	}
	return out
}

// scentSpawnPolicy forks scent-bearing branches first (a chosen
// leader's trail is worth following with a dedicated clone), then any
// remaining branches, up to the slot budget.
func scentSpawnPolicy(w *world.World, a *world.Agent, branches []world.Pos, freeSlots int) []world.Pos {
	scored := make([]scoredBranch, len(branches))
	for i, np := range branches {
		scored[i] = scoredBranch{w.ScentSignedFreshness(a, np.X, np.Y), np}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	out := make([]world.Pos, 0, freeSlots)
	for _, sb := range scored {
		if len(out) >= freeSlots {
			break
		}
		out = append(out, sb.pos)
	}
	return out
}

// qmdpSpawnPolicy forks branches whose one-step expected utility is
// within margin of the best — QMDP forks where it can't cleanly
// separate the top options.
func qmdpSpawnPolicy(w *world.World, a *world.Agent, branches []world.Pos, freeSlots int) []world.Pos {
	scores := make([]float64, len(branches))
	for i, np := range branches {
		var pitP, wumpP float64
		if a.Beliefs != nil {
			pitP = a.Beliefs.PitProb[np]
			wumpP = a.Beliefs.WumpusProb[np]
		}
		safety := (1 - pitP) * (1 - wumpP)
		explore := 0.0
		if d := a.DistFromStart[np.Y][np.X]; d > 0 {
			explore = float64(d)
		}
		scent := w.ScentSignedFreshness(a, np.X, np.Y)
		scores[i] = safety * (qmdpExploreWeight*explore + qmdpScentWeight*scent)
	}
	return branchesWithinMargin(scores, branches, freeSlots)
}

// pomcpSpawnPolicy forks branches whose mean rollout return is within
// margin of the best — POMCP forks where lookahead can't separate the
// promising possibilities.
func pomcpSpawnPolicy(w *world.World, a *world.Agent, branches []world.Pos, freeSlots int) []world.Pos {
	scores := make([]float64, len(branches))
	for i, np := range branches {
		rng := rand.New(rand.NewSource(w.Rng.Int63()))
		scores[i] = meanRolloutReturn(w, a, np, rng)
	}
	return branchesWithinMargin(scores, branches, freeSlots)
}

// dqnSpawnPolicy forks branches whose learned Q-value is within margin
// of the best — the network forks where it's unsure which move is
// best. With no network yet, falls back to forking up to the budget.
func dqnSpawnPolicy(w *world.World, a *world.Agent, branches []world.Pos, freeSlots int) []world.Pos {
	if a.DQN == nil {
		out := branches
		if len(out) > freeSlots {
			out = out[:freeSlots]
		}
		return out
	}
	_, out := a.DQN.Forward(world.AgentDqnFeatures(w, a))
	scores := make([]float64, len(branches))
	for i, np := range branches {
		act := directionAction(a.Pos, np)
		if act >= 0 && act < len(out) {
			scores[i] = out[act]
		}
	}
	return branchesWithinMargin(scores, branches, freeSlots)
}

type scoredBranch struct {
	score float64
	pos   world.Pos
}

// branchesWithinMargin keeps the branches whose score is within
// spawnMarginFrac of the best→worst spread below the best, highest
// first, capped at freeSlots.
func branchesWithinMargin(scores []float64, branches []world.Pos, freeSlots int) []world.Pos {
	if len(branches) == 0 || freeSlots <= 0 {
		return nil
	}
	best, worst := scores[0], scores[0]
	for _, s := range scores {
		if s > best {
			best = s
		}
		if s < worst {
			worst = s
		}
	}
	thresh := best - spawnMarginFrac*(best-worst)
	kept := make([]scoredBranch, 0, len(branches))
	for i, s := range scores {
		if s >= thresh {
			kept = append(kept, scoredBranch{s, branches[i]})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].score > kept[j].score })
	if len(kept) > freeSlots {
		kept = kept[:freeSlots]
	}
	out := make([]world.Pos, len(kept))
	for i, k := range kept {
		out[i] = k.pos
	}
	return out
}
