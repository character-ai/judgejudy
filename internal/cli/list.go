package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/character-ai/judgejudy/pkg/store"
	"github.com/spf13/cobra"
)

// shortID safely truncates an ID to at most 8 characters.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func newListCmd() *cobra.Command {
	var (
		datasetFilter string
		limit         int
	)

	cmd := &cobra.Command{
		Use:   "list [runs|baselines]",
		Short: "List evaluation runs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			listType := "runs"
			if len(args) > 0 {
				listType = args[0]
			}

			switch listType {
			case "runs":
				runs, err := sqliteStore.ListRuns(ctx, store.ListOpts{
					DatasetID: datasetFilter,
					Limit:     limit,
				})
				if err != nil {
					return err
				}

				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintf(w, "ID\tTIMESTAMP\tDATASET\tMODEL\tPASS RATE\tCOST\n")
				for _, r := range runs {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%.1f%%\t$%.4f\n",
						shortID(r.ID),
						r.Timestamp.Format("2006-01-02 15:04"),
						r.DatasetID,
						r.GeneratorConfig.Model,
						r.Aggregate.TotalPassRate*100,
						r.TotalCostUSD,
					)
				}
				w.Flush()

			case "baselines":
				runs, err := sqliteStore.ListRuns(ctx, store.ListOpts{
					DatasetID: datasetFilter,
					Limit:     limit,
				})
				if err != nil {
					return err
				}

				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintf(w, "ID\tTIMESTAMP\tDATASET\tMODEL\tPASS RATE\n")
				for _, r := range runs {
					if !r.IsBaseline {
						continue
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%.1f%%\n",
						shortID(r.ID),
						r.Timestamp.Format("2006-01-02 15:04"),
						r.DatasetID,
						r.GeneratorConfig.Model,
						r.Aggregate.TotalPassRate*100,
					)
				}
				w.Flush()

			default:
				return fmt.Errorf("unknown list type %q (use 'runs' or 'baselines')", listType)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&datasetFilter, "dataset", "", "Filter by dataset ID")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")
	return cmd
}
