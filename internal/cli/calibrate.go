package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/character-ai/judgejudy/internal/calibrate"
	"github.com/character-ai/judgejudy/internal/models"
	"github.com/character-ai/judgejudy/internal/provider"
	"github.com/spf13/cobra"
)

func newCalibrateCmd() *cobra.Command {
	var (
		outputPath string
		threshold  float64
	)

	cmd := &cobra.Command{
		Use:   "calibrate <run-id>",
		Short: "Compare human evaluations against AI judge scores and suggest rubric improvements",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			ctx := cmd.Context()

			run, err := sqliteStore.GetRun(ctx, runID)
			if err != nil {
				return fmt.Errorf("loading run: %w", err)
			}

			humanEvals, err := sqliteStore.GetHumanEvaluations(ctx, runID)
			if err != nil {
				return fmt.Errorf("loading human evaluations: %w", err)
			}
			if len(humanEvals) == 0 {
				return fmt.Errorf("no human evaluations found for run %s\n"+
					"Export scores from the HTML report and import with:\n"+
					"  judgejudy human-eval import %s <file.json>", runID, runID)
			}

			report := calibrate.Calibrate(run, humanEvals, threshold)

			// Print calibration stats
			fmt.Printf("\n=== Calibration Report ===\n")
			fmt.Printf("Run ID: %s\n", report.RunID)
			fmt.Printf("Overall Pearson Correlation: %.3f\n\n", report.OverallPearson)

			fmt.Printf("%-25s %8s %8s %8s %8s %10s\n",
				"Evaluator", "Samples", "Pearson", "Spearman", "Bias", "Agreement")
			fmt.Printf("%-25s %8s %8s %8s %8s %10s\n",
				"─────────────────────────", "────────", "────────", "────────", "────────", "──────────")

			for _, r := range report.Evaluators {
				biasSign := "+"
				if r.MeanBias < 0 {
					biasSign = ""
				}
				pearsonStr := fmt.Sprintf("%8.3f", r.PearsonCorrelation)
				spearmanStr := fmt.Sprintf("%8.3f", r.SpearmanCorrelation)
				if math.IsNaN(r.PearsonCorrelation) {
					pearsonStr = "     N/A"
				}
				if math.IsNaN(r.SpearmanCorrelation) {
					spearmanStr = "     N/A"
				}
				fmt.Printf("%-25s %8d %s %s %s%7.3f %9.1f%%\n",
					r.EvaluatorName, r.SampleCount,
					pearsonStr, spearmanStr,
					biasSign, r.MeanBias,
					r.AgreementRate*100)
			}

			if report.MostAligned != "" {
				fmt.Printf("\nMost aligned with humans:  %s\n", report.MostAligned)
				fmt.Printf("Least aligned with humans: %s\n", report.LeastAligned)
			}

			// Generate rubric suggestions using Claude
			fmt.Printf("\n=== Generating Rubric Suggestions ===\n")
			suggestions, err := generateRubricSuggestions(ctx, run, humanEvals, report)
			if err != nil {
				fmt.Printf("Could not generate suggestions: %v\n", err)
				fmt.Printf("(Set JUDGEJUDY_ANTHROPIC_API_KEY to enable AI-powered rubric suggestions)\n")
			} else {
				fmt.Printf("\n%s\n", suggestions)
			}

			if outputPath != "" {
				out := map[string]any{
					"calibration": report,
					"suggestions": suggestions,
				}
				data, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return fmt.Errorf("marshaling calibration report: %w", err)
				}
				if err := os.WriteFile(outputPath, data, 0600); err != nil {
					return fmt.Errorf("writing output: %w", err)
				}
				fmt.Printf("\nCalibration report written to %s\n", outputPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write calibration report JSON to file")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.1, "Agreement threshold (AI and human within this range)")

	return cmd
}

func generateRubricSuggestions(ctx context.Context, run *models.Run, humanEvals []models.HumanEvaluation, report *models.CalibrationReport) (string, error) {
	prov, err := provider.NewProvider("anthropic", "")
	if err != nil {
		return "", fmt.Errorf("creating anthropic provider: %w", err)
	}

	prompt := calibrate.BuildRubricSuggestionPrompt(run, humanEvals, report)

	resp, err := prov.Generate(ctx, &models.GenerateRequest{
		Prompt:   prompt,
		Modality: models.ModalityText,
		Params: map[string]any{
			"model":      "claude-sonnet-4-6",
			"max_tokens": 4096,
		},
	})
	if err != nil {
		return "", fmt.Errorf("generating suggestions: %w", err)
	}

	return resp.Content, nil
}
