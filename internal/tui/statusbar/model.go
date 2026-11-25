package statusbar

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleBar = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorDim)

	styleConnected = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorGreen)

	styleDisconnected = lipgloss.NewStyle().
				Background(theme.ColorTabBg).
				Foreground(theme.ColorRed)

	styleAlertCount = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorYellow).
			Bold(true)

	styleSep = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorBorder)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleBar = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorDim)
	styleConnected = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorGreen)
	styleDisconnected = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorRed)
	styleAlertCount = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorYellow).
		Bold(true)
	styleSep = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorBorder)
}

type providerEntry struct {
	Name      string
	Connected bool
	Reason    string // why it is down; empty while connected
}

// Freshness thresholds for the data-age segment. Quotes older than
// staleAfter are drawn in the warning color and older than deadAfter in
// the error color, so a feed that has gone quiet stops looking live.
const (
	staleAfter = time.Minute
	deadAfter  = 15 * time.Minute
)

// maxReasonWidth caps a disconnect reason, which is an upstream error
// string of any length.
const maxReasonWidth = 32

// Model is the status bar component.
type Model struct {
	providers   []providerEntry
	lastUpdate  time.Time
	now         time.Time
	alertCount  int
	themeName   string
	searchQuery string
	width       int
}

// New creates a new status bar.
func New() Model {
	return Model{}
}

// SetWidth updates the status bar width.
func (m *Model) SetWidth(w int) {
	m.width = w
}

// SetProviderStatus updates the connection status of a provider. err is
// the reason a disconnected provider gave, or nil.
func (m *Model) SetProviderStatus(name string, connected bool, err error) {
	reason := ""
	if !connected && err != nil {
		reason = format.ErrorReason(err)
	}
	for i, p := range m.providers {
		if p.Name == name {
			m.providers[i].Connected = connected
			m.providers[i].Reason = reason
			return
		}
	}
	m.providers = append(m.providers, providerEntry{Name: name, Connected: connected, Reason: reason})
}

// SetLastUpdate records a quote's time. The age shown is that of the
// newest quote: a delayed quote from a slower feed does not make the
// whole board look older, and a quote with no timestamp counts as
// arriving now.
func (m *Model) SetLastUpdate(t time.Time) {
	if t.IsZero() {
		t = m.clock()
	}
	if t.After(m.lastUpdate) {
		m.lastUpdate = t
	}
}

// SetNow sets the instant the data age is measured against. The app
// advances it on a clock tick, so the age keeps counting while no data
// arrives.
func (m *Model) SetNow(t time.Time) {
	m.now = t
}

func (m Model) clock() time.Time {
	if m.now.IsZero() {
		return time.Now()
	}
	return m.now
}

// SetAlertCount updates the active alert count.
func (m *Model) SetAlertCount(n int) {
	m.alertCount = n
}

// SetThemeName updates the displayed theme name.
func (m *Model) SetThemeName(name string) {
	m.themeName = name
}

// SetSearchQuery updates the search query displayed in the status bar.
func (m *Model) SetSearchQuery(q string) {
	m.searchQuery = q
}

// Age renders a duration in the bar's compact form: "now" under a second,
// then whole seconds, minutes, hours and days.
func Age(d time.Duration) string {
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

// detail is how much of the bar a layout keeps. Layouts are tried from the
// fullest to the sparest and the first that fits on one row is drawn: a
// bar that wraps pushes the frame a row past the terminal and shifts every
// mouse row.
type detail struct {
	reasons   bool // disconnect reasons beside provider names
	theme     bool // the theme name
	search    bool // the active search query
	providers bool // each provider by name, else a connected/total count
	age       bool // the data age
	alerts    bool // the triggered-alert count
}

var layouts = []detail{
	{reasons: true, theme: true, search: true, providers: true, age: true, alerts: true},
	{theme: true, search: true, providers: true, age: true, alerts: true},
	{search: true, providers: true, age: true, alerts: true},
	{providers: true, age: true, alerts: true},
	{age: true, alerts: true},
	{alerts: true},
	{},
}

// View renders the status bar on exactly one row of the given width. A
// non-positive width means the frame has not been sized yet; rendering
// into it would only produce padding.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	for _, d := range layouts {
		left, right := m.segments(d)
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap >= 1 {
			return styleBar.Render(left + format.Spaces(gap) + right)
		}
	}
	// Not even the help hint fits with its margins: keep the hint, clipped.
	hint := format.Clip(" ?:help", m.width)
	return styleBar.Render(hint + format.Spaces(m.width-lipgloss.Width(hint)))
}

func (m Model) segments(d detail) (left, right string) {
	sep := styleSep.Render(" │ ")
	var leftSegs []string

	if seg := m.providerSegment(d); seg != "" {
		leftSegs = append(leftSegs, seg)
	}
	if d.age && !m.lastUpdate.IsZero() {
		age := m.clock().Sub(m.lastUpdate)
		style := styleBar
		switch {
		case age >= deadAfter:
			style = styleDisconnected
		case age >= staleAfter:
			style = styleAlertCount.UnsetBold()
		}
		text := Age(age)
		if text != "now" {
			text += " ago"
		}
		leftSegs = append(leftSegs, style.Render(text))
	}
	if d.search && m.searchQuery != "" {
		leftSegs = append(leftSegs, styleAlertCount.Render("/ "+format.Truncate(m.searchQuery, 16)))
	}
	if d.alerts && m.alertCount > 0 {
		leftSegs = append(leftSegs, styleAlertCount.Render(fmt.Sprintf("🔔 %d", m.alertCount)))
	}

	var rightSegs []string
	if d.theme && m.themeName != "" {
		rightSegs = append(rightSegs, styleBar.Render("T:"+m.themeName))
	}
	rightSegs = append(rightSegs, styleBar.Render("?:help"))

	left = strings.Join(leftSegs, sep)
	if left != "" {
		left = styleBar.Render(" ") + left
	}
	return left, strings.Join(rightSegs, sep) + styleBar.Render(" ")
}

// providerSegment lists each provider by name, or, when the bar is tight,
// counts the connected ones; the count turns red when any is down.
func (m Model) providerSegment(d detail) string {
	if len(m.providers) == 0 {
		return ""
	}
	if !d.providers {
		up := 0
		for _, p := range m.providers {
			if p.Connected {
				up++
			}
		}
		style, dot := styleConnected, "●"
		if up < len(m.providers) {
			style, dot = styleDisconnected, "○"
		}
		return style.Render(fmt.Sprintf("%s %d/%d", dot, up, len(m.providers)))
	}
	parts := make([]string, 0, len(m.providers))
	for _, p := range m.providers {
		if p.Connected {
			parts = append(parts, styleConnected.Render("● "+p.Name))
			continue
		}
		text := "○ " + p.Name
		if d.reasons && p.Reason != "" {
			text += ": " + format.Truncate(p.Reason, maxReasonWidth)
		}
		parts = append(parts, styleDisconnected.Render(text))
	}
	return strings.Join(parts, styleBar.Render("  "))
}
