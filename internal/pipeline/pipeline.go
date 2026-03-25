package pipeline

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/character-ai/judgejudy/internal/config"
	"github.com/character-ai/judgejudy/internal/dataset"
	"github.com/character-ai/judgejudy/internal/evaluator"
	"github.com/character-ai/judgejudy/internal/models"
	"github.com/character-ai/judgejudy/internal/provider"
	"github.com/character-ai/judgejudy/internal/store"
	"golang.org/x/sync/errgroup"
)

// Pipeline orchestrates end-to-end evaluation runs.
type Pipeline struct {
	cfg        *config.EvalConfig
	provider   provider.Provider
	evaluators []evaluator.Evaluator
	store      store.Store
	cache      *store.Cache
	logger     *slog.Logger
	mediaDir   string // directory to write media files to (if set, frees base64 from memory)
}

// New creates a new Pipeline.
func New(cfg *config.EvalConfig, prov provider.Provider, evals []evaluator.Evaluator, st store.Store, cache *store.Cache, logger *slog.Logger) *Pipeline {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pipeline{
		cfg:        cfg,
		provider:   prov,
		evaluators: evals,
		store:      st,
		cache:      cache,
		logger:     logger,
	}
}

// SetMediaDir configures the pipeline to write media files to disk as they are
// generated, freeing base64 content from memory immediately. The media dir path
// is relative to the working directory.
func (p *Pipeline) SetMediaDir(dir string) {
	p.mediaDir = dir
}

// Run executes the full evaluation pipeline.
func (p *Pipeline) Run(ctx context.Context) (*models.Run, error) {
	// Load dataset
	ds, err := dataset.LoadDataset(p.cfg.Dataset)
	if err != nil {
		return nil, fmt.Errorf("loading dataset: %w", err)
	}

	run := &models.Run{
		ID:               models.NewID(),
		Timestamp:        time.Now().UTC(),
		DatasetID:        ds.ID,
		DatasetVersion:   ds.Version,
		GeneratorConfig:  p.cfg.Generator,
		EvaluatorNames:   make([]string, len(p.evaluators)),
		EvaluatorConfigs: p.cfg.Evaluators,
		Results:          make([]models.TestResult, len(ds.TestCases)),
		Metadata:         map[string]any{"dataset_name": ds.Name},
	}
	for i, ev := range p.evaluators {
		run.EvaluatorNames[i] = ev.Name()
	}

	start := time.Now()
	total := len(ds.TestCases)
	var completed atomic.Int64

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(p.cfg.Pipeline.Concurrency)

	var mu sync.Mutex
	var totalCost float64

	for i, tc := range ds.TestCases {
		i, tc := i, tc
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}

			result, err := p.processTestCase(gctx, tc, ds.Modality)
			if err != nil {
				if p.cfg.Pipeline.FailFast {
					return fmt.Errorf("test case %s: %w", tc.ID, err)
				}
				p.logger.Error("test case failed", "id", tc.ID, "error", err)
				result = &models.TestResult{
					TestCaseID: tc.ID,
					Input:      tc.Input,
					Scores:     map[string]models.Score{},
				}
			}

			// Write media to disk immediately to free memory
			if p.mediaDir != "" {
				p.flushMedia(result)
			}

			mu.Lock()
			run.Results[i] = *result
			totalCost += result.GeneratedOutput.CostUSD
			mu.Unlock()

			done := completed.Add(1)
			p.logger.Info("completed", "test_case", tc.ID, "progress", fmt.Sprintf("%d/%d", done, total))
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	run.DurationSeconds = time.Since(start).Seconds()
	run.TotalCostUSD = totalCost
	run.Aggregate = computeAggregate(run.Results, p.evaluators)

	if err := p.store.SaveRun(ctx, run); err != nil {
		return nil, fmt.Errorf("saving run: %w", err)
	}

	return run, nil
}

func (p *Pipeline) processTestCase(ctx context.Context, tc models.TestCase, modality models.Modality) (*models.TestResult, error) {
	timeout := time.Duration(p.cfg.Pipeline.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req := models.GenerateRequest{
		Prompt:   tc.Input,
		Modality: modality,
		Params:   p.cfg.Generator.Params,
	}

	// Check cache
	var genResp *models.GenerateResponse
	var cacheKey string
	if p.cache != nil && p.cfg.Pipeline.CacheEnabled {
		cacheKey = store.GenerateKey(p.cfg.Generator.Provider, p.cfg.Generator.Model, tc.Input, p.cfg.Generator.Params)
		cached, err := p.cache.Get(ctx, cacheKey)
		if err == nil && cached != nil {
			p.logger.Debug("cache hit", "test_case", tc.ID)
			genResp = cached
		}
	}

	// Generate if not cached
	if genResp == nil {
		var err error
		for attempt := 0; attempt <= p.cfg.Pipeline.RetryAttempts; attempt++ {
			genResp, err = p.provider.Generate(ctx, &req)
			if err == nil {
				break
			}
			if pe, ok := err.(*models.ProviderError); ok && !pe.Retryable {
				return nil, err
			}
			if attempt < p.cfg.Pipeline.RetryAttempts {
				p.logger.Warn("retrying generation", "test_case", tc.ID, "attempt", attempt+1, "error", err)
				time.Sleep(time.Duration(attempt+1) * time.Second)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("generation failed after retries: %w", err)
		}

		// Cache result
		if p.cache != nil && p.cfg.Pipeline.CacheEnabled && cacheKey != "" {
			if err := p.cache.Set(ctx, cacheKey, genResp); err != nil {
				p.logger.Warn("cache set failed", "error", err)
			}
		}
	}

	// Run evaluators
	scores := make(map[string]models.Score, len(p.evaluators))
	var scoresMu sync.Mutex
	eg, ectx := errgroup.WithContext(ctx)

	for _, ev := range p.evaluators {
		ev := ev
		eg.Go(func() error {
			score, err := ev.Evaluate(ectx, tc, *genResp)
			if err != nil {
				p.logger.Error("evaluator failed", "evaluator", ev.Name(), "test_case", tc.ID, "error", err)
				return nil // Don't fail the whole test case for one evaluator
			}
			scoresMu.Lock()
			scores[ev.Name()] = *score
			scoresMu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("evaluators for test case %s: %w", tc.ID, err)
	}

	return &models.TestResult{
		TestCaseID:      tc.ID,
		Input:           tc.Input,
		GeneratedOutput: *genResp,
		Scores:          scores,
	}, nil
}

// Compare compares two runs.
func (p *Pipeline) Compare(ctx context.Context, baselineID, candidateID string) (*models.Comparison, error) {
	baseline, err := p.store.GetRun(ctx, baselineID)
	if err != nil {
		return nil, fmt.Errorf("loading baseline run: %w", err)
	}
	candidate, err := p.store.GetRun(ctx, candidateID)
	if err != nil {
		return nil, fmt.Errorf("loading candidate run: %w", err)
	}

	comp := &models.Comparison{
		BaselineRunID:  baselineID,
		CandidateRunID: candidateID,
		Deltas:         make(map[string]models.MetricDelta),
	}

	for evalName, baselineMean := range baseline.Aggregate.MeanScores {
		candidateMean, ok := candidate.Aggregate.MeanScores[evalName]
		if !ok {
			continue
		}

		delta := candidateMean - baselineMean
		var pctDelta float64
		if baselineMean != 0 {
			pctDelta = (delta / baselineMean) * 100
		}

		// NOTE: This assumes higher is better, which is correct for normalized scores (0-1)
		// but would be wrong for raw lower-is-better metrics. A HigherIsBetter flag on the
		// model would be needed to handle both cases. Since the Python metrics already
		// normalize scores to 0-1 where higher is better, this works for most cases.
		improved := delta > 0
		comp.Deltas[evalName] = models.MetricDelta{
			BaselineMean:  baselineMean,
			CandidateMean: candidateMean,
			AbsoluteDelta: delta,
			PercentDelta:  pctDelta,
			Improved:      improved,
		}

		if pctDelta < -5.0 {
			comp.Regressions = append(comp.Regressions, evalName)
		} else if pctDelta > 5.0 {
			comp.Improvements = append(comp.Improvements, evalName)
		}
	}

	if err := p.store.SaveComparison(ctx, comp); err != nil {
		return nil, fmt.Errorf("saving comparison: %w", err)
	}

	return comp, nil
}

func computeAggregate(results []models.TestResult, evals []evaluator.Evaluator) models.AggregateMetrics {
	agg := models.AggregateMetrics{
		MeanScores:   make(map[string]float64),
		MedianScores: make(map[string]float64),
		P5Scores:     make(map[string]float64),
		P95Scores:    make(map[string]float64),
		PassRates:    make(map[string]float64),
	}

	// Collect scores per evaluator
	evalScores := make(map[string][]float64)
	evalPassed := make(map[string]int)
	evalTotal := make(map[string]int)

	for _, r := range results {
		for name, score := range r.Scores {
			evalScores[name] = append(evalScores[name], score.Value)
			if score.Passed != nil {
				evalTotal[name]++
				if *score.Passed {
					evalPassed[name]++
				}
			}
		}
	}

	for name, scores := range evalScores {
		if len(scores) == 0 {
			continue
		}
		sort.Float64s(scores)
		agg.MeanScores[name] = mean(scores)
		agg.MedianScores[name] = median(scores)
		agg.P5Scores[name] = percentile(scores, 5)
		agg.P95Scores[name] = percentile(scores, 95)

		if total, ok := evalTotal[name]; ok && total > 0 {
			agg.PassRates[name] = float64(evalPassed[name]) / float64(total)
		}
	}

	// TotalPassRate: % of test cases where ALL thresholded evaluators passed.
	// Skip test cases with empty Scores maps and those with no thresholded evaluators.
	if len(results) > 0 {
		allPassed := 0
		denominator := 0
		for _, r := range results {
			if len(r.Scores) == 0 {
				continue
			}
			hasThreshold := false
			passed := true
			for _, score := range r.Scores {
				if score.Passed != nil {
					hasThreshold = true
					if !*score.Passed {
						passed = false
						break
					}
				}
			}
			if !hasThreshold {
				continue
			}
			denominator++
			if passed {
				allPassed++
			}
		}
		if denominator > 0 {
			agg.TotalPassRate = float64(allPassed) / float64(denominator)
		}
	}

	return agg
}

// flushMedia writes media content from a test result to disk and replaces
// the base64 content with a MediaPath, freeing memory.
func (p *Pipeline) flushMedia(result *models.TestResult) {
	ct := result.GeneratedOutput.ContentType
	content := result.GeneratedOutput.Content
	if content == "" || ct == "" {
		return
	}
	if !strings.HasPrefix(ct, "audio") && !strings.HasPrefix(ct, "image") && !strings.HasPrefix(ct, "video") {
		return
	}

	os.MkdirAll(p.mediaDir, 0755)

	ext := extForMedia(ct)
	filename := result.TestCaseID + ext
	filePath := filepath.Join(p.mediaDir, filename)

	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return
	}

	relDir := filepath.Base(p.mediaDir)
	result.GeneratedOutput.MediaPath = relDir + "/" + filename
	result.GeneratedOutput.Content = "" // free memory
}

func extForMedia(ct string) string {
	switch {
	case strings.Contains(ct, "wav"):
		return ".wav"
	case strings.Contains(ct, "mp3"), strings.Contains(ct, "mpeg"):
		return ".mp3"
	case strings.Contains(ct, "mp4"):
		return ".mp4"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "webp"):
		return ".webp"
	default:
		return ".bin"
	}
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(n-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper || upper >= n {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
