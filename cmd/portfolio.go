package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/importer"
	"github.com/stxkxs/mkt/internal/portfolio"
)

func init() {
	portfolioCmd := &cobra.Command{
		Use:   "portfolio",
		Short: "Manage portfolio data",
	}

	importCmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Import transactions from a broker CSV export",
		Long: `Reads a CSV file and appends the parsed transactions to a named portfolio.
Auto-detects the format (generic, schwab) from the header line; use --format to override.
The portfolio is created if it does not exist.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			pname, _ := cmd.Flags().GetString("portfolio")
			formatName, _ := cmd.Flags().GetString("format")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			if pname == "" {
				return fmt.Errorf("--portfolio is required")
			}

			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open %s: %w", path, err)
			}
			defer f.Close()

			var fmtImpl importer.Format
			var reader io.Reader = f
			if formatName == "" || formatName == "auto" {
				detected, rest, err := importer.Detect(f)
				if err != nil {
					return err
				}
				fmtImpl = detected
				reader = rest
			} else {
				fmtImpl = importer.ByName(formatName)
				if fmtImpl == nil {
					return fmt.Errorf("unknown format %q (try generic or schwab)", formatName)
				}
			}

			txs, err := fmtImpl.Parse(reader)
			if err != nil {
				return fmt.Errorf("%s: %w", fmtImpl.Name(), err)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			converted := make([]config.Transaction, 0, len(txs))
			for _, t := range txs {
				converted = append(converted, config.Transaction{
					Type:     string(t.Type),
					Symbol:   t.Symbol,
					Quantity: t.Quantity,
					Price:    t.Price,
					Time:     t.Time.Format("2006-01-02"),
					Fee:      t.Fee,
					Note:     t.Note,
				})
			}

			// Find or create portfolio
			pIdx := -1
			for i := range cfg.Portfolios {
				if cfg.Portfolios[i].Name == pname {
					pIdx = i
					break
				}
			}
			if pIdx == -1 {
				cfg.Portfolios = append(cfg.Portfolios, config.Portfolio{Name: pname})
				pIdx = len(cfg.Portfolios) - 1
			}
			cfg.Portfolios[pIdx].Transactions = append(cfg.Portfolios[pIdx].Transactions, converted...)

			summary := summarize(txs)
			fmt.Printf("Parsed %d transactions from %s (format: %s)\n", len(txs), path, fmtImpl.Name())
			fmt.Printf("  %d buy, %d sell, %d dividend\n", summary[portfolio.TxBuy], summary[portfolio.TxSell], summary[portfolio.TxDividend])
			if dryRun {
				fmt.Println("--dry-run set; config not modified")
				return nil
			}
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("Appended to portfolio %q (now %d transactions)\n", pname, len(cfg.Portfolios[pIdx].Transactions))
			return nil
		},
	}
	importCmd.Flags().String("portfolio", "", "destination portfolio name (created if absent)")
	importCmd.Flags().String("format", "auto", "CSV format: auto, generic, or schwab")
	importCmd.Flags().Bool("dry-run", false, "parse and print summary without saving")

	portfolioCmd.AddCommand(importCmd)
	rootCmd.AddCommand(portfolioCmd)
}

func summarize(txs []portfolio.Transaction) map[portfolio.TxType]int {
	out := map[portfolio.TxType]int{}
	for _, t := range txs {
		out[t.Type]++
	}
	return out
}
