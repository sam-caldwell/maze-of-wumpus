# maze-of-wumpus

A terminal-UI maze game written in Go that pits **twelve labeled agents** running a **per-journey rotation of
seven decision algorithms** against a procedurally-generated 120 × 80 maze under strict partial observability.
Strategies include omniscient BFS, a swarm-of-Bayesians that shares perceived terrain, classic Wumpus-World
inductive + Bayesian reasoning, a scent-following hive learner, a deep Q-network, a flat-Monte-Carlo POMCP
planner, and a QMDP-style POMDP utility planner. Hazards (wumpus, fire pits, water pits) and the time-to-live
death rule can be toggled on and off live. The right side of the maze shows two trust heatmaps (agent → agent
and agent → algorithm), a per-strategy run-outcome table, and a scrolling Events log. Code lives under `src/`
with a thin `cmd/maze-of-wumpus` entry; all tests, vet, and gofmt pass.

---

## Contents

- [Quick start](#quick-start)
- [Project layout](#project-layout)
- [The world](#the-world)
- [Agents](#agents) — the 12 labeled actors (1..9, A..C)
- [Strategies](#strategies) — the 7 algorithms (R..X) any agent can pick per journey
- [Per-journey strategy selection](#per-journey-strategy-selection)
- [Scent / trust system](#scent--trust-system)
- [Post-win path optimizer](#post-win-path-optimizer)
- [Swarm graph pruning (strategy S)](#swarm-graph-pruning-strategy-s)
- [Wumpus](#wumpus)
- [Hazards and toggles](#hazards-and-toggles)
- [Cycle phase order](#cycle-phase-order)
- [UI annex](#ui-annex) — trust matrices, strategy tables, Events
- [Controls](#controls)
- [Command-line modes](#command-line-modes)
- [Logs](#logs)
- [Make targets](#make-targets)
- [Determinism](#determinism)

---

## Quick start

```bash
make build                # produces ./build/maze-of-wumpus
./build/maze-of-wumpus    # launches the TUI: all 12 agents enabled,
                          # hazards OFF by default, TTL ON
```

Headless mode for scripted runs and CI:

```bash
./build/maze-of-wumpus --headless --steps=200 --seed=42
```

---

## Project layout

```
cmd/maze-of-wumpus/
    main.go              # CLI entry, runApp / runHeadless wiring
    announce_darwin.go   # macOS-only 'say' call
    announce_other.go    # no-op stub for every other OS
    *_test.go            # cmd-level tests including e2e (subprocess) tests
src/
    world/               # World, Agent, Wumpus, Maze; tick loop; toggles;
                         #   trust + strategy state; events; swarm-graph
                         #   pruning; post-win path optimizer
    strategy/            # Seven strategies (R..X) + factory + helpers
    wumpus/              # Wumpus hunt strategies + crowd-sighting state
    tui/                 # Bubbletea Model, glyphs, trust matrix renderer
Makefile                 # build / lint / test / coverage / clean / run
```

`world.Config` injects strategy callbacks (`StrategyForLetter`,
`StrategyLetters`, `StrategyDescriptionForLetter`) at construction so
`src/world` stays free of strategy package imports. The world package
defines `SwarmStrategyLetter = 'S'` and `StrategyUsesScent(letter)` to
make a few strategy-aware decisions without circular imports.

---

## The world

- **Board:** 120 × 80 grid. Entrance at `(0, 0)`; goal at
  `(BoardWidth−2, BoardHeight−2)`.
- **Cells:** `CellWall`, `CellPath`, `CellEntrance`, `CellGoal`,
  `CellFirePit`, `CellWaterPit`.
- **World state:** `World.Cycle`, the agents, the wumpus list,
  derived grids (`Heat`, `Stench`, `ScentOwner`, `ScentCycle`,
  `AgentAt`, `WumpusAt`, `DistFromStart`), per-agent learning
  state, toggles, event log, swarm-graph cache, strategy
  performance counters.
- **One RNG:** `World.Rng *rand.Rand` is the single deterministic
  source. Same seed → identical run (including snark template
  picks for the Events panel, since `pickTemplate` uses it).

---

## Agents

Twelve agents live on the board. Labels 1..9 and A..C are
identities; the strategy each agent is running is decided **per
journey** (see [Per-journey strategy selection](#per-journey-strategy-selection))
so an agent's label is not its destiny.

| Label | Sensing radius | Notes                           |
|:-----:|:--------------:|---------------------------------|
| 1     | 1              |                                 |
| 2     | 1              |                                 |
| 3     | 1              | Follower-eligible (1)           |
| 4     | 1              | Follower-eligible               |
| 5     | 1              | Follower-eligible               |
| 6     | 1              | Follower-eligible               |
| 7     | 1              | Follower-eligible               |
| 8     | 2              | Far-sight                       |
| 9     | 2              | Far-sight, follower-eligible    |
| A     | 2              | Far-sight, follower-eligible    |
| B     | 2              | Far-sight, follower-eligible    |
| C     | 2              | Far-sight, follower-eligible    |

(1) "Follower-eligible" labels are 4, 5, 6, 7, 9, A, B, C. Only
these labels participate in the scent / trust system; labels 1, 2,
3, 8 act as "leaders" whose scent can be followed but who never
follow anyone else.

**Sensing radius** controls `MarkAgentSensed` — the BFS depth used
to grow `KnownCells` as the agent moves. Walls are perceived
(included in `KnownCells`) but block propagation past them.

**Scent perception range** matches sensing radius via a
Moore-connected BFS (8-neighbor instead of 4) — so radius 1 gives
the standard 3×3 box, radius 2 gives a 5×5 box (minus walls and
cells blocked by walls).

Every agent carries the **union of state slots** any strategy
might need: `Beliefs` (Bayesian PO state), `DQN` (1KB neural net
weights), `KnownCells`, `TrustScores` (trust in leaders), and
`StrategyTrustScores` (trust in algorithms). Cheap; lets every
agent run any strategy on any journey.

---

## Strategies

Seven distinct algorithms, each identified by a letter:

| Letter | Name              | Short description                                                     |
|:------:|-------------------|-----------------------------------------------------------------------|
| **R**  | bfs               | Omniscient breadth-first search to goal                               |
| **S**  | swarm-bayesian    | Bayesian PO with shared (swarm) KnownCells + Beliefs                  |
| **T**  | bayesian          | Inductive Bayesian reasoning, partial observability                   |
| **U**  | scent-follower    | Bayesian + scent: follow a chosen leader's trail                      |
| **V**  | dqn               | Deep Q-network with scent perception                                  |
| **W**  | pomcp             | Flat Monte-Carlo planner (POMCP-lite) with scent                      |
| **X**  | qmdp              | POMDP QMDP-style expected-utility planner with scent                  |

**Strategy R (BFS)** is omniscient — it reads `w.Maze.GoalPos` and
BFSes through walkable, non-hazard cells. Used as a benchmark.

**Strategy S (Swarm-Bayesian)** is the hive-mind variant of T. On
every tick, an agent on S merges its `KnownCells` and `Beliefs`
with every alive peer also on S (union of perceptions, max of
hazard probabilities — cautious bias). It then runs a graph-pruned
Bayesian planner over the unioned view. See [Swarm graph
pruning](#swarm-graph-pruning-strategy-s).

**Strategy T (Bayesian)** is the canonical Wumpus-World inductive
agent: maintains `Beliefs` with `Observed`, `SafeFromPit`,
`PitProb`, `WumpusProb`, plans via BFS through proven-safe cells,
falls back to "loose" cells (not-known-pit) when needed. Strict
PO: never routes through cells outside `KnownCells`. Does NOT
read scent.

**Strategy U (Scent-follower)** builds on T's Bayesian belief
layer but its action selection actively reads `ScentOwner` and
`ScentFreshness` at cardinal neighbors. Picks the neighbor that
carries its `CurrentTrustee`'s freshest scent.

**Strategy V (DQN)** is a small two-layer neural net (`DqnInput =
10`: 2 normalized position, 2 walkability, 2 hazard bits, 4
cardinal scent signed-freshness features). One-step TD with the
PendingBonus reward channel. Per-agent 5× scent magnitude boost
(`ScentMagnitudeFor`) because the DQN's other reward channels
otherwise drown out the scent gradient.

**Strategy W (POMCP-lite)** runs `PomcpRollouts = 12` random-walk
rollouts per candidate cardinal action, each up to
`PomcpRolloutDepth = 100` steps deep. Rollouts use cardinal
neighbors and weight transitions by safety × (1 + distance-from-start) × scent
factor. Strict PO: never reads `w.Maze.GoalPos` outside the
rollout's terminal-cell check.

**Strategy X (QMDP)** scores each cardinal action as
`safety × (qmdpExploreWeight × DistFromStart(next) + qmdpScentWeight × ScentSignedFreshness(next))`
and argmaxes. Strict PO. Fast (no rollouts).

Scent perception: **U, V, W, X** consult scent at decision time;
**R, S, T** do not — see [Scent-blind strategies skip the trustee
pick](#scent--trust-system).

### Post-win path consult (all PO strategies)

Before running its native planner each tick, every PO strategy
(T, U, V, W, X) calls `World.CachedStepFor(a)`. If the agent's
`KnownShortestPath` cache returns a non-`a.Pos` step that's still
walkable and non-hazardous, the strategy commits to it without
re-planning. Falls back to native planning when the next cached
cell is hazardous or the agent has drifted off the path. See
[Post-win path optimizer](#post-win-path-optimizer).

---

## Per-journey strategy selection

`RespawnAgents` runs every tick. For each agent that's coming
alive after a death or world boot:

1. **`Stats.Starts++`** — bumps the agent's lifetime run counter.
2. **`PickStrategy(letters, rng)`** — chooses `CurrentStrategy`
   for this life: 50% softmax over `StrategyTrustScores`, 50%
   uniform random. Early-life agents get more exploration; once
   trust accumulates the agent gravitates to its proven winners.
3. **Trustee gate** — if `StrategyUsesScent(CurrentStrategy)` is
   true AND the agent's label is in `ScentFollowerLabels`, run
   `PickTrustee(w, rng)`. Otherwise `CurrentTrustee = 0`.

A trustee is the leader (or peer, after enough runs) whose scent
the agent will try to follow this journey. See [Scent / trust
system](#scent--trust-system) for the trust update rules.

Strategy trust updates fire from `endJourney` (called by
`KillAgent` and `CheckGoal`):

- **Goal reach** → `+StrategyGoalBonus` (and `+StrategyImproveBonus`
  if the journey beat the agent's prior best `TicksAlive` for that
  strategy).
- **Death** (any cause) → `−StrategyFailurePenalty`.

The Agent-Algorithm Trust matrix in the UI renders these scores
on the same 0..15 heat scale as the per-agent trust matrix.

---

## Scent / trust system

A follower-eligible agent (label in `ScentFollowerLabels`) running
a scent-aware strategy (U/V/W/X) picks a `CurrentTrustee` per
journey, governed by `Stats.Starts`:

| Runs                                 | Trustee pool                                    | Pick rule                       |
|--------------------------------------|-------------------------------------------------|---------------------------------|
| ≤ `ScentRunsForTrustWeighting` (10)  | Leaders {1, 2, 3, 8}                            | uniform random                  |
| ≤ `ScentRunsForPeerExpansion` (20)   | Leaders {1, 2, 3, 8}                            | softmax over `TrustScores`      |
| > `ScentRunsForPeerExpansion`        | 50% leaders, 50% peers (other follower labels)  | softmax over `TrustScores`      |

Dead leaders / peers are filtered out automatically — `PickTrustee`
only considers alive, non-disabled candidates and clears
`CurrentTrustee` if the pool is empty.

### Scent perception

Each tick, `ApplyScentShaping(a)` (called from `MoveAgents`)
aggregates over the agent's `ScentSensedCells` — a Moore-BFS to
the agent's `SensingRadius`, walls blocking. The reward channel
emits:

```
+ScentShapingMagnitude × ScentMagnitudeFor(label) × max(trustee_freshness)
−ScentShapingMagnitude × ScentMagnitudeFor(label) × max(neg_trust_freshness)
```

Agent 5 (DQN) gets a 5× magnitude boost so the scent gradient
survives the network's other shaping signals. Agents on negative
trust scores act as dynamic repels — the "repelled by leaders
that failed me" rule.

### Trust updates

`endJourney(a, success)` updates `TrustScores[CurrentTrustee]`:

```
success && TicksAlive ≤ TTLMultiplier × OptimalDistance →
    +TrustGoalBonus + TrustWithinTTLBonus
success && TicksAlive > TTLMultiplier × OptimalDistance →
    +TrustGoalBonus
!success →
    −TrustFailurePenalty
```

Plus a **contact gate**: if the agent never sustained
`MinTrusteeContactTicks = 5` ticks on the trustee's scent during
the journey, the trust update is skipped entirely. The trustee
isn't blamed for a run where the agent never sensed them — the
"lost the scent" rule.

### Persistence across reseed

`TrustScores`, `StrategyTrustScores`, `Beliefs`, `DQN`, and
`LearnedTTL` graft across `reseedPreservingLearning` (TUI) and
`reseedHeadless` (cmd). `Stats.Starts`, `KnownCells`,
`KnownShortestPath`, and the Events log all reset per map.

### Learned TTL

Each agent maintains `LearnedTTL` — its belief about the
per-map step budget. Updated by two complementary signals:

- **Record on TTL death** (`reason == "ttl"`) — sets
  `LearnedTTL = ActualDistance − 1`. The killer fires the first
  step past threshold, so a single TTL death pins the value to
  ±1 step.
- **Invalidate on survival** — if the agent's `ActualDistance`
  exceeds its current `LearnedTTL` while still alive, the
  estimate is stale (TTL grew) and gets dropped. The next TTL
  death re-pins.

`LearnedTTL` grafts across reseed as a prior.

---

## Post-win path optimizer

Every time an agent reaches goal (`CheckGoal`), the world runs
**BFS over `a.KnownCells`** from entrance to goal and stores the
shortest path the agent could have legitimately taken in
`a.KnownShortestPath`. Strict-PO safe: only perceived cells are
considered.

PO strategies (T, U, V, W, X) consult this cache before running
their native planner via `World.CachedStepFor(a)`:

- If `a.Pos` is on the path AND the next cell is walkable + not
  hazardous → return next cached cell.
- Otherwise → return `(a.Pos, false)` and the caller falls
  through to its native planner.

Each call grows `KnownCells` (or keeps it equal), so the cached
path **monotonically improves**. Once an agent has perceived the
true shortest path, the cache equals it.

### Swarm broadcast

When an agent on **strategy S** reaches the goal, the optimizer
additionally:

1. Unions every alive S-peer's `KnownCells` into the
   goal-reacher's view before running BFS (so the path is built
   over collective perception).
2. Copies the resulting `KnownShortestPath` to every alive S-peer
   (deep clone — peers don't share the slice).

One swarm member's win lifts the whole hive.

---

## Swarm graph pruning (strategy S)

Strategy S maintains a **pruned routing graph** in addition to
its shared knowledge state. Recomputed lazily (dirty-check on
union size) by `World.RecomputeSwarmGraphIfStale`:

**Phase 1: leaf-trim.** Iteratively delete non-anchor cells with
≤1 walkable alive neighbor. Captures dead-end *chains* of any
length. Anchors immune to trimming: entrance, goal, frontier
cells (cardinal neighbor not yet in any swarm member's
`KnownCells`), and every alive swarm member's current cell.

**Phase 2: articulation / loop pruning.** BFS distances from
entrance and each remaining anchor identify cells on some
shortest path between them. A cell `c` survives iff
`dist(entrance, c) + dist(c, A) == dist(entrance, A)` for some
anchor `A`. Closed loops that survived phase 1 (cells have ≥2
neighbors) but aren't on any entrance↔anchor shortest path get
pruned here.

`SwarmBayesianStrategy` builds a pruned view of `a.KnownCells` by
intersecting with `SwarmAliveCell`, swaps it in for the call
duration via `defer`-restore, then runs `BayesianStrategy` on the
filtered graph. Dead-ends and useless loops become wall-equivalent
for the planner.

---

## Wumpus

Each wumpus has:

- **`Aggressiveness` ∈ [0, 15]**: 0 = lazy/opportunistic (random
  wander; only kills agents that walk adjacent on their own); 15
  = always commits to its hunt strategy. Set at spawn uniformly
  random.
- **`HuntMode`** (one of three, picked uniformly at spawn):

| Mode                   | Description                                                            |
|------------------------|------------------------------------------------------------------------|
| `WumpusHuntBayesian`   | Inductive Bayesian smell-tracking; aggressiveness gates per-tick commit |
| `WumpusHuntWander`     | Random walk lightly biased by agent scent (max 50% scent bias)         |
| `WumpusHuntCrowd`      | Swarm hunting — all crowd-hunt wumpus share sightings of agents within `WumpusDetectionRadius` (5) and BFS-route to the nearest one |

`HuntStrategy(w, wm)` dispatches on `wm.HuntMode`. `commitsToHunt`
is the per-tick aggressiveness gate.

Wumpus combat is opportunistic regardless of mode — any wumpus
adjacent to an agent at combat-resolution time kills that agent.

---

## Hazards and toggles

**Defaults:** wumpus / fire pits / water pits all disabled. TTL
**enabled** (default `TTLMultiplier = 5` × `OptimalDistance`).

**Runtime keys:**

| Key | Effect                                          |
|:---:|-------------------------------------------------|
| `w` | toggle wumpus (spawn / clear all)               |
| `f` | toggle BOTH fire pits and water pits together   |
| `t` | toggle TTL death rule                           |
| `1`–`9`, `a`–`c` | toggle the matching agent on/off       |
| `s` | overlay shortest path                           |
| `r` | reseed (preserves learning)                     |
| `q`, Ctrl-C | quit                                    |

When a toggle goes from OFF → ON, the entity is spawned fresh
(wumpus, fire pits, water pits). When ON → OFF, the entity is
completely removed and its derived grids cleared (Heat, Stench).

---

## Cycle phase order

Each `World.Step()` runs:

1. `tickAgentClocks` — bump `TicksAlive` and `LastVisited`.
2. `TickWumpusClocks` — vengeance + sighting-decay counters.
3. `MoveAgents` — strategy dispatch, movement, scent deposit,
   `KnownCells` update, `PendingBonus` shaping, dead-end
   penalty, TTL check, learn-by-dying invalidation.
4. `MoveWumpus` — strategy dispatch + vengeance override.
5. `ResolveCombat` — adjacency kills both ways.
6. `ResolvePitDeaths` — fire-pit kills; water collection.
7. `CheckGoal` — goal-reach → trust update, event, post-win
   optimizer, respawn timer set.
8. `SpawnReplacementWaterPit` — auto-spawn replacements.
9. `RecomputeStench` — wumpus-stench overlay.
10. `RespawnAgents` — strategy pick, trustee pick (if scent
    strategy), spawn at entrance.
11. `Cycle++`.

---

## UI annex

The right side of the maze (next to the first ~30 rows) shows a
scrollable annex with these sections, top to bottom:

```
Agent-Agent Trust
  1 2 3 4 5 6 7 8 9 A B C
1 · - - - - - - - - - - -
... (12 agent rows)
C - - - - - - - - - - - ·

█  0  █  8       ← heat legend (8 rows; 16-step palette 0..15)
...
█  7  █ 15

Agent-Algorithm Trust
  R S T U V W X
1 ...
... (12 agent rows)
C ...

Strategy Performance
    Die.TTL  Win.NoFollow  Win.Following
 R        0             0              0   ← each numeric cell has a
 S        3            12              5   ← background heat color
 T       10             1             87   ← normalized per column,
 U        ...           ...           ...   ← black → red
 V        ...
 W        ...
 X        ...

Agent Strategies
R  Omniscient breadth-first search to goal
S  Bayesian PO with shared (swarm) KnownCells + Beliefs
T  Inductive Bayesian reasoning, partial observability
U  Bayesian + scent: follow a chosen leader's trail
V  Deep Q-network with scent perception
W  Flat Monte-Carlo planner (POMCP-lite) with scent
X  POMDP QMDP-style expected-utility planner with scent

Wumpus Strategies
  3  Inductive Bayesian smell-tracking; aggressiveness gates commit
  5  Random walk lightly biased by agent scent
  2  Swarm hunting: shared sightings, BFS to nearest detected agent

Events
                                                      ← (5 lines, padded
                                                         when buffer
Wumpus had Agent 1 for lunch. Tasty                      shorter; newest
Agent 5 found the gold. Show-off                         at bottom)
```

**Agent-Agent Trust** and **Agent-Algorithm Trust** use the same
16-step heat palette (blue → green → yellow → red, indices 0..15).
**Strategy Performance** uses a *black-to-red* palette
(`strategyPerfHeatBG`, indices 0..15) on a per-column normalization
so the user can identify each column's leader at a glance.

**Events** is a rolling log capped at `EventBufferSize = 100`;
the bottom `EventsVisible = 5` lines render in the panel. Each
event has a semantic color: red for death, green for goal,
yellow for system messages. The first event in every fresh world
is a random pick from `startingMessages` (War Games, 2001, Star
Trek, Dual Core, Dickens, Orwell, plus tech-humor lines).
Subsequent deaths and goal-reaches pull from category-specific
snark pools (`deathByWumpus`, `deathByTTL`, `deathByFire`,
`deathByOther`, `goalReached`) drawing on Office Space, Silicon
Valley, Shakespeare, Solzhenitsyn, Camus, Kafka, Bradbury,
Hemingway, and more.

**Per-agent status row** (one per agent, below the maze):

```
 1 alive    str:R s:003 f:- ttl:---- d:000 k:000 g:000 dist:0017 best:0000/0000 t[min/avg/max/last]:0000/000.0/0000/0000 score:0.000
```

Columns: label, alive/dead, current strategy letter,
`Stats.Starts`, current trustee, learned TTL, deaths, wumpus
killed, goals reached, current-life distance, best
(distance/time), solve-time aggregates, cumulative score.

---

## Controls

```
q / Ctrl-C   quit
r            reseed (preserves Beliefs/QL/DQN/TrustScores/LearnedTTL)
s            toggle shortest-path overlay
w            toggle wumpus
f            toggle fire-pits + water-pits together
t            toggle TTL death rule
1..9, a..c   toggle agent on/off (case-insensitive for letters)
```

---

## Command-line modes

```
maze-of-wumpus [flags]

flags:
  --seed N          rng seed (0 = current time, the default)
  --headless        run without TUI; one line per cycle to stdout
  --steps N         headless: number of ticks to run (default 200)
```

Headless output: one space-separated `key=value` record per
cycle, with per-agent fields for each of the 12 labels. The exact
schema is locked by `cmd/maze-of-wumpus/e2e_test.go`.

---

## Logs

When run interactively (and configured), the simulation writes:

- `build/solves/agent<label>.log` — NDJSON, one record per goal
  reach: run number, distance traveled, ticks elapsed, score.
  Append-only, persists across reseeds.
- `build/stats/<unix_ns>.log` — JSON snapshot when a maze is
  "solved" (≥ `MazeSolvedAgentCount = 3` agents reach
  `MazeSolvedGoals = 999`). One file per solved map.

---

## Make targets

```
make build      # produce ./build/maze-of-wumpus
make lint       # go vet -v ./...
make test       # go test -v ./...
make coverage   # cross-package coverage; per-function summary
make clean      # rm -rf build && mkdir build
make run        # build && launch the TUI
make all        # lint + test + build
```

---

## Determinism

Given a fixed `--seed`, every aspect of the simulation is
reproducible: maze generation, agent spawn ordering, wumpus
placement, strategy and trustee picks, scent-template selections,
Events log content. The single `World.Rng` source is consumed by
every randomness site in deterministic order.

POMCP rollouts (strategy W) do create per-candidate
`*rand.Rand` instances internally, but each is seeded from
`World.Rng.Int63()` consumed in serial *before* the rollouts
launch, so the rollout RNG advances are reproducible too.
