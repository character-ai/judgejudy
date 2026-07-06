package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/character-ai/judgejudy/internal/telemetry"
	"github.com/character-ai/judgejudy/pkg/store"
	"github.com/spf13/cobra"
)

var (
	dbPath    string
	redisAddr string
	verbose   bool

	sqliteStore  store.Store
	redisCache   *store.Cache
	logger       *slog.Logger
	otelShutdown func(context.Context) error
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

			// Initialize OTEL metrics export (no-op unless OTEL_EXPORTER_OTLP_ENDPOINT
			// or OTEL_EXPORTER_OTLP_METRICS_ENDPOINT is set)
			var otelErr error
			otelShutdown, otelErr = telemetry.Setup(cmd.Context(), cmd.Root().Version)
			if otelErr != nil {
				logger.Warn("failed to set up OTEL metrics export", "error", otelErr)
			}

			// Initialize store
			var err error
			sqliteStore, err = store.NewSQLiteStore(dbPath)
			if err != nil {
				return err
			}

			// Initialize cache — default is empty (disabled).
			// Only connect to Redis if explicitly configured.
			if redisAddr != "" {
				var cacheErr error
				redisCache, cacheErr = store.NewCache(redisAddr)
				if cacheErr != nil {
					logger.Warn("failed to connect to Redis cache", "addr", redisAddr, "error", cacheErr)
				}
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if otelShutdown != nil {
				if err := otelShutdown(cmd.Context()); err != nil {
					logger.Warn("failed to flush OTEL metrics", "error", err)
				}
			}
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
	rootCmd.AddCommand(newHumanEvalCmd())
	rootCmd.AddCommand(newCalibrateCmd())

	return rootCmd
}
