package portfolio

import (
	"math"
	"testing"
	"time"
)

func TestSharpe(t *testing.T) {
	t.Run("positive returns gives positive Sharpe", func(t *testing.T) {
		got := Sharpe([]float64{0.01, 0.02, 0.015, 0.018}, 0)
		if math.IsNaN(got) || got <= 0 {
			t.Errorf("expected positive Sharpe, got %v", got)
		}
	})

	t.Run("rf above mean returns negative", func(t *testing.T) {
		got := Sharpe([]float64{0.01, 0.02, 0.015}, 0.05)
		if math.IsNaN(got) || got >= 0 {
			t.Errorf("expected negative Sharpe, got %v", got)
		}
	})

	t.Run("fewer than two returns NaN", func(t *testing.T) {
		if !math.IsNaN(Sharpe([]float64{0.01}, 0)) {
			t.Error("expected NaN for single point")
		}
		if !math.IsNaN(Sharpe(nil, 0)) {
			t.Error("expected NaN for empty")
		}
	})

	t.Run("zero stddev returns NaN", func(t *testing.T) {
		got := Sharpe([]float64{0.01, 0.01, 0.01}, 0)
		if !math.IsNaN(got) {
			t.Errorf("flat returns should yield NaN, got %v", got)
		}
	})
}

func TestSortino(t *testing.T) {
	t.Run("monotonic up has no downside", func(t *testing.T) {
		got := Sortino([]float64{0.01, 0.02, 0.03, 0.04}, 0)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN when no downside, got %v", got)
		}
	})

	t.Run("mixed returns yields finite value", func(t *testing.T) {
		got := Sortino([]float64{0.05, -0.02, 0.03, -0.01, 0.04}, 0)
		if math.IsNaN(got) {
			t.Error("expected finite Sortino for mixed returns")
		}
	})

	// Hand-computed against the Sortino & Price definition:
	//   returns {0.05, -0.02, 0.03, -0.01, 0.04}, target 0
	//   mean = 0.09/5                                  = 0.018
	//   Σ min(r-t,0)² = 0.02² + 0.01²                  = 0.0005
	//   DD = sqrt(0.0005/5)                            = 0.01
	//   Sortino = 0.018 / 0.01                         = 1.8
	// Dividing by the downside count (n_down-1 = 1) instead gives DD = 0.02236
	// and a ratio of 0.805 — the same inputs, a different statistic.
	t.Run("matches the hand-computed definition", func(t *testing.T) {
		got := Sortino([]float64{0.05, -0.02, 0.03, -0.01, 0.04}, 0)
		if math.Abs(got-1.8) > 1e-12 {
			t.Errorf("Sortino = %v, want 1.8", got)
		}
	})

	// A portfolio with one bad month in twenty is exactly what the ratio is for;
	// the old n_down-1 denominator reported NaN for it.
	t.Run("single downside period is defined", func(t *testing.T) {
		returns := make([]float64, 20)
		for i := range returns {
			returns[i] = 0.01
		}
		returns[7] = -0.05

		got := Sortino(returns, 0)
		if math.IsNaN(got) {
			t.Fatal("one downside period must not be NaN")
		}
		// mean = (19*0.01 - 0.05)/20 = 0.007; DD = sqrt(0.0025/20) = 0.01118034
		want := 0.007 / math.Sqrt(0.0025/20)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("Sortino = %v, want %v", got, want)
		}
	})

	t.Run("target shifts what counts as downside", func(t *testing.T) {
		returns := []float64{0.02, 0.01, 0.03, 0.015}
		if !math.IsNaN(Sortino(returns, 0)) {
			t.Error("no return below zero: expected NaN")
		}
		got := Sortino(returns, 0.02)
		if math.IsNaN(got) {
			t.Fatal("expected a finite ratio once the target is above some returns")
		}
		// mean = 0.01875; shortfalls: 0, -0.01, 0, -0.005
		wantDD := math.Sqrt((0.0001 + 0.000025) / 4)
		if math.Abs(got-(0.01875-0.02)/wantDD) > 1e-12 {
			t.Errorf("Sortino = %v, want %v", got, (0.01875-0.02)/wantDD)
		}
	})

	t.Run("fewer than two returns NaN", func(t *testing.T) {
		if !math.IsNaN(Sortino([]float64{-0.01}, 0)) {
			t.Error("expected NaN for single point")
		}
		if !math.IsNaN(Sortino(nil, 0)) {
			t.Error("expected NaN for empty")
		}
	})
}

func TestDownsideDeviationAveragesOverAllPeriods(t *testing.T) {
	// Two identical downside periods diluted by more upside periods must yield
	// a smaller downside deviation, not the same one.
	few := downsideDeviation([]float64{-0.01, -0.01, 0.01, 0.01}, 0)
	many := downsideDeviation([]float64{-0.01, -0.01, 0.01, 0.01, 0.01, 0.01, 0.01, 0.01}, 0)
	if !(many < few) {
		t.Errorf("downside deviation %v (8 periods) should be below %v (4 periods)", many, few)
	}
	if math.Abs(few-math.Sqrt(0.0002/4)) > 1e-15 {
		t.Errorf("few = %v, want sqrt(0.0002/4)", few)
	}
	if got := downsideDeviation(nil, 0); !math.IsNaN(got) {
		t.Errorf("empty = %v, want NaN", got)
	}
}

func TestBeta(t *testing.T) {
	t.Run("perfectly correlated 1:1 returns 1", func(t *testing.T) {
		asset := []float64{0.01, 0.02, 0.03, -0.01}
		bench := []float64{0.01, 0.02, 0.03, -0.01}
		got := Beta(asset, bench)
		if math.Abs(got-1) > 1e-9 {
			t.Errorf("expected beta=1, got %v", got)
		}
	})

	t.Run("2x amplitude returns 2", func(t *testing.T) {
		asset := []float64{0.02, 0.04, 0.06, -0.02}
		bench := []float64{0.01, 0.02, 0.03, -0.01}
		got := Beta(asset, bench)
		if math.Abs(got-2) > 1e-9 {
			t.Errorf("expected beta=2, got %v", got)
		}
	})

	t.Run("length mismatch returns NaN", func(t *testing.T) {
		if !math.IsNaN(Beta([]float64{1, 2}, []float64{1})) {
			t.Error("expected NaN for length mismatch")
		}
	})

	t.Run("zero benchmark variance returns NaN", func(t *testing.T) {
		got := Beta([]float64{1, 2, 3}, []float64{5, 5, 5})
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for zero bench variance, got %v", got)
		}
	})

	t.Run("usable straight off two equity curves", func(t *testing.T) {
		asset := Returns([]float64{100, 102, 100, 104})
		bench := Returns([]float64{50, 51, 50, 52})
		got := Beta(asset, bench)
		if math.IsNaN(got) || math.Abs(got-1) > 1e-9 {
			t.Errorf("beta of a curve against half itself = %v, want 1", got)
		}
	})
}

func TestMaxDrawdown(t *testing.T) {
	t.Run("monotonic up has zero drawdown", func(t *testing.T) {
		got := MaxDrawdown([]float64{100, 110, 120, 130})
		if got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})

	t.Run("flat has zero drawdown", func(t *testing.T) {
		got := MaxDrawdown([]float64{100, 100, 100})
		if got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})

	t.Run("known drawdown", func(t *testing.T) {
		// peak 120, trough 90 → DD = (120-90)/120 = 0.25
		got := MaxDrawdown([]float64{100, 120, 110, 90, 95})
		if math.Abs(got-0.25) > 1e-9 {
			t.Errorf("expected 0.25, got %v", got)
		}
	})

	t.Run("empty or single", func(t *testing.T) {
		if MaxDrawdown(nil) != 0 {
			t.Error("nil should be 0")
		}
		if MaxDrawdown([]float64{100}) != 0 {
			t.Error("single point should be 0")
		}
	})
}

func marksEvery(start time.Time, step time.Duration, values ...float64) []EquityMark {
	out := make([]EquityMark, len(values))
	for i, v := range values {
		out[i] = EquityMark{Time: start.Add(time.Duration(i) * step), PortfolioName: "p", Value: v}
	}
	return out
}

func TestMarkValuesAndReturns(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	marks := marksEvery(start, 24*time.Hour, 100, 110, 99)

	values := MarkValues(marks)
	if len(values) != 3 || values[0] != 100 || values[2] != 99 {
		t.Fatalf("MarkValues = %v, want [100 110 99]", values)
	}

	rets := MarkReturns(marks)
	if len(rets) != 2 {
		t.Fatalf("MarkReturns len = %d, want 2", len(rets))
	}
	if math.Abs(rets[0]-0.1) > 1e-12 || math.Abs(rets[1]+0.1) > 1e-12 {
		t.Errorf("MarkReturns = %v, want [0.1 -0.1]", rets)
	}

	// Metrics consume the result directly.
	if math.IsNaN(Sharpe(rets, 0)) {
		t.Error("Sharpe over MarkReturns should be defined")
	}
}

func TestMarkValuesSortsOutOfOrderMarks(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	marks := []EquityMark{
		{Time: start.Add(2 * time.Hour), Value: 3},
		{Time: start, Value: 1},
		{Time: start.Add(time.Hour), Value: 2},
	}
	got := MarkValues(marks)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("MarkValues = %v, want chronological [1 2 3]", got)
	}
	// The caller's slice is left alone.
	if marks[0].Value != 3 {
		t.Error("input slice was reordered in place")
	}
}

func TestStatsFromMarks(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	marks := marksEvery(start, 24*time.Hour, 100, 110, 99, 108.9)

	s := StatsFromMarks(marks, 0)
	if s.Marks != 4 || s.Periods != 3 {
		t.Fatalf("Marks/Periods = %d/%d, want 4/3", s.Marks, s.Periods)
	}
	if !s.First.Equal(start) || !s.Last.Equal(start.Add(72*time.Hour)) {
		t.Errorf("window = %v..%v", s.First, s.Last)
	}
	if s.Elapsed != 72*time.Hour {
		t.Errorf("Elapsed = %v, want 72h", s.Elapsed)
	}
	if s.StartValue != 100 || math.Abs(s.EndValue-108.9) > 1e-9 {
		t.Errorf("values = %v..%v", s.StartValue, s.EndValue)
	}
	if math.Abs(s.TotalReturn-0.089) > 1e-9 {
		t.Errorf("TotalReturn = %v, want 0.089", s.TotalReturn)
	}
	if s.Period != 24*time.Hour {
		t.Errorf("Period = %v, want 24h", s.Period)
	}
	if math.Abs(s.PeriodsPerYear-365) > 1e-9 {
		t.Errorf("PeriodsPerYear = %v, want 365", s.PeriodsPerYear)
	}
	// peak 110 → trough 99 = 10%
	if math.Abs(s.MaxDrawdown-0.1) > 1e-9 {
		t.Errorf("MaxDrawdown = %v, want 0.1", s.MaxDrawdown)
	}
	rets := MarkReturns(marks)
	if math.Abs(s.MeanReturn-mean(rets)) > 1e-12 {
		t.Errorf("MeanReturn = %v, want %v", s.MeanReturn, mean(rets))
	}
	if math.Abs(s.Sharpe-Sharpe(rets, 0)) > 1e-12 || math.Abs(s.Sortino-Sortino(rets, 0)) > 1e-12 {
		t.Errorf("ratios = %v/%v, want %v/%v", s.Sharpe, s.Sortino, Sharpe(rets, 0), Sortino(rets, 0))
	}
	if math.Abs(s.AnnualizedVolatility-s.Volatility*math.Sqrt(365)) > 1e-12 {
		t.Errorf("AnnualizedVolatility = %v", s.AnnualizedVolatility)
	}
	// 8.9% over 3 days compounds to a very large annual figure — the point of
	// reporting Elapsed alongside it.
	if !(s.AnnualizedReturn > 1) {
		t.Errorf("AnnualizedReturn = %v, want a large positive number", s.AnnualizedReturn)
	}
}

func TestStatsFromMarksDegenerateInputs(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := StatsFromMarks(nil, 0)
		if s.Marks != 0 || !math.IsNaN(s.Sharpe) || !math.IsNaN(s.Sortino) {
			t.Errorf("empty stats = %+v", s)
		}
	})

	t.Run("single mark", func(t *testing.T) {
		start := time.Unix(1_700_000_000, 0).UTC()
		s := StatsFromMarks(marksEvery(start, time.Hour, 100), 0)
		if s.Marks != 1 || s.Periods != 0 {
			t.Errorf("Marks/Periods = %d/%d, want 1/0", s.Marks, s.Periods)
		}
		if s.TotalReturn != 0 {
			t.Errorf("TotalReturn = %v, want 0", s.TotalReturn)
		}
		if !math.IsNaN(s.Sharpe) || !math.IsNaN(s.AnnualizedReturn) {
			t.Errorf("single mark should leave ratios undefined: %+v", s)
		}
		if s.Period != 0 || s.PeriodsPerYear != 0 {
			t.Errorf("Period/PeriodsPerYear = %v/%v, want 0/0", s.Period, s.PeriodsPerYear)
		}
	})

	t.Run("zero start value", func(t *testing.T) {
		start := time.Unix(1_700_000_000, 0).UTC()
		s := StatsFromMarks(marksEvery(start, time.Hour, 0, 100, 110), 0)
		if !math.IsNaN(s.TotalReturn) {
			t.Errorf("TotalReturn off a zero base = %v, want NaN", s.TotalReturn)
		}
		if !math.IsNaN(s.AnnualizedReturn) {
			t.Errorf("AnnualizedReturn off a zero base = %v, want NaN", s.AnnualizedReturn)
		}
	})

	t.Run("all marks at the same instant", func(t *testing.T) {
		start := time.Unix(1_700_000_000, 0).UTC()
		s := StatsFromMarks(marksEvery(start, 0, 100, 110, 120), 0)
		if s.Period != 0 || s.PeriodsPerYear != 0 {
			t.Errorf("no spacing should leave Period unknown, got %v", s.Period)
		}
		if !math.IsNaN(s.AnnualizedSharpe) {
			t.Errorf("AnnualizedSharpe = %v, want NaN without a period", s.AnnualizedSharpe)
		}
	})
}

func TestMedianSpacingIgnoresRestartGap(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	marks := []EquityMark{
		{Time: start},
		{Time: start.Add(5 * time.Minute)},
		{Time: start.Add(10 * time.Minute)},
		{Time: start.Add(72 * time.Hour)}, // process was down for three days
		{Time: start.Add(72*time.Hour + 5*time.Minute)},
	}
	if got := medianSpacing(marks); got != 5*time.Minute {
		t.Errorf("medianSpacing = %v, want 5m", got)
	}
	if got := medianSpacing(marks[:1]); got != 0 {
		t.Errorf("medianSpacing of one mark = %v, want 0", got)
	}
}
