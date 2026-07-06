package pipeline

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/character-ai/judgejudy/pkg/models"
)

// meterName identifies the instrumentation scope for pipeline metrics.
const meterName = "github.com/character-ai/judgejudy/pkg/pipeline"

// Attribute values for the "status" attribute.
const (
	statusSuccess = "success"
	statusError   = "error"
)

// pipelineMetrics holds the OpenTelemetry instruments recorded by the
// Pipeline. A nil *pipelineMetrics is valid and records nothing, so the
// pipeline never has to nil-check at call sites.
type pipelineMetrics struct {
	runs               metric.Int64Counter
	runDuration        metric.Float64Histogram
	testCases          metric.Int64Counter
	testCaseDuration   metric.Float64Histogram
	testCasesInFlight  metric.Int64UpDownCounter
	generations        metric.Int64Counter
	generationDuration metric.Float64Histogram
	generationRetries  metric.Int64Counter
	generationTokens   metric.Int64Counter
	generationCost     metric.Float64Counter
	cacheRequests      metric.Int64Counter
	evaluations        metric.Int64Counter
	evaluationDuration metric.Float64Histogram
	evaluationRetries  metric.Int64Counter
}

func newPipelineMetrics(mp metric.MeterProvider) (*pipelineMetrics, error) {
	meter := mp.Meter(meterName)
	m := &pipelineMetrics{}
	var errs []error

	instr := func(err error) { errs = append(errs, err) }

	var err error
	m.runs, err = meter.Int64Counter("judgejudy.runs",
		metric.WithDescription("Completed evaluation runs"),
		metric.WithUnit("{run}"))
	instr(err)
	m.runDuration, err = meter.Float64Histogram("judgejudy.run.duration",
		metric.WithDescription("End-to-end duration of an evaluation run"),
		metric.WithUnit("s"))
	instr(err)
	m.testCases, err = meter.Int64Counter("judgejudy.test_cases",
		metric.WithDescription("Processed test cases"),
		metric.WithUnit("{test_case}"))
	instr(err)
	m.testCaseDuration, err = meter.Float64Histogram("judgejudy.test_case.duration",
		metric.WithDescription("Duration of a single test case (generation + evaluation)"),
		metric.WithUnit("s"))
	instr(err)
	m.testCasesInFlight, err = meter.Int64UpDownCounter("judgejudy.test_cases.in_flight",
		metric.WithDescription("Test cases currently being processed"),
		metric.WithUnit("{test_case}"))
	instr(err)
	m.generations, err = meter.Int64Counter("judgejudy.generation.requests",
		metric.WithDescription("Generation requests to the provider, by final outcome"),
		metric.WithUnit("{request}"))
	instr(err)
	m.generationDuration, err = meter.Float64Histogram("judgejudy.generation.duration",
		metric.WithDescription("Duration of successful generation calls"),
		metric.WithUnit("s"))
	instr(err)
	m.generationRetries, err = meter.Int64Counter("judgejudy.generation.retries",
		metric.WithDescription("Generation attempts that failed and were retried"),
		metric.WithUnit("{retry}"))
	instr(err)
	m.generationTokens, err = meter.Int64Counter("judgejudy.generation.tokens",
		metric.WithDescription("Tokens used by generation calls"),
		metric.WithUnit("{token}"))
	instr(err)
	m.generationCost, err = meter.Float64Counter("judgejudy.generation.cost",
		metric.WithDescription("Cost of generation calls in US dollars"),
		metric.WithUnit("{USD}"))
	instr(err)
	m.cacheRequests, err = meter.Int64Counter("judgejudy.cache.requests",
		metric.WithDescription("Generation cache lookups, by result (hit or miss)"),
		metric.WithUnit("{request}"))
	instr(err)
	m.evaluations, err = meter.Int64Counter("judgejudy.evaluations",
		metric.WithDescription("Evaluator executions, by final outcome"),
		metric.WithUnit("{evaluation}"))
	instr(err)
	m.evaluationDuration, err = meter.Float64Histogram("judgejudy.evaluation.duration",
		metric.WithDescription("Duration of successful evaluator calls"),
		metric.WithUnit("s"))
	instr(err)
	m.evaluationRetries, err = meter.Int64Counter("judgejudy.evaluation.retries",
		metric.WithDescription("Evaluator attempts that failed and were retried"),
		metric.WithUnit("{retry}"))
	instr(err)

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *pipelineMetrics) recordRun(ctx context.Context, seconds float64, status string) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("status", status))
	m.runs.Add(ctx, 1, attrs)
	m.runDuration.Record(ctx, seconds, attrs)
}

func (m *pipelineMetrics) testCaseStarted(ctx context.Context) {
	if m == nil {
		return
	}
	m.testCasesInFlight.Add(ctx, 1)
}

func (m *pipelineMetrics) testCaseFinished(ctx context.Context, seconds float64, modality models.Modality, status string) {
	if m == nil {
		return
	}
	m.testCasesInFlight.Add(ctx, -1)
	attrs := metric.WithAttributes(
		attribute.String("modality", string(modality)),
		attribute.String("status", status),
	)
	m.testCases.Add(ctx, 1, attrs)
	m.testCaseDuration.Record(ctx, seconds, attrs)
}

func (m *pipelineMetrics) recordGenerationSuccess(ctx context.Context, seconds float64, resp *models.GenerateResponse, provider string, modality models.Modality) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", resp.ModelUsed),
		attribute.String("modality", string(modality)),
	)
	m.generations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", resp.ModelUsed),
		attribute.String("status", statusSuccess),
	))
	m.generationDuration.Record(ctx, seconds, attrs)
	if resp.TokensUsed > 0 {
		m.generationTokens.Add(ctx, int64(resp.TokensUsed), attrs)
	}
	if resp.CostUSD > 0 {
		m.generationCost.Add(ctx, resp.CostUSD, attrs)
	}
}

func (m *pipelineMetrics) recordGenerationFailure(ctx context.Context, provider, model string) {
	if m == nil {
		return
	}
	m.generations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("status", statusError),
	))
}

func (m *pipelineMetrics) recordGenerationRetry(ctx context.Context, provider, model string) {
	if m == nil {
		return
	}
	m.generationRetries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", model),
	))
}

func (m *pipelineMetrics) recordCacheLookup(ctx context.Context, hit bool) {
	if m == nil {
		return
	}
	result := "miss"
	if hit {
		result = "hit"
	}
	m.cacheRequests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *pipelineMetrics) recordEvaluationSuccess(ctx context.Context, evaluator string, seconds float64) {
	if m == nil {
		return
	}
	m.evaluations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("evaluator", evaluator),
		attribute.String("status", statusSuccess),
	))
	m.evaluationDuration.Record(ctx, seconds, metric.WithAttributes(
		attribute.String("evaluator", evaluator),
	))
}

func (m *pipelineMetrics) recordEvaluationFailure(ctx context.Context, evaluator string) {
	if m == nil {
		return
	}
	m.evaluations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("evaluator", evaluator),
		attribute.String("status", statusError),
	))
}

func (m *pipelineMetrics) recordEvaluationRetry(ctx context.Context, evaluator string) {
	if m == nil {
		return
	}
	m.evaluationRetries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("evaluator", evaluator),
	))
}
