// factory.go — ForLabel maps agent labels to their decision
// strategies. Passed to world.Config.StrategyFor at construction time.
//
// Lineup:
//
//	1  bfs                  — omniscient BFS benchmark
//	2  dfs                  — omniscient DFS
//	3  bayesian             — Wumpus-World Bayesian, strict PO, NO scent (radius 1)
//	4  scent-follower       — Bayesian + scent (radius 1)
//	5  dqn                  — deep Q-network with scent perception (radius 1)
//	6  pomcp                — flat Monte-Carlo planner with scent (radius 1)
//	7  qmdp                 — POMDP QMDP-style planner with scent (radius 1)
//	8  bayesian (far-sight)       — same as 3 with SensingRadius=2
//	9  scent-follower (far-sight) — same as 4 with SensingRadius=2
//	A  dqn (far-sight)            — same as 5 with SensingRadius=2
//	B  pomcp (far-sight)          — same as 6 with SensingRadius=2
//	C  qmdp (far-sight)           — same as 7 with SensingRadius=2
package strategy

import (
	"maze-of-wumpus/src/world"
)

// Strategy letter identifiers. The 7 underlying algorithms each get
// a single letter so they can be selected per-journey by ANY agent.
// Labels (1..C) are identity; letters (R..X) are interchangeable
// implementations.
//
// Slot 'S' previously held an omniscient DFS — it now hosts the
// shared-knowledge Bayesian (swarm) variant. The DFS function still
// exists in dfs.go for the legacy ForLabel('2') mapping and a few
// branch-anim tests, but it's no longer part of the per-journey
// strategy pool.
const (
	StrategyBFS = 'R'
	// StrategySwarmBayesian aliases world.SwarmStrategyLetter so
	// the world package can detect swarm members for path-sharing
	// without importing strategy/.
	StrategySwarmBayesian = world.SwarmStrategyLetter
	StrategyBayesian      = 'T'
	StrategyScentFollower = 'U'
	StrategyDQN           = 'V'
	StrategyPOMCP         = 'W'
	StrategyQMDP          = 'X'
)

// StrategyLetters is the canonical iteration order over all 7
// algorithms. Used by PickStrategy, the algorithm trust matrix UI,
// and any test that needs to walk the registry.
var StrategyLetters = []rune{
	StrategyBFS,
	StrategySwarmBayesian,
	StrategyBayesian,
	StrategyScentFollower,
	StrategyDQN,
	StrategyPOMCP,
	StrategyQMDP,
}

// ForLetter returns the strategy function for a given letter, or
// nil if the letter is unrecognised. This is the runtime dispatch
// used when an agent's CurrentStrategy drives action selection.
func ForLetter(letter rune) world.Strategy {
	switch letter {
	case StrategyBFS:
		return BFSStrategy
	case StrategySwarmBayesian:
		return SwarmBayesianStrategy
	case StrategyBayesian:
		return BayesianStrategy
	case StrategyScentFollower:
		return ScentFollowerStrategy
	case StrategyDQN:
		return DQNStrategy
	case StrategyPOMCP:
		return POMCPStrategy
	case StrategyQMDP:
		return QMDPStrategy
	}
	return nil
}

// DescriptionByLetter returns a short (≤64 char) human-readable
// description of the strategy keyed by its letter. Used by the
// Agent-Algorithm Trust legend in the TUI.
func DescriptionByLetter(letter rune) string {
	switch letter {
	case StrategyBFS:
		return "Omniscient breadth-first search to goal"
	case StrategySwarmBayesian:
		return "Bayesian PO with shared (swarm) KnownCells + Beliefs"
	case StrategyBayesian:
		return "Inductive Bayesian reasoning, partial observability"
	case StrategyScentFollower:
		return "Bayesian + scent: follow a chosen leader's trail"
	case StrategyDQN:
		return "Deep Q-network with scent perception"
	case StrategyPOMCP:
		return "Flat Monte-Carlo planner (POMCP-lite) with scent"
	case StrategyQMDP:
		return "POMDP QMDP-style expected-utility planner with scent"
	}
	return "unknown"
}

// NameByLetter returns the human-readable name of a strategy
// keyed by its letter. Useful for logging / status panes.
func NameByLetter(letter rune) string {
	switch letter {
	case StrategyBFS:
		return "bfs"
	case StrategySwarmBayesian:
		return "swarm-bayesian"
	case StrategyBayesian:
		return "bayesian"
	case StrategyScentFollower:
		return "scent-follower"
	case StrategyDQN:
		return "dqn"
	case StrategyPOMCP:
		return "pomcp"
	case StrategyQMDP:
		return "qmdp"
	}
	return "unknown"
}

// ForLabel returns the strategy assigned to the given agent label,
// or nil if the label is unrecognised. The far-sight variants
// share their counterpart's strategy function — the only difference
// is Agent.SensingRadius (set at construction).
func ForLabel(label rune) world.Strategy {
	switch label {
	case '1':
		return BFSStrategy
	case '2':
		return DFSStrategy
	case '3', '8':
		return BayesianStrategy
	case '4', '9':
		return ScentFollowerStrategy
	case '5', 'A':
		return DQNStrategy
	case '6', 'B':
		return POMCPStrategy
	case '7', 'C':
		return QMDPStrategy
	}
	return nil
}

// Name returns a human-readable label for the strategy assigned to
// the given agent label. Far-sight variants suffix "+fs".
func Name(label rune) string {
	switch label {
	case '1':
		return "bfs"
	case '2':
		return "dfs"
	case '3':
		return "bayesian"
	case '4':
		return "scent-follower"
	case '5':
		return "dqn"
	case '6':
		return "pomcp"
	case '7':
		return "qmdp"
	case '8':
		return "bayesian+fs"
	case '9':
		return "scent-follower+fs"
	case 'A':
		return "dqn+fs"
	case 'B':
		return "pomcp+fs"
	case 'C':
		return "qmdp+fs"
	}
	return "unknown"
}
