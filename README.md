# maze-of-wumpus

<p align="center">
  <img src="docs/img/logo.png" alt="Maze of Wumpus logo" width="320">
</p>

A terminal-UI maze simulation written in Go that pits **four labeled agents**
— each running a **fixed decision algorithm** — against a procedurally
generated **1024 × 1024** maze under strict partial observability. The
roster is an omniscient A\* benchmark plus three partially-observable
**swarm** planners that fork-and-disperse clones to explore: a Bayesian
goal-location inferrer, a flat-Monte-Carlo POMCP planner, and a QMDP-style
expected-utility planner.

The only death rule is **time-to-live (TTL)**, scaled per agent to the
true shortest-path length of its start. Movement is 8-direction
Moore-connected with corner-clipping; pathfinding is A\* (octile
heuristic) and weighted Dijkstra (cardinal = 10, diagonal = 14);
perception is a wall-respecting BFS out to a 10-cell sight radius. The
right side of the maze renders an agent → algorithm trust heatmap, a
per-strategy run-outcome table, a strategy legend, and a rolling Events
log with context-aware snark.

The board is runtime-sizable (`--size`) and the simulation runs on either
a single-threaded tick loop or a **worker-per-agent parallel engine**
(`--parallel`). Code lives under `src/` with a thin
`cmd/maze-of-wumpus` entry; all tests, `go vet`, and `gofmt` pass.

> **Note on the name.** "Wumpus" is historical. The current build has **no
> wumpus, fire pits, or water pits** — those hazards were removed. The
> sole hazard is the TTL clock.

---

## Contents

- [Setup](#setup)
- [Quick start](#quick-start)
- [Project layout](#project-layout)
- [The world](#the-world)
- [Movement, perception, and pathfinding](#movement-perception-and-pathfinding)
- [Agents and strategies](#agents-and-strategies)
- [Swarm fork-and-disperse](#swarm-fork-and-disperse)
- [TTL (the only death rule)](#ttl-the-only-death-rule)
- [Post-win path optimizer](#post-win-path-optimizer)
- [Graph pruning](#graph-pruning)
- [Partial-observability guard](#partial-observability-guard)
- [Cycle phase order](#cycle-phase-order)
- [Serial vs. parallel engines](#serial-vs-parallel-engines)
- [UI annex](#ui-annex)
- [Controls](#controls)
- [Command-line modes](#command-line-modes)
- [Logs](#logs)
- [Make targets](#make-targets)
- [Determinism](#determinism)
- [Constants reference](#constants-reference)
- [License](#license)

---

## Setup

The project is pure Go with two direct dependencies
([Bubble Tea](https://github.com/charmbracelet/bubbletea) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss)); everything else
is transitive. It builds and runs on macOS, Linux, and Windows.

### 1. Install Go

This module targets the Go toolchain pinned in `go.mod` (currently
**Go 1.26.3**). Install that version or newer.

**macOS (Homebrew):**

```bash
brew install go
```

**macOS / Linux (official tarball):**

```bash
# adjust version/arch as needed — see https://go.dev/dl/
curl -LO https://go.dev/dl/go1.26.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.3.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin    # add to ~/.zshrc or ~/.bashrc
```

**Linux (Debian/Ubuntu):**

```bash
sudo apt-get update && sudo apt-get install -y golang-go
# If the distro package is older than 1.26.3, prefer the official tarball above.
```

**Windows:** download the MSI installer from <https://go.dev/dl/> and run it.

Verify the install:

```bash
go version    # should print go1.26.3 (or newer)
```

### 2. Fetch dependencies

From the repository root:

```bash
go mod download    # pull the module dependencies into the local cache
```

`go build` / `go test` also fetch on demand, so this step is optional but
makes the first build offline-safe. The dependency set is locked by
`go.mod` / `go.sum`.

### 3. Build and run

```bash
make build                 # produces ./build/maze-of-wumpus
./build/maze-of-wumpus      # launch the TUI (serial engine)
```

A working terminal (TTY) with 256-color support is required for the TUI.
For a non-interactive smoke test that needs no TTY, use headless mode:

```bash
./build/maze-of-wumpus --headless --steps=50 --seed=42
```

### 4. (Optional) Run the test suite

```bash
make all       # go vet + full test suite + build
make test      # tests only
```

The full `world`-package suite runs a number of long simulations and can
take several minutes; `go vet` and the `cmd`/`strategy`/`tui` suites are
fast.

---

## Quick start

```bash
make build                # produces ./build/maze-of-wumpus
./build/maze-of-wumpus    # launch the TUI (serial tick loop):
                          #   - all 4 agents enabled
                          #   - TTL death rule ON
                          #   - default 1024 × 1024 maze
```

Parallel engine (one worker goroutine per agent):

```bash
./build/maze-of-wumpus --parallel       # or: make run
```

Headless mode for scripted runs and CI (deterministic):

```bash
./build/maze-of-wumpus --headless --steps=200 --seed=42
```

Custom board size (columns × rows):

```bash
./build/maze-of-wumpus --size=256,256
```

---

## Project layout

```
cmd/maze-of-wumpus/
    main.go              # CLI entry: runApp / runProgram (TUI serial) /
                         #   runProgramParallel / runHeadless /
                         #   runHeadlessLoop / runBenchmark / writeHeadlessState
    announce_darwin.go   # macOS announce hook (currently a no-op stub)
    announce_other.go    # no-op stub for every other OS
    main_test.go         # unit tests (cmd-layer)
    e2e_test.go          # subprocess e2e tests: build the binary, run it,
                         #   assert stdout schema, exit codes, determinism
src/
    world/               # World, Agent, Maze; tick loop; A*/Dijkstra;
                         #   per-agent TTL; swarm clones + graph pruning;
                         #   goal-belief; post-win optimizer; solve_log /
                         #   stats_log writers; parallel engine
    strategy/            # The four strategies (R/S/U/V) + factory + the
                         #   universal swarm wrapper, frontier detection,
                         #   goal-belief frontier scoring, branch animation
    tui/                 # Bubble Tea models (async serial + parallel),
                         #   maze viewport, trust matrix, strategy-perf
                         #   table, events panel
docs/img/                # README logo
Makefile                 # build / lint / test / coverage / clean / run / bench
```

`world.Config` injects strategy callbacks (`StrategyFor`,
`StrategyForLetter`, `StrategyLetters`, `StrategyLetterForLabel`,
`StrategyDescriptionForLetter`) at construction so `src/world` stays free
of strategy-package imports. The world package owns a few layering
constants — `SwarmStrategyLetter = 'S'`, `BenchmarkStrategyLetter = 'R'` —
to make a few strategy-aware decisions without circular imports. The
`cmd` layer wires `StrategyLetterForLabel = strategy.LetterForLabel`, so
each agent runs a **fixed** strategy for the entire game.

---

## The world

- **Board:** `BoardWidth` × `BoardHeight`, defaulting to **1024 × 1024**.
  These are package **variables** (not constants) set once at startup by
  `SetBoardSize` (wired to `--size`), because the per-cell grids are
  slices allocated at construction time.
- **Canonical entrance:** `(1, 0)` — top edge, non-corner.
- **Goal:** a walkable cell at Manhattan distance ≥
  `MinGoalDistanceCells = (BoardWidth + BoardHeight) / 2` from the
  entrance (1024 at the default size). Falls back to the farthest
  reachable cell if no candidate qualifies.
- **Cell types:** `CellWall`, `CellPath`, `CellEntrance`, `CellGoal`.
  (There are no fire-pit or water-pit cell types — hazards were removed.)
- **Maze generation:** a single connected component, two variants:
  - **Recursive-backtracker maze** (`1 − OpenFieldProbability = 80%`):
    classic carve, then **4–7** irregular flood rooms (target area in
    `[MinRoomArea = 4, MaxRoomArea = 2500]`, bounding box ≤
    `MaxRoomDim = 50`).
  - **Open-field** (`OpenFieldProbability = 20%`): every interior cell
    walkable, perimeter walled.
- **One RNG:** `World.Rng *rand.Rand` is the single deterministic source
  for the serial engine. Same seed → identical serial run (including which
  snark templates fire in the Events panel).

---

## Movement, perception, and pathfinding

**Connectivity.** Movement uses the 8-direction Moore neighborhood:
`Cardinals` is `[N, S, W, E, NW, NE, SW, SE]`. The first
`CardinalCount = 4` entries are the strict cardinals. Diagonal moves
honor a **corner-clipping rule** (`IsCornerClipped`): a diagonal step
from `cur → np` is rejected when *either* of the two orthogonal cells the
diagonal sweeps between is a wall — the agent can't squeeze through a
one-cell wall gap. `CanMoveTo` enforces this for every committed move.

**Pathfinding.** Two routers, both corner-clip-aware:

- **A\*** (`World.AStarPath`) with an octile heuristic
  (`h = 10·max(dx,dy) + 4·min(dx,dy)`). Used by strategy R and to compute
  each agent's omniscient `OptimalDistance` / `ShortestPath` at
  construction.
- **Weighted Dijkstra** (`World.DijkstraPath`) used for partially-
  observable planning over `KnownCells` and the post-win optimizer.

Both use `CardinalStepCost = 10` and `DiagonalStepCost = 14 ≈ 10·√2`, a
`container/heap` priority queue (O(log V) extract-min), and linear
append+reverse path reconstruction.

**Sight perception.** `MarkAgentSensed(a)` runs a wall-respecting,
Moore-connected BFS from `a.Pos` out to `a.SightRadius` (default
`DefaultSightRadius = 10`). Reached cells enter `a.KnownCells`. Walls
also enter `KnownCells` (the agent learns where walls are) but block
propagation. A wall-adjacency rule fires at the boundary: a path cell
dequeued at depth = radius marks its 8 Moore neighbors (no further
propagation), letting the agent recognize dead-ends, corners, and
junctions from a distance.

**Smell perception.** `ScentSensedCells(a)` is a separate Moore-BFS out
to `a.SmellRadius` (default `DefaultSmellRadius = 2`). Scent trails are
still *deposited and rendered* as a visual aid, but **no strategy
consumes scent for decisions any more** (scent-following was removed), so
smell is effectively cosmetic.

**Strict partial observability.** Every PO strategy (S, U, V) only reads
`w.Maze.GoalPos` once the agent has perceived it
(`a.KnownCells[w.Maze.GoalPos]`). An agent that hasn't seen the goal
never routes to it. Only the omniscient benchmark (R) reads `GoalPos`
unconditionally. A compile-time guard
([see below](#partial-observability-guard)) keeps strategies from peeking
at the answer-key path.

---

## Agents and strategies

Four agents live on the board, labels `1..4`. Each runs a **fixed**
strategy for the entire game (`LetterForLabel`):

| Label | Letter | Name           | Description                                           | PO?  | Swarm? |
|:-----:|:------:|:---------------|:------------------------------------------------------|:----:|:------:|
| 1     | **R**  | astar (bfs)    | Omniscient A\* shortest-path benchmark (singleton)    | no   | no     |
| 2     | **S**  | swarm-bayesian | Bayesian goal-location inference; forks & disperses    | yes  | yes    |
| 3     | **U**  | pomcp-swarm    | Flat Monte-Carlo (POMCP-lite) lookahead clones         | yes  | yes    |
| 4     | **V**  | qmdp-swarm     | QMDP-style one-step expected-utility clones            | yes  | yes    |

**Strategy R (A\* benchmark)** is omniscient — `BFSStrategy` reads
`w.Maze.GoalPos` and routes via `BFSToward → World.AStarPath` over the
full walkable graph, caching the plan and re-planning only when it
empties. It is the lone non-swarm strategy and serves as the lower-bound
benchmark. (The name "BFS" is historical; it routes via A\*.)

**Strategies S, U, V** are all partially-observable and all run through
the **universal swarm wrapper** (`SwarmStrategy`), which dispatches to the
letter's own solo planner (`planFor`) for exploitation and forks clones
for exploration:

- **S — Bayesian (`BayesianStrategy`).** Maintains a Bayesian posterior
  over the goal's location (`goal_belief.go`): a prior ∝ Manhattan
  distance from the entrance (hard-zero below `MinGoalDistanceCells`),
  with every perceived non-goal cell subtracted out. When the goal isn't
  yet perceived, it scores frontier (perception-boundary) cells by a
  min-max blend of **goal-pull** (toward `ExpectedGoalLocation`, the
  posterior centroid) and **swarm dispersion** (Chebyshev distance from
  swarm-mates), then walks to the best one. Once the goal is perceived,
  it routes straight to it. Weights: `goalPullWeight = 1.0`,
  `dispersionWeight = 1.0`; frontier choice set capped at
  `frontierCandidateCap = 24`.
- **U — POMCP (`POMCPStrategy`).** Runs `PomcpRollouts = 12` random-walk
  rollouts per candidate move, each up to `PomcpRolloutDepth = 100` steps,
  weighting transitions outward by `(1 + DistFromStart)`. Terminal reward
  `pomcpGoalReward = 10000` fires only on a simulated step onto the
  *perceived* goal; otherwise a depth-limit bonus
  (`pomcpExploreBonus = 10` per `DistFromStart` unit) rewards reaching
  far. Discount `pomcpGamma = 0.99`; per-step cost `pomcpStepCost = 1.0`.
  Strictly outward-biased — it never reads `GoalPos` to score progress.
- **V — QMDP (`QMDPStrategy`).** A one-step argmax over cardinal
  neighbors: `score = qmdpExploreWeight × DistFromStart(next) −
  qmdpRepelWeight × swarmDispersionPenalty(next)`
  (`qmdpExploreWeight = 1.0`, `qmdpRepelWeight = 8.0`). Fast — no
  rollouts. Like U, it uses only the outward `DistFromStart` signal under
  strict PO.

> **Removed strategies.** The earlier roster had twelve agents and seven
> strategies including DFS, a DQN reinforcement learner, and a
> scent-following hive learner, plus a per-journey strategy-rotation and
> trust/quorum system. **DFS, DQN, scent-following, and per-journey
> rotation are all gone.** Some dormant stubs remain for compilation/test
> compatibility, but they do not run.

---

## Swarm fork-and-disperse

A PO strategy agent is a swarm **leader**: a single agent that spawns and
commands up to `SwarmClonesPerLeader = 10` clones (so a swarm is up to 11
entities — one leader + ten clones). Each tick, `SwarmStrategy`:

1. **Prunes dead clones**, freeing slots.
2. **Merges swarm knowledge** (`mergeSwarmKnowledge`): unions
   `KnownCells` and `Beliefs.Observed` across all alive members sharing
   the agent's `SwarmGroupID` and strategy (strict-PO preserved — only
   cells some member actually perceived).
3. **Moves every member** — each clone and the leader runs the letter's
   solo planner over the shared pruned view.
4. **Forks** untaken branches at junctions (`collectForks`) and
   distinct unexplored frontier sectors (`swarmRegionForks`), filling
   open clone slots toward direction-diverse regions. Per-letter spawn
   policies (`spawnPolicyFor`) decide which branches are worth a clone:
   Bayesian forks all survivable branches; POMCP/QMDP fork branches
   within `spawnMarginFrac = 0.25` of the best score.

Clones carry their own `Pos`, `Plan`, `Dist`, and `KnownShortestPath`.
A clone is retired if its individual `Dist` exceeds the swarm TTL limit or
if it **thrashes** (confined to ≤ `swarmThrashDistinctMax = 2` distinct
cells over the trailing `swarmTrailWindow = 8` steps).

**Clone reaches the goal → leader wins.** In `CheckGoal`, a clone on the
goal snaps the leader onto the goal cell (and visually collapses all alive
clones there) so the existing per-agent goal handler records exactly one
swarm win.

**Leader dies → clone is promoted (body-swap).** In `KillAgent`, if a
swarm leader hits its TTL but a clone survives, the clone is promoted into
the leader slot: the leader adopts the clone's position, plan, cached
path, and `Dist`. The journey continues — **no death is recorded** (only
the strategy-level `Die.TTL` tally bumps).

---

## TTL (the only death rule)

The single death rule is **time-to-live**: an agent dies when
`ActualDistance > TTLCeiling(a)`, checked per step in `MoveAgents` (and at
parallel barriers). TTL can be toggled off entirely (`t` key / `TTLDisabled`).

`TTLCeiling(a)`:

1. **After the agent's first solve** → `a.Stats.BestSolveDistance` (the
   agent must match or beat its own best route).
2. **Before any solve** → `TTLMultiplier × a.OptimalDistance +
   a.Stats.MaxReach`, where `TTLMultiplier = 3` and `MaxReach` is the
   deepest entrance-distance the agent has reached on this map (persists
   across deaths). The **commute credit** (`MaxReach`) funds re-traversing
   already-mapped corridors back out to the frontier, so a deep frontier
   doesn't burn the whole exploration budget on the walk back.

**Solve distance never beats the optimum.** When a win is recorded,
`CheckGoal` measures the real entrance→goal route through pooled known
terrain and floors the recorded `BestSolveDistance` at `OptimalDistance`.
This guards a subtle bug: a promoted-clone win carries the clone's tiny
local `ActualDistance`, and recording that raw value would push
`BestSolveDistance` (and thus the TTL ceiling) below anything a fresh life
walking from the entrance could achieve — strangling every future attempt.
(Regression test: `ttl_solve_test.go`.)

**LearnedTTL.** Each agent keeps a belief about its step budget: a TTL
death sets `LearnedTTL = ActualDistance − 1` (the killer fires the first
step past threshold, pinning it to ±1); surviving past `LearnedTTL`
invalidates the stale estimate. It is grafted across reseeds as a prior.

---

## Post-win path optimizer

Every time an agent reaches the goal (`CheckGoal`), the world runs
`optimizeKnownPath`: a BFS over `a.KnownCells` from `a.EntrancePos` to
`w.Maze.GoalPos`, storing the shortest *legally walkable* route the agent
could have taken in `a.KnownShortestPath`. The BFS rejects corner-clipped
diagonals so the cached path replays cleanly under `CanMoveTo` — without
that, a cached diagonal squeeze would be rejected on replay, derailing the
agent off-path into a dead end under the tight post-solve TTL.
(Regression test: `optimize_path_test.go`.)

PO strategies consult this cache before re-planning via
`World.CachedStepFor(a)`: if `a.Pos` is on the path and the next cell is
walkable, that cached step is replayed.

**Swarm broadcast.** When the winner is a swarm member, the optimizer
first unions every alive same-strategy peer's `KnownCells` into the
winner's view, then deep-copies the resulting `KnownShortestPath` to every
alive peer — so one member's win lifts the whole hive.

---

## Graph pruning

To keep planners from wasting effort on perceived dead-ends, the world
prunes the known graph:

- **Solo (`RecomputeAgentPrunedViewIfStale`)** — **Phase 1 (leaf-trim)**
  only: iteratively peel walkable cells with ≤ 1 alive walkable neighbor
  that aren't anchored (entrance, goal, perception-boundary cells, the
  agent's position). Cached on `a.PrunedKnownCells`, invalidated by a
  `KnownCells` size delta (monotone within a life).
- **Swarm (`RecomputeSwarmGraphIfStale`, per `SwarmGroupID`)** — both
  phases. **Phase 2 (articulation/loop pruning)** keeps a cell `c` only if
  `dist(entrance, c) + dist(c, A) == dist(entrance, A)` for some anchor
  `A` (entrance, perceived goal, frontier cells, every alive member's
  position), dropping closed loops that survived phase 1. Phase 2 is safe
  for swarms because the anchor set is dense; it's too aggressive for a
  solo agent.

---

## Partial-observability guard

`strategy/po_guard_test.go` is a compile-time AST scan over every non-test
file in the `strategy` package. It **fails the build** if any strategy
reads the answer-key fields `ShortestPath` / `ShortestPathCells` (the true
entrance→goal route). Only `KnownShortestPath` — built solely from
perceived cells — is allowed. This statically enforces that PO strategies
can't leak omniscient knowledge.

---

## Cycle phase order

Each `World.Step()` (serial engine) runs:

1. `Cycle++`
2. `tickAgentClocks` — bump `TicksAlive` on alive agents.
3. `MoveAgents` — for each enabled agent: run its strategy → next cell;
   update `KnownCells` (sight) and deposit scent; apply
   exploration/dead-end reward shaping; per-step TTL check
   (`ActualDistance > TTLCeiling`) → death; `LearnedTTL` invalidation.
4. `CheckGoal` — clone→leader snap, record the win, post-win optimizer
   (incl. swarm broadcast), solve-log append, respawn timer.
5. `RespawnAgents` — place returning lives back at their entrance with a
   fresh per-life state; clear stale swarm state.

---

## Serial vs. parallel engines

**Serial (`tui.NewAsyncModel`, headless, bench baseline).** One goroutine
owns the world and calls `World.Step()` once per tick (100 ms in the TUI).
Fully deterministic for a fixed seed.

**Parallel (`--parallel`, `tui.NewParallelModel`).** A
`world.ParallelRunner` runs **one worker goroutine per agent** plus a
coordinator. Workers step under a shared read lock; the coordinator runs
bookkeeping (goal checks, respawns, scent-buffer flush, `AgentAt` rebuild)
under a write lock that acts as a periodic barrier
(`parallelBarrierEvery ≈ 20 ms`). Each agent group has a private RNG, and
scent writes buffer per-group and flush at the barrier (eventually
consistent). Because scheduling is asynchronous, **the parallel engine is
not cross-run deterministic** — it trades reproducibility for throughput.
`make bench` runs both engines for `--duration` and prints per-agent
throughput.

Both engines reseed via the same `reseedWorldPreservingLearning` path:
`LearnedTTL` and learned priors graft into the fresh world while
per-map state (`KnownCells`, `KnownShortestPath`, the Events log) resets.

---

## UI annex

The right side of the maze renders an annex, top to bottom:

```
Agent-Algorithm Trust
  R S U V
1 ...        ← 4 agent rows × 4 strategy columns, heat-colored 0..15
2 ...
3 ...
4 ...

Strategy Performance
    Die.TTL  Wins  #Runs    ← per-column-normalized heat backgrounds
 R       0      2      2
 S     ...    ...    ...
 U     ...    ...    ...
 V     ...    ...    ...

Agent Strategies
R  Omniscient A* shortest-path benchmark (singleton)
S  Bayesian swarm: infers goal location, forks & disperses
U  POMCP swarm: Monte-Carlo lookahead clones
V  QMDP swarm: expected-utility clones explore

Events
                                          ← rolling log, newest at bottom
Agent 2 found the gold. Show-off
```

- **Agent-Algorithm Trust** is a 4×4 heatmap (agents × strategy letters)
  on a 16-step blue→red palette. (The old **Agent-Agent** trust matrix
  was removed — agents no longer follow one another.)
- **Strategy Performance** uses a black-to-red palette with per-column
  normalization so each column's leader is obvious at a glance.
- **Events** is a rolling log (`EventBufferSize = 100`) showing the
  bottom `EventsVisible = 5` lines, color-coded: red deaths, green goal
  reaches, yellow startup/system. Snark is drawn from pop-culture and
  literary pools, seeded from the same RNG (deterministic in serial mode).

**Per-agent status row** (one per agent, below the maze) shows: label,
alive/dead, `str:` current strategy letter, `s:` lifetime starts, `f:`
deaths, `g:` goals, `dist:` this-life distance (color-graded against the
per-agent TTL ceiling), `rt:` steps/sec (parallel engine), `TTL:` the
ceiling, `best:` best solve distance/time, the solve-score aggregates, and
cumulative `score:`.

Agent identity colors: **1** bright blue (39), **2** cyan (51), **3**
magenta (213), **4** green (46). Swarm clones render as a `*` on the
leader's color; branch-search ghosts render as a red `◌`.

---

## Controls

```
q / Ctrl-C   quit
space        pause / resume
r            reseed (preserves learned TTL / priors)
s            toggle shortest-path overlay
t            toggle the TTL death rule
1 2 3 4      toggle the matching agent on/off
↑ ↓ ← →      scroll the maze viewport
shift+↑/↓, PgUp/PgDn   page the viewport
```

---

## Command-line modes

```
maze-of-wumpus [flags]

flags:
  --seed N          rng seed (0 = current time, the default)
  --headless        run without TUI; one key=value line per cycle to stdout
  --steps N         headless: number of ticks to run (default 200)
  --parallel        TUI driven by the worker-per-agent parallel engine
  --bench           headless: run serial + parallel engines for --duration,
                    print per-agent throughput
  --duration D      bench: wall-clock duration per pass (default 5s)
  --size COLS,ROWS  maze size (default 1024,1024); applied before any world
                    is constructed
```

**Headless output** is one space-separated `key=value` record per cycle:

```
cycle=N optimal=N paths=N \
  <label>_alive=B <label>_deaths=N <label>_goals=N <label>_dist=N <label>_score=F \
  ... (for each of agents 1..4) \
  game_over=B
```

The schema is locked by `cmd/maze-of-wumpus/e2e_test.go`, which compiles
the binary into a temp directory and exercises the schema, exit codes, and
same-seed determinism via subprocess invocations.

---

## Logs

When run interactively (or after the build dirs are created), the
simulation writes:

- `build/solves/agent<label>.log` — NDJSON, one record per goal reach:
  `{run, distance, cycles, score, world_cycle, world_seed}`. Append-only;
  persists across reseeds. Best-effort — errors are silently dropped so the
  simulation never stalls on disk problems.
- `build/stats/<unix_ns>.log` — pretty-printed JSON snapshot written when a
  maze is "solved" (`MazeSolvedAgentCount = 3` agents reach
  `MazeSolvedGoals = 999` goals). One file per solved map: `written_at`,
  `seed`, `cycle`, `optimal_distance`, `shortest_paths`, and per-agent
  `AgentStats`.

Both the headless loop and the TUI write the stats log at the
maze-solved boundary, immediately before grafting learning state into a
fresh world.

---

## Make targets

```
make build      # produce ./build/maze-of-wumpus
make lint       # go vet -v ./...
make test       # go test -failfast -v ./...
make coverage   # cross-package coverage; per-function summary (tail 20)
make clean      # rm -rf build && mkdir build
make run        # build && launch the TUI with the parallel engine
make bench      # headless serial-vs-parallel throughput benchmark, 5s
make all        # lint + test + build (the default target)
```

---

## Determinism

Given a fixed `--seed`, the **serial / headless** engine is fully
reproducible: maze generation, goal placement, agent entry assignment,
swarm forking, and Events-log content all consume the single `World.Rng`
in deterministic order. Same seed = byte-identical headless output (locked
by the e2e suite).

The **parallel** engine (`--parallel`) is **not** cross-run deterministic:
each agent group steps on its own goroutine with a private RNG, and the
barrier interleaving depends on the scheduler. It exists for throughput,
not reproducibility — use the serial/headless path for deterministic runs.

---

## Constants reference

Selected constants, grouped by concern. Values current as of this build.

### Board and maze

| Constant                     | Value      | Meaning                                                  |
|------------------------------|-----------:|----------------------------------------------------------|
| `BoardWidth` / `BoardHeight` | 1024 / 1024| Default board dimensions (variables; set via `--size`)   |
| `MinGoalDistanceCells`       | (W+H)/2    | Min Manhattan entrance→goal distance (1024 at default)   |
| `OpenFieldProbability`       | 0.20       | Probability a map is the open-field variant              |
| `MinRoomArea`                | 4          | Minimum irregular-room cell count                        |
| `MaxRoomArea`                | 2500       | Maximum irregular-room cell count                        |
| `MaxRoomDim`                 | 50         | Maximum bounding-box side length for a room              |

### Movement and perception

| Constant                     | Value | Meaning                                           |
|------------------------------|------:|---------------------------------------------------|
| `CardinalStepCost`           | 10    | A\*/Dijkstra cost of an axis-aligned step         |
| `DiagonalStepCost`           | 14    | Cost of a diagonal step (≈ 10·√2)                 |
| `CardinalCount`              | 4     | Strict-cardinal entries at head of `Cardinals`    |
| `DefaultSightRadius`         | 10    | BFS depth of `MarkAgentSensed`                    |
| `DefaultSmellRadius`         | 2     | BFS depth of `ScentSensedCells` (cosmetic only)   |

### Time, TTL, respawn

| Constant              | Value | Meaning                                              |
|-----------------------|------:|------------------------------------------------------|
| `RespawnTicks`        | 10    | Respawn delay (1 s at the 100 ms TUI tick)           |
| `TTLMultiplier`       | 3     | Pre-solve TTL = this × `OptimalDistance` + `MaxReach`|

### Swarm

| Constant                  | Value | Meaning                                          |
|---------------------------|------:|--------------------------------------------------|
| `SwarmClonesPerLeader`    | 10    | Max clones a swarm leader commands               |
| `swarmTrailWindow`        | 8     | Trailing window for clone thrash detection       |
| `swarmThrashDistinctMax`  | 2     | Distinct-cell ceiling before a clone is retired  |
| `spawnMarginFrac`         | 0.25  | Branch must be within this of best to fork (U/V) |
| `frontierCandidateCap`    | 24    | Frontier choice-set cap for Bayesian scoring     |

### POMCP and QMDP

| Constant            | Value     | Meaning                                       |
|---------------------|----------:|-----------------------------------------------|
| `PomcpRollouts`     | 12        | Rollouts per candidate action                 |
| `PomcpRolloutDepth` | 100       | Max steps per rollout                         |
| `pomcpGoalReward`   | 10000.0   | Terminal reward on a perceived-goal step      |
| `pomcpGamma`        | 0.99      | Per-step discount                             |
| `pomcpExploreBonus` | 10.0      | Per-`DistFromStart` unit at depth limit       |
| `qmdpExploreWeight` | 1.0       | QMDP outward-distance weight                  |
| `qmdpRepelWeight`   | 8.0       | QMDP swarm-dispersion repulsion weight        |

### Maze-solved + buffers

| Constant                 | Value | Meaning                                           |
|--------------------------|------:|---------------------------------------------------|
| `MazeSolvedGoals`        | 999   | Per-agent goals threshold for "solved"            |
| `MazeSolvedAgentCount`   | 3     | Number of agents at threshold for "solved"        |
| `EventBufferSize`        | 100   | Rolling event-log capacity                        |
| `EventsVisible`          | 5     | Lines rendered in the Events panel                |

---

## License

MIT — see [LICENSE](LICENSE). Copyright (c) 2026 Sam Caldwell.
</content>
</invoke>
