// tui.go — bubbletea Model + renderer for Maze of Wumpus.
//
// Five agents (labels '1'..'5') share the board, each colored distinctly:
//
//	1 — blue   (Bayesian / Wumpus-World reasoning)
//	2 — cyan   (BFS)
//	3 — magenta (DFS)
//	4 — green  (Q-learning)
//	5 — yellow (DQN)
//
// Wumpus are red, fire pits are red on grey, the goal is green on
// yellow. Heat (red bg) and stench (red ~) overlay path cells.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"maze-of-wumpus/src/world"
)

const tickInterval = 100 * time.Millisecond

// Styles.
var (
	wallStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	pathStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("236"))
	agent1Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	agent2Style = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	agent3Style = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	agent4Style = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	agent5Style = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	agent6Style = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true) // orange (POMDP)
	agent7Style = lipgloss.NewStyle().Foreground(lipgloss.Color("177")).Bold(true) // pink (POMCP)
	// Far-sight variants: distinct hues kin to their short-sight
	// counterpart but visually unambiguous on dark terminals.
	agent8Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("165")).Bold(true) // magenta (Bayesian+fs)
	agent9Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)  // mid green (scent-follower+fs)
	agentAStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true) // bright yellow (DQN+fs)
	agentBStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("202")).Bold(true) // red-orange (POMCP+fs)
	agentCStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)  // purple-violet (QMDP+fs)
	wumpusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	firePitStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Background(lipgloss.Color("240")).Bold(true)
	waterPitStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("21")).Background(lipgloss.Color("51")).Bold(true)
	goalStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Background(lipgloss.Color("226")).Bold(true)
	entranceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	heatStyle      = lipgloss.NewStyle().Background(lipgloss.Color("88"))
	stenchStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	stenchOnHeat   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Background(lipgloss.Color("88"))
	ghostStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	scent1Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	scent2Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	scent3Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	scent4Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	scent5Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	scent6Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	scent7Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("177"))
	scent8Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("165"))
	scent9Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	scentAStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	scentBStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("202"))
	scentCStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	statStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	ttlWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	ttlDangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	solveGreen     = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	solveYellow    = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	solveOrange    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	solveRed       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	overStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("46"))
)

// Pre-rendered glyphs.
var (
	wallGlyph     = wallStyle.Render("█")
	pathGlyph     = pathStyle.Render("·")
	agent1Glyph   = agent1Style.Render("1")
	agent2Glyph   = agent2Style.Render("2")
	agent3Glyph   = agent3Style.Render("3")
	agent4Glyph   = agent4Style.Render("4")
	agent5Glyph   = agent5Style.Render("5")
	agent6Glyph   = agent6Style.Render("6")
	agent7Glyph   = agent7Style.Render("7")
	agent8Glyph   = agent8Style.Render("8")
	agent9Glyph   = agent9Style.Render("9")
	agentAGlyph   = agentAStyle.Render("A")
	agentBGlyph   = agentBStyle.Render("B")
	agentCGlyph   = agentCStyle.Render("C")
	wumpusGlyph   = wumpusStyle.Render("W")
	firePitGlyph  = firePitStyle.Render("F")
	waterPitGlyph = waterPitStyle.Render("W")
	goalGlyph     = goalStyle.Render("G")
	scent1Glyph   = scent1Style.Render("~")
	scent2Glyph   = scent2Style.Render("~")
	scent3Glyph   = scent3Style.Render("~")
	scent4Glyph   = scent4Style.Render("~")
	scent5Glyph   = scent5Style.Render("~")
	scent6Glyph   = scent6Style.Render("~")
	scent7Glyph   = scent7Style.Render("~")
	scent8Glyph   = scent8Style.Render("~")
	scent9Glyph   = scent9Style.Render("~")
	scentAGlyph   = scentAStyle.Render("~")
	scentBGlyph   = scentBStyle.Render("~")
	scentCGlyph   = scentCStyle.Render("~")
	entranceGlyph = entranceStyle.Render("S") // 'S' for Start — no longer collides with agent 'E'
	stenchGlyph   = stenchStyle.Render("~")
	heatGlyph     = heatStyle.Render(" ")
	stenchHeatGl  = stenchOnHeat.Render("~")
	ghostGlyph    = ghostStyle.Render("◌")
)

type tickMsg struct{}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

// WorldBuilder constructs a fresh *world.World — used on launch and
// for the 'r' reseed. cmd/main.go wires this with full strategy
// configuration so the model stays strategy-agnostic.
type WorldBuilder func(seed int64) *world.World

// Model is the bubbletea Model for the maze TUI. Exported so
// cmd/main.go can construct it.
type Model struct {
	World    *world.World
	ShowPath bool
	build    WorldBuilder
}

// NewModel constructs a Model. `builder` is the function that turns a
// seed into a fully-configured *world.World (with strategies attached);
// the model uses it on launch and to handle the 'r' reseed key.
func NewModel(seed int64, builder WorldBuilder) Model {
	if builder == nil {
		builder = world.NewWorld
	}
	return Model{World: builder(seed), build: builder}
}

// Init returns the first tick command.
func (m Model) Init() tea.Cmd {
	return tickEvery(tickInterval)
}

// Update handles keyboard / tick messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.reseedPreservingLearning()
		case "s":
			m.ShowPath = !m.ShowPath
		case "w":
			// SetWumpusDisabled both flips the flag and spawns
			// (enable edge) or clears (disable edge) the wumpus
			// population in one shot — the toggle is symmetric.
			m.World.SetWumpusDisabled(!m.World.WumpusDisabled)
		case "f":
			// 'f' toggles BOTH pit types together to the same state.
			next := !m.World.FirePitsDisabled
			m.World.SetFirePitsDisabled(next)
			m.World.SetWaterPitsDisabled(next)
		case "t":
			m.World.TTLDisabled = !m.World.TTLDisabled
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Toggle the matching agent on/off. The agent at label
			// equal to the typed digit is the target.
			if a := m.World.AgentByLabel(rune(msg.String()[0])); a != nil {
				a.Disabled = !a.Disabled
			}
		case "a", "A", "b", "B", "c", "C":
			// Far-sight agents labeled 'A', 'B', 'C' — accept either
			// the lowercase or uppercase form of the key.
			label := rune(msg.String()[0])
			if label >= 'a' && label <= 'z' {
				label = label - 'a' + 'A'
			}
			if a := m.World.AgentByLabel(label); a != nil {
				a.Disabled = !a.Disabled
			}
		}
	case tickMsg:
		m.World.Step()
		// Auto-reseed when the maze is "solved": write a stats log
		// snapshot of the just-finished run, then build a fresh
		// world preserving each agent's learning state. New mazes
		// start with all agents enabled (NewWorldWithConfig default).
		if m.World.MazeSolved() {
			_, _ = m.World.WriteStatsLog(StatsDir)
			m.reseedPreservingLearning()
		}
		return m, tickEvery(tickInterval)
	}
	return m, nil
}

// StatsDir is the directory under which maze-solved JSON snapshots
// land. Default lives next to the build artifacts so `make clean`
// also wipes them.
const StatsDir = "build/stats"

// reseedPreservingLearning constructs a fresh world via m.build and
// grafts each agent's persistent learning state (Beliefs / QL / DQN
// / TrustScores) from the prior world onto the new agents.
//
// Trust scores survive reseed (so an agent's lifetime opinion of
// each leader/peer persists), but the run counter (Stats.Starts)
// does NOT — the new map starts every follower back in stage 1
// (uniform random pick) for its first ScentRunsForTrustWeighting
// runs. Trust updates fire per-journey from KillAgent / CheckGoal,
// not here.
//
// Used by the 'r' key AND the auto-reseed-on-solve path.
func (m *Model) reseedPreservingLearning() {
	prev := m.World.Agents
	m.World = m.build(time.Now().UnixNano())
	for i, oldA := range prev {
		if i >= len(m.World.Agents) {
			break
		}
		newA := m.World.Agents[i]
		if oldA.Beliefs != nil {
			newA.Beliefs = oldA.Beliefs
		}
		if oldA.DQN != nil {
			newA.DQN = oldA.DQN
			newA.DQN.HasPending = false
		}
		if oldA.TrustScores != nil {
			newA.TrustScores = oldA.TrustScores
		}
		// Carry the prior map's LearnedTTL forward as a starting
		// belief for the new map. The invalidation rule in
		// MoveAgents drops it if the new map's TTL is larger.
		newA.LearnedTTL = oldA.LearnedTTL
	}
}

// View renders the model: title + grid + per-agent status + footer.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Maze of Wumpus"))
	totalGoals := 0
	for _, a := range m.World.Agents {
		totalGoals += a.Stats.GoalsReached
	}
	if totalGoals > 0 {
		b.WriteString("  ")
		b.WriteString(overStyle.Render(fmt.Sprintf("[GOALS: %d]", totalGoals)))
	}
	b.WriteString("  ")
	b.WriteString(statStyle.Render(fmt.Sprintf("Seed: %d", m.World.Seed)))
	b.WriteString("\n")
	matrixLines := renderTrustMatrixLines(m.World)
	for y := 0; y < world.BoardHeight; y++ {
		for x := 0; x < world.BoardWidth; x++ {
			g := m.glyphAt(m.World, x, y)
			if m.ShowPath && m.World.ShortestPathCells[world.Pos{X: x, Y: y}] {
				g = "\x1b[43m" + g + "\x1b[49m"
			}
			b.WriteString(g)
		}
		// Splice the trust matrix to the right of the first
		// len(matrixLines) maze rows. Falls off after that.
		if y < len(matrixLines) {
			b.WriteString("  ")
			b.WriteString(matrixLines[y])
		}
		b.WriteString("\n")
	}
	for _, a := range m.World.Agents {
		b.WriteString(statStyle.Render(m.formatAgentStats(a)))
		b.WriteString("\n")
	}
	pathsStr := fmt.Sprintf("%d", m.World.Stats.ShortestPaths)
	if m.World.Stats.ShortestPaths >= world.MaxShortestPathsCount {
		pathsStr = fmt.Sprintf("%d+", world.MaxShortestPathsCount)
	}
	wumpState := "on"
	if m.World.WumpusDisabled {
		wumpState = "OFF"
	}
	pitState := "on"
	if m.World.FirePitsDisabled {
		pitState = "OFF"
	}
	ttlState := "on"
	if m.World.TTLDisabled {
		ttlState = "OFF"
	}
	b.WriteString(statStyle.Render(
		fmt.Sprintf("Cycle %5d | TTL %d ×%d | Paths: %s | W killed: %d | wumpus:%s pits:%s ttl:%s\n[q]uit [r]eseed [s]how-path [w]umpus [f]ire/water [t]tl [1..9 a..c] agent",
			m.World.Cycle,
			world.TTLMultiplier*m.World.Stats.OptimalDistance, world.TTLMultiplier,
			pathsStr, m.World.Stats.WumpusDied,
			wumpState, pitState, ttlState),
	))
	return b.String()
}

// lastSolveTier classifies the agent's most recent solve time against
// its running min / avg / max:
//
//	0 = green   (last ≤ min — new personal best, or first solve)
//	1 = yellow  (last ≤ avg — better than average)
//	2 = orange  (last ≤ max — between average and worst)
//	3 = red     (last > max — somehow exceeded the prior max; only
//	              hits if max bookkeeping lags, kept as a safety tier)
//
// Returns -1 when the agent has not yet solved (LastSolveTime == 0)
// so the caller can render plain text.
func lastSolveTier(last, min, avg, max int) int {
	if last <= 0 {
		return -1
	}
	if last <= min {
		return 0
	}
	if last <= avg {
		return 1
	}
	if last <= max {
		return 2
	}
	return 3
}

// distSeverity classifies a per-agent distance against its TTL.
func distSeverity(actual, ttl int) int {
	if ttl <= 0 {
		return 0
	}
	r := float64(actual) / float64(ttl)
	switch {
	case r >= 0.80:
		return 2
	case r >= 0.75:
		return 1
	default:
		return 0
	}
}

func (m Model) formatAgentStats(a *world.Agent) string {
	alive := "alive   "
	if !a.Alive {
		alive = "dead    "
	}
	distText := fmt.Sprintf("dist:%04d", a.Stats.ActualDistance)
	switch distSeverity(a.Stats.ActualDistance, world.TTLMultiplier*m.World.Stats.OptimalDistance) {
	case 2:
		distText = ttlDangerStyle.Render(distText)
	case 1:
		distText = ttlWarnStyle.Render(distText)
	}
	lastText := fmt.Sprintf("%04d", a.Stats.LastSolveTime)
	switch lastSolveTier(a.Stats.LastSolveTime, a.Stats.MinSolveTime,
		int(a.Stats.AvgSolveTime), a.Stats.MaxSolveTime) {
	case 0:
		lastText = solveGreen.Render(lastText)
	case 1:
		lastText = solveYellow.Render(lastText)
	case 2:
		lastText = solveOrange.Render(lastText)
	case 3:
		lastText = solveRed.Render(lastText)
	}
	following := "-"
	if a.CurrentTrustee != 0 {
		following = string(a.CurrentTrustee)
	}
	strLetter := "-"
	if a.CurrentStrategy != 0 {
		strLetter = string(a.CurrentStrategy)
	}
	learnedTTL := "----"
	if a.LearnedTTL > 0 {
		learnedTTL = fmt.Sprintf("%04d", a.LearnedTTL)
	}
	return fmt.Sprintf(
		" %c %s str:%s s:%03d f:%s ttl:%s d:%03d k:%03d g:%03d %s best:%04d/%04d t[min/avg/max/last]:%04d/%07.1f/%04d/%s score:%.5f",
		a.Label, alive, strLetter,
		a.Stats.Starts, following, learnedTTL,
		a.Stats.Deaths, a.Stats.WumpusKilled, a.Stats.GoalsReached,
		distText,
		a.Stats.BestSolveDistance, a.Stats.BestSolveTime,
		a.Stats.MinSolveTime, a.Stats.AvgSolveTime, a.Stats.MaxSolveTime, lastText,
		a.Stats.Score(m.World.Cycle),
	)
}

// cellIsGhost reports whether (x, y) is currently occupied by any
// active branch-animation ghost. Iterating agents on every cell is
// cheap because at most two agents (2 and 3) animate at a time and
// each has at most 4 branches * SearchAnimMaxDepth cells.
func cellIsGhost(w *world.World, x, y int) bool {
	for _, a := range w.Agents {
		if a.SearchAnim == nil || a.Disabled {
			continue
		}
		s := a.SearchAnim
		for _, dir := range s.BranchDirs {
			for k := 1; k <= s.Depth; k++ {
				if s.Origin.X+k*dir.X == x && s.Origin.Y+k*dir.Y == y {
					return true
				}
			}
		}
	}
	return false
}

func (m Model) glyphAt(w *world.World, x, y int) string {
	if a := w.AgentAt[y][x]; a != nil && a.Alive && !a.Disabled {
		switch a.Label {
		case '1':
			return agent1Glyph
		case '2':
			return agent2Glyph
		case '3':
			return agent3Glyph
		case '4':
			return agent4Glyph
		case '5':
			return agent5Glyph
		case '6':
			return agent6Glyph
		case '7':
			return agent7Glyph
		case '8':
			return agent8Glyph
		case '9':
			return agent9Glyph
		case 'A':
			return agentAGlyph
		case 'B':
			return agentBGlyph
		case 'C':
			return agentCGlyph
		}
	}
	if !w.WumpusDisabled {
		if wm := w.WumpusAt[y][x]; wm != nil && wm.Alive {
			return wumpusGlyph
		}
	}
	// Branch-animation ghosts (red) overlay everything below this
	// point, so the agent and wumpus glyphs above still win.
	if cellIsGhost(w, x, y) {
		return ghostGlyph
	}
	cell := w.Maze.Cells[y][x]
	switch cell {
	case world.CellWall:
		return wallGlyph
	case world.CellFirePit:
		if w.FirePitsDisabled {
			return pathGlyph
		}
		return firePitGlyph
	case world.CellWaterPit:
		if w.WaterPitsDisabled {
			return pathGlyph
		}
		return waterPitGlyph
	case world.CellGoal:
		return goalGlyph
	case world.CellEntrance:
		return entranceGlyph
	}
	heat := w.HeatAt(x, y)
	stench := w.StenchAt(x, y)
	switch {
	case heat && stench:
		return stenchHeatGl
	case heat:
		return heatGlyph
	case stench:
		return stenchGlyph
	case w.ScentOwner[y][x] != 0:
		switch w.ScentOwner[y][x] {
		case '1':
			return scent1Glyph
		case '2':
			return scent2Glyph
		case '3':
			return scent3Glyph
		case '4':
			return scent4Glyph
		case '5':
			return scent5Glyph
		case '6':
			return scent6Glyph
		case '7':
			return scent7Glyph
		case '8':
			return scent8Glyph
		case '9':
			return scent9Glyph
		case 'A':
			return scentAGlyph
		case 'B':
			return scentBGlyph
		case 'C':
			return scentCGlyph
		}
		return pathGlyph
	default:
		return pathGlyph
	}
}
