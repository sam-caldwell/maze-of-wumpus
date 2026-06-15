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
	goalStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Background(lipgloss.Color("226")).Bold(true)
	entranceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	ghostStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	scent1Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	scent2Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	scent3Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	scent4Style    = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
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
	goalGlyph     = goalStyle.Render("G")
	scent1Glyph   = scent1Style.Render("~")
	scent2Glyph   = scent2Style.Render("~")
	scent3Glyph   = scent3Style.Render("~")
	scent4Glyph   = scent4Style.Render("~")
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

	// sim, when non-nil, is the execution backend that owns the live
	// world and advances it on background goroutines (live app): the
	// serial SimLoop or the worker-per-agent ParallelLoop. The Model
	// only ever publishes view intent, posts commands, and reads
	// frames through it. When nil (tests), the Model steps the world
	// synchronously in Update — deterministic, single-goroutine.
	sim driver

	// Terminal dims learned from tea.WindowSizeMsg; zero before the
	// first resize event (e.g. unit tests that never send one). When
	// zero, the renderer falls back to showing the whole board.
	termW, termH int
	// Maze viewport top-left corner in board coordinates. Arrow keys
	// bump these by one cell; clamped to keep the viewport inside
	// [0, BoardWidth] × [0, BoardHeight]. Reset to (0, 0) on reseed.
	offsetX, offsetY int

	// lastRightW caches the right-pane width from the most recent
	// published frame (async mode only). Scroll clamping / paging need
	// it to size the maze viewport, and in async mode the UI must not
	// read the world to measure it — so it rides along on each frame and
	// is refreshed on tick. Zero until the first frame; effectiveRightW
	// falls back to defaultRightW until then.
	lastRightW int

	// paused: in sync (test) mode, gates the inline tick step. In async/
	// live mode the driver owns the real pause state; this copy is only
	// set when a frame is rendered (so the header can show "[PAUSED]").
	paused bool
}

// defaultRightW is the assumed right-pane width before the first frame
// arrives (async mode). Only affects scroll clamping for the brief pre-
// frame window; corrected the moment a real frame is published.
const defaultRightW = 40

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
	sl := NewSimLoop(w, builder, tickInterval)
	sl.togglePause() // start paused — agents wait for <space>
	return Model{World: w, build: builder, sim: sl}
}

// NewParallelModel constructs a Model backed by a ParallelLoop — the live-
// app mode where each agent runs on its own worker goroutine and the maze
// is watched while they navigate it in parallel at their own rates.
func NewParallelModel(seed int64, builder WorldBuilder) Model {
	if builder == nil {
		builder = world.NewWorld
	}
	w := builder(seed)
	pl := NewParallelLoop(w)
	pl.togglePause() // start paused — agents wait for <space>
	return Model{World: w, build: builder, sim: pl}
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

// Update handles keyboard / tick messages. In sync (test) mode the world
// is stepped and mutated inline on this one goroutine. In async mode the
// UI never touches the world: it publishes its view intent and posts any
// world mutation to the SimLoop, which applies it on the sim goroutine.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW = msg.Width
		m.termH = msg.Height
		m.clampOffsets()
		m.publishView()
		return m, nil
	case tea.KeyMsg:
		s := msg.String()
		if s == "q" || s == "ctrl+c" {
			if m.sim != nil {
				m.sim.Stop()
			}
			return m, tea.Quit
		}
		if s == " " || s == "space" {
			// Space toggles play/pause. The driver owns the real pause
			// state in live mode; sync (test) mode gates its inline step.
			if m.sim != nil {
				m.sim.togglePause()
			} else {
				m.paused = !m.paused
			}
			return m, nil
		}
		if m.sim == nil {
			m.keySwitch(s) // sync: mutate the world directly
		} else {
			m.handleKeyAsync(s) // async: local view state + posted commands
			m.publishView()
		}
		return m, nil
	case tickMsg:
		if m.sim == nil {
			// Synchronous (test) mode: step inline (unless paused),
			// auto-reseed on solve.
			if !m.paused {
				m.World.Step()
				if m.World.MazeSolved() {
					_, _ = m.World.WriteStatsLog(StatsDir)
					m.reseedPreservingLearning()
				}
			}
			return m, tickEvery(tickInterval)
		}
		// Async mode: the backend advances the world. If it has solved the
		// maze, auto-reseed (same as pressing 'r') — the parallel engine
		// can't reseed itself from inside its barrier, so the UI drives it.
		if m.sim.needsReseed() {
			m.offsetX, m.offsetY = 0, 0
			m.sim.reseed(m.build)
		}
		// Refresh the cached right-pane width from the latest frame (so
		// scroll clamping/paging stays accurate without reading the world)
		// and re-arm.
		if f := m.sim.latestFrame(); f != nil {
			m.lastRightW = f.rightW
		}
		return m, tickEvery(renderInterval)
	}
	return m, nil
}

// publishView hands the UI's current scroll/size/overlay intent to the
// SimLoop so it renders the viewport the user is looking at. No-op in
// sync mode (no SimLoop).
func (m *Model) publishView() {
	if m.sim == nil {
		return
	}
	m.sim.publishView(&viewState{
		offsetX:  m.offsetX,
		offsetY:  m.offsetY,
		termW:    m.termW,
		termH:    m.termH,
		showPath: m.ShowPath,
	})
}

// applyViewKey handles the UI-local keys — viewport scrolling/paging and
// the shortest-path overlay toggle — that mutate only Model display
// state, never the world. Returns true if the key was a view key. Shared
// by sync (keySwitch) and async (handleKeyAsync) so the two paths can
// never diverge on scrolling behavior.
func (m *Model) applyViewKey(s string) bool {
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
	case "s":
		m.ShowPath = !m.ShowPath
	default:
		return false
	}
	return true
}

// keySwitch applies a non-quit keypress in SYNC mode: view keys via
// applyViewKey, then world-mutating keys (reseed / TTL / agent toggles)
// directly on m.World (single goroutine, no lock).
func (m *Model) keySwitch(s string) {
	if m.applyViewKey(s) {
		return
	}
	switch s {
	case "r":
		m.reseedPreservingLearning()
	case "t":
		m.World.TTLDisabled = !m.World.TTLDisabled
	case "1", "2", "3", "4", "5":
		if a := m.World.AgentByLabel(rune(s[0])); a != nil {
			a.Disabled = !a.Disabled
		}
	}
}

// handleKeyAsync applies a non-quit keypress in ASYNC mode: view keys
// update local Model state; world-mutating keys are posted to the
// SimLoop as commands so the world is only ever touched on the sim
// goroutine. Reseed also resets the UI's scroll offset (display state).
func (m *Model) handleKeyAsync(s string) {
	if m.applyViewKey(s) {
		return
	}
	switch s {
	case "r":
		m.offsetX, m.offsetY = 0, 0
		m.sim.reseed(m.build)
	case "t":
		m.sim.post(func(w *world.World) *world.World {
			w.TTLDisabled = !w.TTLDisabled
			return nil
		})
	case "1", "2", "3", "4", "5":
		label := rune(s[0])
		m.sim.post(func(w *world.World) *world.World {
			if a := w.AgentByLabel(label); a != nil {
				a.Disabled = !a.Disabled
			}
			return nil
		})
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
	// Async mode: display the most recently published frame, composed
	// entirely by the sim goroutine. The UI never touches the world here
	// — no lock, and a slow Step() can't stall the repaint.
	if m.sim != nil {
		if f := m.sim.latestFrame(); f != nil {
			return f.text
		}
		return startingView() // before the first frame / terminal size
	}
	// Sync (test) mode: render live on this single goroutine.
	screen, _ := m.composeScreen()
	return screen
}

// startingView is the placeholder shown in async mode until the sim has
// published its first frame (i.e. until the UI has reported a terminal
// size). Kept minimal so it costs nothing to render repeatedly.
func startingView() string {
	return titleStyle.Render("Maze of Wumpus") + "\n\nstarting…"
}

// composeScreen renders the full TUI — header, maze viewport, right pane,
// bottom pane — from m's world and viewport state, returning the screen
// text and the measured right-pane width. It is the single source of
// truth for layout: sync mode calls it directly in View, and the sim
// goroutine calls it (via a throwaway Model) to produce published
// frames. The rightW return rides along on the frame so the async UI can
// size/clamp its viewport without reading the world.
func (m Model) composeScreen() (string, int) {
	right := m.renderRightPane()
	rightW := paneWidth(right)
	mazeW, mazeH := m.mazeViewSize(rightW)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderMazePane(mazeW, mazeH),
		"  ",
		right,
	)
	screen := lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		body,
		m.renderBottomPane(),
	)
	return screen, rightW
}

// renderHeader is the top line: title, GOALS banner (when any agent
// has reached a goal), and the current seed.
func (m Model) renderHeader() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Maze of Wumpus"))
	if m.paused {
		b.WriteString("  ")
		b.WriteString(ttlWarnStyle.Render("[PAUSED — press space to play]"))
	}
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
		fmt.Sprintf("Cycle %5d | Paths: %s | ttl:%s\n[space] play/pause [q]uit [r]eseed [s]how-path [t]tl [↑↓←→] scroll [pgup/pgdn,⇧←→] page [1..9 a..c] agent",
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
	return m.mazeViewSize(m.effectiveRightW())
}

// effectiveRightW reports the right-pane width used to size the maze
// viewport. Sync mode measures it live from the world. Async mode must
// not read the world from the UI goroutine, so it uses the width carried
// on the latest frame (cached as lastRightW), falling back to
// defaultRightW until the first frame lands.
func (m Model) effectiveRightW() int {
	if m.sim != nil {
		if m.lastRightW > 0 {
			return m.lastRightW
		}
		return defaultRightW
	}
	return paneWidth(m.renderRightPane())
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

// lastScoreTier classifies the most recent solve SCORE against the
// agent's running min/avg/max — higher is better (the inverse of
// lastSolveTier's time semantics):
//
//	0 = green   (≥ max — best or tied-best)
//	1 = yellow  (≥ avg)
//	2 = orange  (≥ min)
//	3 = red     (< min)
//
// Returns -1 when the agent has not solved yet (solves == 0).
func lastScoreTier(last, min, avg, max float64, solves int) int {
	if solves <= 0 {
		return -1
	}
	switch {
	case last >= max:
		return 0
	case last >= avg:
		return 1
	case last >= min:
		return 2
	default:
		return 3
	}
}

func (m Model) formatAgentStats(a *world.Agent) string {
	alive := "alive   "
	if !a.Alive {
		alive = "dead    "
	}
	// Per-agent TTL ceiling: the agent's best solve distance once it has
	// reached the goal, else the exploration window. Drives the dist
	// color-severity heuristic and the printed TTL: column.
	agentTTL := m.World.TTLCeiling(a)
	distText := fmt.Sprintf("dist:%04d", a.Stats.ActualDistance)
	switch distSeverity(a.Stats.ActualDistance, agentTTL) {
	case 2:
		distText = ttlDangerStyle.Render(distText)
	case 1:
		distText = ttlWarnStyle.Render(distText)
	}
	agentTTLText := fmt.Sprintf("TTL:%04d", agentTTL)
	// Most-recent solve score, colored by its rank vs the running
	// min/avg/max (higher is better).
	lastScoreText := fmt.Sprintf("%.3f", a.Stats.LastScore)
	switch lastScoreTier(a.Stats.LastScore, a.Stats.MinScore,
		a.Stats.AvgScore, a.Stats.MaxScore, a.Stats.GoalsReached) {
	case 0:
		lastScoreText = solveGreen.Render(lastScoreText)
	case 1:
		lastScoreText = solveYellow.Render(lastScoreText)
	case 2:
		lastScoreText = solveOrange.Render(lastScoreText)
	case 3:
		lastScoreText = solveRed.Render(lastScoreText)
	}
	strLetter := "-"
	if a.CurrentStrategy != 0 {
		strLetter = string(a.CurrentStrategy)
	}
	// s = starts (#runs), f = fails (deaths), g = goals. All three reset
	// to 0 on a fresh maze (reseed builds new agents), so a new map starts
	// the f column at 0 as expected.
	return fmt.Sprintf(
		" %c %s str:%s s:%03d f:%03d g:%03d %s rt:%04.0f %s best:%04d/%04d s[min/avg/max/last]:%.3f/%.3f/%.3f/%s score:%.3f",
		a.Label, alive, strLetter,
		a.Stats.Starts, a.Stats.Deaths,
		a.Stats.GoalsReached,
		distText, a.StepsPerSec, agentTTLText,
		a.Stats.BestSolveDistance, a.Stats.BestSolveTime,
		a.Stats.MinScore, a.Stats.AvgScore, a.Stats.MaxScore, lastScoreText,
		a.Stats.Score(a.OptimalDistance),
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
	// Scent trails render only while still within their lifetime — a
	// deposit older than ScentMaxAge cycles has been "removed" and the
	// cell renders as plain path. Re-walking the cell resets its age.
	switch {
	case w.ScentOwner[y][x] != 0 && w.ScentFreshness(x, y) > 0:
		switch w.ScentOwner[y][x] {
		case '1':
			return scent1Glyph
		case '2':
			return scent2Glyph
		case '3':
			return scent3Glyph
		case '4':
			return scent4Glyph
		}
		return pathGlyph
	default:
		return pathGlyph
	}
}
