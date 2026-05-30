// world.go — Maze of Wumpus runtime state and per-tick simulation.
//
// Five agents (labeled '1'..'5') share the maze, each running its own
// decision Strategy. The world package is strategy-agnostic: it holds
// the data (World, Agent, Wumpus) and the per-tick loop (Step) but
// does not know which concrete strategy each agent runs. Strategies
// are injected at construction time via Config.StrategyFor /
// Config.WumpusStrategy — keeping the world free of import cycles
// against the strategy and wumpus packages.
package world

import (
	"container/heap"
	"fmt"
	"math"
	"math/rand"
)

// dijkstraItem is one frontier entry: a cell and the best cost found
// so far to reach it. The priority queue orders by cost ascending.
type dijkstraItem struct {
	pos  Pos
	cost int
}

// dijkstraPQ is a min-heap over dijkstraItem.cost. Implements
// container/heap.Interface so heap.Push/Pop run in O(log n) instead
// of the O(n) linear scan the prior implementation used. Used by
// DijkstraPath, CountShortestPaths, and bfsAlive.
type dijkstraPQ []dijkstraItem

func (q dijkstraPQ) Len() int           { return len(q) }
func (q dijkstraPQ) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q dijkstraPQ) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *dijkstraPQ) Push(x any)        { *q = append(*q, x.(dijkstraItem)) }
func (q *dijkstraPQ) Pop() any {
	old := *q
	n := len(old)
	x := old[n-1]
	*q = old[:n-1]
	return x
}

// RespawnTicks: 1 second at 100ms/tick.
const RespawnTicks = 10

// DefaultSmellRadius bounds an agent's scent perception. Moore-
// connected, wall-blocked BFS — the agent can smell its own cell
// plus every cell reachable in ≤ DefaultSmellRadius Moore steps
// without crossing a wall.
const DefaultSmellRadius = 2

// DefaultSightRadius bounds an agent's terrain perception (the BFS
// depth of MarkAgentSensed). Moore-connected, wall-blocked — the
// agent "sees" its own cell plus every cell reachable in ≤
// DefaultSightRadius steps without crossing a wall. Walls themselves
// enter KnownCells but block propagation.
//
// 100 is generous but NOT omniscience: sight is strictly wall-
// respecting. In a typical maze with twisty corridors the agent
// perceives only its current wall-connected component out to 100
// steps — a tiny fraction of the ~1M-cell board. In the
// open-field maze variant (or in large rooms) the same radius
// covers most of the reachable space because there are no walls
// to stop propagation. R (omniscient BFS) keeps its perception
// advantage in maze regions; PO strategies still have to walk to
// learn what's around the corner.
const DefaultSightRadius = 100

// TTLMultiplier: an agent dies if its current-attempt ActualDistance
// exceeds TTLMultiplier × OptimalDistance.
const TTLMultiplier = 3

// ExplorationBonus is the one-time reward credited to RL agents the
// first time they enter a cell each life.
const ExplorationBonus = 40.0

// WumpusKillTimeout: a wumpus that fails to kill an agent for this
// many consecutive cycles teleports to a fresh random walkable cell.
const WumpusKillTimeout = 30

// PackVengeanceCycles: when one wumpus dies, every other live wumpus
// enters scent-chase ("vengeance") mode for this many cycles before
// resuming its own native strategy.
const PackVengeanceCycles = 20

// BackStepPenalty: extra reward subtracted from the next RL update
// when an agent moves DIRECTLY back to the cell it just left.
const BackStepPenalty = 1.0

// DeadEndWindow: cycles during which consecutive dead-end hits compound
// their penalty. Each hit within DeadEndWindow cycles of the previous
// one doubles the cost; once the window expires the counter resets.
const DeadEndWindow = 5

// DeadEndExpCap clamps the doubling so a long run of dead-ends can't
// overflow int.
const DeadEndExpCap = 10

// KnownPathReward: bonus paid the FIRST time per life that the agent
// enters a cell it has visited in a PRIOR life.
const KnownPathReward = 10.0

// RealDistanceShaping is the per-unit reward credited to RL agents
// (D and E) each time their "real distance from start" — BFS distance
// from the entrance through the maze — sets a new personal best. The
// max-tracking means back-and-forth wandering doesn't keep paying
// out; only progress to genuinely-farther cells does.
const RealDistanceShaping = 1.0

// SearchAnim is the per-agent state that drives the branch-decision
// search animation used by agents 2 (BFS) and 3 (DFS).
//
// When an agent enters a branch cell — one with at least two walkable
// non-backwards neighbors — its strategy initializes a SearchAnim and
// the World skips the agent's movement for the duration of the
// animation. Each tick the strategy advances the animation: ghosts
// extend outward along every candidate branch up to MaxDepth, then
// retract back to depth 0. Only then does the agent commit
// ChosenStep.
//
//	Phase 1 (expanding):   Depth ticks up 1 → MaxDepth.
//	Phase 2 (retracting):  Depth ticks down MaxDepth → 0.
//	Phase 0 (idle / done): SearchAnim is set to nil.
//
// Ghost cells at any frame occupy Origin + k*dir for k ∈ [1, Depth]
// along every direction in BranchDirs. The TUI renders these as
// red replicas; the world does not treat them as entities.
type SearchAnim struct {
	Origin     Pos
	BranchDirs []Pos
	ChosenStep Pos
	Phase      int
	Depth      int
	MaxDepth   int
}

// Strategy is an agent's decision function. It receives the World and
// the agent and returns the agent's chosen next cell (or the current
// cell to stay put). Strategies may mutate the agent's Plan / Beliefs.
type Strategy func(*World, *Agent) Pos

// WumpusStrategy is a wumpus's decision function.
type WumpusStrategy func(*World, *Wumpus) Pos

// AgentBeliefs is agent A's reasoning state. Used only by the
// Bayesian strategy.
type AgentBeliefs struct {
	Observed    map[Pos]bool
	SafeFromPit map[Pos]bool
	PitProb     map[Pos]float64
	WumpusProb  map[Pos]float64
}

// NewAgentBeliefs returns an empty belief state.
func NewAgentBeliefs() *AgentBeliefs {
	return &AgentBeliefs{
		Observed:    map[Pos]bool{},
		SafeFromPit: map[Pos]bool{},
		PitProb:     map[Pos]float64{},
		WumpusProb:  map[Pos]float64{},
	}
}

// AgentStats are per-agent counters surfaced in the status line.
//
// Alignment-based scoring fields:
//
//	OnPathSteps:   moves THIS LIFE that landed on a cell in
//	                w.ShortestPathCells.
//	OffPathSteps:  moves THIS LIFE that landed elsewhere.
//	BestAlignment: highest (OnPath - OffPath) / OptimalDistance ratio
//	                achieved on any attempt; persists across deaths so
//	                a single bad respawn doesn't erase prior progress.
//
// OnPathSteps / OffPathSteps reset on respawn; BestAlignment doesn't.
type AgentStats struct {
	Deaths            int
	Starts            int
	WumpusKilled      int
	GoalsReached      int
	ActualDistance    int
	BestSolveDistance int
	BestSolveTime     int
	LastDeathReason   string

	OnPathSteps   int
	OffPathSteps  int
	BestAlignment float64

	// Solve-time aggregate stats. MinSolveTime / MaxSolveTime are the
	// shortest and longest TicksAlive observed on any goal-reach.
	// AvgSolveTime is the running mean. LastSolveTime is the most
	// recent goal-reach's TicksAlive — surfaced separately so the
	// TUI can color-tier it against the running min / avg / max.
	// All four are 0 until the agent's first solve.
	MinSolveTime  int
	MaxSolveTime  int
	AvgSolveTime  float64
	LastSolveTime int
}

// Score is the per-agent figure of merit: cumulative solves-per-cycle
// throughput.
//
//	score = GoalsReached / cycle
//
// All quantities are in cycles of simulated time (TicksAlive,
// MinSolveTime, MaxSolveTime, etc. are all denominated in cycles).
// The score therefore reads as "average solves per cycle of
// elapsed simulation" — a single number that captures how
// efficient the agent's algorithm is overall, normalized for
// deaths, respawn downtime, and exploration.
//
// Returns 0 before any cycle has elapsed.
//
// OnPathSteps / OffPathSteps / BestAlignment are still maintained
// on the struct for downstream analysis but no longer feed into the
// Score formula.
func (s AgentStats) Score(cycle int) float64 {
	if cycle <= 0 {
		return 0
	}
	return float64(s.GoalsReached) / float64(cycle)
}

// Agent: one of the five competing automata.
type Agent struct {
	ID         int
	Label      rune
	Pos        Pos
	Alive      bool
	Plan       []Pos
	Strategy   Strategy
	Beliefs    *AgentBeliefs
	DQN        *DQN
	RespawnIn  int
	Water      int
	TicksAlive int
	Stats      AgentStats

	Visited      map[Pos]bool
	PendingBonus float64
	LastFromCell Pos
	HasLastFrom  bool

	DeadEndCount     int
	LastDeadEndCycle int

	LifetimeVisited map[Pos]bool

	// KnownPathRewarded records cells for which the KnownPathReward
	// has ALREADY been paid in some prior step. Persistent across
	// deaths and respawns — so the +KnownPathReward bonus fires at
	// most once per cell across the agent's entire existence. Without
	// this gate the agent would collect the same +10 ten times if it
	// died and respawned ten times while crossing that cell.
	KnownPathRewarded map[Pos]bool

	// MaxStartDist is the largest BFS-distance-from-entrance the
	// agent has reached this LIFE. Used to gate the real-distance
	// shaping reward so back-and-forth movement between two BFS
	// levels doesn't keep paying out. Reset on respawn.
	MaxStartDist int

	// Disabled, when true, removes the agent from the simulation
	// entirely: no clock ticks, no movement, no combat, no respawn,
	// not rendered by the TUI. Toggled at runtime by the per-agent
	// number keys '1'..'5'. Defaults to true at construction.
	Disabled bool

	// SearchAnim is non-nil while the agent is in the middle of the
	// branch-decision animation (agents 2 and 3 only). The world
	// freezes the agent in place until the animation finishes. See
	// SearchAnim doc for the state-machine semantics.
	SearchAnim *SearchAnim

	// KnownCells is the agent's perceived terrain — every cell it
	// has personally stood on PLUS the cardinal neighbors of those
	// cells (which the agent senses on arrival). Populated by
	// MoveAgents / RespawnAgents. Partial-observability-respecting
	// strategies (agents 1, 4, 5, 6, 7) gate planning on this set so
	// the agent never routes through cells it hasn't seen. Agents 2
	// and 3 keep omniscient terrain access and ignore this field.
	// Persists across deaths but resets on `r` reseed (new maze =
	// fresh exploration).
	KnownCells map[Pos]bool

	// TrustScores is the per-attract-label trust an agent has built
	// up across maps for the scent-following / scent-shaping rules.
	// Higher score = more likely to be picked as CurrentTrustee on
	// the next map. Negative scores cause the corresponding scent
	// to act as an additional repel signal. Persists across reseeds
	// (grafted by the TUI / headless reseed path) — that's where
	// the "learning over many maps" effect comes from.
	TrustScores map[rune]float64

	// CurrentTrustee is the agent's chosen attract label for the
	// CURRENT map — picked once at world construction (via
	// PickTrustee, softmax over TrustScores; uniform when all
	// scores are zero) and used for every scent-shaping decision
	// the agent makes on this map. Cleared on `r` reseed and
	// re-rolled by PickTrustee for the new map.
	CurrentTrustee rune

	// JourneyTrusteeContactTicks counts the ticks during the
	// current journey on which the agent stood on a cell carrying
	// CurrentTrustee's scent. Reset at journey start. Used by
	// endJourney to decide whether the trustee actually influenced
	// the outcome:
	//
	//   contact < MinTrusteeContactTicks → endJourney is a no-op
	//                                       (no reward, no penalty —
	//                                        the agent "lost the
	//                                        scent" and the outcome
	//                                        carries no information
	//                                        about the trustee).
	//   contact ≥ threshold              → apply the normal
	//                                       success/failure trust
	//                                       update.
	JourneyTrusteeContactTicks int

	// OpportunisticFollowed is the set of OTHER-AGENT labels whose
	// scent this agent followed at least once during the current
	// journey. Strategy U (opportunistic scent-follower) commits a
	// label to this set every time it picks a scent-driven move
	// onto a cell whose scent owner isn't the agent itself. On a
	// successful journey, every label in this set receives a
	// TrustGoalBonus (and, if the run came in under TTL, the
	// TrustWithinTTLBonus too). Reset at journey start; cleared
	// after endJourney consumes it.
	OpportunisticFollowed map[rune]bool

	// CurrentStrategy is the algorithm letter (R/S/T/U/V/W/X) the
	// agent is using for THIS journey. Picked at the start of each
	// life by PickStrategy. Drives action selection through the
	// world's strategyForLetter dispatch.
	CurrentStrategy rune

	// StrategyTrustScores records the agent's accumulated trust in
	// each algorithm letter. Higher score → softmax pick favors
	// that strategy. Updated in endJourney based on outcome
	// (reached goal + speed vs prior best).
	StrategyTrustScores map[rune]float64

	// StrategyBestSolveTime is the agent's best TicksAlive value
	// for each strategy that reached the goal. Used by endJourney
	// to give a bonus when a new run improves on the prior best
	// for the same (agent, strategy) pair.
	StrategyBestSolveTime map[rune]int

	// KnownShortestPath is the agent's currently-cached optimal
	// route from EntrancePos to GoalPos through its KnownCells,
	// computed by World.optimizeKnownPath each time the agent
	// reaches the goal. PO strategies consult this path first and
	// fall back to native planning when the next step is now
	// hazardous / unwalkable. Reset implicitly on reseed (new
	// world → new Agent struct → zero value).
	KnownShortestPath []Pos

	// SmellRadius bounds the BFS depth of ScentSensedCells — the
	// agent's *olfactory* range, used for trustee/scent shaping
	// and DQN scent slots. Defaults to DefaultSmellRadius (2).
	// Moore-connected, wall-blocked.
	SmellRadius int

	// SightRadius bounds the BFS depth of MarkAgentSensed — the
	// agent's *visual* range over the maze terrain (KnownCells).
	// Defaults to DefaultSightRadius (10). Moore-connected
	// (8-direction) BFS; walls ARE added to KnownCells (the
	// agent learns wall positions) but block propagation.
	//
	// Note: sight ≥ 10 means an agent within line-of-sight of the
	// goal cell will perceive it (and thus enter the goal-known
	// PO branch). Strict-PO contract is preserved — agents only
	// "know" GoalPos once it's in KnownCells.
	SightRadius int

	// PrunedKnownCells is the cached output of
	// World.RecomputeAgentPrunedViewIfStale — KnownCells after
	// leaf-trim + articulation pruning. Solo PO strategies plan
	// against this view so interior dead-ends and unreachable loops
	// are skipped. nil until the first prune-recompute.
	PrunedKnownCells map[Pos]bool

	// SwarmGroupID uniquely identifies an INDEPENDENT swarm. Set
	// when the agent picks strategy S (in RespawnAgents, after
	// quorum/singleton enforcement). Multiple agents on S with the
	// SAME SwarmGroupID share KnownCells/Beliefs as a single
	// cohesive unit. Different SwarmGroupIDs are walled off — two
	// distinct swarms never share knowledge. 0 = not currently in a
	// swarm.
	SwarmGroupID int

	// SwarmClones holds the 10 follower entities that move alongside
	// a swarm leader. Each clone shares `a.KnownCells` and
	// `a.Beliefs` with the leader (the leader IS the shared state).
	// nil if the agent is not currently a swarm leader. When the
	// leader dies, one clone is promoted to leader (the swarm
	// shrinks 11→10) until no clones remain and the swarm
	// dissolves.
	SwarmClones []*SwarmClone

	// SwarmPeers holds the positions of the OTHER alive members of this
	// agent's swarm during a swarm planning tick (set by the swarm
	// wrapper before each member's planner runs). The per-algorithm
	// dispersion term reads it so members are repelled from one another
	// while exploring. nil for solo agents / non-swarm ticks, making
	// dispersion a no-op there.
	SwarmPeers []Pos

	// prunedKnownSize is len(KnownCells) at the last prune-recompute.
	// KnownCells is monotonic within a map life so a size delta is
	// a sufficient dirty signal.
	prunedKnownSize int

	// EntrancePos is THIS agent's spawn cell. Each agent in the
	// world is assigned a distinct perimeter cell at construction
	// (see World.pickAgentEntrances). Replaces the legacy "every
	// agent spawns at Maze.EntrancePos" behavior. RespawnAgents
	// uses this when re-spawning the agent on a fresh life.
	EntrancePos Pos

	// OptimalDistance is the step-count of the shortest path from
	// THIS agent's EntrancePos to the maze goal. Drives the
	// per-agent TTL kill rule:
	//   ActualDistance > TTLMultiplier × OptimalDistance → death
	// Agents spawning closer to the goal get a tighter TTL window;
	// agents spawning farther get a generous one — proportional
	// to the actual difficulty of their start.
	OptimalDistance int

	// ShortestPath is the set of cells lying on one Dijkstra-min
	// path from EntrancePos to GoalPos for this agent. The TUI 's'
	// overlay unions every agent's ShortestPath so the user can
	// see all 12 routes at once.
	ShortestPath map[Pos]bool

	// DistFromStart[y][x] is the BFS distance from THIS agent's
	// EntrancePos to (x, y) — the per-agent "outward bias" signal
	// used by POMCP/QMDP/ScentFollower as the only legitimate
	// spatial heuristic under strict PO. -1 means unreachable.
	// Computed once at world construction.
	DistFromStart [BoardHeight][BoardWidth]int

	// LearnedTTL is the agent's belief about how many steps it can
	// take before the TTL killer fires. 0 means "unknown" (the
	// agent has never died of TTL on a map this large).
	//
	// Maintained by two complementary signals (learn-by-dying is
	// ALWAYS active so the agent keeps re-learning if TTL drifts):
	//
	//   record: on any TTL death (KillAgent reason="ttl"), we set
	//           LearnedTTL = ActualDistance − 1 — the world's TTL
	//           killer is deterministic so a single death pins the
	//           value down to within ±1 step.
	//
	//   invalidate: if the agent SURVIVES past its current
	//               LearnedTTL (per-step check in MoveAgents),
	//               the estimate is stale (TTL grew between maps
	//               or by config change). Drop it and wait for
	//               the next TTL death to re-pin.
	//
	// Grafted across reseed as a prior — useful for the first
	// journey of a new map until either signal updates it.
	LearnedTTL int
}

// Wumpus: adversarial automaton.
type Wumpus struct {
	ID    int
	Pos   Pos
	Alive bool
	// Aggressiveness in [0, WumpusAggressionMax]. 0 → opportunistic
	// (lazy random wander; only kills agents who walk adjacent on
	// their own). WumpusAggressionMax → actively hunts agents via
	// scent every tick. Assigned uniformly at random when the
	// wumpus is constructed (NewWorldWithConfig or
	// SpawnReplacementWumpus). The same value is also displayed by
	// the wumpus's strategy at decision time — see
	// wumpus.HuntStrategy.
	Aggressiveness int
	// HuntMode picks one of three strategies (Bayesian-smell,
	// Wander+scent, Crowd-hunt) for the lifetime of the wumpus.
	// Assigned uniformly at construction.
	HuntMode        WumpusHuntMode
	Strategy        WumpusStrategy
	QL              *QLearning
	DQN             *DQN
	CyclesSinceKill int
	VengeanceCycles int
}

// WumpusAggressionMax is the upper bound of Wumpus.Aggressiveness;
// matches the 0-15 trust heat scale so the value fits the same
// visual encoding if exposed in the UI later.
const WumpusAggressionMax = 15

// WumpusHuntMode picks which of three hunting strategies a wumpus
// uses for its entire life. Assigned at construction.
type WumpusHuntMode int

const (
	// WumpusHuntBayesian — inductive Bayesian reasoning with smell
	// detection: the wumpus scores its cardinal neighbors by the
	// strongest agent-scent freshness within smelling range and
	// moves toward the inferred agent direction. Aggressiveness
	// scales the commit ratio (lower aggression → more random
	// noise per step).
	WumpusHuntBayesian WumpusHuntMode = iota
	// WumpusHuntWander — random walk lightly attracted by agent
	// scent. Even at full aggressiveness this stays exploratory
	// (50% scent-bias / 50% random at max).
	WumpusHuntWander
	// WumpusHuntCrowd — swarm hunting. Every crowd-hunt wumpus
	// shares its detections (alive agents within DetectionRadius)
	// and converges on the nearest one in the union.
	WumpusHuntCrowd
)

// WumpusHuntModeCount is the number of distinct hunt modes; used by
// the spawn code to pick uniformly.
const WumpusHuntModeCount = 3

// SwarmClonesPerLeader is the number of clone entities spawned
// alongside each swarm leader. Total swarm size on spawn = 1 leader
// + 10 clones = 11 entities.
const SwarmClonesPerLeader = 10

// SwarmClone is a follower entity that moves alongside a swarm
// leader. Clones share their leader's KnownCells and Beliefs but
// track their own per-tick position, plan, and short-path cache.
// When a clone reaches the goal, its leader gets the credit.
// When the leader dies, one clone is promoted to leader to keep
// the swarm coherent.
type SwarmClone struct {
	Pos               Pos
	Plan              []Pos
	KnownShortestPath []Pos
	Alive             bool

	// Dist is the number of cells THIS clone has travelled this life.
	// Each clone is judged against TTL on its own Dist (not the
	// swarm's aggregate), so a clone expires only after it personally
	// travels TTLMultiplier × ttlBudget cells. On promotion the leader
	// adopts the promoted clone's Dist.
	Dist int

	// Trail is a small ring of the clone's most-recent positions, used
	// to detect thrashing (oscillating over a few cells). When a clone
	// is found thrashing it is terminated (Alive=false) and respawned
	// from the leader next tick.
	Trail []Pos
}

// SwarmStrategyLetter is the strategy letter whose agents share
// knowledge (KnownCells, Beliefs, KnownShortestPath) with their
// peers. Currently 'S' (Swarm-Bayesian). Kept here so the world
// package can detect swarm members without importing strategy/.
// The strategy package re-exports this as StrategySwarmBayesian.
const SwarmStrategyLetter rune = 'S'

// QmdpSwarmStrategyLetter is retained as a named handle for the QMDP
// swarm ('X'). Every non-benchmark strategy is now a swarm (see
// IsSwarmStrategy), so this is no longer a special case — it's kept
// for the rare call site that wants to name the QMDP letter directly.
const QmdpSwarmStrategyLetter rune = 'X'

// IsSwarmStrategy reports whether a strategy letter spawns a
// knowledge-sharing, branch-spreading clone swarm. EVERY real
// strategy letter does EXCEPT the omniscient benchmark R, which is a
// singleton that must never swarm. 0 (unset) is not a swarm.
// Centralizes the swarm-membership predicate so clone spawn,
// knowledge union, graph pruning, and leader promotion all agree.
func IsSwarmStrategy(letter rune) bool {
	return letter != 0 && letter != BenchmarkStrategyLetter
}

// SwarmMinQuorum is the smallest viable size for the S swarm. If
// fewer than this many agents are alive on S after RespawnAgents
// finishes, EnforceSwarmQuorum drafts alive non-S agents into the
// swarm until the quorum is met (or no more agents can be drafted).
// A swarm of 1-2 doesn't really share meaningful knowledge so we
// guarantee the strategy operates as designed by topping it up.
const SwarmMinQuorum = 3

// BenchmarkStrategyLetter is the omniscient BFS strategy used as
// the reference benchmark. Reserved as a singleton — at most
// MaxBenchmarkAgents (1) alive agent runs it per tick. Lets the
// other strategies compare against one clean baseline rather than
// a chorus of identical optimal solvers.
const (
	BenchmarkStrategyLetter rune = 'R'
	MaxBenchmarkAgents           = 1
)

// StrategyUsesScent reports whether a strategy letter's decision
// pipeline actually consults the scent channel. Used by the
// respawn flow to gate trustee selection: agents on strategies
// that ignore scent shouldn't pick a leader to "follow," since
// they'd never actually sense the trail and the trustee would
// just absorb unearned penalties at journey end.
//
//	U scent-follower — yes (planner reads ScentOwner / ScentFreshness)
//	V dqn            — yes (cardinal scent features in DqnInput)
//	W pomcp          — yes (scent weighting in rollouts)
//	X qmdp           — yes (ScentSignedFreshness in utility score)
//	R / S / T        — no (BFS / swarm-Bayesian / Bayesian, scent-blind)
func StrategyUsesScent(letter rune) bool {
	switch letter {
	case 'U', 'V', 'W', 'X':
		return true
	}
	return false
}

// WumpusHuntModeDescription returns a short (≤64 char) human-
// readable description of a wumpus hunt mode. Used by the TUI's
// Wumpus Strategies legend.
func WumpusHuntModeDescription(mode WumpusHuntMode) string {
	switch mode {
	case WumpusHuntBayesian:
		return "Inductive Bayesian smell-tracking; aggressiveness gates commit"
	case WumpusHuntWander:
		return "Random walk lightly biased by agent scent"
	case WumpusHuntCrowd:
		return "Swarm hunting: shared sightings, BFS to nearest detected agent"
	}
	return "unknown"
}

// ActiveWumpusModes returns the set of WumpusHuntMode values
// currently in use by at least one alive wumpus, in spawn order
// and deduplicated. The TUI's Wumpus Strategies legend lists only
// these — modes whose wumpus all died (or were never spawned)
// don't render.
func (w *World) ActiveWumpusModes() []WumpusHuntMode {
	seen := map[WumpusHuntMode]bool{}
	out := make([]WumpusHuntMode, 0, WumpusHuntModeCount)
	for _, wm := range w.Wumpus {
		if !wm.Alive || seen[wm.HuntMode] {
			continue
		}
		seen[wm.HuntMode] = true
		out = append(out, wm.HuntMode)
	}
	return out
}

// WumpusModeCount returns the number of currently-alive wumpus
// using the given hunt mode. Surfaced by the TUI's Wumpus
// Strategies legend so each row can show "<count>  <description>".
func (w *World) WumpusModeCount(mode WumpusHuntMode) int {
	n := 0
	for _, wm := range w.Wumpus {
		if wm.Alive && wm.HuntMode == mode {
			n++
		}
	}
	return n
}

// WumpusDetectionRadius bounds how close a crowd-hunt wumpus must
// be to an alive agent to add that agent to the shared sighting
// pool. Manhattan distance. 5 cells = roughly "around the corner."
const WumpusDetectionRadius = 5

// Stats are world-wide counters.
type Stats struct {
	OptimalDistance int
	ShortestPaths   int
	WumpusDied      int
}

// MaxShortestPathsCount: clamp the path counter so a branchy maze
// doesn't overflow.
const MaxShortestPathsCount = 10

// World is the entire mutable game state.
type World struct {
	Cycle int
	Maze  *Maze

	// Seed is the RNG seed used to build this world. Kept around for
	// display (status bar / logs) and for tests that need to surface
	// it without poking the internal *rand.Rand.
	Seed int64

	Heat   [BoardHeight][BoardWidth]bool
	Stench [BoardHeight][BoardWidth]bool

	// ScentOwner[y][x]: label of the most recent agent to walk this
	// cell, 0 = unscented. ScentCycle[y][x]: World.Cycle at deposit
	// time, 0 = unscented. Together they drive the freshness signal
	// agent 6 follows (decays linearly to zero over ScentMaxAge
	// cycles).
	ScentOwner [BoardHeight][BoardWidth]rune
	ScentCycle [BoardHeight][BoardWidth]int

	// Events is the rolling log of agent-lifecycle moments
	// (deaths / goal reaches). Appended in order; the TUI shows
	// the last EventsVisible entries. Capped at EventBufferSize.
	Events []Event

	// DecisionLog is the rolling per-tick trace of agent/clone
	// navigation decisions, surfaced by the TUI's toggleable decision
	// viewport ('l'). Only populated while DecisionLogEnabled is set
	// (the UI flips it on demand) so it costs nothing when not viewed.
	DecisionLog        []string
	DecisionLogEnabled bool

	// StrategyPerf accumulates per-strategy run-end counts since
	// the last reseed. Indexed by strategy letter. Surfaced by
	// the TUI's Strategy Performance table.
	StrategyPerf map[rune]*StrategyPerfCounts

	AgentAt  [BoardHeight][BoardWidth]*Agent
	WumpusAt [BoardHeight][BoardWidth]*Wumpus

	Agents []*Agent
	Wumpus []*Wumpus

	GameOver bool
	Stats    Stats

	// WumpusDisabled, when true, freezes all wumpus behavior: they
	// don't move, fight, or emit stench, and the TUI hides them.
	// Toggled at runtime by the 'w' key. Default false.
	WumpusDisabled bool

	// FirePitsDisabled, when true, makes fire pits inert: agents may
	// walk over them without dying, heat sensors read clean, and the
	// TUI hides them. Toggled at runtime by the 'f' key (which also
	// flips WaterPitsDisabled). Default false.
	FirePitsDisabled bool

	// WaterPitsDisabled, when true, makes water pits inert: agents
	// can't collect water from them, strategies don't treat them as
	// secondary goals, and the TUI hides them. Flipped together with
	// FirePitsDisabled by the 'f' key. Default false.
	WaterPitsDisabled bool

	// TTLDisabled, when true, suppresses the time-to-live death rule
	// that kills an agent once its current-attempt distance exceeds
	// TTLMultiplier × OptimalDistance. Useful for letting D and E
	// roam past the cap while their RL signal converges. Toggled at
	// runtime by the 't' key. Default false.
	TTLDisabled bool

	ShortestPathCells map[Pos]bool

	// DistFromStart[y][x] is the BFS distance from the entrance cell
	// to (x, y) through walkable cells. -1 marks unreached / wall
	// cells. Computed once in NewWorldWithConfig and used by the
	// real-distance shaping reward for RL agents. (Note: start-
	// distance only — there is intentionally no cached distance-to-
	// goal field. This is a partially-observable environment; the
	// goal's location relative to any given cell must be inferred
	// by agents, not pre-cached by the world.)
	DistFromStart [BoardHeight][BoardWidth]int

	nextAgentID       int
	nextWumpusID      int

	// nextSwarmGroupID is the per-world monotonic counter for
	// issuing independent swarm IDs. Bumped each time an agent
	// commits to strategy S without an existing SwarmGroupID. IDs
	// are unique per world (reset to 0 on construction).
	nextSwarmGroupID int
	Rng               *rand.Rand
	wumpusStrategyFn  func(*rand.Rand) WumpusStrategy // factory for new wumpus
	vengeanceStrategy WumpusStrategy
	// strategyForLetter dispatches per-journey strategy by letter
	// (R/S/T/U/V/W/X). Plumbed in from Config.StrategyForLetter so
	// the world package never imports strategy/.
	strategyForLetter func(rune) Strategy
	// strategyLetters is the canonical list of available strategy
	// letters (e.g. {'R','S','T','U','V','W','X'}) used by
	// PickStrategy. Plumbed via Config.StrategyLetters.
	strategyLetters []rune
	// swarmGraphs holds one pruned-alive-cell cache per independent
	// swarm group (keyed by SwarmGroupID). Each entry is rebuilt
	// lazily by RecomputeSwarmGraphIfStale when that group's
	// KnownCells union grows. Separate keys = walled-off swarms.
	// See swarm_graph.go.
	swarmGraphs map[int]*swarmGraphState
	// strategyDescriptionForLetter renders a strategy letter as a
	// ≤64-char description. Plumbed via
	// Config.StrategyDescriptionForLetter.
	strategyDescriptionForLetter func(rune) string
}

// MinAcceptablePaths: a generated maze must have at least this many
// distinct shortest paths from entrance to goal.
const MinAcceptablePaths = 3

// Config selects construction-time options for NewWorldWithConfig.
//
// StrategyFor: legacy label → Strategy lookup. Kept for tests; the
// per-journey runtime now dispatches via StrategyForLetter and the
// agent's CurrentStrategy.
//
// StrategyForLetter: letter (R/S/T/U/V/W/X) → Strategy. Used at every
// per-tick action step to dispatch the agent's currently-picked
// strategy. Required when agents are allowed to switch strategies
// at runtime (which is the default).
//
// WumpusStrategy: factory called for each new wumpus. If nil, wumpus
// are constructed with nil Strategy and do not move.
//
// VengeanceStrategy: temporary strategy used while a wumpus's
// VengeanceCycles counter is positive (pack vengeance after a sibling
// kill). If nil, MoveWumpus falls back to the wumpus's native
// Strategy during vengeance.
type Config struct {
	Seed              int64
	StrategyFor       func(rune) Strategy
	StrategyForLetter func(rune) Strategy
	// StrategyLetters is the runtime list of strategy letters
	// available to PickStrategy (typically strategy.StrategyLetters).
	// Nil disables per-journey switching and the agent's legacy
	// Strategy field is used instead.
	StrategyLetters []rune
	// StrategyDescriptionForLetter returns a human-readable (≤64
	// char) description for a strategy letter — surfaced by the
	// TUI's Agent-Algorithm Trust legend.
	StrategyDescriptionForLetter func(rune) string
	WumpusStrategy               func(*rand.Rand) WumpusStrategy
	VengeanceStrategy            WumpusStrategy
}

// NewWorld is the convenience entry point: builds a world from a seed
// with no strategy callbacks attached. Tests use this; production
// uses NewWorldWithConfig.
func NewWorld(seed int64) *World {
	return NewWorldWithConfig(Config{Seed: seed})
}

// NewWorldWithConfig builds a fresh world. Strategy callbacks come
// from cfg; nil callbacks leave the corresponding Strategy fields nil.
func NewWorldWithConfig(cfg Config) *World {
	w := &World{
		Seed:                         cfg.Seed,
		Rng:                          rand.New(rand.NewSource(cfg.Seed)),
		wumpusStrategyFn:             cfg.WumpusStrategy,
		vengeanceStrategy:            cfg.VengeanceStrategy,
		strategyForLetter:            cfg.StrategyForLetter,
		strategyLetters:              cfg.StrategyLetters,
		strategyDescriptionForLetter: cfg.StrategyDescriptionForLetter,
		// Hazard toggles default to DISABLED so a freshly-
		// constructed world is friendly to RL convergence.
		// Operators can re-enable each one at runtime via the
		// 'w' / 'f' keys.
		//
		// TTL defaults to ENABLED so agents have a real failure
		// signal to learn from (LearnedTTL only updates on TTL
		// deaths). Operators can disable it with 't'.
		WumpusDisabled:    true,
		FirePitsDisabled:  true,
		WaterPitsDisabled: true,
		TTLDisabled:       false,
	}
	for attempt := 0; attempt < 50; attempt++ {
		w.Maze = GenerateMaze(w.Rng)
		n := w.CountShortestPaths(w.Maze.EntrancePos, w.Maze.GoalPos, MinAcceptablePaths)
		if n >= MinAcceptablePaths {
			break
		}
	}

	for _, p := range w.Maze.FirePits {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nx, ny := p.X+dx, p.Y+dy
				if nx < 0 || nx >= BoardWidth || ny < 0 || ny >= BoardHeight {
					continue
				}
				if w.Maze.Cells[ny][nx] != CellWall {
					w.Heat[ny][nx] = true
				}
			}
		}
	}

	numWumpus := 5 + w.Rng.Intn(8)
	for i := 0; i < numWumpus; i++ {
		p := w.RandomWumpusSpawn()
		wm := &Wumpus{ID: w.nextWumpusID, Pos: p, Alive: true, Strategy: w.newWumpusStrategy(), Aggressiveness: w.Rng.Intn(WumpusAggressionMax + 1), HuntMode: WumpusHuntMode(w.Rng.Intn(WumpusHuntModeCount))}
		w.nextWumpusID++
		w.Wumpus = append(w.Wumpus, wm)
		w.WumpusAt[p.Y][p.X] = wm
	}

	stratFor := cfg.StrategyFor
	if stratFor == nil {
		stratFor = func(rune) Strategy { return nil }
	}
	// Per-journey strategy switching means EVERY agent can run ANY
	// algorithm, so every agent gets the union of state slots needed
	// by any strategy: AgentBeliefs (Bayesian / scent-follower /
	// POMCP / QMDP) and DQN (deep Q-network). DQN weights take ~1KB
	// per agent — negligible at 12 agents.
	labels := []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C'}
	// All twelve agents spawn simultaneously on the first tick. The
	// per-agent perimeter entrances (assigned below) already
	// distribute them across the maze, so we no longer stagger
	// arrivals — there's no visual clumping at one door to avoid.
	w.Agents = make([]*Agent, 0, len(labels))
	for _, l := range labels {
		a := newAgent(&w.nextAgentID, l, stratFor(l), NewAgentBeliefs(), 1)
		a.DQN = NewDQN(w.Rng)
		w.Agents = append(w.Agents, a)
	}
	// All twelve agents start enabled — the user toggles individual
	// agents off (or back on) via their label key.
	for _, a := range w.Agents {
		a.Disabled = false
	}
	// Uniform perception: every agent smells in a Moore radius of
	// DefaultSmellRadius (2 cells, wall-blocked) and sees in a
	// 4-connected radius of DefaultSightRadius (10 cells, wall-
	// blocked). Far-sight labels 8/9/A/B/C used to receive a
	// boosted SensingRadius=2; that distinction is gone — perception
	// is uniform across the roster.
	for _, a := range w.Agents {
		a.SmellRadius = DefaultSmellRadius
		a.SightRadius = DefaultSightRadius
	}

	// Initial CurrentTrustee is left at 0; RespawnAgents calls
	// PickTrustee the moment each follower agent comes alive (and
	// again at the start of every subsequent journey).

	// Per-agent entry assignment: pick distinct perimeter cells (one
	// per agent) and carve each into the maze with a guaranteed path
	// to the goal. Falls back to the canonical Maze.EntrancePos for
	// every agent if the picker can't satisfy 12 distinct entries
	// (extremely rare; the perimeter is ~400 cells).
	entries := w.pickAgentEntrances(len(w.Agents))
	// One goal-rooted Dijkstra here serves every per-agent OptimalDistance
	// / ShortestPath derivation that follows, replacing what used to be
	// 2N full-grid Dijkstras (two per agent). At 1024² this is the
	// difference between a sub-second construction and ~10 seconds.
	costFromGoal := w.computeCostFromGoal()
	for i, a := range w.Agents {
		w.initAgentEntrance(a, entries[i], costFromGoal)
	}

	// w.Stats.OptimalDistance is the canonical entrance→goal step
	// count. Derive it from the same cost map: trace the path and
	// use its length to match the prior len(DijkstraPath) semantics.
	if path := w.tracePathToGoal(w.Maze.EntrancePos, costFromGoal); path != nil {
		w.Stats.OptimalDistance = len(path)
	}
	w.Stats.ShortestPaths = w.CountShortestPaths(w.Maze.EntrancePos, w.Maze.GoalPos, MaxShortestPathsCount)
	// ShortestPathCells is the union of every agent's individual
	// entrance→goal path. The TUI 's' overlay highlights this set,
	// so the user sees all 12 agents' shortest routes simultaneously.
	w.ShortestPathCells = map[Pos]bool{}
	for _, a := range w.Agents {
		for p := range a.ShortestPath {
			w.ShortestPathCells[p] = true
		}
	}
	w.computeDistFromStart()
	// Permanently strip any entity whose toggle is currently disabled.
	// With the default config (everything disabled) this leaves a
	// hazard-free board; tests that need entities re-spawn them via
	// EnableHazards.
	w.ApplyToggles()
	// First event in the rolling log is a random pick from the
	// startingMessages pool — surfaces a friendly cue in the TUI
	// before any death/goal happens. Yellow (neutral) so it reads
	// distinct from the red death / green goal messages that
	// follow.
	w.RecordEvent("yellow", w.pickTemplate(startingMessages))
	return w
}

// pickAgentEntrances returns `n` distinct perimeter cells, each one
// guaranteed to have a path to GoalPos. Cells are sampled randomly
// from all four sides of the board. If a chosen perimeter cell's
// inward neighbor is a wall, a corridor is carved straight inward
// until existing maze path is reached — this guarantees connection
// without disturbing the rest of the maze topology.
//
// The maze's canonical Maze.EntrancePos is always returned as the
// first entry to preserve legacy behavior (stats / pruner anchor).
// Subsequent entries are random perimeter cells satisfying:
//   - distinct from the goal and from every previously-picked entry
//   - Manhattan distance ≥ MinGoalDistanceCells/2 from goal (so
//     spawns aren't trivially adjacent to the goal cell)
//
// Each picked entry is marked as CellEntrance after carving so the
// TUI renders all entry doorways with the entrance glyph.
func (w *World) pickAgentEntrances(n int) []Pos {
	out := make([]Pos, 0, n)
	used := map[Pos]bool{}
	// Canonical entrance leads the list.
	out = append(out, w.Maze.EntrancePos)
	used[w.Maze.EntrancePos] = true
	if n <= 1 {
		return out
	}

	// Enumerate perimeter candidates, excluding the four corner
	// cells (they're shared between two sides and aesthetically
	// "feel" like neither edge) and the goal. Each side gets its
	// own pool so we can distribute picks across all four edges.
	type side []Pos
	sides := make([]side, 4)
	for x := 1; x < BoardWidth-1; x++ {
		sides[0] = append(sides[0], Pos{x, 0})              // top
		sides[1] = append(sides[1], Pos{x, BoardHeight - 1}) // bottom
	}
	for y := 1; y < BoardHeight-1; y++ {
		sides[2] = append(sides[2], Pos{0, y})             // left
		sides[3] = append(sides[3], Pos{BoardWidth - 1, y}) // right
	}
	for i := range sides {
		w.Rng.Shuffle(len(sides[i]), func(a, b int) {
			sides[i][a], sides[i][b] = sides[i][b], sides[i][a]
		})
	}

	minGoalDist := MinGoalDistanceCells / 2
	cursor := 0
	for len(out) < n {
		picked := false
		for tries := 0; tries < 4; tries++ {
			idx := (cursor + tries) % 4
			s := sides[idx]
			for j, p := range s {
				if used[p] || p == w.Maze.GoalPos {
					continue
				}
				if AbsInt(p.X-w.Maze.GoalPos.X)+AbsInt(p.Y-w.Maze.GoalPos.Y) < minGoalDist {
					continue
				}
				if !w.carveEntryConnection(p) {
					continue
				}
				out = append(out, p)
				used[p] = true
				// Drop this candidate from its side so we don't
				// re-scan it next round.
				sides[idx] = append(s[:j], s[j+1:]...)
				picked = true
				break
			}
			if picked {
				break
			}
		}
		if !picked {
			// No perimeter cell satisfies all constraints; fall back
			// to the canonical entrance for the remaining agents.
			for len(out) < n {
				out = append(out, w.Maze.EntrancePos)
			}
			return out
		}
		cursor = (cursor + 1) % 4
	}
	return out
}

// carveEntryConnection ensures the perimeter cell `p` is walkable
// AND has a path to the rest of the maze. If `p`'s inward neighbor
// is wall, a straight corridor is carved inward until existing path
// is hit (or the boundary on the other side, which is degenerate).
// Returns true if the carve resulted in a perimeter cell that can
// reach the goal; false otherwise (the caller drops this candidate).
func (w *World) carveEntryConnection(p Pos) bool {
	if w.Maze.Cells[p.Y][p.X] == CellGoal {
		return false
	}
	// Pick the inward direction (only one is valid for a perimeter
	// cell).
	var dx, dy int
	switch {
	case p.Y == 0:
		dy = 1
	case p.Y == BoardHeight-1:
		dy = -1
	case p.X == 0:
		dx = 1
	case p.X == BoardWidth-1:
		dx = -1
	default:
		return false // not a perimeter cell
	}
	w.Maze.Cells[p.Y][p.X] = CellEntrance
	// Walk inward, carving walls until existing path is reached.
	x, y := p.X+dx, p.Y+dy
	for InBounds(x, y) && w.Maze.Cells[y][x] == CellWall {
		w.Maze.Cells[y][x] = CellPath
		x += dx
		y += dy
	}
	// Verify connectivity to the goal. A 4-conn reachability BFS is
	// sufficient here — on a grid-carved maze with 1-cell walls the
	// 4-conn and 8-conn reachable sets coincide, and BFS over a 1M-
	// cell board is ~20× cheaper than the weighted Dijkstra it
	// replaced. Path quality doesn't matter at this site; only the
	// reach predicate does.
	if !w.reachableFromTo(p, w.Maze.GoalPos) {
		return false
	}
	return true
}

// reachableFromTo reports whether `to` is reachable from `from` over
// the 4-connected walkable graph. Plain BFS — no weights, no heap.
// Used by carveEntryConnection (connectivity check during agent-entry
// picking) where path cost/shape is irrelevant.
func (w *World) reachableFromTo(from, to Pos) bool {
	if from == to {
		return true
	}
	if !w.Maze.IsWalkable(from) || !w.Maze.IsWalkable(to) {
		return false
	}
	visited := map[Pos]bool{from: true}
	queue := []Pos{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range Cardinals[:CardinalCount] {
			np := Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if !InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
				continue
			}
			if np == to {
				return true
			}
			if visited[np] {
				continue
			}
			visited[np] = true
			queue = append(queue, np)
		}
	}
	return false
}

// computeCostFromGoal runs ONE Dijkstra rooted at w.Maze.GoalPos and
// returns a per-cell minimum-cost map (10/14-weighted 8-conn with
// corner-clipping). Unreachable cells stay at -1. Used at world
// construction so per-agent OptimalDistance / ShortestPath can be
// derived by O(path) greedy descent on the cost map instead of N
// independent Dijkstras (one per agent).
func (w *World) computeCostFromGoal() *[BoardHeight][BoardWidth]int {
	cost := new([BoardHeight][BoardWidth]int)
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			cost[y][x] = -1
		}
	}
	goal := w.Maze.GoalPos
	cost[goal.Y][goal.X] = 0
	pq := &dijkstraPQ{{goal, 0}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(dijkstraItem)
		if cur.cost > cost[cur.pos.Y][cur.pos.X] {
			continue
		}
		for _, d := range Cardinals {
			np := Pos{X: cur.pos.X + d.X, Y: cur.pos.Y + d.Y}
			if !InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur.pos, np) {
				continue
			}
			newCost := cur.cost + StepCost(d)
			if cc := cost[np.Y][np.X]; cc != -1 && newCost >= cc {
				continue
			}
			cost[np.Y][np.X] = newCost
			heap.Push(pq, dijkstraItem{np, newCost})
		}
	}
	return cost
}

// tracePathToGoal returns the cells visited stepping from `from` to
// w.Maze.GoalPos by greedy descent on `cost` — at each step we pick
// the first walkable neighbor whose cost matches `cost[cur] −
// StepCost(dir)`. The return excludes `from` and includes the goal
// as the last entry, matching the contract of DijkstraPath. nil when
// `from` is unreachable or already on the goal.
func (w *World) tracePathToGoal(from Pos, cost *[BoardHeight][BoardWidth]int) []Pos {
	if from == w.Maze.GoalPos {
		return nil
	}
	if cost[from.Y][from.X] < 0 {
		return nil
	}
	path := []Pos{}
	cur := from
	for cur != w.Maze.GoalPos {
		cc := cost[cur.Y][cur.X]
		found := false
		var next Pos
		for _, d := range Cardinals {
			np := Pos{X: cur.X + d.X, Y: cur.Y + d.Y}
			if !InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur, np) {
				continue
			}
			nc := cost[np.Y][np.X]
			if nc < 0 {
				continue
			}
			if nc+StepCost(d) == cc {
				next = np
				found = true
				break
			}
		}
		if !found {
			// `from` was supposed to be reachable but descent stalled
			// — a real bug in the cost map would land here. Fail
			// safe by returning nil rather than looping forever.
			return nil
		}
		path = append(path, next)
		cur = next
	}
	return path
}

// initAgentEntrance attaches a perimeter entry to an agent: stamps
// the agent's EntrancePos, computes its OptimalDistance and
// ShortestPath to the goal, and populates its per-agent DistFromStart
// BFS table. Called once per agent at world construction; values
// stay fixed for the life of the maze.
//
// `costFromGoal` is the shared goal-rooted cost map (see
// computeCostFromGoal). Reusing it across agents replaces the per-
// agent pair of Dijkstras (one for OptimalDistance, one for the
// ShortestPath set) with a single O(path) greedy trace — the chief
// reason NewWorld is tractable on the 1024² board.
func (w *World) initAgentEntrance(a *Agent, entry Pos, costFromGoal *[BoardHeight][BoardWidth]int) {
	a.EntrancePos = entry
	path := w.tracePathToGoal(entry, costFromGoal)
	a.ShortestPath = map[Pos]bool{}
	if path != nil {
		a.ShortestPath[entry] = true
		for _, p := range path {
			a.ShortestPath[p] = true
		}
		a.OptimalDistance = len(path)
	} else {
		a.OptimalDistance = 0
	}
	// Per-agent DistFromStart BFS (4-connected, matches the existing
	// World.computeDistFromStart semantics so strategy outward-bias
	// math stays comparable).
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			a.DistFromStart[y][x] = -1
		}
	}
	a.DistFromStart[entry.Y][entry.X] = 0
	queue := []Pos{entry}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := a.DistFromStart[cur.Y][cur.X]
		for _, dir := range Cardinals {
			np := Pos{X: cur.X + dir.X, Y: cur.Y + dir.Y}
			if !InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur, np) {
				continue
			}
			if a.DistFromStart[np.Y][np.X] != -1 {
				continue
			}
			a.DistFromStart[np.Y][np.X] = d + 1
			queue = append(queue, np)
		}
	}
}

// computeDistFromStart populates DistFromStart with the BFS distance
// from the entrance to every walkable cell. Unreached / wall cells
// stay at -1. Maze topology is fixed after generation so this only
// needs to run once per world.
func (w *World) computeDistFromStart() {
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			w.DistFromStart[y][x] = -1
		}
	}
	start := w.Maze.EntrancePos
	w.DistFromStart[start.Y][start.X] = 0
	queue := []Pos{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := w.DistFromStart[cur.Y][cur.X]
		for _, dir := range Cardinals {
			np := Pos{X: cur.X + dir.X, Y: cur.Y + dir.Y}
			if !InBounds(np.X, np.Y) {
				continue
			}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur, np) {
				continue
			}
			if w.DistFromStart[np.Y][np.X] != -1 {
				continue
			}
			w.DistFromStart[np.Y][np.X] = d + 1
			queue = append(queue, np)
		}
	}
}

// DijkstraPath returns the min-cost path from `from` to `to` over
// the 8-connected grid, weighted by StepCost (cardinal=10,
// diagonal=14). Only cells where `walkable(p)` returns true are
// considered; corner-clipped diagonal moves are rejected. Returns
// nil when no path exists; the returned slice does NOT include
// `from` (so len(path) == step count).
//
// Exported so the strategy package can use it for PO-respecting
// planning without re-implementing weighted shortest-paths.
func (w *World) DijkstraPath(from, to Pos, walkable func(Pos) bool) []Pos {
	if from == to {
		return nil
	}
	dist := map[Pos]int{from: 0}
	prev := map[Pos]Pos{}
	pq := &dijkstraPQ{{from, 0}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(dijkstraItem)
		if cur.cost > dist[cur.pos] {
			continue
		}
		if cur.pos == to {
			// Walk back through prev, append in reverse, then flip
			// once. Avoids the O(L²) prepend the prior implementation
			// did via `append([]Pos{p}, path...)`.
			path := []Pos{}
			for p := to; p != from; p = prev[p] {
				path = append(path, p)
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		for _, d := range Cardinals {
			np := Pos{X: cur.pos.X + d.X, Y: cur.pos.Y + d.Y}
			if !InBounds(np.X, np.Y) || !walkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur.pos, np) {
				continue
			}
			newCost := cur.cost + StepCost(d)
			if cd, ok := dist[np]; ok && newCost >= cd {
				continue
			}
			dist[np] = newCost
			prev[np] = cur.pos
			heap.Push(pq, dijkstraItem{np, newCost})
		}
	}
	return nil
}

// AStarPath returns a min-cost path from `from` to `to` over the
// 8-connected grid, weighted by StepCost (cardinal=10, diagonal=14)
// with corner-clipping enforced. Same signature and return contract
// as DijkstraPath — path excludes `from`, nil when unreachable —
// so it is a drop-in replacement at any call site where the caller
// has a cheap admissible heuristic. Used by `strategy.BFSToward`
// (agent strategy R routing).
//
// Heuristic: octile distance, h = 10·max(dx,dy) + 4·min(dx,dy).
// Admissible and consistent against the 10/14 step costs — A* is
// guaranteed to return the same optimal cost as DijkstraPath; only
// the number of cells expanded differs (A* expects fewer, especially
// as the board grows).
func (w *World) AStarPath(from, to Pos, walkable func(Pos) bool) []Pos {
	if from == to {
		return nil
	}
	gScore := map[Pos]int{from: 0}
	prev := map[Pos]Pos{}
	pq := &dijkstraPQ{{from, octile(from, to)}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(dijkstraItem)
		// Stale entry: a cheaper path to cur.pos has already been
		// finalized, so this popped f-score is no longer current.
		if cur.cost > gScore[cur.pos]+octile(cur.pos, to) {
			continue
		}
		if cur.pos == to {
			path := []Pos{}
			for p := to; p != from; p = prev[p] {
				path = append(path, p)
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		curG := gScore[cur.pos]
		for _, d := range Cardinals {
			np := Pos{X: cur.pos.X + d.X, Y: cur.pos.Y + d.Y}
			if !InBounds(np.X, np.Y) || !walkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur.pos, np) {
				continue
			}
			tentative := curG + StepCost(d)
			if cg, ok := gScore[np]; ok && tentative >= cg {
				continue
			}
			gScore[np] = tentative
			prev[np] = cur.pos
			heap.Push(pq, dijkstraItem{np, tentative + octile(np, to)})
		}
	}
	return nil
}

// octile returns the octile-distance heuristic between two cells in
// the 10/14-weighted 8-conn grid. dx, dy are absolute coordinate
// deltas; the cheapest unconstrained path takes min(dx,dy) diagonal
// steps and |dx−dy| cardinal steps, so the cost is
// 14·min + 10·(max−min) = 10·max + 4·min. Admissible because the
// real path can only be longer (walls, hazards, corner-clipping).
func octile(a, b Pos) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return CardinalStepCost*dx + (DiagonalStepCost-CardinalStepCost)*dy
	}
	return CardinalStepCost*dy + (DiagonalStepCost-CardinalStepCost)*dx
}

// newWumpusStrategy returns a Strategy for a freshly-spawned wumpus
// using the world's configured factory, or nil if no factory was set.
func (w *World) newWumpusStrategy() WumpusStrategy {
	if w.wumpusStrategyFn == nil {
		return nil
	}
	return w.wumpusStrategyFn(w.Rng)
}

// ShortestPathSet returns the set of cells on ONE Dijkstra-minimum-
// cost path from `from` to `to`. When from == to, returns the
// singleton {from}. Empty set when no path exists.
// Used by the TUI 's' overlay.
func (w *World) ShortestPathSet(from, to Pos) map[Pos]bool {
	set := map[Pos]bool{}
	if from == to {
		set[from] = true
		return set
	}
	path := w.DijkstraPath(from, to, w.Maze.IsWalkable)
	if len(path) == 0 {
		return set
	}
	set[from] = true
	for _, p := range path {
		set[p] = true
	}
	return set
}

// CountShortestPaths returns the number of distinct paths from
// `from` to `to` achieving the minimum Dijkstra cost, saturated at
// `cap`. Edge weights match the global step costs.
func (w *World) CountShortestPaths(from, to Pos, cap int) int {
	dist := map[Pos]int{from: 0}
	paths := map[Pos]int{from: 1}
	clamp := func(v int) int {
		if v > cap {
			return cap
		}
		return v
	}
	pq := &dijkstraPQ{{from, 0}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(dijkstraItem)
		if cur.cost > dist[cur.pos] {
			continue
		}
		for _, d := range Cardinals {
			np := Pos{X: cur.pos.X + d.X, Y: cur.pos.Y + d.Y}
			if !InBounds(np.X, np.Y) || !w.Maze.IsWalkable(np) {
				continue
			}
			if w.Maze.IsCornerClipped(cur.pos, np) {
				continue
			}
			newCost := cur.cost + StepCost(d)
			if cd, ok := dist[np]; !ok || newCost < cd {
				dist[np] = newCost
				paths[np] = clamp(paths[cur.pos])
				heap.Push(pq, dijkstraItem{np, newCost})
			} else if newCost == cd {
				paths[np] = clamp(paths[np] + paths[cur.pos])
			}
		}
	}
	return paths[to]
}

func newAgent(idCounter *int, label rune, strat Strategy, beliefs *AgentBeliefs, initialRespawnIn int) *Agent {
	a := &Agent{
		ID:        *idCounter,
		Label:     label,
		Alive:     false,
		Strategy:  strat,
		Beliefs:   beliefs,
		RespawnIn: initialRespawnIn,
		Disabled:  true, // agents default to disabled; user enables via '1'..'5'
	}
	*idCounter++
	return a
}

// ShortestPathLength returns the STEP COUNT of the Dijkstra-min-cost
// path from `from` to `to`. Returns 0 if unreachable. Step count
// rather than cost so TTL math (ActualDistance vs OptimalDistance)
// stays consistent — both are counts of moves regardless of
// direction.
func (w *World) ShortestPathLength(from, to Pos) int {
	if from == to {
		return 0
	}
	path := w.DijkstraPath(from, to, w.Maze.IsWalkable)
	return len(path)
}

// RandomWumpusSpawn returns an unoccupied walkable cell at least 20
// Manhattan units from the entrance.
func (w *World) RandomWumpusSpawn() Pos {
	for {
		x := w.Rng.Intn(BoardWidth)
		y := w.Rng.Intn(BoardHeight)
		c := w.Maze.Cells[y][x]
		if c == CellWall || c == CellGoal || c == CellEntrance || c == CellFirePit {
			continue
		}
		p := Pos{x, y}
		if w.WumpusAt[y][x] != nil {
			continue
		}
		if AbsInt(p.X-w.Maze.EntrancePos.X)+AbsInt(p.Y-w.Maze.EntrancePos.Y) < 20 {
			continue
		}
		return p
	}
}

// Step advances the simulation by one tick.
func (w *World) Step() {
	if w.GameOver {
		return
	}
	w.Cycle++
	w.tickAgentClocks()
	if !w.WumpusDisabled {
		w.TickWumpusClocks()
	}
	w.RecomputeStench()
	if !w.WumpusDisabled {
		w.ResolveCombat()
	}
	w.MoveAgents()
	if !w.WumpusDisabled {
		w.MoveWumpus()
		w.ResolveCombat()
	}
	w.ResolvePitDeaths()
	w.CollectWater()
	w.CheckGoal()
	w.RespawnAgents()
}

func (w *World) tickAgentClocks() {
	for _, a := range w.Agents {
		if a.Disabled {
			continue
		}
		if a.Alive {
			a.TicksAlive++
		}
	}
}

// TickWumpusClocks bumps CyclesSinceKill for each live wumpus and
// teleports any that reach WumpusKillTimeout. Reset to 0 happens in
// ResolveCombat on a successful agent kill OR whenever the wumpus is
// standing on an agent's scent trail.
func (w *World) TickWumpusClocks() {
	for _, wm := range w.Wumpus {
		if !wm.Alive {
			continue
		}
		if w.ScentOwner[wm.Pos.Y][wm.Pos.X] != 0 {
			wm.CyclesSinceKill = 0
			continue
		}
		wm.CyclesSinceKill++
		if wm.CyclesSinceKill >= WumpusKillTimeout {
			w.RelocateWumpus(wm)
		}
	}
}

// RelocateWumpus moves a live wumpus to a random walkable, unoccupied
// cell and resets its boredom timer.
func (w *World) RelocateWumpus(wm *Wumpus) {
	for i := 0; i < 100; i++ {
		x := w.Rng.Intn(BoardWidth)
		y := w.Rng.Intn(BoardHeight)
		if w.Maze.Cells[y][x] != CellPath {
			continue
		}
		if w.WumpusAt[y][x] != nil || w.AgentAt[y][x] != nil {
			continue
		}
		w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
		wm.Pos = Pos{x, y}
		w.WumpusAt[y][x] = wm
		wm.CyclesSinceKill = 0
		return
	}
}

// ResolveCombat: every adjacent agent/wumpus pair flips a coin; the
// loser dies. Wumpus-vs-wumpus combat resolves similarly with 30% chance.
func (w *World) ResolveCombat() {
	for _, a := range w.Agents {
		if a.Disabled || !a.Alive {
			continue
		}
		for _, d := range Cardinals {
			nx, ny := a.Pos.X+d.X, a.Pos.Y+d.Y
			if !InBounds(nx, ny) {
				continue
			}
			wm := w.WumpusAt[ny][nx]
			if wm == nil || !wm.Alive {
				continue
			}
			if w.Rng.Float64() < 0.5 {
				w.KillWumpus(wm)
				a.Stats.WumpusKilled++
				w.recordAgentWumpusKill(a)
			} else {
				w.KillAgent(a, "wumpus")
				wm.CyclesSinceKill = 0
				break
			}
		}
	}
	// Wumpus-vs-clone combat: clones can kill or be killed just
	// like agents. A clone kill credits the leader (Stats.WumpusKilled++).
	for _, leader := range w.Agents {
		if !leader.Alive || leader.Disabled || leader.SwarmGroupID == 0 {
			continue
		}
		for _, c := range leader.SwarmClones {
			if c == nil || !c.Alive {
				continue
			}
			for _, d := range Cardinals {
				nx, ny := c.Pos.X+d.X, c.Pos.Y+d.Y
				if !InBounds(nx, ny) {
					continue
				}
				wm := w.WumpusAt[ny][nx]
				if wm == nil || !wm.Alive {
					continue
				}
				if w.Rng.Float64() < 0.5 {
					w.KillWumpus(wm)
					leader.Stats.WumpusKilled++
					w.recordAgentWumpusKill(leader)
				} else {
					c.Alive = false
					wm.CyclesSinceKill = 0
					break
				}
			}
		}
	}
	seen := map[[2]int]bool{}
	for _, w1 := range w.Wumpus {
		if !w1.Alive {
			continue
		}
		for _, d := range Cardinals {
			nx, ny := w1.Pos.X+d.X, w1.Pos.Y+d.Y
			if !InBounds(nx, ny) {
				continue
			}
			w2 := w.WumpusAt[ny][nx]
			if w2 == nil || !w2.Alive || w2 == w1 {
				continue
			}
			a, b := w1.ID, w2.ID
			if a > b {
				a, b = b, a
			}
			key := [2]int{a, b}
			if seen[key] {
				continue
			}
			seen[key] = true
			if w.Rng.Float64() < 0.3 {
				if w.Rng.Float64() < 0.5 {
					w.KillWumpus(w1)
					break
				}
				w.KillWumpus(w2)
			}
		}
	}
}

// MoveAgents drives every live agent through its strategy and commits
// a move. Every agent MUST move at least one cell per cycle.
func (w *World) MoveAgents() {
	for _, a := range w.Agents {
		if a.Disabled || !a.Alive {
			continue
		}
		var target Pos
		// Dispatch order: CurrentStrategy via letter (per-journey
		// pick) wins. Fall back to the legacy a.Strategy if no
		// letter is set (older construction paths / tests).
		if a.CurrentStrategy != 0 && w.strategyForLetter != nil {
			if s := w.strategyForLetter(a.CurrentStrategy); s != nil {
				target = s(w, a)
			} else {
				target = a.Pos
			}
		} else if a.Strategy != nil {
			target = a.Strategy(w, a)
		} else {
			target = a.Pos
		}
		if !w.CanMoveTo(a, target) {
			// Don't fall back during a branch animation — the agent
			// is intentionally frozen in place while ghosts expand
			// and retract. Strategies return a.Pos every tick of the
			// animation; we just let them freeze.
			if a.SearchAnim != nil {
				continue
			}
			target = w.FallbackMove(a)
		}
		if target == a.Pos {
			continue
		}
		// Wumpus only block movement when they're an active gameplay
		// entity. When WumpusDisabled is set they're inert — agents
		// walk over them like empty path cells.
		if !w.WumpusDisabled {
			if wm := w.WumpusAt[target.Y][target.X]; wm != nil && wm.Alive {
				continue
			}
		}
		oldPos := a.Pos
		w.AgentAt[a.Pos.Y][a.Pos.X] = nil
		w.ScentOwner[a.Pos.Y][a.Pos.X] = a.Label
		w.ScentCycle[a.Pos.Y][a.Pos.X] = w.Cycle
		a.Pos = target
		w.AgentAt[target.Y][target.X] = a
		w.MarkAgentSensed(a)
		a.Stats.ActualDistance++
		if w.DecisionLogEnabled {
			st := a.CurrentStrategy
			if st == 0 {
				st = '?'
			}
			w.LogDecision(fmt.Sprintf("t%d %c/%c (%d,%d)->(%d,%d) d%d",
				w.Cycle, a.Label, st, oldPos.X, oldPos.Y, a.Pos.X, a.Pos.Y, a.Stats.ActualDistance))
		}
		// Path-alignment bookkeeping: ShortestPathCells holds one
		// chosen shortest entrance→goal route; landing on it counts
		// toward OnPathSteps, anywhere else is OffPathSteps.
		if w.ShortestPathCells[a.Pos] {
			a.Stats.OnPathSteps++
		} else {
			a.Stats.OffPathSteps++
		}
		if a.Visited == nil {
			a.Visited = map[Pos]bool{}
		}
		if a.LifetimeVisited == nil {
			a.LifetimeVisited = map[Pos]bool{}
		}
		if !a.Visited[a.Pos] {
			a.Visited[a.Pos] = true
			if a.LifetimeVisited[a.Pos] {
				// Cell visited in a prior life. KnownPathReward fires
				// AT MOST ONCE per cell EVER — once paid, never again.
				if a.KnownPathRewarded == nil {
					a.KnownPathRewarded = map[Pos]bool{}
				}
				if !a.KnownPathRewarded[a.Pos] {
					a.PendingBonus += KnownPathReward
					a.KnownPathRewarded[a.Pos] = true
				}
			} else {
				a.PendingBonus += ExplorationBonus
				a.LifetimeVisited[a.Pos] = true
			}
		}
		if a.HasLastFrom && a.Pos == a.LastFromCell {
			a.PendingBonus -= BackStepPenalty
		}
		// Real-distance shaping: pay only when the agent advances its
		// own personal max BFS-distance-from-entrance THIS life. Back-
		// and-forth wandering between two adjacent BFS levels never
		// re-pays because the max only ratchets upward.
		curStartDist := w.DistFromStart[a.Pos.Y][a.Pos.X]
		if curStartDist > a.MaxStartDist {
			a.PendingBonus += float64(curStartDist-a.MaxStartDist) * RealDistanceShaping
			a.MaxStartDist = curStartDist
		}
		walkables := 0
		for _, dd := range Cardinals {
			np := Pos{a.Pos.X + dd.X, a.Pos.Y + dd.Y}
			if w.Maze.IsWalkable(np) {
				walkables++
			}
		}
		if walkables == 1 {
			if a.LastDeadEndCycle != 0 && w.Cycle-a.LastDeadEndCycle > DeadEndWindow {
				a.DeadEndCount = 0
			}
			exp := a.DeadEndCount
			if exp > DeadEndExpCap {
				exp = DeadEndExpCap
			}
			a.PendingBonus -= float64(int(1) << exp)
			a.DeadEndCount++
			a.LastDeadEndCycle = w.Cycle
		}
		a.LastFromCell = oldPos
		a.HasLastFrom = true
		a.PendingBonus += w.ApplyScentShaping(a)
		// Per-agent TTL: each agent is judged against its OWN
		// EntrancePos→GoalPos shortest path, not the world-wide one.
		// Agents that spawn closer to the goal get a tighter TTL
		// window; agents far from goal get a generous one — exactly
		// scaled to the difficulty of their spawn.
		ttlBudget := a.OptimalDistance
		if ttlBudget <= 0 {
			// Legacy fallback for agents whose per-agent OptimalDistance
			// wasn't initialized (e.g., unit tests that build Agents
			// manually). Use the world-wide value as a conservative
			// default.
			ttlBudget = w.Stats.OptimalDistance
		}
		if !w.TTLDisabled && ttlBudget > 0 && a.Stats.ActualDistance > TTLMultiplier*ttlBudget {
			w.KillAgent(a, "ttl")
		}
		// Learn-by-dying invalidation half: if the agent's still
		// alive and has already taken more steps than its
		// LearnedTTL belief, that belief is stale (TTL grew, or
		// the new map's TTL is larger than the prior grafted
		// estimate). Drop it so the next TTL death re-pins.
		if a.Alive && a.LearnedTTL > 0 && a.Stats.ActualDistance > a.LearnedTTL {
			a.LearnedTTL = 0
		}
	}
	// Note: swarm clone movement is driven from inside the swarm
	// strategy wrapper (strategy.SwarmStrategy) when the leader is
	// planned above. Each clone takes its own branch-steered step
	// against the shared KnownCells, so the swarm fans across the
	// frontier rather than trailing the leader.
}

// CanMoveTo reports whether agent `a` could legally move to `target`
// this tick. Same-cell is treated as NOT a valid move. Other agents
// do NOT block — agents may overlap on the same cell. Wumpus and
// fire-pit blocking are handled separately in MoveAgents (gated on
// the corresponding toggle).
func (w *World) CanMoveTo(a *Agent, target Pos) bool {
	if target == a.Pos {
		return false
	}
	if !InBounds(target.X, target.Y) {
		return false
	}
	// Disallow moves longer than a single Moore step (|dx| ≤ 1,
	// |dy| ≤ 1). Same-cell already filtered above.
	dx := target.X - a.Pos.X
	dy := target.Y - a.Pos.Y
	if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
		return false
	}
	if !w.Maze.IsWalkable(target) {
		return false
	}
	// Corner-clipping: diagonal moves require both orthogonal-
	// adjacent cells to also be walkable. Prevents agents from
	// squeezing through a one-cell diagonal gap between walls.
	if w.Maze.IsCornerClipped(a.Pos, target) {
		return false
	}
	return true
}

// FallbackMove picks an arbitrary walkable cardinal neighbor when the
// agent's strategy can't (or won't) commit to a move.
func (w *World) FallbackMove(a *Agent) Pos {
	dirs := make([]Pos, len(Cardinals))
	copy(dirs, Cardinals)
	w.Rng.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
	for _, d := range dirs {
		np := Pos{a.Pos.X + d.X, a.Pos.Y + d.Y}
		if !w.CanMoveTo(a, np) {
			continue
		}
		if !w.FirePitsDisabled && w.Maze.Cells[np.Y][np.X] == CellFirePit {
			continue
		}
		if !w.WumpusDisabled {
			if wm := w.WumpusAt[np.Y][np.X]; wm != nil && wm.Alive {
				continue
			}
		}
		return np
	}
	for _, d := range dirs {
		np := Pos{a.Pos.X + d.X, a.Pos.Y + d.Y}
		if !w.CanMoveTo(a, np) {
			continue
		}
		return np
	}
	return a.Pos
}

// MoveWumpus: each wumpus invokes its assigned hunting Strategy.
func (w *World) MoveWumpus() {
	order := w.Rng.Perm(len(w.Wumpus))
	for _, idx := range order {
		wm := w.Wumpus[idx]
		if !wm.Alive {
			continue
		}
		if w.HasAdjacentLiveAgent(wm) {
			continue
		}
		if wm.Strategy == nil {
			wm.Strategy = w.newWumpusStrategy()
		}
		var target Pos
		switch {
		case wm.VengeanceCycles > 0 && w.vengeanceStrategy != nil:
			wm.VengeanceCycles--
			target = w.vengeanceStrategy(w, wm)
		case wm.VengeanceCycles > 0:
			wm.VengeanceCycles--
			if wm.Strategy == nil {
				continue
			}
			target = wm.Strategy(w, wm)
		case wm.Strategy != nil:
			target = wm.Strategy(w, wm)
		default:
			continue
		}
		if target == wm.Pos {
			continue
		}
		if !InBounds(target.X, target.Y) || !w.Maze.IsWalkable(target) {
			continue
		}
		if w.WumpusAt[target.Y][target.X] != nil {
			continue
		}
		w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
		wm.Pos = target
		w.WumpusAt[target.Y][target.X] = wm
	}
}

// HasAdjacentLiveAgent reports whether any of the wumpus's 4 cardinal
// neighbors holds a live agent.
func (w *World) HasAdjacentLiveAgent(wm *Wumpus) bool {
	for _, d := range Cardinals {
		nx, ny := wm.Pos.X+d.X, wm.Pos.Y+d.Y
		if !InBounds(nx, ny) {
			continue
		}
		if a := w.AgentAt[ny][nx]; a != nil && a.Alive {
			return true
		}
	}
	return false
}

// ResolvePitDeaths: any live entity on a fire pit dies — unless an
// agent has water charges, in which case the water is consumed AND
// the fire pit is extinguished. When FirePitsDisabled is set, fire
// pits are inert (no deaths, no water consumption).
func (w *World) ResolvePitDeaths() {
	if w.FirePitsDisabled {
		return
	}
	for _, a := range w.Agents {
		if a.Disabled || !a.Alive {
			continue
		}
		if w.Maze.Cells[a.Pos.Y][a.Pos.X] != CellFirePit {
			continue
		}
		if a.Water > 0 {
			a.Water--
			w.ExtinguishFirePit(a.Pos)
			continue
		}
		w.KillAgent(a, "fire pit")
		if w.Rng.Float64() < 0.5 {
			w.SpawnReplacementWaterPit()
		}
	}
	for _, wm := range w.Wumpus {
		if wm.Alive && w.Maze.Cells[wm.Pos.Y][wm.Pos.X] == CellFirePit {
			w.KillWumpus(wm)
		}
	}
	// Swarm clones die on fire pits with no water-share fallback —
	// the leader's water charge protects only the leader's own
	// cell. Killed clones leave the swarm's count down by one.
	for _, leader := range w.Agents {
		if !leader.Alive || leader.Disabled || leader.SwarmGroupID == 0 {
			continue
		}
		for _, c := range leader.SwarmClones {
			if c == nil || !c.Alive {
				continue
			}
			if w.Maze.Cells[c.Pos.Y][c.Pos.X] == CellFirePit {
				c.Alive = false
			}
		}
	}
}

// ExtinguishFirePit converts a fire-pit cell back into a path cell,
// removes it from the FirePits slice, and recomputes Heat in its
// Moore-neighborhood (a cell stays hot only if *some other* fire pit
// still neighbours it).
func (w *World) ExtinguishFirePit(p Pos) {
	if w.Maze.Cells[p.Y][p.X] != CellFirePit {
		return
	}
	w.Maze.Cells[p.Y][p.X] = CellPath
	for i, fp := range w.Maze.FirePits {
		if fp == p {
			w.Maze.FirePits = append(w.Maze.FirePits[:i], w.Maze.FirePits[i+1:]...)
			break
		}
	}
	// Recompute Heat in the 3x3 around p — each cell stays hot iff at
	// least one surviving fire pit is in ITS Moore-neighborhood.
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := p.X+dx, p.Y+dy
			if !InBounds(nx, ny) {
				continue
			}
			if w.Maze.Cells[ny][nx] == CellWall {
				w.Heat[ny][nx] = false
				continue
			}
			hot := false
			for _, fp := range w.Maze.FirePits {
				if AbsInt(fp.X-nx) <= 1 && AbsInt(fp.Y-ny) <= 1 && !(fp.X == nx && fp.Y == ny) {
					hot = true
					break
				}
			}
			w.Heat[ny][nx] = hot
		}
	}
}

// SpawnReplacementWaterPit places a fresh water pit on a random path
// cell at least 5 Manhattan units from the entrance.
func (w *World) SpawnReplacementWaterPit() {
	if w.WaterPitsDisabled {
		return
	}
	for i := 0; i < 100; i++ {
		x := w.Rng.Intn(BoardWidth)
		y := w.Rng.Intn(BoardHeight)
		p := Pos{x, y}
		if w.Maze.Cells[y][x] != CellPath {
			continue
		}
		if w.AgentAt[y][x] != nil || w.WumpusAt[y][x] != nil {
			continue
		}
		if AbsInt(p.X-w.Maze.EntrancePos.X)+AbsInt(p.Y-w.Maze.EntrancePos.Y) < 5 {
			continue
		}
		w.Maze.Cells[y][x] = CellWaterPit
		w.Maze.WaterPits = append(w.Maze.WaterPits, p)
		return
	}
}

// CollectWater: agents picking up water pits consume them and gain a
// water charge. No-op when WaterPitsDisabled is set.
func (w *World) CollectWater() {
	if w.WaterPitsDisabled {
		return
	}
	for _, a := range w.Agents {
		if a.Disabled || !a.Alive {
			continue
		}
		if w.Maze.Cells[a.Pos.Y][a.Pos.X] == CellWaterPit {
			w.Maze.Cells[a.Pos.Y][a.Pos.X] = CellPath
			a.Water++
		}
	}
}

// MaxStartsPerMaze is a historical per-agent spawn cap. Retirement
// is now goal-based (see RespawnAgents) so this constant is kept
// only as a reference / soft expectation of how many lives an
// average run uses on a single maze. RespawnAgents does NOT consult
// it.
const MaxStartsPerMaze = 999

// MazeSolvedGoals is the per-agent goal-reach threshold that
// counts toward the win condition: when at least
// MazeSolvedAgentCount agents have GoalsReached >= MazeSolvedGoals
// the maze is considered solved.
const MazeSolvedGoals = 999
const MazeSolvedAgentCount = 3

// MazeSolved reports whether the win condition is met:
// MazeSolvedAgentCount agents have reached GoalsReached >=
// MazeSolvedGoals on the current maze.
func (w *World) MazeSolved() bool {
	hit := 0
	for _, a := range w.Agents {
		if a.Stats.GoalsReached >= MazeSolvedGoals {
			hit++
			if hit >= MazeSolvedAgentCount {
				return true
			}
		}
	}
	return false
}

// RespawnAgents counts respawn timers down; at 0 it places the agent
// at its EntrancePos. Agents may share the entrance cell at spawn time.
// Retirement is goal-based: once an agent's Stats.GoalsReached hits
// MazeSolvedGoals, mission-accomplished — the agent stops respawning
// (it's done its share toward the maze-solved condition). Agents that
// keep dying without reaching that threshold keep getting fresh
// lives — there's no per-map retry cap that would lock a struggling
// agent out of the maze before the map gets reseeded.
func (w *World) RespawnAgents() {
	// justSpawned collects the agents that came alive on this tick.
	// We bump StrategyPerf[…].Started AFTER quorum/singleton
	// enforcement so the count reflects the FINAL strategy
	// assignment (a respawn that got demoted from R or drafted into
	// S should be tallied against the post-enforcement letter).
	var justSpawned []*Agent
	for _, a := range w.Agents {
		if a.Disabled || a.Alive {
			continue
		}
		// Goal-based retirement: stop respawning once the agent has
		// already contributed its MazeSolvedGoals worth of solves.
		// (Start-based retirement was buggy — agents could exhaust
		// MaxStartsPerMaze while still well below the goal threshold
		// and get permanently locked out of the map.)
		if a.Stats.GoalsReached >= MazeSolvedGoals {
			continue
		}
		if a.RespawnIn > 0 {
			a.RespawnIn--
		}
		if a.RespawnIn != 0 {
			continue
		}
		// Fresh journey starts here. Any stale swarm state carried
		// over from the previous life (clones that survived to the
		// goal-collapse, or the prior SwarmGroupID) must be cleared
		// so maintainSwarmMembership below allocates a brand-new
		// independent swarm if the agent picks S again.
		a.SwarmGroupID = 0
		a.SwarmClones = nil
		// Each agent spawns at its OWN EntrancePos (assigned once at
		// world construction by pickAgentEntrances). Falls back to
		// the maze's canonical entrance when an agent's entry
		// wasn't initialized (e.g., tests that bypass construction).
		entrance := a.EntrancePos
		if entrance == (Pos{}) || !w.Maze.IsWalkable(entrance) {
			entrance = w.Maze.EntrancePos
		}
		a.Alive = true
		a.Stats.Starts++
		// New journey begins. Pick the strategy FIRST (50% softmax
		// over trust, 50% uniform random); the trustee decision
		// gates on whether the chosen strategy can even sense
		// scent.
		if len(w.strategyLetters) > 0 {
			a.PickStrategy(w.strategyLetters, w.Rng)
		}
		// Only pick a trustee when the chosen strategy actually
		// consults the scent channel. Otherwise the agent would
		// "follow" a leader it can't sense, and the leader's
		// trust score would absorb undeserved penalties at
		// journey end.
		if StrategyUsesScent(a.CurrentStrategy) {
			a.PickTrustee(w, w.Rng)
		} else {
			a.CurrentTrustee = 0
		}
		a.Pos = entrance
		a.Plan = nil
		a.Stats.ActualDistance = 0
		a.Stats.OnPathSteps = 0
		a.Stats.OffPathSteps = 0
		a.TicksAlive = 0
		a.Water = 0
		a.Visited = nil
		a.PendingBonus = 0
		a.HasLastFrom = false
		a.LastFromCell = Pos{}
		a.DeadEndCount = 0
		a.LastDeadEndCycle = 0
		a.MaxStartDist = 0
		a.SearchAnim = nil
		a.JourneyTrusteeContactTicks = 0
		a.OpportunisticFollowed = nil
		w.AgentAt[entrance.Y][entrance.X] = a
		w.MarkAgentSensed(a)
		a.RespawnIn = -1
		justSpawned = append(justSpawned, a)
	}
	w.EnforceSwarmQuorum()
	w.EnforceBenchmarkSingleton()
	// Post-enforcement bookkeeping: tally Started (for #Runs) and
	// set up swarm membership for any agent whose final strategy
	// is S.
	for _, a := range justSpawned {
		if a.CurrentStrategy == 0 {
			continue
		}
		w.ensureStrategyPerf(a.CurrentStrategy).Started++
		w.maintainSwarmMembership(a)
	}
}

// maintainSwarmMembership updates a.SwarmGroupID + a.SwarmClones to
// reflect the agent's current strategy. Called after every spawn /
// enforcement pass.
//
//   - If a.CurrentStrategy is a swarm letter and a.SwarmGroupID is 0,
//     allocate a fresh swarm ID. The leader starts SOLO — clones are
//     NOT pre-spawned; each algorithm forks them lazily at decision
//     points during movement (see strategy.SwarmStrategy), capped at
//     SwarmClonesPerLeader alive clones.
//   - If a.CurrentStrategy is anything else and a.SwarmGroupID is
//     non-zero, the agent has left the swarm — clear membership and
//     drop clones.
//
// Swarm members keep their SwarmGroupID across respawns so that group
// cohesion persists through individual deaths (the promotion-on-
// leader-death path).
//
// Run-counting note: the leader's per-agent Started bump in
// RespawnAgents counts the whole swarm once; lazily-forked clones are
// the swarm's body, not independent runs, so forking never touches
// #Runs (keeps Started consistent with the outcome columns).
func (w *World) maintainSwarmMembership(a *Agent) {
	if IsSwarmStrategy(a.CurrentStrategy) {
		if a.SwarmGroupID == 0 {
			w.nextSwarmGroupID++
			a.SwarmGroupID = w.nextSwarmGroupID
		}
		// Leader starts solo: no clones until the algorithm forks them.
		return
	}
	// Left the swarm.
	if a.SwarmGroupID != 0 {
		a.SwarmGroupID = 0
		a.SwarmClones = nil
	}
}

// EnforceBenchmarkSingleton caps the omniscient R strategy at
// MaxBenchmarkAgents (1) alive user at any time. If two or more
// agents picked R this tick, the extras are demoted to plain
// Bayesian (T) — keeping the comparison clean by ensuring at most
// one benchmark runner is in play.
func (w *World) EnforceBenchmarkSingleton() {
	keptOne := false
	for _, a := range w.Agents {
		if !a.Alive || a.Disabled {
			continue
		}
		if a.CurrentStrategy != BenchmarkStrategyLetter {
			continue
		}
		if !keptOne {
			keptOne = true
			continue
		}
		// Demote: T is the closest non-omniscient Bayesian relative
		// and doesn't require scent perception or a trustee.
		a.CurrentStrategy = 'T'
		a.CurrentTrustee = 0
	}
}

// EnforceSwarmQuorum is now a no-op under the independent-swarm
// model. Previously this function drafted agents into S to meet a
// SwarmMinQuorum=3 threshold; that made sense when ALL S-agents
// shared one global hive. Under per-agent swarms — each S-picker
// is its own complete swarm of 1 leader + SwarmClonesPerLeader
// clones — quorum is automatic: every S-picker already has 11
// members.
//
// Kept as an exported method so existing call sites (and tests)
// continue to compile. Safe to remove in a future cleanup.
func (w *World) EnforceSwarmQuorum() {
	// Intentional no-op. See doc comment.
}

// RecomputeStench rebuilds the stench grid from current wumpus
// positions. When WumpusDisabled is set, the grid is cleared and the
// wumpus loop is skipped so sensors read clean.
func (w *World) RecomputeStench() {
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			w.Stench[y][x] = false
		}
	}
	if w.WumpusDisabled {
		return
	}
	for _, wm := range w.Wumpus {
		if !wm.Alive {
			continue
		}
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nx, ny := wm.Pos.X+dx, wm.Pos.Y+dy
				if !InBounds(nx, ny) {
					continue
				}
				if w.Maze.Cells[ny][nx] == CellWall {
					continue
				}
				w.Stench[ny][nx] = true
			}
		}
	}
}

// CheckGoal: when an agent reaches the goal, record solve stats,
// bump GoalsReached, queue respawn, and spawn a fresh hazard.
func (w *World) CheckGoal() {
	// Swarm clones reaching the goal credit their leader. We do
	// this BEFORE the leader-position pass so a swarm can still
	// score even if the leader itself never reached the goal cell.
	// The per-leader inner loop breaks after the first clone-at-
	// goal match so the swarm gets exactly ONE win regardless of
	// how many clones happen to step onto the goal in the same tick.
	for _, leader := range w.Agents {
		if !leader.Alive || leader.SwarmGroupID == 0 || len(leader.SwarmClones) == 0 {
			continue
		}
		for _, c := range leader.SwarmClones {
			if c == nil || !c.Alive || c.Pos != w.Maze.GoalPos {
				continue
			}
			// Snap the leader onto the goal cell so the existing
			// per-agent goal-handling treats this as a leader win.
			if w.AgentAt[leader.Pos.Y][leader.Pos.X] == leader {
				w.AgentAt[leader.Pos.Y][leader.Pos.X] = nil
			}
			leader.Pos = w.Maze.GoalPos
			w.AgentAt[leader.Pos.Y][leader.Pos.X] = leader
			// Clones STAY ALIVE for the duration of this CheckGoal
			// call — they'll be visibly collapsed onto the goal in
			// the per-agent handler below. Stale-swarm cleanup
			// happens at the leader's next RespawnAgents pass so
			// the new journey starts with a fresh group ID + 10
			// fresh clones.
			break
		}
	}
	for _, a := range w.Agents {
		if !a.Alive || a.Pos != w.Maze.GoalPos {
			continue
		}
		// Swarm collapse: when an S leader wins (either by walking
		// onto the goal itself or by having a clone get there), all
		// alive clones snap onto the goal cell as a single visual
		// "all together at the finish" pose. The collapse fires
		// before the win is recorded so the rendering of this tick
		// shows the assembled swarm at the goal.
		if a.SwarmGroupID != 0 && len(a.SwarmClones) > 0 {
			for _, c := range a.SwarmClones {
				if c != nil && c.Alive {
					c.Pos = w.Maze.GoalPos
				}
			}
		}
		a.Stats.GoalsReached++
		t := a.TicksAlive
		if a.Stats.BestSolveDistance == 0 || a.Stats.ActualDistance < a.Stats.BestSolveDistance {
			a.Stats.BestSolveDistance = a.Stats.ActualDistance
			a.Stats.BestSolveTime = t
		}
		// Roll min / max / avg solve time. Min seeds on first solve.
		if a.Stats.MinSolveTime == 0 || t < a.Stats.MinSolveTime {
			a.Stats.MinSolveTime = t
		}
		if t > a.Stats.MaxSolveTime {
			a.Stats.MaxSolveTime = t
		}
		// Running mean: new_avg = old_avg + (t - old_avg) / n.
		n := float64(a.Stats.GoalsReached)
		a.Stats.AvgSolveTime += (float64(t) - a.Stats.AvgSolveTime) / n
		a.Stats.LastSolveTime = t
		if w.Stats.OptimalDistance > 0 {
			alignment := float64(a.Stats.OnPathSteps-a.Stats.OffPathSteps) /
				float64(w.Stats.OptimalDistance)
			if alignment > a.Stats.BestAlignment {
				a.Stats.BestAlignment = alignment
			}
		}
		// Append this solve to build/solves/agent<label>.log BEFORE
		// flipping Alive so the record reflects the just-finished
		// run's TicksAlive / ActualDistance.
		w.appendSolveRecord(a)
		// Journey ended in success — update trust before flipping.
		w.endJourney(a, true)
		w.recordAgentGoal(a)
		// Strategy Performance: goal reach bumps Win.NoFollow or
		// Win.Following based on trustee state.
		w.recordStrategyGoal(a)
		// Post-win path optimization: BFS through the agent's
		// perceived terrain to find the shortest entrance→goal
		// route it could have taken. Subsequent lives consult this
		// cache before running their native planner.
		w.optimizeKnownPath(a)
		a.Alive = false
		if w.AgentAt[a.Pos.Y][a.Pos.X] == a {
			w.AgentAt[a.Pos.Y][a.Pos.X] = nil
		}
		a.RespawnIn = RespawnTicks
		w.SpawnGoalHazard()
	}
}

// SpawnGoalHazard places a random fire pit or fresh wumpus far from
// the entrance whenever an agent solves the maze. Skips when BOTH
// hazard families are disabled; when only one is disabled, falls back
// to the other.
func (w *World) SpawnGoalHazard() {
	if w.FirePitsDisabled && w.WumpusDisabled {
		return
	}
	const maxAttempts = 200
	for i := 0; i < maxAttempts; i++ {
		x := w.Rng.Intn(BoardWidth)
		y := w.Rng.Intn(BoardHeight)
		p := Pos{x, y}
		if w.Maze.Cells[y][x] != CellPath {
			continue
		}
		if w.AgentAt[y][x] != nil || w.WumpusAt[y][x] != nil {
			continue
		}
		if AbsInt(p.X-w.Maze.EntrancePos.X)+AbsInt(p.Y-w.Maze.EntrancePos.Y) < 10 {
			continue
		}
		// Pick whichever hazard family is enabled. If both are, 50/50.
		spawnFire := !w.FirePitsDisabled
		spawnWumpus := !w.WumpusDisabled
		switch {
		case spawnFire && spawnWumpus:
			spawnFire = w.Rng.Float64() < 0.5
			spawnWumpus = !spawnFire
		case spawnFire:
			// only fire
		case spawnWumpus:
			// only wumpus
		default:
			return
		}
		if spawnFire {
			w.Maze.Cells[y][x] = CellFirePit
			w.Maze.FirePits = append(w.Maze.FirePits, p)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := x+dx, y+dy
					if !InBounds(nx, ny) {
						continue
					}
					if w.Maze.Cells[ny][nx] != CellWall {
						w.Heat[ny][nx] = true
					}
				}
			}
		} else {
			wm := &Wumpus{ID: w.nextWumpusID, Pos: p, Alive: true, Strategy: w.newWumpusStrategy(), Aggressiveness: w.Rng.Intn(WumpusAggressionMax + 1), HuntMode: WumpusHuntMode(w.Rng.Intn(WumpusHuntModeCount))}
			w.nextWumpusID++
			w.Wumpus = append(w.Wumpus, wm)
			w.WumpusAt[y][x] = wm
		}
		return
	}
}

// KillAgent removes the agent from the spatial index, increments per-
// agent death counter, records the cause, and starts the respawn timer.
func (w *World) KillAgent(a *Agent, reason ...string) {
	// Swarm leader promotion: if this agent is a swarm leader with
	// at least one surviving clone, the clone is promoted into the
	// leader slot instead of the swarm dissolving. The leader's
	// life continues (no Stats.Deaths bump, no respawn, no trust
	// update for this "death" — it's a body swap, not a journey end).
	if a.SwarmGroupID != 0 && a.SwarmClones != nil {
		for i, c := range a.SwarmClones {
			if c == nil || !c.Alive {
				continue
			}
			// Promote clone i into the leader slot.
			if w.AgentAt[a.Pos.Y][a.Pos.X] == a {
				w.AgentAt[a.Pos.Y][a.Pos.X] = nil
			}
			a.Pos = c.Pos
			a.Plan = c.Plan
			a.KnownShortestPath = c.KnownShortestPath
			// Adopt the promoted clone's individual travel distance so
			// the leader's TTL reflects the SURVIVING entity's budget —
			// otherwise the new leader inherits the dead leader's over-
			// TTL ActualDistance and the whole swarm cascades to death.
			a.Stats.ActualDistance = c.Dist
			w.AgentAt[a.Pos.Y][a.Pos.X] = a
			if w.DecisionLogEnabled {
				w.LogDecision(fmt.Sprintf("t%d %c promote clone@(%d,%d) d%d",
					w.Cycle, a.Label, a.Pos.X, a.Pos.Y, c.Dist))
			}
			// Shrink the clone slice (remove the promoted one).
			a.SwarmClones = append(a.SwarmClones[:i], a.SwarmClones[i+1:]...)
			a.SearchAnim = nil
			return
		}
		// No clones left to promote — the swarm dies for real.
		// Fall through to normal death handling.
	}
	// Journey ended in failure — update trust before clearing state.
	w.endJourney(a, false)
	a.Alive = false
	a.SearchAnim = nil
	if w.AgentAt[a.Pos.Y][a.Pos.X] == a {
		w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	}
	a.Stats.Deaths++
	r := "unknown"
	if len(reason) > 0 && reason[0] != "" {
		r = reason[0]
	}
	a.Stats.LastDeathReason = r
	// Learn-by-dying: a TTL death pins the agent's belief about
	// its step budget. The killer fires the first step PAST the
	// threshold, so TTL = ActualDistance − 1. We overwrite every
	// time so the most recent observation always wins (the policy
	// stays alive in case TTL drifts).
	if r == "ttl" && a.Stats.ActualDistance > 0 {
		a.LearnedTTL = a.Stats.ActualDistance - 1
	}
	// Surface the death in the TUI's rolling Events log.
	w.recordAgentDeath(a, r)
	// Strategy Performance: tally per-strategy outcome counts.
	w.recordStrategyDeath(a, r == "ttl")
	a.RespawnIn = RespawnTicks
}

// KillWumpus removes a wumpus, bumps WumpusDied, spawns a replacement,
// and arms pack vengeance on every survivor.
func (w *World) KillWumpus(wm *Wumpus) {
	wm.Alive = false
	if w.WumpusAt[wm.Pos.Y][wm.Pos.X] == wm {
		w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	}
	w.Stats.WumpusDied++
	for _, other := range w.Wumpus {
		if other == wm || !other.Alive {
			continue
		}
		other.VengeanceCycles = PackVengeanceCycles
	}
	w.SpawnReplacementWumpus()
}

// SpawnReplacementWumpus drops a new wumpus on a random walkable cell
// well away from the entrance.
func (w *World) SpawnReplacementWumpus() {
	if w.WumpusDisabled {
		return
	}
	for attempts := 0; attempts < 100; attempts++ {
		x := w.Rng.Intn(BoardWidth)
		y := w.Rng.Intn(BoardHeight)
		p := Pos{x, y}
		if w.Maze.Cells[y][x] != CellPath {
			continue
		}
		if w.WumpusAt[y][x] != nil || w.AgentAt[y][x] != nil {
			continue
		}
		if AbsInt(p.X-w.Maze.EntrancePos.X)+AbsInt(p.Y-w.Maze.EntrancePos.Y) < 10 {
			continue
		}
		nw := &Wumpus{ID: w.nextWumpusID, Pos: p, Alive: true, Strategy: w.newWumpusStrategy(), Aggressiveness: w.Rng.Intn(WumpusAggressionMax + 1), HuntMode: WumpusHuntMode(w.Rng.Intn(WumpusHuntModeCount))}
		w.nextWumpusID++
		w.Wumpus = append(w.Wumpus, nw)
		w.WumpusAt[y][x] = nw
		return
	}
}

// SetWumpusDisabled flips the WumpusDisabled toggle and SPAWNS or
// CLEARS entities to match. Going from enabled → disabled removes
// every wumpus from the board; disabled → enabled scatters a fresh
// random population (5..12) on path cells far from the entrance,
// using the standard RandomWumpusSpawn placement.
func (w *World) SetWumpusDisabled(disabled bool) {
	if disabled == w.WumpusDisabled {
		return
	}
	w.WumpusDisabled = disabled
	if disabled {
		w.ClearWumpus()
		return
	}
	w.spawnInitialWumpus()
}

// SetFirePitsDisabled is the fire-pit analog of SetWumpusDisabled.
// Enable-edge re-carves fire pits into the existing maze rooms (the
// same recipe as GenerateMaze) and re-seeds the surrounding Heat
// envelope. Disable-edge converts every fire-pit cell back to path
// and zeroes Heat.
func (w *World) SetFirePitsDisabled(disabled bool) {
	if disabled == w.FirePitsDisabled {
		return
	}
	w.FirePitsDisabled = disabled
	if disabled {
		w.ClearFirePits()
		return
	}
	w.spawnInitialFirePits()
}

// SetWaterPitsDisabled is the water-pit analog. Enable-edge scatters
// 3..10 fresh water pits on random path cells.
func (w *World) SetWaterPitsDisabled(disabled bool) {
	if disabled == w.WaterPitsDisabled {
		return
	}
	w.WaterPitsDisabled = disabled
	if disabled {
		w.ClearWaterPits()
		return
	}
	w.spawnInitialWaterPits()
}

// spawnInitialWumpus scatters 5..12 wumpus on the board using the
// standard RandomWumpusSpawn placement (path cells, ≥20 Manhattan
// from the entrance, no overlap with existing wumpus). Used by
// NewWorldWithConfig and by SetWumpusDisabled(false).
func (w *World) spawnInitialWumpus() {
	n := 5 + w.Rng.Intn(8)
	for i := 0; i < n; i++ {
		p := w.RandomWumpusSpawn()
		wm := &Wumpus{
			ID:             w.nextWumpusID,
			Pos:            p,
			Alive:          true,
			Strategy:       w.newWumpusStrategy(),
			Aggressiveness: w.Rng.Intn(WumpusAggressionMax + 1),
			HuntMode:       WumpusHuntMode(w.Rng.Intn(WumpusHuntModeCount)),
		}
		w.nextWumpusID++
		w.Wumpus = append(w.Wumpus, wm)
		w.WumpusAt[p.Y][p.X] = wm
	}
}

// spawnInitialFirePits carves fire pits inside the maze rooms (same
// recipe as GenerateMaze) and re-seeds the Heat envelope around each.
// Skips cells that are not currently CellPath, the entrance, or the
// goal so we don't clobber other terrain.
func (w *World) spawnInitialFirePits() {
	for _, r := range w.Maze.Rooms {
		nPits := w.Rng.Intn(3)
		for j := 0; j < nPits; j++ {
			x := r.X + w.Rng.Intn(r.W)
			y := r.Y + w.Rng.Intn(r.H)
			p := Pos{x, y}
			if p == w.Maze.EntrancePos || p == w.Maze.GoalPos {
				continue
			}
			if w.Maze.Cells[y][x] != CellPath {
				continue
			}
			w.Maze.Cells[y][x] = CellFirePit
			w.Maze.FirePits = append(w.Maze.FirePits, p)
		}
	}
	for _, p := range w.Maze.FirePits {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nx, ny := p.X+dx, p.Y+dy
				if !InBounds(nx, ny) {
					continue
				}
				if w.Maze.Cells[ny][nx] != CellWall {
					w.Heat[ny][nx] = true
				}
			}
		}
	}
}

// spawnInitialWaterPits scatters 3..10 water pits on random path
// cells. Uses up to 100 placement attempts per pit before giving up
// on that particular pit.
func (w *World) spawnInitialWaterPits() {
	numWater := 3 + w.Rng.Intn(8)
	for j := 0; j < numWater; j++ {
		for attempts := 0; attempts < 100; attempts++ {
			x := w.Rng.Intn(BoardWidth)
			y := w.Rng.Intn(BoardHeight)
			p := Pos{x, y}
			if w.Maze.Cells[y][x] != CellPath {
				continue
			}
			if p == w.Maze.EntrancePos || p == w.Maze.GoalPos {
				continue
			}
			w.Maze.Cells[y][x] = CellWaterPit
			w.Maze.WaterPits = append(w.Maze.WaterPits, p)
			break
		}
	}
}

// ClearWumpus permanently removes every wumpus from the world: nils
// the WumpusAt spatial index, marks each Wumpus as dead, drops the
// Wumpus slice. After this returns there are no wumpus entities to
// move, fight, or render. New wumpus only appear via the normal
// spawn paths IF the WumpusDisabled flag becomes false AND something
// (KillWumpus, SpawnGoalHazard) decides to spawn one.
func (w *World) ClearWumpus() {
	for _, wm := range w.Wumpus {
		if wm.Alive {
			w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
			wm.Alive = false
		}
	}
	w.Wumpus = nil
	// Stench is derived from wumpus positions — wipe it so the next
	// RecomputeStench cycle sees zeros.
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			w.Stench[y][x] = false
		}
	}
}

// ClearFirePits permanently removes every fire pit from the maze:
// converts each CellFirePit back to CellPath, empties the FirePits
// slice, and zeroes the Heat grid (heat is fire-pit-derived).
func (w *World) ClearFirePits() {
	for _, p := range w.Maze.FirePits {
		if w.Maze.Cells[p.Y][p.X] == CellFirePit {
			w.Maze.Cells[p.Y][p.X] = CellPath
		}
	}
	w.Maze.FirePits = nil
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			w.Heat[y][x] = false
		}
	}
}

// ClearWaterPits permanently removes every water pit: each
// CellWaterPit becomes CellPath, the WaterPits slice empties.
func (w *World) ClearWaterPits() {
	for _, p := range w.Maze.WaterPits {
		if w.Maze.Cells[p.Y][p.X] == CellWaterPit {
			w.Maze.Cells[p.Y][p.X] = CellPath
		}
	}
	w.Maze.WaterPits = nil
}

// ApplyToggles clears entities whose toggle is currently disabled.
// Idempotent — safe to call after every toggle flip and at the end of
// world construction. Does NOT re-spawn anything when a toggle flips
// back to enabled.
func (w *World) ApplyToggles() {
	if w.WumpusDisabled {
		w.ClearWumpus()
	}
	if w.FirePitsDisabled {
		w.ClearFirePits()
	}
	if w.WaterPitsDisabled {
		w.ClearWaterPits()
	}
}

// Scent-following follower set: each follower picks a trustee per
// journey and shapes its reward based on whether the trustee's
// scent appears under it. The "leader" pool is the set of agents
// that have NO scent perception themselves — they are the canonical
// pickable trustees (plus, after stage 3, peer followers).
//
// Lineup mapped to scent role:
//
//	Leaders:    1 (BFS), 2 (DFS), 3 (Bayesian), 8 (Bayesian+fs)
//	Followers:  4 (scent-follower),     9 (scent-follower+fs),
//	            5 (DQN),                A (DQN+fs),
//	            6 (POMCP),              B (POMCP+fs),
//	            7 (QMDP),               C (QMDP+fs)
var (
	ScentLeaderLabels   = []rune{'1', '2', '3', '8'}
	ScentFollowerLabels = []rune{'4', '5', '6', '7', '9', 'A', 'B', 'C'}
)

// ScentRunsForTrustWeighting: how many initial runs the agent makes
// uniform-random picks from {1,2,3} before switching to trust-
// weighted selection. The user-facing rule: "After the first 10
// random selections, agents will begin using their perceived trust."
//
// ScentRunsForPeerExpansion: after this many runs, the agent picks
// 50/50 between trust-weighted-{1,2,3} and uniform-peer-{4..7}\self.
const (
	ScentRunsForTrustWeighting = 10
	ScentRunsForPeerExpansion  = 20
)

// IsScentFollower reports whether `label` belongs to the follower
// set (agents 4-7). Agents 1-3 are leaders, not followers.
func IsScentFollower(label rune) bool {
	for _, l := range ScentFollowerLabels {
		if l == label {
			return true
		}
	}
	return false
}

// ScentPeerLabels returns the follower-set labels EXCLUDING `self`.
// Used in stage 3 of trustee selection (runs > ScentRunsForPeerExpansion).
func ScentPeerLabels(self rune) []rune {
	peers := make([]rune, 0, len(ScentFollowerLabels)-1)
	for _, l := range ScentFollowerLabels {
		if l != self {
			peers = append(peers, l)
		}
	}
	return peers
}

// ScentShapingMagnitude is the BASE bonus (or penalty) credited per
// step when the agent stands on attract-scent (or dynamic-repel
// scent). Scaled by ScentFreshness so faded trails contribute less,
// and by ScentMagnitudeFor(label) so individual agents can have a
// stronger pull. Sized larger than ExplorationBonus (40) so
// following / avoiding leader scent dominates the per-step shaping
// channel even at the 1.0 baseline multiplier.
const ScentShapingMagnitude = 50.0

// ScentMagnitudeFor returns a per-follower multiplier on
// ScentShapingMagnitude. Agent 5 (DQN) gets a 5× boost: its Bellman
// update competes with RealDistanceShaping deltas that can spike
// into the hundreds when the agent reaches a new max distance from
// the entrance, so a plain ±50 scent signal gets drowned out. The
// stronger multiplier gives the trusted-scent gradient enough
// weight to actually shape the network's policy.
//
// Other followers (4, 6, 7) keep the 1.0 baseline — agent 4 uses
// Q-learning (lower-magnitude updates), and 6 / 7 read scent
// directly in their planners, not through PendingBonus.
func ScentMagnitudeFor(label rune) float64 {
	if label == '5' {
		return 5.0
	}
	return 1.0
}

// mooreDeltas is the 8-cell king-move neighborhood (cardinal +
// diagonal) used by Moore-connected BFS for scent perception.
var mooreDeltas = [8]Pos{
	{X: -1, Y: -1}, {X: 0, Y: -1}, {X: 1, Y: -1},
	{X: -1, Y: 0}, {X: 1, Y: 0},
	{X: -1, Y: 1}, {X: 0, Y: 1}, {X: 1, Y: 1},
}

// ScentSensedCells returns the set of cells whose scent agent `a`
// can currently perceive. Computed as a Moore-connected BFS from
// a.Pos out to a.SmellRadius (default DefaultSmellRadius = 2).
// Walls block propagation — a wall cell itself is NOT included in
// the result (walls carry no scent), but the BFS stops at walls so
// cells past them are unreachable.
//
// Used by:
//   - ApplyScentShaping (aggregate trustee / negative-trust signal).
//   - ScentSignedFreshness (cell-level gate for DQN / QL features).
//   - JourneyTrusteeContactTicks (contact bump for trust update).
//
// At the default radius of 2, the result contains up to 25 cells in
// a 5×5 box minus walls and cells blocked by walls.
func (w *World) ScentSensedCells(a *Agent) map[Pos]bool {
	radius := a.SmellRadius
	if radius < 1 {
		radius = DefaultSmellRadius
	}
	sensed := map[Pos]bool{a.Pos: true}
	type node struct {
		p     Pos
		depth int
	}
	queue := []node{{a.Pos, 0}}
	visited := map[Pos]bool{a.Pos: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= radius {
			continue
		}
		for _, d := range mooreDeltas {
			np := Pos{X: cur.p.X + d.X, Y: cur.p.Y + d.Y}
			if !InBounds(np.X, np.Y) || visited[np] {
				continue
			}
			visited[np] = true
			// Walls don't carry scent and they block propagation —
			// don't add them to the sensed set and don't enqueue.
			if w.Maze.Cells[np.Y][np.X] == CellWall {
				continue
			}
			sensed[np] = true
			queue = append(queue, node{np, cur.depth + 1})
		}
	}
	return sensed
}

// ScentSignedFreshness returns the agent's signed perception of
// scent at cell (x, y):
//
//	owner == a.CurrentTrustee     → +freshness  (attract)
//	TrustScores[owner] < 0        → −freshness  (dynamic repel)
//	otherwise                      → 0
//
// This is the canonical "what does agent `a` smell here?" function,
// shared by:
//   - DQN feature vector (agent 5) — 4 cardinal-neighbor entries
//   - Q-learning argmax bias (agent 4)
//   - any future perception-based learner
//
// Returns 0 for non-follower agents and for cells out of bounds /
// with no fresh scent. Range: [-1, +1].
func (w *World) ScentSignedFreshness(a *Agent, x, y int) float64 {
	if !IsScentFollower(a.Label) {
		return 0
	}
	freshness := w.ScentFreshness(x, y)
	if freshness <= 0 {
		return 0
	}
	owner := w.ScentOwner[y][x]
	if owner == 0 {
		return 0
	}
	if a.CurrentTrustee != 0 && owner == a.CurrentTrustee {
		return freshness
	}
	if a.TrustScores != nil && a.TrustScores[owner] < 0 {
		return -freshness
	}
	return 0
}

// ApplyScentShaping returns the per-step scent-shaping bonus for `a`
// aggregated across every cell the agent can currently perceive
// (a Moore-connected, wall-respecting neighborhood out to
// a.SmellRadius — see ScentSensedCells):
//
//	bonus = mag × (maxTrusteeFreshness − maxNegativeTrustFreshness)
//
// where:
//
//	maxTrusteeFreshness  = strongest freshness reading from any
//	                        sensed cell carrying CurrentTrustee scent
//	maxNegativeTrustFreshness = strongest freshness reading from any
//	                            sensed cell carrying a label whose
//	                            TrustScores entry has gone negative
//	mag = ScentShapingMagnitude × ScentMagnitudeFor(a.Label)
//
// There is no static-repel slot — agents are dynamically repelled
// only by leaders/peers whose accumulated trust has gone negative.
//
// Side effect: if any sensed cell carries trustee scent,
// JourneyTrusteeContactTicks is bumped by 1 (one tick of contact,
// regardless of how many sensed cells the trustee covers).
//
// Factored out of MoveAgents so tests can isolate the scent
// contribution from other PendingBonus channels.
func (w *World) ApplyScentShaping(a *Agent) float64 {
	if !IsScentFollower(a.Label) {
		return 0
	}
	sensed := w.ScentSensedCells(a)
	maxAttract := 0.0
	maxRepel := 0.0
	contact := false
	for p := range sensed {
		owner := w.ScentOwner[p.Y][p.X]
		if owner == 0 {
			continue
		}
		freshness := w.ScentFreshness(p.X, p.Y)
		if freshness <= 0 {
			continue
		}
		switch {
		case a.CurrentTrustee != 0 && owner == a.CurrentTrustee:
			contact = true
			if freshness > maxAttract {
				maxAttract = freshness
			}
		case a.TrustScores != nil && a.TrustScores[owner] < 0:
			if freshness > maxRepel {
				maxRepel = freshness
			}
		}
	}
	if contact {
		a.JourneyTrusteeContactTicks++
	}
	if maxAttract == 0 && maxRepel == 0 {
		return 0
	}
	mag := ScentShapingMagnitude * ScentMagnitudeFor(a.Label)
	return mag * (maxAttract - maxRepel)
}

// PickTrustee selects a.CurrentTrustee for the journey that's about
// to start, based on the agent's lifetime run count (Stats.Starts —
// incremented in RespawnAgents BEFORE this call fires):
//
//  1. runs ≤ ScentRunsForTrustWeighting           → uniform pick from leaders
//  2. runs ≤ ScentRunsForPeerExpansion            → softmax over TrustScores
//     restricted to leaders
//  3. runs > ScentRunsForPeerExpansion             → 50/50:
//     heads — softmax over leaders
//     tails — softmax over peers (followers minus self)
//
// `w` is used to filter candidates down to currently-ALIVE,
// non-disabled agents — a dead leader has no scent to follow.
// `rng` drives the random/softmax selection.
//
// No-op (CurrentTrustee = 0) when:
//   - the agent is not a scent follower (it's a leader), OR
//   - no alive candidate is available in the eligible pool — the
//     strategy then falls back to its non-follower algorithm
//     (outward bias / DQN / POMCP / QMDP base behavior).
func (a *Agent) PickTrustee(w *World, rng *rand.Rand) {
	if !IsScentFollower(a.Label) {
		a.CurrentTrustee = 0
		return
	}
	if a.TrustScores == nil {
		a.TrustScores = map[rune]float64{}
	}
	leaders := aliveLabels(w, ScentLeaderLabels)
	peers := aliveLabels(w, ScentPeerLabels(a.Label))
	runs := a.Stats.Starts
	switch {
	case runs <= ScentRunsForTrustWeighting:
		if len(leaders) == 0 {
			a.CurrentTrustee = 0
			return
		}
		a.CurrentTrustee = leaders[rng.Intn(len(leaders))]
	case runs <= ScentRunsForPeerExpansion:
		a.CurrentTrustee = softmaxPickLabel(rng, leaders, a.TrustScores)
	default:
		// 50/50 between leader pool and peer pool, with graceful
		// fallback when one side is empty.
		if rng.Float64() < 0.5 && len(leaders) > 0 {
			a.CurrentTrustee = softmaxPickLabel(rng, leaders, a.TrustScores)
		} else if len(peers) > 0 {
			a.CurrentTrustee = softmaxPickLabel(rng, peers, a.TrustScores)
		} else if len(leaders) > 0 {
			a.CurrentTrustee = softmaxPickLabel(rng, leaders, a.TrustScores)
		} else {
			a.CurrentTrustee = 0
		}
	}
}

// PickStrategy selects a.CurrentStrategy for the upcoming journey
// from the world's StrategyLetters pool:
//
//	50% softmax over a.StrategyTrustScores  → bias toward proven winners
//	50% uniform random                      → keep exploring new options
//
// Called from RespawnAgents AFTER Stats.Starts++ and PickTrustee.
// `letters` is the iteration order of available strategies (e.g.
// strategy.StrategyLetters); passed by the world's wiring so the
// world package needn't import strategy/.
func (a *Agent) PickStrategy(letters []rune, rng *rand.Rand) {
	if len(letters) == 0 {
		a.CurrentStrategy = 0
		return
	}
	if a.StrategyTrustScores == nil {
		a.StrategyTrustScores = map[rune]float64{}
	}
	if rng.Float64() < 0.5 {
		a.CurrentStrategy = softmaxPickLabel(rng, letters, a.StrategyTrustScores)
		return
	}
	a.CurrentStrategy = letters[rng.Intn(len(letters))]
}

// StrategyLettersForWorld returns the runtime list of strategy
// letters the world should expose to PickStrategy. Plumbed from the
// strategy package's StrategyLetters constant via Config wiring; if
// no letters are configured, returns nil and PickStrategy is a
// no-op (agents keep whatever CurrentStrategy they were created
// with, or fall back to the legacy a.Strategy field).
func (w *World) StrategyLettersForWorld() []rune {
	return w.strategyLetters
}

// StrategyDescription returns a human-readable (≤64 char) name for
// `letter`. Returns "" when no description lookup is configured —
// the UI then renders just the letter without a tail string.
func (w *World) StrategyDescription(letter rune) string {
	if w.strategyDescriptionForLetter == nil {
		return ""
	}
	return w.strategyDescriptionForLetter(letter)
}

// aliveLabels returns the subset of `pool` whose agents are
// currently alive and enabled. Used by PickTrustee to constrain
// the candidate pool — a dead or disabled leader can't be followed
// (their trail decays naturally; the agent should consider a
// different trustee or fall back).
func aliveLabels(w *World, pool []rune) []rune {
	out := make([]rune, 0, len(pool))
	for _, l := range pool {
		a := w.AgentByLabel(l)
		if a != nil && a.Alive && !a.Disabled {
			out = append(out, l)
		}
	}
	return out
}

// softmaxPickLabel samples one label from `candidates` weighted by
// exp(trustScores[label]). All-zero scores yield a uniform pick.
// Empty candidate slice returns 0.
func softmaxPickLabel(rng *rand.Rand, candidates []rune, trustScores map[rune]float64) rune {
	if len(candidates) == 0 {
		return 0
	}
	maxTrust := math.Inf(-1)
	for _, c := range candidates {
		if v := trustScores[c]; v > maxTrust {
			maxTrust = v
		}
	}
	if math.IsInf(maxTrust, -1) {
		maxTrust = 0
	}
	weights := make([]float64, len(candidates))
	total := 0.0
	for i, c := range candidates {
		weights[i] = math.Exp(trustScores[c] - maxTrust)
		total += weights[i]
	}
	if total <= 0 {
		return candidates[rng.Intn(len(candidates))]
	}
	r := rng.Float64() * total
	acc := 0.0
	for i, w := range weights {
		acc += w
		if r <= acc {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

// Trust update constants: a journey's outcome rewards (or penalizes)
// trust in the agent's CurrentTrustee.
//
//	Goal reached                  → +TrustGoalBonus
//	  ... and journey ≤ TTL       → +TrustWithinTTLBonus
//	Goal NOT reached (any death)  → −TrustFailurePenalty
//
// MinTrusteeContactTicks gates BOTH the reward and the penalty:
// if the agent never sustained contact with the trustee's scent for
// at least this many ticks during the journey, the outcome carries
// no information about the trustee (the agent "lost the scent")
// and endJourney is a no-op.
const (
	TrustGoalBonus         = 1.0
	TrustWithinTTLBonus    = 2.0
	TrustFailurePenalty    = 1.0
	MinTrusteeContactTicks = 5
)

// Strategy-trust constants: per-journey update for the algorithm
// the agent used.
//
//	Reach goal                       → +StrategyGoalBonus
//	... and faster than prior best   → +StrategyImproveBonus
//	... and slower than prior best   → +StrategyGoalBonus only
//	Did not reach goal               → −StrategyFailurePenalty
const (
	StrategyGoalBonus      = 1.0
	StrategyImproveBonus   = 2.0
	StrategyFailurePenalty = 1.0
)

// endJourney runs the per-life trust update when a journey ends —
// either by goal-reach (`success=true`) or by death of any cause
// (`success=false`). Reads w.Stats.OptimalDistance to compare the
// agent's TicksAlive against the TTL budget.
//
// No-op for:
//   - non-follower labels (agents 1, 2, 3)
//   - no CurrentTrustee (e.g. died before PickTrustee fired)
//   - JourneyTrusteeContactTicks < MinTrusteeContactTicks — the
//     agent "lost the scent" / never actually followed the trustee,
//     so the outcome doesn't reflect on them either way (this
//     avoids unfairly penalizing a trustee whose trail just never
//     came near this agent on this journey).
func (w *World) endJourney(a *Agent, success bool) {
	w.updateStrategyTrust(a, success)
	if !IsScentFollower(a.Label) {
		return
	}
	if a.TrustScores == nil {
		a.TrustScores = map[rune]float64{}
	}
	// Opportunistic-following credit: on a successful run, every
	// OTHER-AGENT label whose scent this agent followed at least
	// once during the journey gets a TrustGoalBonus (plus the
	// within-TTL bonus if applicable). This is independent of the
	// CurrentTrustee contact gate — opportunistic followings count
	// even when no formal trustee was set.
	if success {
		ttlBudget := a.OptimalDistance
		if ttlBudget <= 0 {
			ttlBudget = w.Stats.OptimalDistance
		}
		optimalTTL := TTLMultiplier * ttlBudget
		withinTTL := optimalTTL > 0 && a.TicksAlive > 0 && a.TicksAlive <= optimalTTL
		for owner := range a.OpportunisticFollowed {
			if owner == a.CurrentTrustee {
				continue // trustee gets its own credit below
			}
			a.TrustScores[owner] += TrustGoalBonus
			if withinTTL {
				a.TrustScores[owner] += TrustWithinTTLBonus
			}
		}
	}
	// CurrentTrustee credit: gated on the existing contact threshold
	// — the trustee isn't blamed (or credited) when the agent never
	// actually sniffed them during the journey.
	if a.CurrentTrustee == 0 {
		return
	}
	if a.JourneyTrusteeContactTicks < MinTrusteeContactTicks {
		return
	}
	if !success {
		a.TrustScores[a.CurrentTrustee] -= TrustFailurePenalty
		return
	}
	a.TrustScores[a.CurrentTrustee] += TrustGoalBonus
	ttlBudget := a.OptimalDistance
	if ttlBudget <= 0 {
		ttlBudget = w.Stats.OptimalDistance
	}
	optimalTTL := TTLMultiplier * ttlBudget
	if optimalTTL > 0 && a.TicksAlive > 0 && a.TicksAlive <= optimalTTL {
		a.TrustScores[a.CurrentTrustee] += TrustWithinTTLBonus
	}
}

// StrategyPerfCounts tabulates per-strategy run-end outcomes for
// the TUI's Strategy Performance table. Resets to nil on each new
// world (i.e., per-map, not lifetime).
//
//	TTLExpiry: runs that ended in a TTL-expiry death.
//	NoFollow:  runs that ended (any cause) with no CurrentTrustee.
//	Following: runs that ended (any cause) WITH a CurrentTrustee.
//
// NoFollow + Following == total runs counted for that strategy.
// TTLExpiry is a subset; it's tallied independently regardless of
// follow state.
type StrategyPerfCounts struct {
	// Started: total runs launched on this strategy — bumped once
	// per journey at RespawnAgents (after quorum / singleton
	// enforcement has settled the assignment). The TUI's #Runs
	// column reads this directly so deaths-by-any-cause and
	// goal-reaches are all included in the total.
	Started   int
	TTLExpiry int
	NoFollow  int
	Following int
}

// recordStrategyDeath bumps Strategy Performance counters when an
// agent's journey ends in death. Only Die.TTL fires (and only for
// reason == "ttl") — Win.NoFollow / Win.Following are reserved for
// successful goal-reaches.
func (w *World) recordStrategyDeath(a *Agent, ttlExpiry bool) {
	if a.CurrentStrategy == 0 || !ttlExpiry {
		return
	}
	c := w.ensureStrategyPerf(a.CurrentStrategy)
	c.TTLExpiry++
}

// recordStrategyGoal bumps Win.NoFollow or Win.Following depending
// on the agent's trustee status at goal-reach. Never touches
// Die.TTL.
func (w *World) recordStrategyGoal(a *Agent) {
	if a.CurrentStrategy == 0 {
		return
	}
	c := w.ensureStrategyPerf(a.CurrentStrategy)
	if a.CurrentTrustee != 0 {
		c.Following++
	} else {
		c.NoFollow++
	}
}

// ensureStrategyPerf returns the counter struct for `letter`,
// allocating it (and the parent map) on demand.
func (w *World) ensureStrategyPerf(letter rune) *StrategyPerfCounts {
	if w.StrategyPerf == nil {
		w.StrategyPerf = map[rune]*StrategyPerfCounts{}
	}
	c, ok := w.StrategyPerf[letter]
	if !ok {
		c = &StrategyPerfCounts{}
		w.StrategyPerf[letter] = c
	}
	return c
}

// updateStrategyTrust mutates StrategyTrustScores for the algorithm
// the agent just used (a.CurrentStrategy). Reward structure:
//
//	success && (no prior best || TicksAlive < prior best)
//	    → +StrategyGoalBonus + StrategyImproveBonus
//	    + record new best in StrategyBestSolveTime
//
//	success && TicksAlive ≥ prior best
//	    → +StrategyGoalBonus
//
//	!success
//	    → −StrategyFailurePenalty
//
// No-op when a.CurrentStrategy == 0 (e.g. agent died before
// PickStrategy fired, or world has no letter dispatch configured).
// optimizeKnownPath recomputes a.KnownShortestPath via BFS through
// a.KnownCells, from w.Maze.EntrancePos to w.Maze.GoalPos. Called
// from CheckGoal on every goal-reach: the agent has just proved
// (entrance → goal) is connected within its perceived terrain, so
// BFS over that subgraph yields the shortest path the agent could
// have legitimately taken. Each successive call uses a non-smaller
// KnownCells set, so the cached path monotonically improves (or
// stays equal) until it equals the true shortest path.
//
// Strict-PO safe: only cells in KnownCells are considered.
//
// Swarm extension: when `a` is using SwarmStrategyLetter, the
// optimizer first unions the KnownCells of every alive swarm peer
// into a's view (so BFS runs over collective perception), then
// broadcasts the resulting path back to every alive swarm peer's
// KnownShortestPath. One swarm member's win lifts the whole hive.
func (w *World) optimizeKnownPath(a *Agent) {
	if IsSwarmStrategy(a.CurrentStrategy) {
		if a.KnownCells == nil {
			a.KnownCells = map[Pos]bool{}
		}
		for _, peer := range w.Agents {
			// Pool only with same-strategy swarm peers so an S hive and
			// an X hive don't cross-contaminate paths.
			if peer == a || !peer.Alive || peer.CurrentStrategy != a.CurrentStrategy {
				continue
			}
			for p := range peer.KnownCells {
				a.KnownCells[p] = true
			}
		}
	}
	from := w.Maze.EntrancePos
	to := w.Maze.GoalPos
	if a.KnownCells == nil || !a.KnownCells[from] || !a.KnownCells[to] {
		return
	}
	type node struct {
		pos  Pos
		prev int
	}
	nodes := []node{{from, -1}}
	seen := map[Pos]int{from: 0}
	for head := 0; head < len(nodes); head++ {
		cur := nodes[head]
		if cur.pos == to {
			path := make([]Pos, 0, head+1)
			for i := head; i != -1; i = nodes[i].prev {
				path = append(path, nodes[i].pos)
			}
			// Reverse so path[0] is entrance, path[len-1] is goal.
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			a.KnownShortestPath = path
			// Swarm broadcast: copy the freshly-optimized path
			// into every alive swarm peer so they can replay it
			// without having to reach the goal themselves first.
			// Path is cloned so peer mutation (e.g., partial
			// replay) doesn't bleed back. Same-strategy gating keeps
			// S and X hives separate.
			if IsSwarmStrategy(a.CurrentStrategy) {
				for _, peer := range w.Agents {
					if peer == a || !peer.Alive || peer.CurrentStrategy != a.CurrentStrategy {
						continue
					}
					peer.KnownShortestPath = append([]Pos(nil), path...)
				}
			}
			return
		}
		for _, d := range Cardinals {
			np := Pos{X: cur.pos.X + d.X, Y: cur.pos.Y + d.Y}
			if !InBounds(np.X, np.Y) || !a.KnownCells[np] {
				continue
			}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if _, ok := seen[np]; ok {
				continue
			}
			seen[np] = len(nodes)
			nodes = append(nodes, node{np, head})
		}
	}
}

// CachedStepFor returns the next cell along a.KnownShortestPath
// after a.Pos, or (a.Pos, false) when no usable cached step exists:
//
//   - path is shorter than 2 cells (no cache yet, or just goal)
//   - a.Pos isn't on the cached path (the agent has drifted off
//     and needs native re-planning)
//   - the next cell is now unwalkable or hazardous (terrain
//     changed since the path was cached — wumpus moved in, fire
//     pit spawned, etc.)
//
// PO strategies should consult this BEFORE running their own
// planner so previously-proven-optimal routes get replayed.
func (w *World) CachedStepFor(a *Agent) (Pos, bool) {
	if len(a.KnownShortestPath) < 2 {
		return a.Pos, false
	}
	for i, p := range a.KnownShortestPath {
		if p != a.Pos {
			continue
		}
		if i+1 >= len(a.KnownShortestPath) {
			return a.Pos, false
		}
		next := a.KnownShortestPath[i+1]
		if !w.Maze.IsWalkable(next) || w.IsHazard(next) {
			return a.Pos, false
		}
		return next, true
	}
	return a.Pos, false
}

func (w *World) updateStrategyTrust(a *Agent, success bool) {
	if a.CurrentStrategy == 0 {
		return
	}
	if a.StrategyTrustScores == nil {
		a.StrategyTrustScores = map[rune]float64{}
	}
	if !success {
		a.StrategyTrustScores[a.CurrentStrategy] -= StrategyFailurePenalty
		return
	}
	if a.StrategyBestSolveTime == nil {
		a.StrategyBestSolveTime = map[rune]int{}
	}
	prior, hadPrior := a.StrategyBestSolveTime[a.CurrentStrategy]
	a.StrategyTrustScores[a.CurrentStrategy] += StrategyGoalBonus
	if !hadPrior || a.TicksAlive < prior {
		a.StrategyTrustScores[a.CurrentStrategy] += StrategyImproveBonus
		a.StrategyBestSolveTime[a.CurrentStrategy] = a.TicksAlive
	}
}

// ScentMaxAge bounds how many cycles a deposited scent remains
// perceptible. ScentFreshness decays linearly from 1.0 at deposit
// time to 0.0 at this age. 1000 cycles ≈ 100 seconds of game time
// at the 100ms tick rate — long enough for followers to pick up a
// trail from across the maze, but still short enough that very
// old paths don't permanently bias routing.
const ScentMaxAge = 1000

// ScentFreshness returns the local scent intensity at (x, y) in
// [0.0, 1.0]. 1.0 means a fresh deposit this very cycle; the value
// decays linearly to 0.0 over ScentMaxAge cycles. Returns 0 for
// cells that have never been scented or where the deposit has
// fully aged out.
func (w *World) ScentFreshness(x, y int) float64 {
	if !InBounds(x, y) {
		return 0
	}
	deposited := w.ScentCycle[y][x]
	if deposited <= 0 {
		return 0
	}
	age := w.Cycle - deposited
	if age >= ScentMaxAge {
		return 0
	}
	if age < 0 {
		age = 0
	}
	return 1.0 - float64(age)/float64(ScentMaxAge)
}

// MarkAgentSensed extends a.KnownCells with every cell the agent
// can perceive this tick. Wall-respecting Moore-connected
// (8-direction) BFS out to a.SightRadius steps (default
// DefaultSightRadius = 10), plus a wall-adjacency rule: when the
// agent perceives a path cell, it also perceives that cell's 8
// Moore neighbors. This lets the agent see whether perceived
// path cells are at dead-ends (1 walkable neighbor), corners
// (perpendicular walkable neighbors), or junctions (≥ 3 walkable
// neighbors).
//
// The adjacency rule is implemented as a 1-step "lookahead": every
// path cell dequeued at the boundary (depth == radius) still marks
// its Moore neighbors as known, just doesn't enqueue them. So
// perception effectively extends 1 cell past the BFS radius, but
// only as a marking pass — no propagation through walls beyond
// the boundary.
//
// Walls themselves block propagation: a wall cell enters KnownCells
// but the BFS doesn't extend past it, so the agent never sees cells
// hidden behind a wall.
//
// Called by MoveAgents (post-move) and RespawnAgents (post-spawn).
func (w *World) MarkAgentSensed(a *Agent) {
	if a.KnownCells == nil {
		a.KnownCells = map[Pos]bool{}
	}
	radius := a.SightRadius
	if radius < 1 {
		radius = DefaultSightRadius
	}
	a.KnownCells[a.Pos] = true
	type node struct {
		p     Pos
		depth int
	}
	visited := map[Pos]bool{a.Pos: true}
	queue := []node{{a.Pos, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		// Boundary handling: when cur is a path cell at the
		// perception edge, still mark its Moore neighbors so the
		// agent can see local shape. The neighbors aren't
		// enqueued — perception doesn't propagate further.
		atBoundary := cur.depth >= radius
		for _, d := range Cardinals {
			np := Pos{X: cur.p.X + d.X, Y: cur.p.Y + d.Y}
			if !InBounds(np.X, np.Y) {
				continue
			}
			a.KnownCells[np] = true
			if atBoundary {
				continue
			}
			if visited[np] {
				continue
			}
			visited[np] = true
			// Walls block further BFS expansion.
			if w.Maze.Cells[np.Y][np.X] == CellWall {
				continue
			}
			queue = append(queue, node{np, cur.depth + 1})
		}
	}
}

// HeatAt is the canonical sensor read for fire-pit proximity. Returns
// false whenever FirePitsDisabled is set, so agent perception, belief
// updates, and DQN features all see a "clean" board when fire pits
// are off — not just the TUI overlay.
func (w *World) HeatAt(x, y int) bool {
	if w.FirePitsDisabled {
		return false
	}
	if !InBounds(x, y) {
		return false
	}
	return w.Heat[y][x]
}

// StenchAt is the canonical sensor read for wumpus proximity. Returns
// false whenever WumpusDisabled is set. RecomputeStench already clears
// the underlying grid when wumpus are off, but this guard also covers
// the case where the grid hasn't been recomputed yet.
func (w *World) StenchAt(x, y int) bool {
	if w.WumpusDisabled {
		return false
	}
	if !InBounds(x, y) {
		return false
	}
	return w.Stench[y][x]
}

// IsHazard: omniscient hazard check used by BFS/DFS strategies.
// Respects the WumpusDisabled / FirePitsDisabled toggles — when a
// hazard type is disabled it's not reported as a hazard.
func (w *World) IsHazard(p Pos) bool {
	if !InBounds(p.X, p.Y) {
		return true
	}
	if !w.FirePitsDisabled && w.Maze.Cells[p.Y][p.X] == CellFirePit {
		return true
	}
	if !w.WumpusDisabled {
		if wm := w.WumpusAt[p.Y][p.X]; wm != nil && wm.Alive {
			return true
		}
	}
	return false
}

// AgentByLabel returns the agent matching the given letter or nil.
func (w *World) AgentByLabel(label rune) *Agent {
	for _, a := range w.Agents {
		if a.Label == label {
			return a
		}
	}
	return nil
}

// Cardinals: 4 cardinal directions.
// Cardinals enumerates every movement / sensing direction. Despite
// the historic name, this is now the Moore neighborhood (8 dirs):
// 4 cardinals followed by 4 diagonals. Movement and pathfinding
// treat the 4 cardinals as cost CardinalStepCost (10) and the 4
// diagonals as cost DiagonalStepCost (14 ≈ 10·√2) — Dijkstra is
// the pathfinder of record. Diagonal moves additionally honor the
// corner-clipping rule (see CanMoveTo / IsCornerClipped) so an
// agent can't squeeze through a one-cell wall gap.
var Cardinals = []Pos{
	{0, -1},           // N
	{0, 1},            // S
	{-1, 0},           // W
	{1, 0},            // E
	{-1, -1}, {1, -1}, // NW, NE
	{-1, 1}, {1, 1}, // SW, SE
}

// CardinalCount is the number of strict-cardinal (4-conn) entries
// at the head of Cardinals. Code that needs strict cardinal-only
// neighbors (e.g., recursive-backtracker maze carving with 2-step
// jumps) can iterate Cardinals[:CardinalCount].
const CardinalCount = 4

// StepCost returns the path-cost weight for moving by direction d:
// 10 for a cardinal step (axis-aligned), 14 for a diagonal step
// (≈ 10·√2). All Dijkstra-based pathfinding uses these weights.
const (
	CardinalStepCost = 10
	DiagonalStepCost = 14
)

// StepCost reports the Dijkstra-weight for a single move whose
// displacement is d (must be a unit-1 cardinal or diagonal offset).
func StepCost(d Pos) int {
	if d.X != 0 && d.Y != 0 {
		return DiagonalStepCost
	}
	return CardinalStepCost
}

// IsDiagonal reports whether a direction offset is one of the 4
// diagonal Moore offsets (both |dx|=1 and |dy|=1).
func IsDiagonal(d Pos) bool {
	return d.X != 0 && d.Y != 0
}

// IsCornerClipped reports whether moving from `from` to `to` would
// squeeze through a wall corner. Only meaningful for diagonal
// steps: a NE move from (x,y) to (x+1,y-1) is corner-clipped iff
// either (x+1,y) or (x,y-1) is unwalkable. Walls are solid: the
// agent can't dart between them through a one-cell diagonal gap.
func (m *Maze) IsCornerClipped(from, to Pos) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y
	if dx == 0 || dy == 0 {
		return false
	}
	// Two orthogonal cells the diagonal sweeps between.
	side1 := Pos{X: from.X + dx, Y: from.Y}
	side2 := Pos{X: from.X, Y: from.Y + dy}
	return !m.IsWalkable(side1) || !m.IsWalkable(side2)
}

// InBounds reports whether (x, y) is inside the board.
func InBounds(x, y int) bool {
	return x >= 0 && x < BoardWidth && y >= 0 && y < BoardHeight
}

// AbsInt returns |x|.
func AbsInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
