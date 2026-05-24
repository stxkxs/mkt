package palette

import "testing"

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

func TestParseUnknownCancels(t *testing.T) {
	m := New([]string{"Watch"})
	m.Open()
	m.query = "xyz"
	res := m.parse()
	if res.Action != ActionCancel {
		t.Errorf("got %+v want Cancel", res)
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
