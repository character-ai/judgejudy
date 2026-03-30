// Package judgejudy provides a Go library API for running multimodal AI evaluations.
//
// This is the top-level entry point for programmatic use. For CLI usage, see the
// judgejudy command.
//
// Basic usage:
//
//	cfg, err := config.LoadConfig("eval.yaml")
//	if err != nil { ... }
//
//	result, err := judgejudy.Run(ctx, cfg, judgejudy.Options{})
//	// result.Run contains the full evaluation run with per-test-case results
package judgejudy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/character-ai/judgejudy/pkg/config"
	"github.com/character-ai/judgejudy/pkg/evaluator"
	"github.com/character-ai/judgejudy/pkg/models"
	"github.com/character-ai/judgejudy/pkg/pipeline"
	"github.com/character-ai/judgejudy/pkg/provider"
	"github.com/character-ai/judgejudy/pkg/report"
	"github.com/character-ai/judgejudy/pkg/store"
)

// Options configures a library-driven evaluation run.
type Options struct {
	// Store is the persistence backend for runs and comparisons.
	// If nil, a default SQLite store at ~/.judgejudy/judgejudy.db is used.
	Store store.Store

	// Logger is the structured logger for pipeline events.
	// If nil, slog.Default() is used.
	Logger *slog.Logger

	// Concurrency overrides the pipeline concurrency from the config.
	// Zero means use the config value.
	Concurrency int

	// SampleSize overrides the dataset sample size from the config.
	// Zero means use the config value.
	SampleSize int

	// CompareRunID, if set, triggers a comparison of the new run against
	// the specified run ID. Requires Store to be set.
	CompareRunID string

	// MediaDir is the directory to write media files to. If empty, media
	// content is kept in memory as base64.
	MediaDir string
}

// RunResult contains the output of a completed evaluation run.
type RunResult struct {
	// Run is the full evaluation run with per-test-case results, aggregate
	// scores, cost, and duration.
	Run *models.Run

	// Comparison is populated if CompareRunID was set in Options.
	// Nil if no comparison was requested.
	Comparison *models.Comparison
}

// Run executes a full evaluation pipeline using the given config and options.
// This is the primary entry point for library consumers.
func Run(ctx context.Context, cfg *config.EvalConfig, opts Options) (*RunResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Apply overrides
	if opts.SampleSize > 0 {
		cfg.Dataset.Sample = opts.SampleSize
	}
	if opts.Concurrency > 0 {
		cfg.Pipeline.Concurrency = opts.Concurrency
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Initialize store
	st := opts.Store
	if st == nil {
		var err error
		st, err = store.NewSQLiteStore("")
		if err != nil {
			return nil, fmt.Errorf("creating default store: %w", err)
		}
		defer st.Close()
	}

	// Initialize generator provider
	prov, err := provider.NewProvider(cfg.Generator.Provider, "")
	if err != nil {
		return nil, fmt.Errorf("creating provider: %w", err)
	}

	// Build provider resolver for AI judge evaluators
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
			return nil, fmt.Errorf("creating evaluator %q: %w", evCfg.Name, err)
		}
		evals = append(evals, ev)
	}

	// Create and run pipeline
	p := pipeline.New(cfg, prov, evals, st, nil, logger)

	if opts.MediaDir != "" {
		p.SetMediaDir(opts.MediaDir)
	}

	run, err := p.Run(ctx)
	if err != nil {
		return nil, err
	}

	result := &RunResult{Run: run}

	// Compare against another run if requested
	if opts.CompareRunID != "" {
		comp, err := p.Compare(ctx, opts.CompareRunID, run.ID)
		if err != nil {
			logger.Warn("comparison failed", "error", err)
		} else {
			result.Comparison = comp
		}
	}

	return result, nil
}

// GenerateReport creates an HTML evaluation report from a RunResult.
func GenerateReport(result *RunResult, outputPath string) error {
	return report.GenerateReport(result.Run, result.Comparison, outputPath)
}
