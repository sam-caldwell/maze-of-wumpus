// factory.go — ForLabel maps agent labels to their decision
// strategies. Passed to world.Config.StrategyFor at construction time.
//
// Lineup:
//
//	1  bfs             — omniscient BFS benchmark (singleton)
//	2  dfs             — omniscient DFS
//	3  bayesian        — Wumpus-World Bayesian, strict PO, NO scent
//	4  swarm-bayesian  — shared-knowledge Bayesian swarm
//	5  pomcp           — flat Monte-Carlo planner with scent
//	6  qmdp            — POMDP QMDP-style planner with scent
//
// All six agents share the same world.DefaultSmellRadius (2) and
// world.DefaultSightRadius (10) — every agent sees out to 10 cells
// and smells out to 2.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// Strategy letter identifiers. The 5 underlying algorithms each get
// a single letter so they can be selected per-journey by ANY agent.
// Labels (1..6) are identity; letters (R..V) are interchangeable
// implementations.
const (
	// StrategyBFS ('R') is the omniscient benchmark. The name is
	// historical — it now routes via A* (BFSToward → World.AStarPath),
	// and it's the lone non-swarm strategy (a singleton, per
	// world.IsSwarmStrategy).
	StrategyBFS = 'R'
	// StrategySwarmBayesian aliases world.SwarmStrategyLetter so
	// the world package can detect swarm members for path-sharing
	// without importing strategy/.
	StrategySwarmBayesian = world.SwarmStrategyLetter
	StrategyBayesian      = 'T'
	StrategyPOMCP         = 'U'
	StrategyQMDP          = 'V'
)

// StrategyLetters is the canonical iteration order over all 5
// algorithms. Used by PickStrategy, the algorithm trust matrix UI,
// and any test that needs to walk the registry.
var StrategyLetters = []rune{
	StrategyBFS,
	StrategySwarmBayesian,
	StrategyBayesian,
	StrategyPOMCP,
	StrategyQMDP,
}

// ForLetter returns the strategy function for a given letter, or
// nil if the letter is unrecognised. This is the runtime dispatch
// used when an agent's CurrentStrategy drives action selection.
func ForLetter(letter rune) world.Strategy {
	// R (omniscient benchmark) is the only non-swarm letter. Every
	// other letter runs the universal branch-spreading swarm wrapper,
	// which dispatches to the letter's own solo planner for
	// exploitation (see SwarmStrategy / planFor). The solo planner
	// functions themselves are still used directly by ForLabel for
	// the fixed agent-identity mappings.
	switch letter {
	case StrategyBFS:
		return BFSStrategy
	case StrategySwarmBayesian, StrategyBayesian, StrategyPOMCP, StrategyQMDP:
		return SwarmStrategy
	}
	return nil
}

// DescriptionByLetter returns a short (≤64 char) human-readable
// description of the strategy keyed by its letter. Used by the
// Agent-Algorithm Trust legend in the TUI.
func DescriptionByLetter(letter rune) string {
	switch letter {
	case StrategyBFS:
		return "Omniscient A* shortest-path benchmark (singleton)"
	case StrategySwarmBayesian:
		return "Bayesian swarm: shared beliefs, forks & disperses"
	case StrategyBayesian:
		return "Bayesian swarm (strict-PO inductive reasoning)"
	case StrategyPOMCP:
		return "POMCP swarm: Monte-Carlo lookahead clones"
	case StrategyQMDP:
		return "QMDP swarm: expected-utility clones explore"
	}
	return "unknown"
}

// NameByLetter returns the human-readable name of a strategy
// keyed by its letter. Useful for logging / status panes.
func NameByLetter(letter rune) string {
	switch letter {
	case StrategyBFS:
		return "astar"
	case StrategySwarmBayesian:
		return "swarm-bayesian"
	case StrategyBayesian:
		return "bayesian-swarm"
	case StrategyPOMCP:
		return "pomcp-swarm"
	case StrategyQMDP:
		return "qmdp-swarm"
	}
	return "unknown"
}

// ForLabel returns the strategy assigned to the given agent label,
// or nil if the label is unrecognised. This is the legacy identity
// mapping; the live runtime dispatches per-journey via ForLetter and
// the agent's CurrentStrategy. Label 4 (swarm-Bayesian) resolves to
// the Bayesian planner, exactly as letter S does.
func ForLabel(label rune) world.Strategy {
	switch label {
	case '1':
		return BFSStrategy
	case '2':
		return DFSStrategy
	case '3':
		return BayesianStrategy
	case '4':
		return BayesianStrategy
	case '5':
		return POMCPStrategy
	case '6':
		return QMDPStrategy
	}
	return nil
}

// Name returns a human-readable label for the strategy assigned to
// the given agent label.
func Name(label rune) string {
	switch label {
	case '1':
		return "bfs"
	case '2':
		return "dfs"
	case '3':
		return "bayesian"
	case '4':
		return "swarm-bayesian"
	case '5':
		return "pomcp"
	case '6':
		return "qmdp"
	}
	return "unknown"
}
