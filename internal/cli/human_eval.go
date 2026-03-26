package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
	"github.com/spf13/cobra"
)

type humanEvalExport struct {
	RunID       string                   `json:"run_id"`
	Evaluations []models.HumanEvaluation `json:"evaluations"`
}

func newHumanEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "human-eval",
		Short: "Manage human evaluations",
	}
	cmd.AddCommand(newHumanEvalImportCmd())
	return cmd
}

func newHumanEvalImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <run-id> <file.json>",
		Short: "Import human evaluation scores from a JSON file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			ctx := cmd.Context()

			run, err := sqliteStore.GetRun(ctx, runID)
			if err != nil {
				return fmt.Errorf("run %q not found: %w", runID, err)
			}

			data, err := os.ReadFile(args[1])
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}

			var export humanEvalExport
			if err := json.Unmarshal(data, &export); err != nil {
				return fmt.Errorf("parsing JSON: %w", err)
			}

			// Build set of valid test case IDs
			tcSet := make(map[string]bool)
			for _, r := range run.Results {
				tcSet[r.TestCaseID] = true
			}

			// Validate: score 1-5, test case exists
			var valid []models.HumanEvaluation
			var skipped int
			for _, e := range export.Evaluations {
				e.RunID = runID
				if e.EvaluatorName == "" {
					e.EvaluatorName = "human"
				}
				if e.ScoredAt.IsZero() {
					e.ScoredAt = time.Now().UTC()
				}
				if e.HumanScore < 1 || e.HumanScore > 5 {
					skipped++
					continue
				}
				if !tcSet[e.TestCaseID] {
					skipped++
					continue
				}
				valid = append(valid, e)
			}

			if len(valid) == 0 {
				return fmt.Errorf("no valid evaluations in file (%d skipped)", skipped)
			}

			if err := sqliteStore.SaveHumanEvaluations(ctx, valid); err != nil {
				return fmt.Errorf("saving: %w", err)
			}

			fmt.Printf("Imported %d human evaluations for run %s\n", len(valid), runID)
			if skipped > 0 {
				fmt.Printf("  (%d skipped: invalid score or unknown test case)\n", skipped)
			}
			return nil
		},
	}
}
