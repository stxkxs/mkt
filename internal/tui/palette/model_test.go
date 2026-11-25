package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParseTabName(t *testing.T) {
	m := New([]string{"Watch", "Portfolio", "Alerts"})
	m.Open()
	m.query = "port"
	res := m.parse()
	if res.Action != ActionJumpTab || res.Arg != "Portfolio" {
		t.Errorf("got %+v want jump=Portfolio", res)
	}
}

func TestParseTheme(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	m.query = "theme dracula"
	res := m.parse()
	if res.Action != ActionSetTheme || res.Arg != "dracula" {
		t.Errorf("got %+v want SetTheme dracula", res)
	}
}

func TestParseQuit(t *testing.T) {
	m := New([]string{"Watch"})
	for _, q := range []string{"q", "quit", "QUIT"} {
		m.Open()
		m.query = q
		res := m.parse()
		if res.Action != ActionQuit {
			t.Errorf("query %q: got %+v want Quit", q, res)
		}
	}
}

// A command that does nothing says so, naming what was typed and what
// would have worked.
func TestParseUnknownIsReported(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	m.query = "xyz"
	res := m.parse()
	if res.Action != ActionUnknown || !strings.Contains(res.Arg, `"xyz"`) || !strings.Contains(res.Arg, "theme") {
		t.Errorf("got %+v want Unknown naming the query and the accepted forms", res)
	}
}

func TestParseUnknownThemeIsReported(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	m.query = "theme nosuch"
	res := m.parse()
	if res.Action != ActionUnknown || !strings.Contains(res.Arg, "dracula") {
		t.Errorf("got %+v want Unknown listing the themes", res)
	}

	m.Open()
	m.query = "theme dark"
	if res := m.parse(); res.Action != ActionSetTheme {
		t.Errorf("the dark alias: got %+v want SetTheme", res)
	}
}

func TestParseEmptyCancels(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	res := m.parse()
	if res.Action != ActionCancel {
		t.Errorf("got %+v want Cancel", res)
	}
}

func TestViewSurvivesEveryWidth(t *testing.T) {
	for _, w := range []int{-4, 0, 1, 2, 3, 4, 8, 12, 20, 40, 80, 120, 200} {
		m := New([]string{"Watch", "Chart", "Macro"})
		m.Open()
		m, _ = m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
		out := m.View(w)
		if w <= 0 && out != "" {
			t.Errorf("width %d rendered %q, want empty", w, out)
		}
	}
}

// typeKeys sends each rune as the key press a terminal delivers for it. A
// space arrives as the named key "space" whose Text is " ", which is the
// case that has to reach the query as a space rather than as its name.
func typeKeys(m Model, s string) Model {
	for _, r := range s {
		k := tea.KeyPressMsg{Code: r, Text: string(r)}
		if r == ' ' {
			k = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		}
		m, _ = m.Update(k)
	}
	return m
}

func TestTypedThemeCommandSwitchesTheme(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	m = typeKeys(m, "theme nord")
	_, res := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if res.Action != ActionSetTheme || res.Arg != "nord" {
		t.Errorf("got %+v, want SetTheme nord", res)
	}
}

func TestBackspaceRemovesAWholeCharacter(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	m = typeKeys(m, "wé")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "w" {
		t.Errorf("query %q after backspace, want %q", m.query, "w")
	}
}
