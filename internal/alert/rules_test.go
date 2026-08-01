package alert

import (
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// TestCooldownSurvivesRuleRemoval is the slice-index-keying defect:
// deleting rule 0 used to hand its neighbour's cooldown to whatever slid
// into its slot, so a just-fired rule fired again immediately.
func TestCooldownSurvivesRuleRemoval(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.SetRules([]Rule{
		{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true},
		{Symbol: "ETH", Condition: CondAbove, Value: 3000, Enabled: true},
	})

	e.Check(provider.Quote{Symbol: "ETH", Price: 2900})
	e.Check(provider.Quote{Symbol: "ETH", Price: 3100})
	if len(*fired) != 1 {
		t.Fatalf("ETH rule should fire once, got %d", len(*fired))
	}

	// Delete the BTC rule; ETH slides from index 1 to index 0.
	e.RemoveRule(0)
	if got := e.Rules(); len(got) != 1 || got[0].Symbol != "ETH" {
		t.Fatalf("rules after removal = %+v", got)
	}

	// Both the cooldown and the edge baseline must still belong to ETH.
	e.Check(provider.Quote{Symbol: "ETH", Price: 2900})
	e.Check(provider.Quote{Symbol: "ETH", Price: 3200})
	if len(*fired) != 1 {
		t.Fatalf("ETH cooldown must survive the removal, got %d fires", len(*fired))
	}
}

func TestSetRulesPreservesStateForSurvivingRules(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	btc := Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true}
	eth := Rule{Symbol: "ETH", Condition: CondAbove, Value: 3000, Enabled: true}
	e.SetRules([]Rule{btc, eth})

	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("BTC should fire once, got %d", len(*fired))
	}

	// Reorder — a rule's identity is its content, not its slot.
	e.SetRules([]Rule{eth, btc})
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 52000})
	if len(*fired) != 1 {
		t.Fatalf("reorder must not reset the cooldown, got %d fires", len(*fired))
	}
}

func TestRemoveRuleDropsItsState(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	r := Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true}
	e.AddRule(r)

	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(*fired))
	}

	// Deleting and re-adding is the user asking for a clean slate: the
	// re-added rule re-establishes its baseline instead of inheriting a
	// cooldown it never earned.
	e.RemoveRule(0)
	e.AddRule(r)
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("re-added rule must establish a baseline, got %d", len(*fired))
	}
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 2 {
		t.Fatalf("re-added rule should fire on its next crossing, got %d", len(*fired))
	}
}

func TestRuleKeyIsContentAddressed(t *testing.T) {
	a := Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Period: 14}
	b := Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Period: 14, Enabled: true}
	if ruleKey(a) != ruleKey(b) {
		t.Error("Enabled must not change a rule's identity — toggling keeps its cooldown")
	}

	diff := []Rule{
		{Symbol: "ETH", Condition: CondAbove, Value: 50000, Period: 14},
		{Symbol: "BTC", Condition: CondBelow, Value: 50000, Period: 14},
		{Symbol: "BTC", Condition: CondAbove, Value: 50001, Period: 14},
		{Symbol: "BTC", Condition: CondAbove, Value: 50000, Period: 20},
		{Symbol: "BTC", Condition: CondAbove, Value: 50000, Period: 14, Match: MatchAny},
		{Symbol: "BTC", Condition: CondAbove, Value: 50000, Period: 14, Conditions: []SubCondition{{Type: CondBelow, Value: 1}}},
	}
	for i, r := range diff {
		if ruleKey(r) == ruleKey(a) {
			t.Errorf("diff[%d] %+v collides with the base rule key", i, r)
		}
	}

	// Compound rules differ by their sub-conditions, in order.
	c1 := Rule{Symbol: "BTC", Match: MatchAll, Conditions: []SubCondition{{Type: CondAbove, Value: 1}, {Type: CondBelow, Value: 2}}}
	c2 := Rule{Symbol: "BTC", Match: MatchAll, Conditions: []SubCondition{{Type: CondBelow, Value: 2}, {Type: CondAbove, Value: 1}}}
	if ruleKey(c1) == ruleKey(c2) {
		t.Error("compound rules with reordered conditions must not share a key")
	}
}

func TestOnRulesChangedFiresForEveryMutation(t *testing.T) {
	e := NewEngine(time.Hour, nil)
	var snapshots [][]Rule
	e.SetOnRulesChanged(func(rules []Rule) {
		// Re-entrancy: the hook must not run under the engine lock.
		_ = e.Rules()
		snapshots = append(snapshots, rules)
	})

	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})
	e.AddRule(Rule{Symbol: "ETH", Condition: CondBelow, Value: 3000, Enabled: true})
	e.ToggleRule(0)
	e.RemoveRule(1)

	if len(snapshots) != 4 {
		t.Fatalf("expected 4 hook invocations, got %d", len(snapshots))
	}
	if len(snapshots[0]) != 1 || len(snapshots[1]) != 2 {
		t.Errorf("AddRule snapshots wrong: %v", snapshots[:2])
	}
	if snapshots[2][0].Enabled {
		t.Error("ToggleRule snapshot should show the rule disabled")
	}
	if len(snapshots[3]) != 1 || snapshots[3][0].Symbol != "BTC" {
		t.Errorf("RemoveRule snapshot wrong: %+v", snapshots[3])
	}
}

func TestOnRulesChangedNotFiredBySetRules(t *testing.T) {
	e := NewEngine(time.Hour, nil)
	var calls int
	e.SetOnRulesChanged(func([]Rule) { calls++ })
	e.SetRules([]Rule{{Symbol: "BTC", Condition: CondAbove, Value: 1, Enabled: true}})
	if calls != 0 {
		t.Fatalf("SetRules is the load path and must not trigger a write-back, got %d calls", calls)
	}
}

func TestMutationsOutOfRangeAreNoops(t *testing.T) {
	e := NewEngine(time.Hour, nil)
	var calls int
	e.SetOnRulesChanged(func([]Rule) { calls++ })
	e.RemoveRule(0)
	e.RemoveRule(-1)
	e.ToggleRule(3)
	if calls != 0 {
		t.Fatalf("out-of-range mutations must not fire the hook, got %d calls", calls)
	}
	if len(e.Rules()) != 0 {
		t.Fatal("rules should be unchanged")
	}
}

func TestRulesSnapshotIsACopy(t *testing.T) {
	e := NewEngine(time.Hour, nil)
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})
	got := e.Rules()
	got[0].Symbol = "MUTATED"
	if e.Rules()[0].Symbol != "BTC" {
		t.Fatal("Rules must hand back a copy")
	}
}

func TestRequiredHistory(t *testing.T) {
	cases := []struct {
		cond   Condition
		period int
		want   int
	}{
		{CondAbove, 0, 0},
		{CondBelow, 0, 0},
		{CondPctUp, 0, 0},
		{CondVolumeAbove, 0, 0},
		{CondRSIAbove, 0, defaultRSIPeriod + 1},
		{CondRSIBelow, 21, 22},
		{CondSMACrossAbove, 0, defaultSMAPeriod + 1},
		{CondSMACrossBelow, 50, 51},
		{CondMACDCross, 0, macdMinSamples},
		{CondStddevAbove, 0, defaultStddevPeriod},
		{CondStddevAbove, 30, 30},
	}
	for _, tc := range cases {
		if got := requiredHistory(tc.cond, tc.period); got != tc.want {
			t.Errorf("requiredHistory(%s, %d) = %d, want %d", tc.cond, tc.period, got, tc.want)
		}
	}
}

func TestRuleHistoryUsesDeepestSubCondition(t *testing.T) {
	r := Rule{Symbol: "NVDA", Match: MatchAll, Conditions: []SubCondition{
		{Type: CondAbove, Value: 150},
		{Type: CondRSIBelow, Value: 40, Period: 14},
		{Type: CondSMACrossAbove, Period: 50},
	}}
	if got, want := ruleHistory(r), 51; got != want {
		t.Fatalf("ruleHistory = %d, want %d", got, want)
	}
}

// TestShortHistoryIsSurfaced covers the silent-never-fires defect: an
// sma_cross(50) against an empty ring is inert, not false, and the engine
// has to say so.
func TestShortHistoryIsSurfaced(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	ps := &pricesStub{vals: flat(10, 100)}
	e.SetPriceSource(ps)
	var warned []RuleStatus
	e.SetOnShortHistory(func(st RuleStatus) { warned = append(warned, st) })
	e.AddRule(Rule{Symbol: "AMD", Condition: CondSMACrossAbove, Period: 50, Enabled: true})

	for i := 0; i < 5; i++ {
		e.Check(provider.Quote{Symbol: "AMD", Price: 100})
	}
	if len(warned) != 1 {
		t.Fatalf("expected exactly one warning, got %d", len(warned))
	}
	if warned[0].Ready || warned[0].Have != 10 || warned[0].Need != 51 {
		t.Errorf("status = %+v, want not-ready 10/51", warned[0])
	}
	if warned[0].Reason == "" {
		t.Error("status should carry a human-readable reason")
	}
	if len(*fired) != 0 {
		t.Fatalf("an unevaluable rule must not fire, got %d", len(*fired))
	}

	st := e.Statuses()
	if len(st) != 1 || st[0].Ready {
		t.Fatalf("Statuses should report the rule as not ready: %+v", st)
	}

	// Ring fills: the rule goes live and stops warning.
	ps.set(append(flat(51, 100), 200))
	e.Check(provider.Quote{Symbol: "AMD", Price: 200})
	if len(warned) != 1 {
		t.Fatalf("a ready rule must not warn again, got %d", len(warned))
	}
	if len(*fired) != 1 {
		t.Fatalf("rule should evaluate once history is long enough, got %d", len(*fired))
	}
	if st := e.Statuses(); !st[0].Ready {
		t.Errorf("Statuses should now report ready: %+v", st[0])
	}
}

func TestShortHistoryWarnsAgainAfterRecovery(t *testing.T) {
	e := NewEngine(time.Hour, nil)
	ps := &pricesStub{vals: flat(5, 100)}
	e.SetPriceSource(ps)
	var warned int
	e.SetOnShortHistory(func(RuleStatus) { warned++ })
	e.AddRule(Rule{Symbol: "SOL-USD", Condition: CondRSIAbove, Value: 70, Period: 14, Enabled: true})

	e.Check(provider.Quote{Symbol: "SOL-USD", Price: 100})
	ps.set(flat(20, 100)) // ring fills
	e.Check(provider.Quote{Symbol: "SOL-USD", Price: 100})
	ps.set(flat(5, 100)) // restart empties it again
	e.Check(provider.Quote{Symbol: "SOL-USD", Price: 100})

	if warned != 2 {
		t.Fatalf("expected one warning per dry spell, got %d", warned)
	}
}

func TestStatusesReportsNoPriceSource(t *testing.T) {
	e := NewEngine(time.Hour, nil)
	e.SetRules([]Rule{
		{Symbol: "BTC", Condition: CondAbove, Value: 1, Enabled: true},
		{Symbol: "BTC", Condition: CondMACDCross, Enabled: true},
	})
	st := e.Statuses()
	if len(st) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(st))
	}
	if !st[0].Ready || st[0].Need != 0 {
		t.Errorf("price rule needs no history: %+v", st[0])
	}
	if st[1].Ready || st[1].Reason == "" {
		t.Errorf("indicator rule without a price source is not ready: %+v", st[1])
	}
	if st[1].Index != 1 {
		t.Errorf("Index = %d, want 1", st[1].Index)
	}
}
