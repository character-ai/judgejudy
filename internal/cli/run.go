package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/character-ai/judgejudy/internal/config"
	"github.com/character-ai/judgejudy/internal/evaluator"
	"github.com/character-ai/judgejudy/internal/models"
	"github.com/character-ai/judgejudy/internal/pipeline"
	"github.com/character-ai/judgejudy/internal/provider"
	"github.com/character-ai/judgejudy/internal/report"
	"github.com/spf13/cobra"
)

// errEvalFailed is a sentinel error indicating the evaluation had failures.
var errEvalFailed = errors.New("evaluation failed: one or more evaluators did not pass")

func newRunCmd() *cobra.Command {
	var (
		reportPath  string
		baseline    bool
		compareID   string
		sampleSize  int
		concurrency int
	)

	cmd := &cobra.Command{
		Use:   "run <config.yaml>",
		Short: "Run an evaluation pipeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(args[0])
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Apply overrides
			if sampleSize > 0 {
				cfg.Dataset.Sample = sampleSize
			}
			if concurrency > 0 {
				cfg.Pipeline.Concurrency = concurrency
			}

			// Initialize provider
			prov, err := provider.NewProvider(cfg.Generator.Provider, "")
			if err != nil {
				return fmt.Errorf("creating provider: %w", err)
			}

			// Build a provider resolver for evaluators (AI judges need providers)
			resolver := func(provName, model string) (evaluator.ProviderFunc, error) {
				p, err := provider.NewProvider(provName, "")
				if err != nil {
					return nil, err
				}
				return func(ctx context.Context, req models.GenerateRequest) (*models.GenerateResponse, error) {
					if model != "" {
						if req.Params == nil {
							req.Params = make(map[string]any)
						}
						if _, ok := req.Params["model"]; !ok {
							req.Params["model"] = model
						}
					}
					return p.Generate(ctx, &req)
				}, nil
			}

			// Initialize evaluators
			evals := make([]evaluator.Evaluator, 0, len(cfg.Evaluators))
			for _, evCfg := range cfg.Evaluators {
				ev, err := evaluator.NewEvaluator(evCfg, resolver)
				if err != nil {
					return fmt.Errorf("creating evaluator %q: %w", evCfg.Name, err)
				}
				evals = append(evals, ev)
			}

			// Create and run pipeline
			p := pipeline.New(cfg, prov, evals, sqliteStore, redisCache, logger)
			run, err := p.Run(cmd.Context())
			if err != nil {
				return err
			}

			// Print summary
			fmt.Fprintf(os.Stdout, "\n=== Evaluation Complete ===\n")
			fmt.Fprintf(os.Stdout, "Run ID:     %s\n", run.ID)
			fmt.Fprintf(os.Stdout, "Duration:   %.1fs\n", run.DurationSeconds)
			fmt.Fprintf(os.Stdout, "Total Cost: $%.4f\n", run.TotalCostUSD)
			fmt.Fprintf(os.Stdout, "Pass Rate:  %.1f%%\n", run.Aggregate.TotalPassRate*100)
			fmt.Fprintf(os.Stdout, "\nMean Scores:\n")
			for name, score := range run.Aggregate.MeanScores {
				fmt.Fprintf(os.Stdout, "  %-20s %.3f\n", name, score)
			}

			// Set baseline if requested
			if baseline {
				if err := sqliteStore.SetBaseline(cmd.Context(), run.ID); err != nil {
					return fmt.Errorf("setting baseline: %w", err)
				}
				fmt.Fprintf(os.Stdout, "\nMarked as baseline.\n")
			}

			// Compare if requested
			var comp *models.Comparison
			if compareID != "" {
				comparison, err := p.Compare(cmd.Context(), compareID, run.ID)
				if err != nil {
					return fmt.Errorf("comparison: %w", err)
				}
				comp = comparison
				fmt.Fprintf(os.Stdout, "\n=== Comparison ===\n")
				for name, delta := range comparison.Deltas {
					status := "stable"
					if delta.PercentDelta < -5 {
						status = "REGRESSED"
					} else if delta.PercentDelta > 5 {
						status = "improved"
					}
					fmt.Fprintf(os.Stdout, "  %-20s %.3f → %.3f (%+.1f%%) %s\n",
						name, delta.BaselineMean, delta.CandidateMean, delta.PercentDelta, status)
				}
			} else if !baseline {
				// Auto-compare against baseline if one exists
				bl, err := sqliteStore.GetBaseline(cmd.Context(), run.DatasetID)
				if err == nil && bl != nil {
					comparison, err := p.Compare(cmd.Context(), bl.ID, run.ID)
					if err == nil {
						comp = comparison
					}
				}
			}

			// Generate report only if explicitly requested via flag or config
			outPath := reportPath
			if outPath == "" {
				outPath = cfg.Report.OutputPath
			}
			if outPath != "" {
				if err := report.GenerateReport(run, comp, outPath); err != nil {
					return fmt.Errorf("generating report: %w", err)
				}
				fmt.Fprintf(os.Stdout, "\nReport generated: %s\n", outPath)
			}

			// Return error (not os.Exit) if evaluators failed — allows cleanup to run
			for _, pr := range run.Aggregate.PassRates {
				if pr < 1.0 {
					return errEvalFailed
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&reportPath, "report", "r", "", "Output path for HTML report")
	cmd.Flags().BoolVarP(&baseline, "baseline", "b", false, "Mark this run as baseline")
	cmd.Flags().StringVar(&compareID, "compare", "", "Run ID to compare against")
	cmd.Flags().IntVarP(&sampleSize, "sample", "s", 0, "Override dataset sample size")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0, "Override concurrency")

	return cmd
}
