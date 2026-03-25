package cli

import (
	"log/slog"
	"os"

	"github.com/character-ai/judgejudy/internal/store"
	"github.com/spf13/cobra"
)

var (
	dbPath    string
	redisAddr string
	verbose   bool

	sqliteStore store.Store
	redisCache  *store.Cache
	logger      *slog.Logger
)

// NewRootCmd creates the root cobra command.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "judgejudy",
		Short: "Multimodal AI evaluation framework",
		Long:  "judgejudy — judge text, image, audio, and video generation with any model.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Setup logging
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

			// Initialize store
			var err error
			sqliteStore, err = store.NewSQLiteStore(dbPath)
			if err != nil {
				return err
			}

			// Initialize cache — default is empty (disabled).
			// Only connect to Redis if explicitly configured.
			redisCache, _ = store.NewCache(redisAddr)
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if redisCache != nil {
				redisCache.Close()
			}
			if sqliteStore != nil {
				return sqliteStore.Close()
			}
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "SQLite database path (default ~/.judgejudy/judgejudy.db)")
	rootCmd.PersistentFlags().StringVar(&redisAddr, "redis", "", "Redis address (e.g. localhost:6379, empty to disable)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newCompareCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newReportCmd())

	return rootCmd
}
