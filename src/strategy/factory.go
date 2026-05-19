// factory.go — ForLabel maps agent labels (A..E) to their decision
// strategies. Passed to world.Config.StrategyFor at construction time.
package strategy

import (
	"maze-of-wumpus/src/world"
)

// ForLabel returns the strategy assigned to the given agent label,
// or nil if the label is unrecognised.
func ForLabel(label rune) world.Strategy {
	switch label {
	case '1':
		return BayesianStrategy
	case '2':
		return BFSStrategy
	case '3':
		return DFSStrategy
	case '4':
		return QLearningStrategy
	case '5':
		return DQNStrategy
	case '6':
		return POMDPStrategy
	case '7':
		return POMCPStrategy
	}
	return nil
}

// Name returns a human-readable label for the strategy assigned to
// the given agent label. Used by logging.
func Name(label rune) string {
	switch label {
	case '1':
		return "bayesian"
	case '2':
		return "bfs"
	case '3':
		return "dfs"
	case '4':
		return "q-learning"
	case '5':
		return "dqn"
	case '6':
		return "pomdp"
	case '7':
		return "pomcp"
	}
	return "unknown"
}
