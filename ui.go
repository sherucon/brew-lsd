package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Colour palette ────────────────────────────────────────────────────────────

var (
	clrLeaf   = lipgloss.Color("#4ade80")
	clrCask   = lipgloss.Color("#60a5fa")
	clrDep    = lipgloss.Color("#94a3b8")
	clrAccent = lipgloss.Color("#a855f7")
	clrSelBg  = lipgloss.Color("#6358f9ff")
	clrBorder = lipgloss.Color("#30363d")
	clrMuted  = lipgloss.Color("#6b7280")
	clrText   = lipgloss.Color("#e2e8f0")
	clrError  = lipgloss.Color("#f87171")
	clrTitle  = lipgloss.Color("#c084fc")
	clrGold   = lipgloss.Color("#fbbf24")
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(clrTitle).PaddingLeft(2)
	styleMuted = lipgloss.NewStyle().Foreground(clrMuted)
	styleHelp  = lipgloss.NewStyle().Foreground(clrMuted)
	styleError = lipgloss.NewStyle().Foreground(clrError)
	styleVer   = lipgloss.NewStyle().Foreground(clrGold)
	styleSec   = lipgloss.NewStyle().Bold(true).Foreground(clrAccent)
	styleDesc  = lipgloss.NewStyle().Foreground(clrMuted).Italic(true)
	styleSel   = lipgloss.NewStyle().Background(clrSelBg).Foreground(clrText)

	styleTab    = lipgloss.NewStyle().Padding(0, 2).Foreground(clrMuted)
	styleTabAct = lipgloss.NewStyle().Padding(0, 2).Foreground(clrAccent).Bold(true).Underline(true)

	badgeLeaf = lipgloss.NewStyle().Foreground(clrLeaf).Bold(true)
	badgeCask = lipgloss.NewStyle().Foreground(clrCask).Bold(true)
	badgeDep  = lipgloss.NewStyle().Foreground(clrDep)

	styleListLeaf = lipgloss.NewStyle().Foreground(clrLeaf)
	styleListCask = lipgloss.NewStyle().Foreground(clrCask)
	styleListDep  = lipgloss.NewStyle().Foreground(clrDep)
)

// ── App state machine ─────────────────────────────────────────────────────────

type appState int

const (
	stateLoading appState = iota
	stateList
	stateDetail
	stateConfirmUninstall
	stateUninstalling
	stateError
)

type tabID int

const (
	tabAll tabID = iota
	tabLeaves
	tabFormulas
	tabCasks
)

var tabLabels = []string{"All", "Leaves", "Formulas", "Casks"}

// ── Messages ──────────────────────────────────────────────────────────────────

type packagesLoadedMsg struct {
	packages []Package
	err      error
}

type uninstallDoneMsg struct{ err error }

// ── Model ─────────────────────────────────────────────────────────────────────

type model struct {
	state    appState
	err      error
	packages []Package // all packages
	filtered []Package // currently visible (after tab + search filter)
	cursor   int
	offset   int
	tab      tabID
	search   textinput.Model
	typing   bool // whether search input is focused
	detail   *Package
	spinner  spinner.Model
	width    int
	height   int
}

func newModel() model {
	ti := textinput.New()
	ti.Placeholder = "type to search…"
	ti.CharLimit = 80

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(clrAccent)

	return model{
		state:   stateLoading,
		search:  ti,
		spinner: sp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			pkgs, err := loadPackages()
			return packagesLoadedMsg{packages: pkgs, err: err}
		},
	)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case packagesLoadedMsg:
		if msg.err != nil {
			m.state = stateError
			m.err = msg.err
			return m, nil
		}
		m.packages = msg.packages
		m.filtered = msg.packages
		m.state = stateList
		m.detail = nil
		return m, nil

	case uninstallDoneMsg:
		if msg.err != nil {
			m.state = stateError
			m.err = msg.err
			return m, nil
		}
		// Reload package list after successful uninstall
		m.state = stateLoading
		return m, tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				pkgs, err := loadPackages()
				return packagesLoadedMsg{packages: pkgs, err: err}
			},
		)

	case spinner.TickMsg:
		if m.state == stateLoading || m.state == stateUninstalling {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.typing {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		m = m.applyFilter()
		return m, cmd
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ── Error screen ────────────────────────────────────────────────────────
	if m.state == stateError {
		if key == "q" || key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}

	// ── Detail screen ────────────────────────────────────────────────────────
	if m.state == stateDetail {
		switch key {
		case "esc", "q", "backspace":
			m.state = stateList
			m.detail = nil
		case "ctrl+c":
			return m, tea.Quit
		case "u", "U":
			m.state = stateConfirmUninstall
		}
		return m, nil
	}

	// ── Confirm uninstall screen ──────────────────────────────────────────────
	if m.state == stateConfirmUninstall {
		switch key {
		case "y", "Y":
			pkg := m.detail // capture for closure
			m.state = stateUninstalling
			return m, tea.Batch(
				m.spinner.Tick,
				func() tea.Msg {
					err := uninstallPackage(pkg.Name, pkg.IsCask)
					return uninstallDoneMsg{err: err}
				},
			)
		case "n", "N", "esc", "backspace":
			m.state = stateDetail
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	// ── Uninstalling screen (no input accepted) ───────────────────────────────
	if m.state == stateUninstalling {
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}

	// ── List screen ──────────────────────────────────────────────────────────
	switch key {
	case "ctrl+c":
		return m, tea.Quit

	case "q":
		if m.typing {
			// 'q' is a valid search character, forward to input
			break
		}
		return m, tea.Quit

	case "esc":
		if m.typing {
			m.typing = false
			m.search.Blur()
			m.search.SetValue("")
			m = m.applyFilter()
		}
		return m, nil

	case "/":
		if !m.typing {
			m.typing = true
			m.search.Focus()
			return m, textinput.Blink
		}

	case "enter":
		if m.typing {
			m.typing = false
			m.search.Blur()
			return m, nil
		}
		if len(m.filtered) > 0 {
			pkg := m.filtered[m.cursor]
			m.detail = &pkg
			m.state = stateDetail
		}
		return m, nil

	case "up", "k":
		if !m.typing && m.cursor > 0 {
			m.cursor--
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		}
		return m, nil

	case "down", "j":
		if !m.typing && m.cursor < len(m.filtered)-1 {
			m.cursor++
			lh := m.listHeight()
			if m.cursor >= m.offset+lh {
				m.offset = m.cursor - lh + 1
			}
		}
		return m, nil

	case "tab":
		if !m.typing {
			m.tab = (m.tab + 1) % 4
			m.cursor = 0
			m.offset = 0
			m = m.applyFilter()
		}

	case "shift+tab":
		if !m.typing {
			if m.tab == 0 {
				m.tab = 3
			} else {
				m.tab--
			}
			m.cursor = 0
			m.offset = 0
			m = m.applyFilter()
		}
	}

	// Forward remaining keystrokes to search input while typing
	if m.typing {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		m = m.applyFilter()
		return m, cmd
	}

	return m, nil
}

func (m model) applyFilter() model {
	query := strings.ToLower(m.search.Value())

	var out []Package
	for _, p := range m.packages {
		switch m.tab {
		case tabLeaves:
			if !p.IsLeaf {
				continue
			}
		case tabFormulas:
			if p.IsCask {
				continue
			}
		case tabCasks:
			if !p.IsCask {
				continue
			}
		}
		if query != "" {
			if !strings.Contains(strings.ToLower(p.Name), query) &&
				!strings.Contains(strings.ToLower(p.Desc), query) {
				continue
			}
		}
		out = append(out, p)
	}
	m.filtered = out
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	return m
}

// listHeight returns the number of rows available for the package list.
func (m model) listHeight() int {
	// header(3) + divider + tabs(1) + search(1) + divider + footer(2) = ~9
	return m.height - 9
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.state {
	case stateLoading:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("%s  Loading Homebrew data…\n\n%s",
				m.spinner.View(),
				styleMuted.Render("Running brew commands, this may take a moment"),
			),
		)
	case stateError:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("❌  %s\n\n%s",
				styleError.Render(m.err.Error()),
				styleHelp.Render("Press q to quit"),
			),
		)
	case stateDetail:
		return m.detailView()
	case stateConfirmUninstall:
		return m.confirmView()
	case stateUninstalling:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("%s  Uninstalling %s…",
				m.spinner.View(),
				styleTitle.Render(m.detail.Name),
			),
		)
	default:
		return m.listView()
	}
}

// ── Confirm uninstall view ────────────────────────────────────────────────────

func (m model) confirmView() string {
	p := m.detail
	w := m.width
	var b strings.Builder

	b.WriteString(styleTitle.Render("🍺  "+p.Name) + "\n")
	b.WriteString(strings.Repeat("─", w) + "\n\n")

	// Warning box
	warningStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(clrError).
		Padding(1, 3).
		Width(w - 6)

	var warningLines []string
	warningLines = append(warningLines, styleError.Bold(true).Render("⚠️   Uninstall "+p.Name+"?"))
	warningLines = append(warningLines, "")
	if p.IsCask {
		warningLines = append(warningLines, styleMuted.Render("This will remove the cask and its associated files."))
	} else if len(p.RequiredBy) > 0 {
		warningLines = append(warningLines,
			styleError.Render(fmt.Sprintf("⚡  %d package(s) depend on this formula:", len(p.RequiredBy))),
		)
		for _, dep := range p.RequiredBy {
			warningLines = append(warningLines, "    "+lipgloss.NewStyle().Foreground(clrCask).Render(dep))
		}
		warningLines = append(warningLines, "")
		warningLines = append(warningLines, styleMuted.Render("Brew may refuse to uninstall unless you use --ignore-dependencies."))
	} else {
		warningLines = append(warningLines, styleMuted.Render("Nothing depends on this — it is safe to remove."))
	}
	warningLines = append(warningLines, "")
	warningLines = append(warningLines,
		lipgloss.NewStyle().Foreground(clrLeaf).Bold(true).Render("  y  ")+"confirm"+
			"    "+
			lipgloss.NewStyle().Foreground(clrError).Bold(true).Render("  n / esc  ")+"cancel",
	)

	b.WriteString("  " + warningStyle.Render(strings.Join(warningLines, "\n")) + "\n")

	return b.String()
}

// ── List view ─────────────────────────────────────────────────────────────────

func (m model) listView() string {
	w := m.width
	var b strings.Builder

	// ── Header ─────────────────────────────────────────────────────────────
	nFormulas, nCasks, nLeaves := 0, 0, 0
	for _, p := range m.packages {
		if p.IsCask {
			nCasks++
		} else {
			nFormulas++
			if p.IsLeaf {
				nLeaves++
			}
		}
	}
	b.WriteString(styleTitle.Render("🍺  Brew Search") + "\n")
	b.WriteString(styleMuted.PaddingLeft(2).Render(fmt.Sprintf(
		"%d formulas · %d casks · %s leaves",
		nFormulas, nCasks,
		lipgloss.NewStyle().Foreground(clrLeaf).Render(fmt.Sprintf("%d", nLeaves)),
	)) + "\n")
	b.WriteString(strings.Repeat("─", w) + "\n")

	// ── Tab bar ────────────────────────────────────────────────────────────
	tabCounts := [4]int{len(m.packages), 0, 0, 0}
	for _, p := range m.packages {
		if p.IsLeaf {
			tabCounts[tabLeaves]++
		}
		if !p.IsCask {
			tabCounts[tabFormulas]++
		} else {
			tabCounts[tabCasks]++
		}
	}
	var tabs []string
	for i, label := range tabLabels {
		text := fmt.Sprintf("%s (%d)", label, tabCounts[i])
		if tabID(i) == m.tab {
			tabs = append(tabs, styleTabAct.Render(text))
		} else {
			tabs = append(tabs, styleTab.Render(text))
		}
	}
	b.WriteString(strings.Join(tabs, "") + "\n")

	// ── Search bar ─────────────────────────────────────────────────────────
	prefix := styleHelp.Render("  / ")
	if m.typing {
		prefix = lipgloss.NewStyle().Foreground(clrAccent).Render("  / ")
	}
	b.WriteString(prefix + m.search.View() + "\n")
	b.WriteString(strings.Repeat("─", w) + "\n")

	// ── Package list ───────────────────────────────────────────────────────
	lh := m.listHeight()
	nameW := 22
	verW := 10
	// badge is 4 chars + 2 padding spaces = 6
	descW := w - nameW - verW - 8
	if descW < 10 {
		descW = 10
	}

	if len(m.filtered) == 0 {
		b.WriteString(styleMuted.PaddingLeft(4).Render("No packages match") + "\n")
	}

	for i := m.offset; i < m.offset+lh && i < len(m.filtered); i++ {
		p := m.filtered[i]
		sel := i == m.cursor

		// Badge (4 chars wide)
		var badge string
		if p.IsCask {
			badge = badgeCask.Render("CASK")
		} else if p.IsLeaf {
			badge = badgeLeaf.Render("LEAF")
		} else {
			badge = badgeDep.Render("DEP ")
		}

		// Name (styled by type)
		nameStr := truncPad(p.Name, nameW)
		var nameStyled string
		if p.IsCask {
			nameStyled = styleListCask.Render(nameStr)
		} else if p.IsLeaf {
			nameStyled = styleListLeaf.Render(nameStr)
		} else {
			nameStyled = styleListDep.Render(nameStr)
		}

		ver := p.Version
		actualVerLen := lipgloss.Width(ver)

		// Recalculate space for description so the row fits inside terminal width W.
		// Layout: "  " (2) + badge (4) + "  " (2) + name (22) + "  " (2) + desc + "  " (2) + ver
		// Total fixed characters = 34. We subtract 36 to leave 2 extra chars of slack
		// so that the total string length is W-2. This prevents lipgloss internal
		// wrapper from accidentally wrapping the version text to the next line.
		// styleSel.Width(w) will perfectly fill out the remaining gap with the background color.
		dynamicDescW := w - 36 - actualVerLen
		if dynamicDescW < 5 {
			dynamicDescW = 5
		}

		// Desc
		desc := p.Desc
		if desc == "" {
			desc = "—"
		}
		descStr := truncPad(desc, dynamicDescW)

		row := fmt.Sprintf("  %s  %s  %s  %s",
			badge, nameStyled, styleMuted.Render(descStr), styleVer.Render(ver))

		if sel {
			row = styleSel.Width(w).Render(row)
		}
		b.WriteString(row + "\n")
	}

	// Fill remaining list rows so the footer stays pinned
	filled := m.offset + lh
	if filled > len(m.filtered) {
		filled = len(m.filtered)
	}
	for i := filled - m.offset; i < lh; i++ {
		b.WriteString("\n")
	}

	// ── Footer ─────────────────────────────────────────────────────────────
	count := lipgloss.NewStyle().Foreground(clrAccent).Render(
		fmt.Sprintf("  %d / %d", len(m.filtered), len(m.packages)),
	)
	keys := styleHelp.Render("  ↑↓ / jk  navigate   tab  switch view   /  search   enter  details   q  quit")
	b.WriteString(strings.Repeat("─", w) + "\n")
	b.WriteString(keys + count)

	return b.String()
}

// ── Detail view ───────────────────────────────────────────────────────────────

func (m model) detailView() string {
	p := m.detail
	w := m.width
	var b strings.Builder

	// Header
	b.WriteString(styleTitle.Render("🍺  "+p.Name) + "\n")

	var typeTag string
	if p.IsCask {
		typeTag = badgeCask.Render("CASK")
	} else if p.IsLeaf {
		typeTag = badgeLeaf.Render("LEAF  ") + styleMuted.Render("not required by anything")
	} else {
		typeTag = badgeDep.Render("DEP   ") + styleMuted.Render(
			fmt.Sprintf("required by %d package(s)", len(p.RequiredBy)))
	}
	b.WriteString("  " + typeTag + "\n")

	if p.Version != "" {
		b.WriteString("  " + styleVer.Render("v"+p.Version) + "\n")
	}
	if p.Desc != "" {
		b.WriteString("  " + styleDesc.Render(p.Desc) + "\n")
	}
	b.WriteString(strings.Repeat("─", w) + "\n\n")

	// ── Depends on ─────────────────────────────────────────────────────────
	b.WriteString("  " + styleSec.Render(fmt.Sprintf("Depends on  (%d)", len(p.Deps))) + "\n\n")
	if len(p.Deps) == 0 {
		b.WriteString("  " + styleHelp.Render("(no dependencies)") + "\n")
	} else {
		for _, line := range wrapTokens(p.Deps, w-4, clrLeaf) {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n")

	// ── Required by ────────────────────────────────────────────────────────
	b.WriteString("  " + styleSec.Render(fmt.Sprintf("Required by  (%d)", len(p.RequiredBy))) + "\n\n")
	if len(p.RequiredBy) == 0 {
		if p.IsCask {
			b.WriteString("  " + styleHelp.Render("(casks are top-level installs)") + "\n")
		} else {
			b.WriteString("  " + badgeLeaf.Render("✨  Nothing depends on this — safe to remove if you don't need it") + "\n")
		}
	} else {
		for _, line := range wrapTokens(p.RequiredBy, w-4, clrCask) {
			b.WriteString("  " + line + "\n")
		}
	}

	// ── Footer ─────────────────────────────────────────────────────────────
	used := strings.Count(b.String(), "\n")
	for i := used; i < m.height-2; i++ {
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("─", w) + "\n")
	b.WriteString(styleHelp.Render("  esc  back to list   u  uninstall"))

	return b.String()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// truncPad truncates s to maxW runes, then pads with spaces to exactly maxW.
func truncPad(s string, maxW int) string {
	runes := []rune(s)
	if len(runes) > maxW {
		return string(runes[:maxW-1]) + "…"
	}
	return s + strings.Repeat(" ", maxW-len(runes))
}

// wrapTokens renders a list of package names as comma-separated colour tokens,
// word-wrapped to maxWidth.
func wrapTokens(tokens []string, maxWidth int, color lipgloss.Color) []string {
	tokenStyle := lipgloss.NewStyle().Foreground(color)
	sep := styleHelp.Render(", ")
	sepLen := 2

	var lines []string
	var cur strings.Builder
	lineLen := 0

	for i, t := range tokens {
		rawLen := utf8.RuneCountInString(t)
		if i < len(tokens)-1 {
			rawLen += sepLen
		}
		if lineLen > 0 && lineLen+rawLen > maxWidth {
			lines = append(lines, cur.String())
			cur.Reset()
			lineLen = 0
		}
		cur.WriteString(tokenStyle.Render(t))
		if i < len(tokens)-1 {
			cur.WriteString(sep)
		}
		lineLen += rawLen
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
