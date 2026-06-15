// factory.go — maps agent labels to their decision strategies. Passed to
// world.Config at construction time.
//
// Lineup (one agent per strategy letter):
//
//	1  R  bfs            — omniscient A* benchmark (singleton)
//	2  S  swarm-bayesian — goal-location Bayesian swarm
//	3  U  pomcp          — flat Monte-Carlo planner swarm
//	4  V  qmdp           — POMDP QMDP-style planner swarm
//
// All agents share the same world.DefaultSmellRadius (2) and
// world.DefaultSightRadius (10).
package strategy

import (
	"maze-of-wumpus/src/world"
)

// Strategy letter identifiers. Each underlying algorithm has a single
// letter. Labels (1..4) are agent identities; letters (R/S/U/V) are the
// interchangeable implementations.
const (
	// StrategyBFS ('R') is the omniscient benchmark. The name is
	// historical — it now routes via A* (BFSToward → World.AStarPath),
	// and it's the lone non-swarm strategy (a singleton, per
	// world.IsSwarmStrategy).
	StrategyBFS = 'R'
	// StrategySwarmBayesian ('S') aliases world.SwarmStrategyLetter so
	// the world package can detect swarm members for path-sharing
	// without importing strategy/.
	StrategySwarmBayesian = world.SwarmStrategyLetter
	StrategyPOMCP         = 'U'
	StrategyQMDP          = 'V'
)

// StrategyLetters is the canonical iteration order over all algorithms.
// Used by the algorithm trust matrix UI and any test that walks the
// registry.
var StrategyLetters = []rune{
	StrategyBFS,
	StrategySwarmBayesian,
	StrategyPOMCP,
	StrategyQMDP,
}

// ForLetter returns the strategy function for a given letter, or nil if
// the letter is unrecognised. This is the runtime dispatch used when an
// agent's CurrentStrategy drives action selection.
func ForLetter(letter rune) world.Strategy {
	// R (omniscient benchmark) is the only non-swarm letter. Every other
	// letter runs the universal branch-spreading swarm wrapper, which
	// dispatches to the letter's own solo planner for exploitation (see
	// SwarmStrategy / planFor).
	switch letter {
	case StrategyBFS:
		return BFSStrategy
	case StrategySwarmBayesian, StrategyPOMCP, StrategyQMDP:
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
		return "Bayesian swarm: infers goal location, forks & disperses"
	case StrategyPOMCP:
		return "POMCP swarm: Monte-Carlo lookahead clones"
	case StrategyQMDP:
		return "QMDP swarm: expected-utility clones explore"
	}
	return "unknown"
}

// NameByLetter returns the human-readable name of a strategy keyed by its
// letter. Useful for logging / status panes.
func NameByLetter(letter rune) string {
	switch letter {
	case StrategyBFS:
		return "astar"
	case StrategySwarmBayesian:
		return "swarm-bayesian"
	case StrategyPOMCP:
		return "pomcp-swarm"
	case StrategyQMDP:
		return "qmdp-swarm"
	}
	return "unknown"
}

// ForLabel returns the strategy assigned to the given agent label, or nil
// if the label is unrecognised. This is the legacy identity mapping; the
// live runtime dispatches via ForLetter and the agent's fixed
// CurrentStrategy. The values are each letter's SOLO planner (the swarm
// wrapper calls these internally via planFor).
func ForLabel(label rune) world.Strategy {
	switch label {
	case '1':
		return BFSStrategy
	case '2':
		return BayesianStrategy
	case '3':
		return POMCPStrategy
	case '4':
		return QMDPStrategy
	}
	return nil
}

// LetterForLabel returns the FIXED strategy letter each agent runs for
// the entire game, keyed by its label. Drives the per-agent fixed-
// strategy assignment (see world.Config.StrategyLetterForLabel) so an
// agent never switches strategy across journeys. 1:1 onto the letters.
func LetterForLabel(label rune) rune {
	switch label {
	case '1':
		return StrategyBFS // R
	case '2':
		return StrategySwarmBayesian // S
	case '3':
		return StrategyPOMCP // U
	case '4':
		return StrategyQMDP // V
	}
	return 0
}

// Name returns a human-readable label for the strategy assigned to the
// given agent label.
func Name(label rune) string {
	switch label {
	case '1':
		return "bfs"
	case '2':
		return "swarm-bayesian"
	case '3':
		return "pomcp"
	case '4':
		return "qmdp"
	}
	return "unknown"
}
