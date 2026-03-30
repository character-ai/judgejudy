package cli

import (
	"fmt"

	"github.com/character-ai/judgejudy/pkg/models"
	"github.com/character-ai/judgejudy/pkg/report"
	"github.com/spf13/cobra"
)

func newReportCmd() *cobra.Command {
	var (
		outputPath string
		compareID  string
	)

	cmd := &cobra.Command{
		Use:   "report <run-id>",
		Short: "Generate an HTML report for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			runID := args[0]

			run, err := sqliteStore.GetRun(ctx, runID)
			if err != nil {
				return fmt.Errorf("loading run: %w", err)
			}

			if outputPath == "" {
				outputPath = fmt.Sprintf("report_%s.html", shortID(runID))
			}

			var comp *models.Comparison
			if compareID != "" {
				compRun, err := sqliteStore.GetRun(ctx, compareID)
				if err != nil {
					return fmt.Errorf("loading comparison run: %w", err)
				}
				comp = buildComparison(compareID, runID, compRun.Aggregate.MeanScores, run.Aggregate.MeanScores)
			}

			if err := report.GenerateReport(run, comp, outputPath); err != nil {
				return fmt.Errorf("generating report: %w", err)
			}
			fmt.Printf("Report generated: %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path (default: report_<run-id>.html)")
	cmd.Flags().StringVar(&compareID, "compare", "", "Run ID to include comparison data")
	return cmd
}
