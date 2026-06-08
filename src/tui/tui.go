// tui.go — bubbletea Model + renderer for Maze of Wumpus.
//
// Five agents (labels '1'..'5') share the board, each colored distinctly:
//
//	1 — blue    (BFS benchmark)
//	2 — cyan    (Bayesian)
//	3 — magenta (swarm-Bayesian)
//	4 — green   (POMCP)
//	5 — yellow  (QMDP)
//
// The goal is green on yellow; the entrance is cyan. Walls are grey
// blocks, paths are dim dots.
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
	goalStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Background(lipgloss.Color("226")).Bold(true)
	entranceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	ghostStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	scent1Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	scent2Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	scent3Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	scent4Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	scent5Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
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
	goalGlyph     = goalStyle.Render("G")
	scent1Glyph   = scent1Style.Render("~")
	scent2Glyph   = scent2Style.Render("~")
	scent3Glyph   = scent3Style.Render("~")
	scent4Glyph   = scent4Style.Render("~")
	scent5Glyph   = scent5Style.Render("~")
	entranceGlyph = entranceStyle.Render("S") // generic fallback (no per-agent claim)

	// agentEntranceGlyph maps an agent label to a "white agent-rune
	// on the agent's color background" glyph (e.g., entrance for
	// agent 1 renders as a white "1" on a blue square). The agent's
	// own identifier on the doorway makes the home-door of each
	// agent unmistakable. Built from the same 256-color codes
	// used by the agent-N styles.
	agentEntranceColors = map[rune]string{
		'1': "39",  // bright blue
		'2': "208", // orange
		'3': "129", // purple
		'4': "199", // pink-magenta
		'5': "46",  // bright green
	}
	agentEntranceGlyph = func() map[rune]string {
		out := map[rune]string{}
		for label, bg := range agentEntranceColors {
			// Direct ANSI: bold + fg 255 (white) + bg <agent color>.
			// Bypasses lipgloss color-profile auto-strip so the bg
			// always renders in test and headless contexts.
			out[label] = fmt.Sprintf("\x1b[1;38;5;255;48;5;%sm%c\x1b[0m", bg, label)
		}
		return out
	}()
	ghostGlyph = ghostStyle.Render("◌")
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

	// sim, when non-nil, owns the live world and advances it on a
	// background goroutine (live app). All world access then goes
	// through sim's lock. When nil (tests), the Model steps the world
	// synchronously in Update — deterministic, single-goroutine.
	sim *SimLoop

	// Terminal dims learned from tea.WindowSizeMsg; zero before the
	// first resize event (e.g. unit tests that never send one). When
	// zero, the renderer falls back to showing the whole board.
	termW, termH int
	// Maze viewport top-left corner in board coordinates. Arrow keys
	// bump these by one cell; clamped to keep the viewport inside
	// [0, BoardWidth] × [0, BoardHeight]. Reset to (0, 0) on reseed.
	offsetX, offsetY int
}

// NewModel constructs a SYNCHRONOUS Model: the world steps inline in
// Update. Used by tests (deterministic, no goroutine). `builder` turns
// a seed into a fully-configured *world.World.
func NewModel(seed int64, builder WorldBuilder) Model {
	if builder == nil {
		builder = world.NewWorld
	}
	return Model{World: builder(seed), build: builder}
}

// NewAsyncModel constructs a Model backed by a background SimLoop — the
// live-app mode that decouples simulation from rendering.
func NewAsyncModel(seed int64, builder WorldBuilder) Model {
	if builder == nil {
		builder = world.NewWorld
	}
	w := builder(seed)
	return Model{World: w, build: builder, sim: NewSimLoop(w, builder, tickInterval)}
}

// Init returns the first repaint command, and (async mode) starts the
// background simulation goroutine.
func (m Model) Init() tea.Cmd {
	if m.sim != nil {
		m.sim.Start()
		return tickEvery(renderInterval)
	}
	return tickEvery(tickInterval)
}

// Update handles keyboard / tick messages. In async mode all world
// access is serialized through the SimLoop's lock; in sync mode it's
// direct (single goroutine).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW = msg.Width
		m.termH = msg.Height
		// clampOffsets reads the world (right-pane width), so take the
		// read lock in async mode.
		m.withWorldRead(func() { m.clampOffsets() })
		return m, nil
	case tea.KeyMsg:
		if s := msg.String(); s == "q" || s == "ctrl+c" {
			if m.sim != nil {
				m.sim.Stop()
			}
			return m, tea.Quit
		}
		// keySwitch both reads (scroll sizing) and mutates (toggles /
		// reseed) the world, so run it under the write lock in async
		// mode; the lock also adopts a UI-driven reseed's new world.
		m.withWorldWrite(func() { m.keySwitch(msg.String()) })
		return m, nil
	case tickMsg:
		if m.sim == nil {
			// Synchronous (test) mode: step inline, auto-reseed on solve.
			m.World.Step()
			if m.World.MazeSolved() {
				_, _ = m.World.WriteStatsLog(StatsDir)
				m.reseedPreservingLearning()
			}
			return m, tickEvery(tickInterval)
		}
		// Async mode: the SimLoop goroutine advances the world; this
		// tick only re-arms the repaint.
		return m, tickEvery(renderInterval)
	}
	return m, nil
}

// withWorldRead runs fn with m.World pointed at the live world under a
// read lock (async), or directly (sync). For read-only world access
// that may mutate Model UI state (offsets).
func (m *Model) withWorldRead(fn func()) {
	if m.sim == nil {
		fn()
		return
	}
	m.sim.mu.RLock()
	defer m.sim.mu.RUnlock()
	m.World = m.sim.world
	fn()
}

// withWorldWrite runs fn under the write lock with m.World pointed at
// the live world, then publishes m.World back to the loop so a reseed
// performed inside fn swaps the live world atomically.
func (m *Model) withWorldWrite(fn func()) {
	if m.sim == nil {
		fn()
		return
	}
	m.sim.mu.Lock()
	defer m.sim.mu.Unlock()
	m.World = m.sim.world
	fn()
	m.sim.world = m.World
}

// keySwitch applies a non-quit keypress: viewport scrolling, overlay /
// hazard / agent toggles, and reseed. Mutates m (offsets, ShowPath)
// and m.World; callers handle any locking.
func (m *Model) keySwitch(s string) {
	switch s {
	case "up":
		m.offsetY--
		m.clampOffsets()
	case "down":
		m.offsetY++
		m.clampOffsets()
	case "left":
		m.offsetX--
		m.clampOffsets()
	case "right":
		m.offsetX++
		m.clampOffsets()
	case "shift+up", "pgup":
		// PgUp/PgDn are the reliable vertical-page keys: many terminals
		// intercept shift+↑/↓ (line-select / scrollback) and forward a
		// bare ↑/↓, so shift alone can't be trusted for vertical paging.
		// shift+↑/↓ stay bound for terminals that do forward them.
		_, viewH := m.currentViewSize()
		m.offsetY -= viewH
		m.clampOffsets()
	case "shift+down", "pgdown":
		_, viewH := m.currentViewSize()
		m.offsetY += viewH
		m.clampOffsets()
	case "shift+left":
		viewW, _ := m.currentViewSize()
		m.offsetX -= viewW
		m.clampOffsets()
	case "shift+right":
		viewW, _ := m.currentViewSize()
		m.offsetX += viewW
		m.clampOffsets()
	case "r":
		m.reseedPreservingLearning()
	case "s":
		m.ShowPath = !m.ShowPath
	case "t":
		m.World.TTLDisabled = !m.World.TTLDisabled
	case "1", "2", "3", "4", "5":
		if a := m.World.AgentByLabel(rune(s[0])); a != nil {
			a.Disabled = !a.Disabled
		}
	}
}

// StatsDir is the directory under which maze-solved JSON snapshots
// land. Default lives next to the build artifacts so `make clean`
// also wipes them.
const StatsDir = "build/stats"

// reseedPreservingLearning constructs a fresh world via m.build and
// grafts each agent's persistent learning state (Beliefs /
// TrustScores) from the prior world onto the new agents.
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
	m.World = reseedWorldPreservingLearning(m.World, m.build)
	m.offsetX, m.offsetY = 0, 0
}

// reseedWorldPreservingLearning builds a fresh world and grafts each
// agent's persistent learning state (Beliefs / TrustScores /
// LearnedTTL) from `prev` onto the new agents. Shared by the Model's
// reseed path and the SimLoop's auto-reseed-on-solve so both keep
// learning across maps identically.
func reseedWorldPreservingLearning(prev *world.World, build WorldBuilder) *world.World {
	prevAgents := prev.Agents
	nw := build(time.Now().UnixNano())
	for i, oldA := range prevAgents {
		if i >= len(nw.Agents) {
			break
		}
		newA := nw.Agents[i]
		if oldA.Beliefs != nil {
			newA.Beliefs = oldA.Beliefs
		}
		if oldA.TrustScores != nil {
			newA.TrustScores = oldA.TrustScores
		}
		// Carry the prior map's LearnedTTL forward as a starting
		// belief for the new map. The invalidation rule in
		// MoveAgents drops it if the new map's TTL is larger.
		newA.LearnedTTL = oldA.LearnedTTL
	}
	return nw
}

// View composes the four panes — header, maze (scrollable), right
// (trust matrix + info), bottom (per-agent stats + status / keys) —
// into a single frame. Only the maze pane scrolls; the others are
// pure projections of world state.
func (m Model) View() string {
	// Async mode: stat panes come from the decoupled aggregator (no
	// world access on the render path); only the maze viewport reads
	// the live world, briefly, under the read lock.
	if m.sim != nil {
		if frame := m.sim.stats.Latest(); frame != nil {
			m.sim.mu.RLock()
			m.World = m.sim.world
			mazeW, mazeH := m.mazeViewSize(paneWidth(frame.right()))
			maze := m.renderMazePane(mazeW, mazeH)
			m.sim.mu.RUnlock()
			body := lipgloss.JoinHorizontal(lipgloss.Top, maze, "  ", frame.right())
			return lipgloss.JoinVertical(lipgloss.Left, frame.header, body, frame.bottom)
		}
		// Before the first published frame: render everything live.
		m.sim.mu.RLock()
		defer m.sim.mu.RUnlock()
		m.World = m.sim.world
	}
	right := m.renderRightPane()
	rightW := paneWidth(right)
	mazeW, mazeH := m.mazeViewSize(rightW)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderMazePane(mazeW, mazeH),
		"  ",
		right,
	)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		body,
		m.renderBottomPane(),
	)
}

// renderHeader is the top line: title, GOALS banner (when any agent
// has reached a goal), and the current seed.
func (m Model) renderHeader() string {
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
	return b.String()
}

// renderMazePane emits exactly viewH lines of viewW cells starting at
// (offsetX, offsetY) in board coordinates. The ShowPath highlight is
// applied per-cell as it was in the pre-pane View().
func (m Model) renderMazePane(viewW, viewH int) string {
	var b strings.Builder
	for row := 0; row < viewH; row++ {
		y := m.offsetY + row
		if y >= world.BoardHeight {
			break
		}
		for col := 0; col < viewW; col++ {
			x := m.offsetX + col
			if x >= world.BoardWidth {
				break
			}
			g := m.glyphAt(m.World, x, y)
			if m.ShowPath && m.World.ShortestPathCells[world.Pos{X: x, Y: y}] {
				g = "\x1b[43m" + g + "\x1b[49m"
			}
			b.WriteString(g)
		}
		if row < viewH-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderRightPane is the trust matrix + companion info, rendered as
// an isolated column. Pure projection of world state; never scrolls.
func (m Model) renderRightPane() string {
	return strings.Join(renderTrustMatrixLines(m.World), "\n")
}

// renderBottomPane is the per-agent stats feed plus the global status
// / keybindings footer. Rendered as a single block below the maze and
// right panes.
func (m Model) renderBottomPane() string {
	var b strings.Builder
	for _, a := range m.World.Agents {
		b.WriteString(statStyle.Render(m.formatAgentStats(a)))
		b.WriteByte('\n')
	}
	pathsStr := fmt.Sprintf("%d", m.World.Stats.ShortestPaths)
	if m.World.Stats.ShortestPaths >= world.MaxShortestPathsCount {
		pathsStr = fmt.Sprintf("%d+", world.MaxShortestPathsCount)
	}
	ttlState := "on"
	if m.World.TTLDisabled {
		ttlState = "OFF"
	}
	b.WriteString(statStyle.Render(
		fmt.Sprintf("Cycle %5d | Paths: %s | ttl:%s\n[q]uit [r]eseed [s]how-path [t]tl [↑↓←→] scroll [pgup/pgdn,⇧←→] page [1..9 a..c] agent",
			m.World.Cycle,
			pathsStr, ttlState),
	))
	return b.String()
}

// paneWidth returns the visible width (ANSI-stripped) of the widest
// line in `s`. Used to size the right pane so the maze viewport can
// claim whatever terminal width remains.
func paneWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
		}
	}
	return max
}

// mazeViewSize computes the maze viewport size from the terminal
// dimensions, leaving room for the right pane (rightW + 2-col gutter)
// horizontally and for the header + bottom pane vertically. Falls
// back to the full board when the terminal size is unknown (the
// pre-resize state, hit by tests that never send a WindowSizeMsg).
func (m Model) mazeViewSize(rightW int) (w, h int) {
	if m.termW == 0 || m.termH == 0 {
		return world.BoardWidth, world.BoardHeight
	}
	const gutter = 2
	bottomH := len(m.World.Agents) + 2 // agent rows + 2-line footer
	headerH := 1
	w = m.termW - rightW - gutter
	h = m.termH - headerH - bottomH
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w > world.BoardWidth {
		w = world.BoardWidth
	}
	if h > world.BoardHeight {
		h = world.BoardHeight
	}
	return
}

// currentViewSize returns the maze viewport's (width, height) in
// cells for the current terminal size. Wraps the right-pane width
// measurement + mazeViewSize so the arrow-key handlers and
// clampOffsets share one source of truth for "how big is a page."
func (m Model) currentViewSize() (int, int) {
	rightW := paneWidth(m.renderRightPane())
	return m.mazeViewSize(rightW)
}

// clampOffsets keeps (offsetX, offsetY) within
// [0, BoardWidth-viewW] × [0, BoardHeight-viewH]. Called after every
// arrow-key bump and after a WindowSizeMsg (since shrinking the
// terminal may push a previously-valid offset out of range).
func (m *Model) clampOffsets() {
	viewW, viewH := m.currentViewSize()
	maxX := world.BoardWidth - viewW
	maxY := world.BoardHeight - viewH
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	if m.offsetX > maxX {
		m.offsetX = maxX
	}
	if m.offsetY > maxY {
		m.offsetY = maxY
	}
	if m.offsetX < 0 {
		m.offsetX = 0
	}
	if m.offsetY < 0 {
		m.offsetY = 0
	}
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
	// Per-agent TTL ceiling = TTLMultiplier × the agent's own
	// EntrancePos→GoalPos shortest path. Used both for the dist
	// color-severity heuristic and as a printed column.
	agentTTL := world.TTLMultiplier * a.OptimalDistance
	distText := fmt.Sprintf("dist:%04d", a.Stats.ActualDistance)
	switch distSeverity(a.Stats.ActualDistance, agentTTL) {
	case 2:
		distText = ttlDangerStyle.Render(distText)
	case 1:
		distText = ttlWarnStyle.Render(distText)
	}
	agentTTLText := fmt.Sprintf("TTL:%04d", agentTTL)
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
		" %c %s str:%s s:%03d f:%s ttl:%s d:%03d g:%03d %s %s best:%04d/%04d t[min/avg/max/last]:%04d/%07.1f/%04d/%s score:%.5f",
		a.Label, alive, strLetter,
		a.Stats.Starts, following, learnedTTL,
		a.Stats.Deaths, a.Stats.GoalsReached,
		distText, agentTTLText,
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

// swarmCloneGlyph maps an agent label to a "white * on the agent's
// color background" glyph. Used to render every swarm clone with a
// generic asterisk in its leader's identity color so the swarm
// reads at a glance as a coherent unit on the map without the
// glyph clashing with the leader's labeled glyph.
var swarmCloneGlyph = func() map[rune]string {
	out := map[rune]string{}
	for label, bg := range agentEntranceColors {
		out[label] = fmt.Sprintf("\x1b[1;38;5;255;48;5;%sm*\x1b[0m", bg)
	}
	return out
}()

// cellHasSwarmClone returns the owning leader's label and true if
// any alive swarm clone occupies cell (x, y). Linear-scan over the
// 5 agent slots × ≤10 clones per agent — at most 50 checks per
// cell. Acceptable for first-cut TUI rendering.
func cellHasSwarmClone(w *world.World, x, y int) (rune, bool) {
	for _, leader := range w.Agents {
		if leader.SwarmGroupID == 0 || len(leader.SwarmClones) == 0 {
			continue
		}
		for _, c := range leader.SwarmClones {
			if c == nil || !c.Alive {
				continue
			}
			if c.Pos.X == x && c.Pos.Y == y {
				return leader.Label, true
			}
		}
	}
	return 0, false
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
		}
	}
	// Swarm clones: each alive clone renders as a white "*" on the
	// leader's color background. Layered below leader-agent glyphs so
	// a leader standing on a clone's tile shows the leader; layered
	// above terrain glyphs so the swarm trail is visible against
	// walls/path.
	if leaderLabel, ok := cellHasSwarmClone(w, x, y); ok {
		if g := swarmCloneGlyph[leaderLabel]; g != "" {
			return g
		}
	}
	// Branch-animation ghosts (red) overlay everything below this
	// point, so the agent glyphs above still win.
	if cellIsGhost(w, x, y) {
		return ghostGlyph
	}
	cell := w.Maze.Cells[y][x]
	switch cell {
	case world.CellWall:
		return wallGlyph
	case world.CellGoal:
		return goalGlyph
	case world.CellEntrance:
		// Prefer the per-agent entrance glyph (white "S" on the
		// agent's color background). Falls back to the generic
		// cyan "S" when no agent claims this cell.
		pos := world.Pos{X: x, Y: y}
		for _, a := range w.Agents {
			if a.EntrancePos == pos {
				if g, ok := agentEntranceGlyph[a.Label]; ok {
					return g
				}
			}
		}
		return entranceGlyph
	}
	switch {
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
		}
		return pathGlyph
	default:
		return pathGlyph
	}
}
