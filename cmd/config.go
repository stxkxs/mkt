package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
	"gopkg.in/yaml.v3"
)

func init() {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			fmt.Print(string(data))
			return nil
		},
	}

	setCmd := &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			switch args[0] {
			case "poll_interval":
				cfg.PollInterval = args[1]
			case "theme":
				cfg.Theme = args[1]
			default:
				return fmt.Errorf("unknown config key: %s", args[0])
			}

			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Set %s = %s\n", args[0], args[1])
			return nil
		},
	}

	addSymbolCmd := &cobra.Command{
		Use:   "add [symbol...]",
		Short: "Add symbols to watchlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			for _, sym := range args {
				if cfg.AddSymbol(sym) {
					fmt.Printf("Added %s to watchlist\n", sym)
				} else {
					fmt.Printf("%s already in watchlist\n", sym)
				}
			}
			return config.Save(cfg)
		},
	}

	removeSymbolCmd := &cobra.Command{
		Use:   "remove [symbol...]",
		Short: "Remove symbols from watchlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			for _, sym := range args {
				if cfg.RemoveSymbol(sym) {
					fmt.Printf("Removed %s from watchlist\n", sym)
				} else {
					fmt.Printf("%s not in watchlist\n", sym)
				}
			}
			return config.Save(cfg)
		},
	}

	configCmd.AddCommand(showCmd, setCmd, addSymbolCmd, removeSymbolCmd)
	rootCmd.AddCommand(configCmd)
}
