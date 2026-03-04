package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	quiet   bool
)

var rootCmd = &cobra.Command{
	Use:   "mkt",
	Short: "Real-time stock & crypto market dashboard",
	Long:  "A terminal dashboard for tracking crypto and stock prices in real-time.",
	RunE:  runDashboard,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("mkt %s (commit: %s, built: %s)\n", version, commit, date)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
