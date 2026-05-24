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
trades subtract mult*ATR, shorts add).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			equity, _ := cmd.Flags().GetFloat64("equity")
			riskPct, _ := cmd.Flags().GetFloat64("risk")
			entry, _ := cmd.Flags().GetFloat64("entry")
			stop, _ := cmd.Flags().GetFloat64("stop")
			atr, _ := cmd.Flags().GetFloat64("atr")
			atrMult, _ := cmd.Flags().GetFloat64("atr-mult")
			long, _ := cmd.Flags().GetBool("long")

			if equity <= 0 {
				return fmt.Errorf("--equity must be positive")
			}
			if entry <= 0 {
				return fmt.Errorf("--entry must be positive")
			}

			if stop == 0 {
				if atr <= 0 {
					return fmt.Errorf("provide either --stop or --atr (with --atr-mult and --long)")
				}
				stop = portfolio.ATRStop(entry, atr, atrMult, long)
			}

			shares, dollar := portfolio.PositionSize(equity, riskPct, entry, stop)
			notional := shares * entry
			side := "long"
			if !long {
				side = "short"
			}
			fmt.Printf("Side:       %s\n", side)
			fmt.Printf("Equity:     $%.2f\n", equity)
			fmt.Printf("Risk:       %.2f%%  ($%.2f)\n", riskPct, dollar)
			fmt.Printf("Entry:      $%.4f\n", entry)
			fmt.Printf("Stop:       $%.4f\n", stop)
			fmt.Printf("Shares:     %.4f\n", shares)
			fmt.Printf("Notional:   $%.2f\n", notional)
			return nil
		},
	}
	positionCmd.Flags().Float64("equity", 0, "account equity (required)")
	positionCmd.Flags().Float64("risk", 1, "max risk per trade in percent")
	positionCmd.Flags().Float64("entry", 0, "entry price (required)")
	positionCmd.Flags().Float64("stop", 0, "stop price (or use --atr)")
	positionCmd.Flags().Float64("atr", 0, "ATR value for ATR-implied stop")
	positionCmd.Flags().Float64("atr-mult", 2, "ATR multiplier for stop distance")
	positionCmd.Flags().Bool("long", true, "long position (false for short)")

	rootCmd.AddCommand(positionCmd)
}
