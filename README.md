# maze-of-wumpus

A terminal-UI maze game written in Go that pits **seven AI agents** with seven fundamentally different decision
algorithms against a procedurally-generated 120×80 maze under **partial observability**. Agents include faithful
Wumpus-World inductive + Bayesian reasoning, BFS, DFS, tabular Q-learning, a deep Q-network, a QMDP-style POMDP
agent, and a flat-Monte-Carlo POMCP-lite planner. Hazards (wumpus, fire pits, water pits) and the time-to-live death
rule can be toggled on and off interactively. Per-agent learning state survives deaths AND maze reseeds, so you can
watch agents 4, 5, 6, and 7 measurably improve over many lives. Code is laid out as a small library under `src/`
with a thin `cmd/maze-of-wumpus` entry point and a 96–98% test coverage gate.

---

## Contents

- [Quick start](#quick-start)
- [Project layout](#project-layout)
- [The world](#the-world)
- [Partial observability — the design principle](#partial-observability--the-design-principle)
- [Maze generation](#maze-generation)
- [Hazards](#hazards)
  - [Wumpus](#wumpus)
  - [Fire pits](#fire-pits)
  - [Water pits](#water-pits)
  - [Heat / stench / scent overlays](#heat--stench--scent-overlays)
- [Wumpus hunting strategies](#wumpus-hunting-strategies)
- [Agents](#agents)
  - [Agent 1 — Wumpus-World (inductive + Bayesian)](#agent-1--wumpus-world-inductive--bayesian)
  - [Agent 2 — BFS](#agent-2--bfs)
  - [Agent 3 — DFS](#agent-3--dfs)
  - [Agent 4 — tabular Q-learning](#agent-4--tabular-q-learning)
  - [Agent 5 — Deep Q-Network](#agent-5--deep-q-network)
  - [Agent 6 — POMDP / QMDP](#agent-6--pomdp--qmdp)
  - [Agent 7 — POMCP-lite](#agent-7--pomcp-lite)
- [Reward shaping for D and E](#reward-shaping-for-d-and-e)
- [Cross-life and cross-maze knowledge persistence](#cross-life-and-cross-maze-knowledge-persistence)
- [Branch-decision animation (agents 2 and 3)](#branch-decision-animation-agents-2-and-3)
- [Combat](#combat)
- [Scoring](#scoring)
- [Cycle phase order](#cycle-phase-order)
- [Controls](#controls)
- [Toggles and entity lifecycle](#toggles-and-entity-lifecycle)
- [Command-line modes](#command-line-modes)
- [macOS startup announcement](#macos-startup-announcement)
- [Per-agent JSON logs](#per-agent-json-logs)
- [Tunable constants](#tunable-constants)
- [Make targets](#make-targets)
- [Test architecture](#test-architecture)
- [Determinism](#determinism)

---

## Quick start

```bash
make build                # produces ./build/maze-of-wumpus
./build/maze-of-wumpus    # launches the TUI with all hazards disabled
                          # and only agent 1 active
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
    world/               # World, Agent, Wumpus, Maze, learning data types,
                         #   Step loop, toggle setters
    strategy/            # Agent 1..7 strategies + branch-decision animation
                         #   + water-as-secondary-goal helpers
    wumpus/              # Five wumpus hunting strategies + PickStrategy
    tui/                 # Bubbletea Model, glyphs, color tiers, key handlers
    logging/             # Per-agent JSON Lines (NDJSON) log writer
Makefile                 # build / lint / test / coverage / clean / run
```

`world.Config` injects strategy factories at construction time, which keeps `src/world` strategy-agnostic and
breaks the obvious world↔strategy import cycle.

---

## The world

The board is a fixed **120 × 80** grid of cells. Each cell is one of `CellWall`, `CellPath`, `CellEntrance`,
`CellGoal`, `CellFirePit`, `CellWaterPit`. The entrance is always at `(0, 0)`; the goal cell is always at
`(BoardWidth-2, BoardHeight-2)` — i.e., the bottom-right corner.

`World` owns the simulation tick (`Step`), seven agents, the wumpus list, derived grids (`Heat`, `Stench`,
`ScentOwner`, `AgentAt`, `WumpusAt`, `DistFromStart`), persistent learning data on each agent, and the toggle
state (`WumpusDisabled`, `FirePitsDisabled`, `WaterPitsDisabled`, `TTLDisabled`).

`World.Rng` is a single deterministic `*rand.Rand` — every randomness in the simulation flows from this source so
runs are reproducible given a seed.

---

## Partial observability — the design principle

This project deliberately treats the maze as a **partially observable environment**. The world does NOT precompute
or expose distance-to-goal anywhere accessible to agents. Specifically:

- `World.DistFromStart` exists (for the `RealDistanceShaping` reward of agents 4 / 5) — it's start-relative, not
  goal-relative, and tells the agent only "how far you've gotten from where you started."
- There is intentionally **no `World.DistToGoal`** field. Agents that want goal distance (agents 6 and 7) compute
  it themselves via on-demand BFS in the strategy package.
- The DQN feature vector (`world.AgentDqnFeatures`) does NOT include goal-relative position. Slots 2 / 3 are local
  walkability flags rather than `(goal_x − x)` / `(goal_y − y)`.
- Sensor accessors `World.HeatAt(x, y)` and `World.StenchAt(x, y)` return `false` whenever the corresponding
  hazard family is toggled off, so agent beliefs never ingest phantom evidence from a disabled hazard.
- Wumpus `DqnFeatures` (predator/prey) DO include relative position to the nearest live agent — the wumpus knows
  about its prey; that's not a partial-observability violation.

Agents may still know the goal *cell's* coordinates (`w.Maze.GoalPos`) — that's the "target" identity. What they
don't get is a free distance scalar; if they want one they have to search for it themselves.

---

## Maze generation

`src/world/maze.go::GenerateMaze` pipeline:

1. Fill the entire grid with `CellWall`.
2. Recursive backtracker carves a perfect maze on even-coordinate cells, knocking down the odd-coordinate wall
   between connected cells.
3. 4..10 random rectangular **rooms** (each 2×2 to 4×4) are carved as fully open path regions. Rooms naturally
   inherit entry/exit points from any underlying maze paths they overlap.
4. Entrance (top-left) and goal (bottom-right) cells are stamped.
5. Optional fire pits are scattered into rooms (0..2 per room).
6. 3..10 water pits are scattered onto random walkable cells.

The accepted-maze gate (`MinAcceptablePaths = 3`) re-rolls the generator up to 50 times until the result has at
least three distinct shortest entrance→goal paths — single-corridor mazes are rejected.

`World.ShortestPathCells` is the set of cells on **one chosen** shortest path; the TUI's `s` key overlays them
in yellow. This set is also the reference path for the alignment-based score.

---

## Hazards

All hazard families default to **disabled** at world construction. The simulator builds them anyway, then
`ApplyToggles` strips entities matching disabled flags. Hazards re-appear only when explicitly toggled on via the
TUI or test helpers — the toggle setters (`SetWumpusDisabled` / `SetFirePitsDisabled` / `SetWaterPitsDisabled`)
are symmetric: enable-edge spawns fresh random entities, disable-edge wipes them.

### Wumpus

Each wumpus is a movable adversary. Spawn placement uses `RandomWumpusSpawn` — path cells ≥ 20 Manhattan from the
entrance, no overlap with existing wumpus. At spawn each wumpus picks one of five hunting strategies uniformly at
random (see [Wumpus hunting strategies](#wumpus-hunting-strategies)).

State on each `Wumpus`:

- `Strategy` — function pointer to its assigned hunting strategy.
- `QL` / `DQN` — lazily allocated by the RL hunting strategies; persist across the wumpus's lifetime.
- `CyclesSinceKill` — boredom counter; resets to 0 if the wumpus is on a scented cell OR after a successful agent
  kill. On reaching `WumpusKillTimeout = 30` cycles, the wumpus teleports to a fresh random walkable cell.
- `VengeanceCycles` — when ANY wumpus dies, every survivor gets `PackVengeanceCycles = 20` ticks of "pack
  vengeance" during which their native strategy is overridden by `ScentStrategy` (the most aggressive chase mode).

When a wumpus dies, `KillWumpus` immediately spawns a replacement on a random walkable cell far from entrance and
goal (population stays approximately constant — though spawns no-op while `WumpusDisabled` is set).

### Fire pits

Stepping onto a fire pit kills the agent **unless** the agent has a water charge — in that case the water charge
is consumed AND the fire pit is **extinguished** (`ExtinguishFirePit` converts the cell to `CellPath`, removes it
from `FirePits`, and recomputes Heat in the surrounding 3×3 — a cell stays hot only if some *other* surviving
fire pit still neighbors it). When an agent dies in a fire pit, there's a 50% chance a fresh water pit spawns
somewhere on the maze (mercy mechanic — keeps the resource economy alive without piling indefinitely).

### Water pits

Walking over a water pit grants one water charge. Picked-up water pits revert to `CellPath`. Agents with at
least one water charge will spend it on the next fire-pit step (see above).

### Heat / stench / scent overlays

- **Heat** (`Heat[y][x]`): set wherever a fire pit's 3×3 Moore-neighborhood includes the cell. Recomputed locally
  by `ExtinguishFirePit` when a pit is destroyed. Read via the canonical accessor `World.HeatAt(x, y)`, which
  returns `false` when `FirePitsDisabled` so no sensor reads stale data.
- **Stench** (`Stench[y][x]`): rebuilt every tick by `RecomputeStench` from current live wumpus positions
  (3×3 Moore around each). Bails out and zeroes the grid when `WumpusDisabled` is set.
- **Scent** (`ScentOwner[y][x] rune`): persistent label of the most recent agent that walked across the cell.
  Agents 1..7 each have a distinct scent color. Stamped in `MoveAgents` whenever an agent vacates a cell. Used
  by wumpus `ScentStrategy` (and the agent-1-style scent-chase, and pack-vengeance mode) to track prey trails.

---

## Wumpus hunting strategies

`src/wumpus/strategies.go` defines five strategies that mirror agents 1..5:

1. **`ScentStrategy`** (A-style) — hill-climbs random scented cardinal neighbor; falls back to random walk.
2. **`BfsStrategy`** (B-style) — BFS to the nearest live agent; falls back to random walk.
3. **`DfsStrategy`** (C-style) — DFS to nearest live agent.
4. **`QLStrategy`** (D-style) — tabular Q-learning, reward = −1 per step plus +100 if the wumpus ends the tick
   adjacent to a live agent.
5. **`DqnStrategy`** (E-style) — same reward, NN policy. Features include relative agent position (wumpus knows
   about its prey).

`PickStrategy(rng)` chooses uniformly at spawn. Wumpus block on walls / fire pits / other wumpus but NOT on agents
(walking into an agent is the whole point — combat resolves it).

---

## Agents

Seven agents share the board, each labeled `'1'`..`'7'`. They have distinct glyph colors:

| Label | Color   | Algorithm                              |
|-------|---------|----------------------------------------|
| 1     | blue    | Wumpus-World (inductive + Bayesian)    |
| 2     | cyan    | BFS                                    |
| 3     | magenta | DFS                                    |
| 4     | green   | tabular Q-learning                     |
| 5     | yellow  | Deep Q-Network                         |
| 6     | orange  | POMDP / QMDP                           |
| 7     | pink    | POMCP-lite (flat Monte Carlo)          |

**Default state at launch**: agent 1 enabled, agents 2..7 disabled. Press `1`..`7` to flip each agent
individually. Agents share the entrance spawn point with staggered initial respawn timers (1, 4, 7, 10, 13, 16, 19
ticks) so they don't collide on first spawn.

### Agent 1 — Wumpus-World (inductive + Bayesian)

`src/strategy/bayesian.go`. A faithful AIMA-style Wumpus-World agent. Maintains four sets in `AgentBeliefs`:

- `Observed` — cells the agent has personally stood on.
- `SafeFromPit` — inductive certainty derived from "no heat at visited cell" (the 3×3 neighborhood is pit-free).
  Persistent (pits are static).
- `PitProb` — Bayesian posterior `P(pit at cell)`. 0 = certain safe, 1 = certain pit, in between = uncertain.
- `WumpusProb` — Bayesian posterior `P(wumpus at cell)` for the CURRENT tick only — wiped each cycle because
  wumpus move.

Decision pipeline (`wwPlanPath`) in order:

0. **Water** — if `NeedsWater(w, a)` (zero charges + water pits exist), strict-safe BFS to the nearest water pit.
1. **Strict goal** — BFS to goal through cells the KB has proven safe (visited OR SafeFromPit, AND no current
   stench AND PitProb < 0.5 AND WumpusProb == 0).
2. **Frontier** — walk to the nearest safe-but-unvisited cell to gather more observations.
3. **Calculated risk** — relax the predicate to "not PROVABLY hazardous" (PitProb < 1.0, WumpusProb == 0) and
   re-plan to goal.

When `FirePitsDisabled`, `World.HeatAt` returns 0 everywhere so the inductive pit reasoning sees a clean board.
Same for `StenchAt` when `WumpusDisabled`.

### Agent 2 — BFS

`src/strategy/bfs.go`. Cached BFS plan that re-plans when the next cached step becomes a hazard. Targets the
**nearest water pit** when out of water (and water pits enabled), otherwise targets the goal — same dynamic-
target logic as agent 3.

At every cell with two or more walkable non-backwards neighbors, agent 2 enters the
[branch-decision animation](#branch-decision-animation-agents-2-and-3) — visualizes the search by extending red
ghosts down each candidate branch and retracting them before committing the move.

### Agent 3 — DFS

`src/strategy/dfs.go`. Recursive depth-first search to the same dynamic target. Same branch-decision animation
as agent 2.

### Agent 4 — tabular Q-learning

`src/strategy/qlearn.go`. State = current grid cell. Action = one of four cardinal directions. Standard ε-greedy
Bellman update with `α = 0.1`, `γ = 0.95`, `ε = 0.05`. Reward = −1 per step + reward shaping for fresh
exploration, known-path bonus, back-step penalty, dead-end escalation, and real-distance shaping (see
[Reward shaping](#reward-shaping-for-d-and-e)). Terminal reward = **+10000** on goal reach, **−100** on death.

When `NeedsWater`, the action is overridden with the first step of a BFS toward the nearest water pit (the Q
update still records the chosen action, so the agent still learns from the trajectory).

### Agent 5 — Deep Q-Network

`src/strategy/dqn.go`. Pure-Go MLP: 6 inputs → 16 hidden ReLU → 4 outputs (linear, one per action). Manual
backprop, online SGD with `α = 0.01`, `γ = 0.95`, `ε = 0.05`. Same reward + water override behavior as agent 4.

Input vector (`world.AgentDqnFeatures`):

| Slot | Signal                                |
|------|---------------------------------------|
| 0    | normalized X position                 |
| 1    | normalized Y position                 |
| 2    | east neighbor walkable (0/1)          |
| 3    | south neighbor walkable (0/1)         |
| 4    | heat at current cell (0/1)            |
| 5    | stench at current cell (0/1)          |

Slots 2 and 3 are NOT goal-relative — that was removed in service of the partial-observability principle. The
DQN can still learn position-conditional policies, but it has to discover where the goal is from terminal
rewards.

### Agent 6 — POMDP / QMDP

`src/strategy/pomdp.go`. Full POMDP value iteration is intractable for our state space; this implements the
standard pragmatic approximation, **QMDP** (Littman, Cassandra, Kaelbling 1995):

```
score(s, a) = safety(s')           × value(s')
            = (1 − PitProb[s'])    × (goalReward × γ^DistToGoal(s') − 1)
              (1 − WumpusProb[s'])
```

Action = `argmax_a score(s, a)`. Shares `AgentBeliefs` with agent 1 (`UpdateAgentBeliefs` is called each tick).
Distance-to-goal is computed by an on-demand BFS (`strategy.bfsDistToGoal`) — one per candidate neighbor per
tick. The world doesn't expose a cached distance grid.

Constants: `pomdpGoalReward = 10000`, `pomdpGamma = 0.99`.

### Agent 7 — POMCP-lite

`src/strategy/pomcp.go`. Inspired by Silver & Veness 2010 POMCP but trades the UCT tree for a flat-Monte-Carlo
evaluation in service of staying readable in ~80 lines. For each candidate first move, runs
`PomcpRollouts = 12` random-walk rollouts of depth `PomcpRolloutDepth = 25`. Each rollout step:

1. Charge `pomcpStepCost = 1`, discounted by `γ^step`.
2. If on goal, add `pomcpGoalReward = 10000` (discounted) and end.
3. If belief says cell is hazardous (`PitProb ≥ 0.5` or `WumpusProb > 0`), subtract `pomcpDeathPenalty = 100`
   and end.
4. Otherwise softmax-sample the next cell from walkable cardinal neighbors weighted by
   `safety × (1 / (BFS_dist_to_goal + 1))`.

Depth-limit fallback: bias the trailing reward by a discounted goal estimate from the trailing cell's BFS
distance. Picks the action with the highest mean rollout return.

---

## Reward shaping for D and E

Agents 4 and 5 share `PendingBonus` — a per-step accumulator credited / debited inside `MoveAgents` and folded
into the next Bellman update by the strategy. Shaping signals:

- **`ExplorationBonus = 40.0`** — paid the first time a cell is visited LIFETIME (gated by
  `LifetimeVisited` which persists across deaths). One payment per cell, forever.
- **`KnownPathReward = 10.0`** — paid the first time per cell that the agent re-enters a cell visited in some
  prior life. Gated by `KnownPathRewarded` so the same cell can only pay once across all lives.
- **`BackStepPenalty = 1.0`** — subtracted when the agent moves directly back to the cell it just left.
- **Dead-end escalation** — at a cell with only one walkable cardinal neighbor, cost is `2^DeadEndCount` where
  `DeadEndCount` increments each time within a `DeadEndWindow = 5` cycle rolling window and resets after.
- **`RealDistanceShaping = 1.0`** — paid each time the agent advances its personal `MaxStartDist` (BFS distance
  from entrance through the maze). Back-and-forth between two BFS levels never re-pays because the max only
  ratchets upward.

Goal reward = **+10000**, death penalty = **−100**, `+10` per wumpus kill, `+5` per water pickup. All shaping
flows through `PendingBonus`; the strategy's Bellman update consumes it on the next tick.

---

## Cross-life and cross-maze knowledge persistence

| Field                                       | Reset on death | Survives `r` reseed |
|---------------------------------------------|----------------|---------------------|
| `AgentStats.Deaths/GoalsReached/...`        | no             | no (new world)      |
| `AgentStats.ActualDistance/TicksAlive`      | yes            | yes                 |
| `AgentStats.MinSolveTime/.../LastSolveTime` | no             | no (new world)      |
| `AgentStats.BestAlignment`                  | no             | no (new world)      |
| `Agent.Visited`                             | yes            | yes                 |
| `Agent.LifetimeVisited`                     | no             | no (new world)      |
| `Agent.KnownPathRewarded`                   | no             | no (new world)      |
| `Agent.MaxStartDist`                        | yes            | yes                 |
| `Agent.Beliefs` (agent 1, 6, 7)             | no             | **yes** (grafted)   |
| `Agent.QL`     (agent 4)                    | no             | **yes** (grafted)   |
| `Agent.DQN`    (agent 5)                    | no             | **yes** (grafted)   |

The TUI's `r` key constructs a fresh world but explicitly grafts `Beliefs`, `QL`, and `DQN` from the previous
world's agents onto the new ones. `HasPending` flags on QL / DQN are cleared so cross-maze rewards aren't
accidentally credited to the new maze's first step.

---

## Branch-decision animation (agents 2 and 3)

When agents 2 or 3 reach a cell with two or more walkable non-backwards neighbors, the strategy creates a
`world.SearchAnim` on the agent: `Phase=1`, `Depth=1`, `MaxDepth=SearchAnimMaxDepth (= 3)`. Each subsequent
tick advances the animation — ghosts at `Origin + k * dir` for `k ∈ [1, Depth]` along every `BranchDir` are
rendered in red. After three expand ticks, three retract ticks, and one commit tick (~700 ms at 100 ms/tick),
the agent finally moves the planned step. `MoveAgents` suppresses `FallbackMove` while `SearchAnim != nil` so
the agent really freezes.

`SearchAnim` is also cleared in `RespawnAgents` and `KillAgent` so death mid-animation doesn't leak ghosts.

---

## Combat

Resolved twice per cycle in `ResolveCombat`:

- **Agent ↔ wumpus** (any cardinal adjacency): 50/50 coin flip. Loser dies. Wumpus survives → its
  `CyclesSinceKill` resets. Agent dies → `LastDeathReason = "wumpus"`. Skipped entirely when `WumpusDisabled`.
- **Wumpus ↔ wumpus** (cardinal adjacency, each pair scored once per cycle): 30% chance the pair fights; if it
  does, 50/50 which one dies.

---

## Scoring

`AgentStats.Score(optimal int) float64` is **path-alignment-based**:

```
score = (OnPathSteps − OffPathSteps) / OptimalDistance
```

- `OnPathSteps`: cells visited THIS LIFE that lie in `World.ShortestPathCells`.
- `OffPathSteps`: cells visited THIS LIFE that do not.
- Both counters reset on respawn.

So `1.0` is a flawless solve along the chosen shortest path, `0.0` is even-split, **negative** means deviation
outweighs alignment. `BestAlignment` snapshots the highest ratio achieved on any past goal-reach.

### Solve-time aggregate stats

`CheckGoal` rolls four quantities from each agent's `TicksAlive`:

- `MinSolveTime` — fastest goal reach ever.
- `MaxSolveTime` — slowest.
- `AvgSolveTime` — running mean.
- `LastSolveTime` — most recent.

The TUI status row prints `t[min/avg/max/last]:MMMM/AAAA.A/XXXX/LLLL`, with `last` color-tiered:

| Condition           | Color  |
|---------------------|--------|
| `last ≤ min`        | green  |
| `last ≤ avg`        | yellow |
| `last ≤ max`        | orange |
| `last > max`        | red    |

Distance (`dist:NNNN`) is colored too: orange at ≥ 75% of TTL, red at ≥ 80% of TTL.

---

## Cycle phase order

`World.Step()` runs each phase in this exact order. Phases gated by toggles are noted.

1. `Cycle++`.
2. `tickAgentClocks` — bump `TicksAlive` for each live, non-disabled agent.
3. `TickWumpusClocks` — only if `!WumpusDisabled`.
4. `RecomputeStench` — clears the grid; refills from wumpus only if `!WumpusDisabled`.
5. `ResolveCombat` — only if `!WumpusDisabled`.
6. `MoveAgents` — every active agent calls its strategy; pos-only animation runs here too.
7. `MoveWumpus` — only if `!WumpusDisabled`.
8. `ResolveCombat` — second pass, only if `!WumpusDisabled`.
9. `ResolvePitDeaths` — only if `!FirePitsDisabled`.
10. `CollectWater` — only if `!WaterPitsDisabled`.
11. `CheckGoal` — score / solve-time aggregates / goal-hazard spawn.
12. `RespawnAgents` — entrance respawn for any non-disabled agent whose timer hit 0.

Recomputing stench BEFORE `MoveAgents` (rather than at end-of-tick) ensures agent 1's belief always sees current-
cycle wumpus positions instead of last cycle's stale stench.

---

## Controls

```
q / ctrl+c   quit
r            reseed: build a fresh maze. Agent beliefs / Q-table / DQN
             weights are GRAFTED onto the new world's agents so they
             keep learning across mazes
s            toggle the yellow shortest-path overlay
w            toggle wumpus on/off. ON spawns 5..12 fresh wumpus; OFF
             removes all wumpus entirely
f            toggle fire AND water pits together. ON re-carves fire pits
             into existing rooms (re-seeds Heat) and scatters 3..10 fresh
             water pits; OFF removes all of both kinds and zeroes Heat
t            toggle TTL death rule
1..7         toggle individual agent on/off
```

Status footer reads `wumpus:on/OFF pits:on/OFF ttl:on/OFF` for the toggle state. Title bar shows `Seed: <N>`.

---

## Toggles and entity lifecycle

Every hazard family defaults to disabled in `NewWorldWithConfig`. The runtime toggle setters are symmetric:

- `World.SetWumpusDisabled(false)` → 5..12 fresh wumpus randomly spawned far from entrance.
- `World.SetWumpusDisabled(true)`  → every wumpus stricken, slice nil-ed, stench grid zeroed.
- `World.SetFirePitsDisabled(false)` → re-carves 0..2 fire pits per room; re-seeds Heat.
- `World.SetFirePitsDisabled(true)`  → fire-pit cells revert to path; `FirePits` empty; Heat zeroed.
- `World.SetWaterPitsDisabled(false)` → 3..10 water pits scattered on path cells.
- `World.SetWaterPitsDisabled(true)`  → water-pit cells revert to path; `WaterPits` empty.

Re-disabling never preserves layout — a subsequent enable produces a fresh random distribution. The TUI's `f`
key flips fire and water as a paired toggle.

`World.IsHazard(p)` respects the toggles too — pathfinders (agents 2, 3, 6) won't avoid hazards that aren't
currently "real."

---

## Command-line modes

```
--seed N        deterministic RNG seed; 0 means use wall-clock time
--headless      no TUI; one key=value record per tick on stdout
--steps N       headless: number of cycles to run (default 200)
```

Headless example record (truncated):

```
cycle=12 wumpus_died=0 wumpus_alive=0 optimal=1784 paths=10 \
  1_alive=true 1_deaths=0 1_kills=0 1_goals=0 1_dist=11 1_score=0.000 \
  2_alive=false ... 7_alive=false ... game_over=false
```

The headless record format is regex-tested by `cmd/maze-of-wumpus/e2e_test.go`. Same seed → byte-identical
output across runs.

---

## macOS startup announcement

On darwin, the program invokes `say "starting...the maze of wumpus."` non-blocking right before the bubbletea
TUI starts. On every other platform the call is a no-op. Build tags split the implementation:

- `cmd/maze-of-wumpus/announce_darwin.go` (`//go:build darwin`)
- `cmd/maze-of-wumpus/announce_other.go`  (`//go:build !darwin`)

The function is declared as a swappable `var` so tests can stub it.

---

## Per-agent JSON logs

`src/logging/logger.go` opens seven NDJSON files in `build/logs/`:

```
build/logs/1.log
build/logs/2.log
... 7.log
```

Each file is **truncated** at every game launch (so historical logs are gone, but you always know exactly what
"this run" produced). One record per agent per cycle. Schema:

```json
{
  "cycle": 123,
  "label": "5",
  "strategy": "dqn",
  "alive": true,
  "pos": [40, 40],
  "ticks_alive": 50,
  "water": 1,
  "plan_len": 0,
  "dead_end_count": 0,
  "last_from_cell": [39, 40],
  "has_last_from": true,
  "pending_bonus": 42.0,
  "deaths": 1,
  "wumpus_killed": 0,
  "goals_reached": 0,
  "actual_distance": 50,
  "best_solve_dist": 0,
  "best_solve_time": 0,
  "score": -0.12,
  "lifetime_visited_cells": 137,
  "optimal_distance": 1784,
  "beliefs_size": 0,
  "q_table_size": 0,
  "dqn_q": [0.1, 0.2, 0.3, 0.4]
}
```

A nil logger is safe (`LogTick` checks for nil). `SetStrategyNamer(strategy.Name)` is wired by `cmd/main.go` so
the `strategy` field always reads a meaningful name. The `dqn_q` field is populated only for agent 5; the
`beliefs_size` field only for agents 1 / 6 / 7; the `q_table_size` field only for agent 4.

---

## Tunable constants

| Constant                     | Default        | Where                         | Role                                              |
|------------------------------|----------------|-------------------------------|---------------------------------------------------|
| `BoardWidth`                 | 120            | `src/world/maze.go`           | grid width                                        |
| `BoardHeight`                | 80             | `src/world/maze.go`           | grid height                                       |
| `RespawnTicks`               | 10             | `src/world/world.go`          | post-death respawn delay                          |
| `TTLMultiplier`              | 5              | `src/world/world.go`          | TTL kill ratio over optimal distance              |
| `MinAcceptablePaths`         | 3              | `src/world/world.go`          | minimum distinct shortest paths a maze must have  |
| `MaxShortestPathsCount`      | 10             | `src/world/world.go`          | clamp for ShortestPaths counter                   |
| `ExplorationBonus`           | 40.0           | `src/world/world.go`          | first-ever-visit reward                           |
| `KnownPathReward`            | 10.0           | `src/world/world.go`          | first re-visit-after-death reward (once-per-cell) |
| `BackStepPenalty`            | 1.0            | `src/world/world.go`          | direct-reversal penalty                           |
| `DeadEndWindow`              | 5              | `src/world/world.go`          | cycles for dead-end penalty escalation            |
| `DeadEndExpCap`              | 10             | `src/world/world.go`          | clamp on `2^count` to avoid overflow              |
| `RealDistanceShaping`        | 1.0            | `src/world/world.go`          | per BFS-unit advance from entrance                |
| `WumpusKillTimeout`          | 30             | `src/world/world.go`          | wumpus boredom-teleport threshold                 |
| `PackVengeanceCycles`        | 20             | `src/world/world.go`          | vengeance scent-chase length after sibling kill   |
| `QLearnAlpha/Gamma/Epsilon`  | 0.1/0.95/0.05  | `src/strategy/qlearn.go`      | agent 4 hyperparameters                           |
| `DqnLearnRate/Gamma/Epsilon` | 0.01/0.95/0.05 | `src/strategy/dqn.go`         | agent 5 hyperparameters                           |
| `pomdpGoalReward/Gamma`      | 10000/0.99     | `src/strategy/pomdp.go`       | agent 6 utility scale                             |
| `PomcpRollouts/Depth`        | 12/25          | `src/strategy/pomcp.go`       | agent 7 rollouts per action, depth limit          |
| `SearchAnimMaxDepth`         | 3              | `src/strategy/branch_anim.go` | branch-animation reach in cells                   |

---

## Make targets

```
make build      # go build -o build/maze-of-wumpus ./cmd/maze-of-wumpus
make lint       # go vet -v ./...
make test       # go test -v ./...
make coverage   # go test -coverpkg=./... -coverprofile=build/coverage.out ./...
                # + go tool cover -func ...
make clean      # rm -rf build && mkdir -p build
make run        # build then ./build/maze-of-wumpus
```

The current cross-package coverage gate is **≥ 96%** total. `go vet` is clean.

---

## Test architecture

| Package                       | Notable tests                                              |
|-------------------------------|------------------------------------------------------------|
| `src/world/`                  | Maze connectivity, hazard toggle setters, combat,          |
|                               | respawn timing, real-distance shaping, solve-time stats,   |
|                               | water-shield extinguish, branch-anim cleanup on death.     |
| `src/strategy/`               | Bayesian pipeline branches, BFS / DFS strategy lifecycle,  |
|                               | Q-learning + DQN nil-init and weight updates, water-       |
|                               | secondary-goal, branch-animation state machine, agents 6/7 |
|                               | wiring.                                                    |
| `src/wumpus/`                 | All five wumpus strategies, BFS / DFS unreachable paths,   |
|                               | scent with / without trails, pick-strategy distribution.   |
| `src/tui/`                    | Glyph rendering for every cell type / agent / scent /      |
|                               | ghost overlay; tier colorings; every toggle key.           |
| `src/logging/`                | File creation, truncation, nil safety, dir creation,       |
|                               | DQN Q-values surfaced for agent 5.                         |
| `cmd/maze-of-wumpus/`         | `runApp` flag parsing, headless format, e2e subprocess     |
|                               | determinism / column-presence checks, announce stub.       |

The `EnableHazards()` test helper (in `src/world/testsupport.go`) flips all four hazard toggles to enabled AND
spawns fresh entities — used by every test that needs hazards to be live.

---

## Determinism

A single `*rand.Rand` (`World.Rng`) drives every randomized decision: maze generation, wumpus spawn placement,
strategy choice, ε-greedy action selection, fallback move shuffles, and Monte-Carlo rollouts in agent 7. With a
fixed `--seed`, headless output is byte-identical across runs. The e2e test `TestE2E_HeadlessDeterminism`
enforces this.

The `r` reseed in the TUI deliberately uses `time.Now().UnixNano()`, so it produces a fresh maze every press —
this is the *only* place wall-clock time enters the simulation.
