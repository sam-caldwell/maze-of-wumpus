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
	"math/rand"
)

// RespawnTicks: 1 second at 100ms/tick.
const RespawnTicks = 10

// TTLMultiplier: an agent dies if its current-attempt ActualDistance
// exceeds TTLMultiplier × OptimalDistance.
const TTLMultiplier = 5

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

// Score is the per-agent figure of merit: alignment with the shortest
// path, penalizing deviation.
//
//	score = (OnPathSteps - OffPathSteps) / OptimalDistance
//
// 1.0  = flawless solve along the chosen shortest path.
// 0.0  = equal time spent on and off the path (or no movement yet).
// <0   = mostly off-path — the deviation penalty.
//
// Score resets on respawn (because OnPathSteps / OffPathSteps reset).
// The sticky career best lives in BestAlignment, surfaced separately.
func (s AgentStats) Score(optimal int) float64 {
	if optimal <= 0 {
		return 0
	}
	return float64(s.OnPathSteps-s.OffPathSteps) / float64(optimal)
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
	QL         *QLearning
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
}

// Wumpus: adversarial automaton.
type Wumpus struct {
	ID              int
	Pos             Pos
	Alive           bool
	Strategy        WumpusStrategy
	QL              *QLearning
	DQN             *DQN
	CyclesSinceKill int
	VengeanceCycles int
}

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

	ScentOwner [BoardHeight][BoardWidth]rune

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
	Rng               *rand.Rand
	wumpusStrategyFn  func(*rand.Rand) WumpusStrategy // factory for new wumpus
	vengeanceStrategy WumpusStrategy
}

// MinAcceptablePaths: a generated maze must have at least this many
// distinct shortest paths from entrance to goal.
const MinAcceptablePaths = 3

// Config selects construction-time options for NewWorldWithConfig.
//
// StrategyFor: returns the Strategy for an agent given its label. If
// nil, agents are constructed with nil Strategy and MoveAgents falls
// back to FallbackMove.
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
	WumpusStrategy    func(*rand.Rand) WumpusStrategy
	VengeanceStrategy WumpusStrategy
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
		Seed:              cfg.Seed,
		Rng:               rand.New(rand.NewSource(cfg.Seed)),
		wumpusStrategyFn:  cfg.WumpusStrategy,
		vengeanceStrategy: cfg.VengeanceStrategy,
		// Hazard / TTL toggles default to DISABLED so a freshly-
		// constructed world is friendly to RL convergence. Operators
		// can re-enable each one at runtime via the 'w' / 'f' / 't'
		// keys, or by setting the corresponding *Disabled field
		// directly before stepping.
		WumpusDisabled:    true,
		FirePitsDisabled:  true,
		WaterPitsDisabled: true,
		TTLDisabled:       true,
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
		wm := &Wumpus{ID: w.nextWumpusID, Pos: p, Alive: true, Strategy: w.newWumpusStrategy()}
		w.nextWumpusID++
		w.Wumpus = append(w.Wumpus, wm)
		w.WumpusAt[p.Y][p.X] = wm
	}

	stratFor := cfg.StrategyFor
	if stratFor == nil {
		stratFor = func(rune) Strategy { return nil }
	}
	w.Agents = []*Agent{
		newAgent(&w.nextAgentID, '1', stratFor('1'), NewAgentBeliefs(), 1),
		newAgent(&w.nextAgentID, '2', stratFor('2'), nil, 4),
		newAgent(&w.nextAgentID, '3', stratFor('3'), nil, 7),
		newAgent(&w.nextAgentID, '4', stratFor('4'), nil, 10),
		newAgent(&w.nextAgentID, '5', stratFor('5'), nil, 13),
		newAgent(&w.nextAgentID, '6', stratFor('6'), NewAgentBeliefs(), 16),
		newAgent(&w.nextAgentID, '7', stratFor('7'), NewAgentBeliefs(), 19),
	}
	// Agent 1 is enabled by default; agents 2..7 stay disabled until
	// the user toggles them via the '2'..'7' keys.
	w.Agents[0].Disabled = false
	w.Agents[3].QL = NewQLearning()
	w.Agents[4].DQN = NewDQN(w.Rng)

	w.Stats.OptimalDistance = w.ShortestPathLength(w.Maze.EntrancePos, w.Maze.GoalPos)
	w.Stats.ShortestPaths = w.CountShortestPaths(w.Maze.EntrancePos, w.Maze.GoalPos, MaxShortestPathsCount)
	w.ShortestPathCells = w.ShortestPathSet(w.Maze.EntrancePos, w.Maze.GoalPos)
	w.computeDistFromStart()
	// Permanently strip any entity whose toggle is currently disabled.
	// With the default config (everything disabled) this leaves a
	// hazard-free board; tests that need entities re-spawn them via
	// EnableHazards.
	w.ApplyToggles()
	return w
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
			if w.DistFromStart[np.Y][np.X] != -1 {
				continue
			}
			w.DistFromStart[np.Y][np.X] = d + 1
			queue = append(queue, np)
		}
	}
}

// newWumpusStrategy returns a Strategy for a freshly-spawned wumpus
// using the world's configured factory, or nil if no factory was set.
func (w *World) newWumpusStrategy() WumpusStrategy {
	if w.wumpusStrategyFn == nil {
		return nil
	}
	return w.wumpusStrategyFn(w.Rng)
}

// ShortestPathSet returns the set of cells on ONE chosen shortest
// path from `from` to `to`. Used by the TUI 's' overlay.
func (w *World) ShortestPathSet(from, to Pos) map[Pos]bool {
	set := map[Pos]bool{}
	type node struct {
		Pos
		parent int
	}
	nodes := []node{{from, -1}}
	visited := map[Pos]int{from: 0}
	for head := 0; head < len(nodes); head++ {
		cur := nodes[head]
		if cur.Pos == to {
			for i := head; i != -1; i = nodes[i].parent {
				set[nodes[i].Pos] = true
			}
			return set
		}
		for _, d := range Cardinals {
			np := Pos{cur.X + d.X, cur.Y + d.Y}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if _, seen := visited[np]; seen {
				continue
			}
			visited[np] = len(nodes)
			nodes = append(nodes, node{np, head})
		}
	}
	return set
}

// CountShortestPaths returns the number of distinct shortest-length
// paths from `from` to `to`, saturated at `cap`.
func (w *World) CountShortestPaths(from, to Pos, cap int) int {
	dist := map[Pos]int{from: 0}
	paths := map[Pos]int{from: 1}
	queue := []Pos{from}
	clamp := func(v int) int {
		if v > cap {
			return cap
		}
		return v
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range Cardinals {
			np := Pos{cur.X + d.X, cur.Y + d.Y}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if _, seen := dist[np]; !seen {
				dist[np] = dist[cur] + 1
				paths[np] = clamp(paths[cur])
				queue = append(queue, np)
			} else if dist[np] == dist[cur]+1 {
				paths[np] = clamp(paths[np] + paths[cur])
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

// ShortestPathLength does a wall-only BFS from `from` to `to`. Returns
// 0 if unreachable.
func (w *World) ShortestPathLength(from, to Pos) int {
	if from == to {
		return 0
	}
	type node struct {
		Pos
		dist int
	}
	visited := map[Pos]bool{from: true}
	queue := []node{{from, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range Cardinals {
			np := Pos{cur.X + d.X, cur.Y + d.Y}
			if !w.Maze.IsWalkable(np) {
				continue
			}
			if visited[np] {
				continue
			}
			if np == to {
				return cur.dist + 1
			}
			visited[np] = true
			queue = append(queue, node{np, cur.dist + 1})
		}
	}
	return 0
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
			} else {
				w.KillAgent(a, "wumpus")
				wm.CyclesSinceKill = 0
				break
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
		if a.Strategy != nil {
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
		a.Pos = target
		w.AgentAt[target.Y][target.X] = a
		a.Stats.ActualDistance++
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
		if !w.TTLDisabled && w.Stats.OptimalDistance > 0 && a.Stats.ActualDistance > TTLMultiplier*w.Stats.OptimalDistance {
			w.KillAgent(a, "ttl")
		}
	}
}

// CanMoveTo reports whether agent `a` could legally move to `target`
// this tick. Same-cell is treated as NOT a valid move.
func (w *World) CanMoveTo(a *Agent, target Pos) bool {
	if target == a.Pos {
		return false
	}
	if !InBounds(target.X, target.Y) {
		return false
	}
	if !w.Maze.IsWalkable(target) {
		return false
	}
	if other := w.AgentAt[target.Y][target.X]; other != nil && other != a && other.Alive {
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

// RespawnAgents counts respawn timers down; at 0 it places the agent
// at the entrance (or holds if another agent is there).
func (w *World) RespawnAgents() {
	for _, a := range w.Agents {
		if a.Disabled || a.Alive {
			continue
		}
		if a.RespawnIn > 0 {
			a.RespawnIn--
		}
		if a.RespawnIn != 0 {
			continue
		}
		entrance := w.Maze.EntrancePos
		if other := w.AgentAt[entrance.Y][entrance.X]; other != nil && other.Alive {
			continue
		}
		a.Alive = true
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
		w.AgentAt[entrance.Y][entrance.X] = a
		a.RespawnIn = -1
	}
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
	for _, a := range w.Agents {
		if !a.Alive || a.Pos != w.Maze.GoalPos {
			continue
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
			wm := &Wumpus{ID: w.nextWumpusID, Pos: p, Alive: true, Strategy: w.newWumpusStrategy()}
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
		nw := &Wumpus{ID: w.nextWumpusID, Pos: p, Alive: true, Strategy: w.newWumpusStrategy()}
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
			ID:       w.nextWumpusID,
			Pos:      p,
			Alive:    true,
			Strategy: w.newWumpusStrategy(),
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
var Cardinals = []Pos{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

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
