package pipeline

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/character-ai/judgejudy/pkg/config"
	"github.com/character-ai/judgejudy/pkg/evaluator"
	"github.com/character-ai/judgejudy/pkg/models"
	"github.com/character-ai/judgejudy/pkg/store"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeProvider fails the first failFirst Generate calls with a retryable
// error, then succeeds.
type fakeProvider struct {
	calls     atomic.Int64
	failFirst int64
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) SupportsModality(m models.Modality) bool { return true }

func (f *fakeProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	if f.calls.Add(1) <= f.failFirst {
		return nil, &models.ProviderError{Provider: "fake", Message: "transient", Retryable: true}
	}
	return &models.GenerateResponse{
		Content:    "output",
		LatencyMs:  5,
		CostUSD:    0.001,
		TokensUsed: 10,
		ModelUsed:  "fake-model",
	}, nil
}

type fakeEvaluator struct{}

func (fakeEvaluator) Name() string          { return "fake-eval" }
func (fakeEvaluator) Type() models.EvalType { return models.EvalTypeMetric }
func (fakeEvaluator) Evaluate(ctx context.Context, input models.TestCase, output models.GenerateResponse) (*models.Score, error) {
	return &models.Score{Value: 1}, nil
}

type fakeStore struct{}

func (fakeStore) SaveRun(ctx context.Context, run *models.Run) error         { return nil }
func (fakeStore) GetRun(ctx context.Context, id string) (*models.Run, error) { return nil, nil }
func (fakeStore) ListRuns(ctx context.Context, opts store.ListOpts) ([]models.Run, error) {
	return nil, nil
}
func (fakeStore) GetBaseline(ctx context.Context, datasetID string) (*models.Run, error) {
	return nil, nil
}
func (fakeStore) SetBaseline(ctx context.Context, runID string) error { return nil }
func (fakeStore) SaveComparison(ctx context.Context, comp *models.Comparison) error {
	return nil
}
func (fakeStore) SaveHumanEvaluations(ctx context.Context, evals []models.HumanEvaluation) error {
	return nil
}
func (fakeStore) GetHumanEvaluations(ctx context.Context, runID string) ([]models.HumanEvaluation, error) {
	return nil, nil
}
func (fakeStore) Close() error { return nil }

func testConfig(numCases int) *config.EvalConfig {
	tcs := make([]models.TestCase, numCases)
	for i := range tcs {
		tcs[i] = models.TestCase{Input: "prompt"}
	}
	cfg := &config.EvalConfig{
		Name: "metrics-test",
		Dataset: config.DatasetRef{
			Inline: &models.Dataset{
				ID:        "ds-test",
				Name:      "ds-test",
				Modality:  models.ModalityText,
				TestCases: tcs,
			},
		},
		Generator: models.ProviderConfig{Provider: "fake", Model: "fake-model"},
	}
	cfg.Defaults()
	return cfg
}

// collect gathers all metrics recorded so far, keyed by instrument name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collecting metrics: %v", err)
	}
	out := make(map[string]metricdata.Metrics)
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != meterName {
			continue
		}
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

// sumInt64 totals all data points of an int64 sum instrument.
func sumInt64(t *testing.T, metrics map[string]metricdata.Metrics, name string) int64 {
	t.Helper()
	m, ok := metrics[name]
	if !ok {
		t.Fatalf("metric %q not recorded; got %v", name, keys(metrics))
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("metric %q is %T, want Sum[int64]", name, m.Data)
	}
	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

func keys(m map[string]metricdata.Metrics) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPipelineRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(context.Background())

	cfg := testConfig(3)
	cfg.Pipeline.RetryAttempts = 1

	prov := &fakeProvider{failFirst: 1} // first Generate call fails, forcing one retry
	p := New(cfg, prov, []evaluator.Evaluator{fakeEvaluator{}}, fakeStore{}, nil, nil)
	p.SetMeterProvider(mp)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	metrics := collect(t, reader)

	if got := sumInt64(t, metrics, "judgejudy.runs"); got != 1 {
		t.Errorf("judgejudy.runs = %d, want 1", got)
	}
	if got := sumInt64(t, metrics, "judgejudy.test_cases"); got != 3 {
		t.Errorf("judgejudy.test_cases = %d, want 3", got)
	}
	if got := sumInt64(t, metrics, "judgejudy.test_cases.in_flight"); got != 0 {
		t.Errorf("judgejudy.test_cases.in_flight = %d, want 0 after run", got)
	}
	if got := sumInt64(t, metrics, "judgejudy.generation.requests"); got != 3 {
		t.Errorf("judgejudy.generation.requests = %d, want 3", got)
	}
	if got := sumInt64(t, metrics, "judgejudy.generation.retries"); got != 1 {
		t.Errorf("judgejudy.generation.retries = %d, want 1", got)
	}
	if got := sumInt64(t, metrics, "judgejudy.generation.tokens"); got != 30 {
		t.Errorf("judgejudy.generation.tokens = %d, want 30", got)
	}
	if got := sumInt64(t, metrics, "judgejudy.evaluations"); got != 3 {
		t.Errorf("judgejudy.evaluations = %d, want 3", got)
	}

	if _, ok := metrics["judgejudy.run.duration"]; !ok {
		t.Error("judgejudy.run.duration not recorded")
	}
	if _, ok := metrics["judgejudy.generation.duration"]; !ok {
		t.Error("judgejudy.generation.duration not recorded")
	}
	if _, ok := metrics["judgejudy.evaluation.duration"]; !ok {
		t.Error("judgejudy.evaluation.duration not recorded")
	}
}

// TestPipelineWithoutMeterProvider ensures the pipeline runs cleanly with the
// default (no-op) global meter provider.
func TestPipelineWithoutMeterProvider(t *testing.T) {
	cfg := testConfig(2)
	p := New(cfg, &fakeProvider{}, []evaluator.Evaluator{fakeEvaluator{}}, fakeStore{}, nil, nil)
	run, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	if len(run.Results) != 2 {
		t.Errorf("got %d results, want 2", len(run.Results))
	}
}
