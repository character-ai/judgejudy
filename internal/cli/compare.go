package cli

import (
	"fmt"
	"os"

	"github.com/character-ai/judgejudy/pkg/models"
	"github.com/character-ai/judgejudy/pkg/report"
	"github.com/spf13/cobra"
)

func newCompareCmd() *cobra.Command {
	var reportPath string

	cmd := &cobra.Command{
		Use:   "compare <run-id-1> <run-id-2>",
		Short: "Compare two evaluation runs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			baselineID := args[0]
			candidateID := args[1]

			baseline, err := sqliteStore.GetRun(ctx, baselineID)
			if err != nil {
				return fmt.Errorf("loading baseline: %w", err)
			}
			candidate, err := sqliteStore.GetRun(ctx, candidateID)
			if err != nil {
				return fmt.Errorf("loading candidate: %w", err)
			}

			comp := buildComparison(baselineID, candidateID, baseline.Aggregate.MeanScores, candidate.Aggregate.MeanScores)

			if err := sqliteStore.SaveComparison(ctx, comp); err != nil {
				return fmt.Errorf("saving comparison: %w", err)
			}

			// Print table
			fmt.Fprintf(os.Stdout, "\n=== Comparison: %s vs %s ===\n\n", shortID(baselineID), shortID(candidateID))
			fmt.Fprintf(os.Stdout, "%-20s %10s %10s %10s %10s %10s\n",
				"Evaluator", "Baseline", "Candidate", "Delta", "% Change", "Status")
			fmt.Fprintf(os.Stdout, "%s\n", "--------------------------------------------------------------------------------")

			for name, d := range comp.Deltas {
				status := "stable"
				if d.PercentDelta < -5 {
					status = "REGRESSED"
				} else if d.PercentDelta > 5 {
					status = "improved"
				}
				fmt.Fprintf(os.Stdout, "%-20s %10.3f %10.3f %+10.3f %+9.1f%% %10s\n",
					name, d.BaselineMean, d.CandidateMean, d.AbsoluteDelta, d.PercentDelta, status)
			}

			if reportPath != "" {
				if err := report.GenerateReport(candidate, comp, reportPath); err != nil {
					return fmt.Errorf("generating report: %w", err)
				}
				fmt.Fprintf(os.Stdout, "\nReport generated: %s\n", reportPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&reportPath, "report", "r", "", "Output path for HTML report")
	return cmd
}

// buildComparison computes deltas between baseline and candidate mean scores.
func buildComparison(baselineID, candidateID string, baselineScores, candidateScores map[string]float64) *models.Comparison {
	comp := &models.Comparison{
		BaselineRunID:  baselineID,
		CandidateRunID: candidateID,
		Deltas:         make(map[string]models.MetricDelta),
	}
	for evalName, baselineMean := range baselineScores {
		candidateMean, ok := candidateScores[evalName]
		if !ok {
			continue
		}
		delta := candidateMean - baselineMean
		var pctDelta float64
		if baselineMean != 0 {
			pctDelta = (delta / baselineMean) * 100
		}
		comp.Deltas[evalName] = models.MetricDelta{
			BaselineMean:  baselineMean,
			CandidateMean: candidateMean,
			AbsoluteDelta: delta,
			PercentDelta:  pctDelta,
			Improved:      delta > 0,
		}
		if pctDelta < -5.0 {
			comp.Regressions = append(comp.Regressions, evalName)
		} else if pctDelta > 5.0 {
			comp.Improvements = append(comp.Improvements, evalName)
		}
	}
	return comp
}
