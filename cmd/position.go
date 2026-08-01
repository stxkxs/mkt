package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/portfolio"
)

func init() {
	positionCmd := &cobra.Command{
		Use:   "position",
		Short: "Compute share size and dollar risk for a planned trade",
		Long: `Given account equity, max risk per trade, and an entry/stop price,
compute the share count and dollar risk. The stop may be given
directly with --stop, or derived from an ATR with --atr (long
trades subtract mult*ATR, shorts add).

Inputs that would produce a meaningless size are rejected rather than
silently returning zero shares: a non-positive risk, a stop equal to the
entry, or a stop on the wrong side of the entry for the chosen side.`,
		Args: cobra.NoArgs,
		RunE: runPosition,
	}
	positionCmd.Flags().Float64("equity", 0, "account equity (required)")
	positionCmd.Flags().Float64("risk", 1, "max risk per trade in percent (0 < risk <= 100)")
	positionCmd.Flags().Float64("entry", 0, "entry price (required)")
	positionCmd.Flags().Float64("stop", 0, "stop price (or use --atr)")
	positionCmd.Flags().Float64("atr", 0, "ATR value for ATR-implied stop")
	positionCmd.Flags().Float64("atr-mult", 2, "ATR multiplier for stop distance")
	positionCmd.Flags().Bool("long", true, "long position (false for short)")

	rootCmd.AddCommand(positionCmd)
}

func runPosition(cmd *cobra.Command, args []string) error {
	equity, _ := cmd.Flags().GetFloat64("equity")
	riskPct, _ := cmd.Flags().GetFloat64("risk")
	entry, _ := cmd.Flags().GetFloat64("entry")
	stop, _ := cmd.Flags().GetFloat64("stop")
	atr, _ := cmd.Flags().GetFloat64("atr")
	atrMult, _ := cmd.Flags().GetFloat64("atr-mult")
	long, _ := cmd.Flags().GetBool("long")

	stop, err := planStop(equity, riskPct, entry, stop, atr, atrMult, long)
	if err != nil {
		return err
	}

	shares, dollar := portfolio.PositionSize(equity, riskPct, entry, stop)
	notional := shares * entry
	side := "long"
	if !long {
		side = "short"
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Side:       %s\n", side)
	fmt.Fprintf(out, "Equity:     $%.2f\n", equity)
	fmt.Fprintf(out, "Risk:       %.2f%%  ($%.2f)\n", riskPct, dollar)
	fmt.Fprintf(out, "Entry:      $%.4f\n", entry)
	fmt.Fprintf(out, "Stop:       $%.4f\n", stop)
	fmt.Fprintf(out, "Shares:     %.4f\n", shares)
	fmt.Fprintf(out, "Notional:   $%.2f\n", notional)
	return nil
}

// planStop validates the sizing inputs and resolves the stop price,
// deriving it from the ATR when --stop was not given.
//
// PositionSize answers (0, 0) for every degenerate input, which as a CLI
// result is indistinguishable from a correct answer of "this trade is too
// small to take". Each degenerate case is therefore caught here and named.
func planStop(equity, riskPct, entry, stop, atr, atrMult float64, long bool) (float64, error) {
	side := "long"
	if !long {
		side = "short"
	}

	if equity <= 0 {
		return 0, fmt.Errorf("--equity must be positive")
	}
	if riskPct <= 0 {
		return 0, fmt.Errorf("--risk must be positive: a risk of %g%% sizes every trade at zero shares", riskPct)
	}
	if riskPct > 100 {
		return 0, fmt.Errorf("--risk %g%% exceeds 100%% of equity", riskPct)
	}
	if entry <= 0 {
		return 0, fmt.Errorf("--entry must be positive")
	}
	if stop < 0 {
		return 0, fmt.Errorf("--stop must be positive (got %g)", stop)
	}

	if stop == 0 {
		if atr <= 0 {
			return 0, fmt.Errorf("provide either --stop or --atr (with --atr-mult and --long)")
		}
		if atrMult <= 0 {
			return 0, fmt.Errorf("--atr-mult must be positive (got %g)", atrMult)
		}
		stop = portfolio.ATRStop(entry, atr, atrMult, long)
		if stop <= 0 {
			return 0, fmt.Errorf("ATR stop is %g: %g × ATR %g is at or beyond the entry price %g — use a smaller --atr-mult or set --stop directly",
				stop, atrMult, atr, entry)
		}
	}

	if stop == entry {
		return 0, fmt.Errorf("--stop equals --entry (%g): risk per share is zero, so no size is definable", entry)
	}
	if long && stop > entry {
		return 0, fmt.Errorf("a %s stop must be below the entry: --stop %g is above --entry %g (pass --long=false for a short)", side, stop, entry)
	}
	if !long && stop < entry {
		return 0, fmt.Errorf("a %s stop must be above the entry: --stop %g is below --entry %g (pass --long for a long)", side, stop, entry)
	}
	return stop, nil
}
