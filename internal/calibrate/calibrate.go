package calibrate

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
)

// Calibrate compares human evaluations against AI scores and returns statistics.
func Calibrate(run *models.Run, humanEvals []models.HumanEvaluation, agreementThreshold float64) *models.CalibrationReport {
	if agreementThreshold <= 0 {
		agreementThreshold = 0.1
	}

	// Index AI scores: testCaseID -> evaluatorName -> score
	aiScores := make(map[string]map[string]float64)
	for _, r := range run.Results {
		m := make(map[string]float64)
		for name, score := range r.Scores {
			m[name] = score.Value
		}
		aiScores[r.TestCaseID] = m
	}

	// Index human evals by test case ID.
	// Human evals use evaluator_name "human" for a single overall score,
	// or a specific evaluator name for per-evaluator scoring.
	byTC := make(map[string]models.HumanEvaluation)
	byEvaluator := make(map[string][]models.HumanEvaluation)
	for _, he := range humanEvals {
		if he.EvaluatorName == "human" {
			byTC[he.TestCaseID] = he
		} else {
			byEvaluator[he.EvaluatorName] = append(byEvaluator[he.EvaluatorName], he)
		}
	}

	var results []models.CalibrationResult
	var allAI, allHuman []float64

	for _, evalName := range run.EvaluatorNames {
		// Use per-evaluator scores if available, otherwise use overall human scores
		hevals := byEvaluator[evalName]
		useOverall := len(hevals) == 0

		var ai, human []float64
		if useOverall {
			for tc, he := range byTC {
				if tcScores, ok := aiScores[tc]; ok {
					if aiScore, ok := tcScores[evalName]; ok {
						ai = append(ai, aiScore)
						human = append(human, (he.HumanScore-1.0)/4.0)
					}
				}
			}
		} else {
			for _, he := range hevals {
				if tcScores, ok := aiScores[he.TestCaseID]; ok {
					if aiScore, ok := tcScores[evalName]; ok {
						ai = append(ai, aiScore)
						human = append(human, (he.HumanScore-1.0)/4.0)
					}
				}
			}
		}

		if len(ai) < 2 {
			continue
		}

		allAI = append(allAI, ai...)
		allHuman = append(allHuman, human...)

		results = append(results, models.CalibrationResult{
			EvaluatorName:       evalName,
			SampleCount:         len(ai),
			PearsonCorrelation:  pearson(ai, human),
			SpearmanCorrelation: spearman(ai, human),
			MeanBias:            meanBias(ai, human),
			AgreementRate:       agreementRate(ai, human, agreementThreshold),
		})
	}

	report := &models.CalibrationReport{
		RunID:       run.ID,
		Evaluators:  results,
		GeneratedAt: time.Now().UTC(),
	}

	if len(allAI) >= 2 {
		report.OverallPearson = pearson(allAI, allHuman)
	}

	if len(results) > 0 {
		best, worst := results[0], results[0]
		for _, r := range results {
			// Skip NaN correlations (undefined: no variance in scores)
			if math.IsNaN(r.PearsonCorrelation) {
				continue
			}
			if math.IsNaN(best.PearsonCorrelation) || r.PearsonCorrelation > best.PearsonCorrelation {
				best = r
			}
			if math.IsNaN(worst.PearsonCorrelation) || r.PearsonCorrelation < worst.PearsonCorrelation {
				worst = r
			}
		}
		report.MostAligned = best.EvaluatorName
		report.LeastAligned = worst.EvaluatorName
	}

	return report
}

func pearson(x, y []float64) float64 {
	n := len(x)
	if n < 2 {
		return 0
	}
	mx, my := mean(x), mean(y)
	var num, dx2, dy2 float64
	for i := 0; i < n; i++ {
		dx := x[i] - mx
		dy := y[i] - my
		num += dx * dy
		dx2 += dx * dx
		dy2 += dy * dy
	}
	denom := math.Sqrt(dx2 * dy2)
	if denom == 0 {
		return math.NaN() // undefined: no variance in one or both series
	}
	return num / denom
}

func spearman(x, y []float64) float64 {
	return pearson(rank(x), rank(y))
}

func rank(data []float64) []float64 {
	type indexedVal struct {
		val float64
		idx int
	}
	n := len(data)
	iv := make([]indexedVal, n)
	for i, v := range data {
		iv[i] = indexedVal{v, i}
	}
	sort.Slice(iv, func(i, j int) bool { return iv[i].val < iv[j].val })

	ranks := make([]float64, n)
	i := 0
	for i < n {
		j := i + 1
		for j < n && iv[j].val == iv[i].val {
			j++
		}
		avgRank := float64(i+j+1) / 2.0 // average rank for ties
		for k := i; k < j; k++ {
			ranks[iv[k].idx] = avgRank
		}
		i = j
	}
	return ranks
}

func meanBias(ai, human []float64) float64 {
	var sum float64
	for i := range ai {
		sum += ai[i] - human[i]
	}
	return sum / float64(len(ai))
}

func agreementRate(ai, human []float64, threshold float64) float64 {
	agreed := 0
	for i := range ai {
		if math.Abs(ai[i]-human[i]) <= threshold {
			agreed++
		}
	}
	return float64(agreed) / float64(len(ai))
}

func mean(x []float64) float64 {
	var s float64
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

// Divergence captures a single AI-vs-human score disagreement.
type Divergence struct {
	TestCaseID     string
	Input          string
	AIScore        float64
	HumanScore     float64 // normalized 0-1
	HumanRaw       float64 // original 1-5
	Delta          float64 // AI - Human (positive = AI scored higher)
	AIReasoning    string
	HumanReasoning string
}

// BuildRubricSuggestionPrompt creates a prompt for an LLM to suggest rubric improvements
// based on where AI judges diverged most from human evaluators.
func BuildRubricSuggestionPrompt(run *models.Run, humanEvals []models.HumanEvaluation, report *models.CalibrationReport) string {
	// Index human evals by test case
	humanByTC := make(map[string]models.HumanEvaluation)
	for _, he := range humanEvals {
		if he.EvaluatorName == "human" {
			humanByTC[he.TestCaseID] = he
		}
	}

	// Index test case inputs
	inputByTC := make(map[string]string)
	for _, r := range run.Results {
		inputByTC[r.TestCaseID] = r.Input
	}

	// Find evaluator configs
	evalCfgByName := make(map[string]models.EvaluatorConfig)
	for _, cfg := range run.EvaluatorConfigs {
		evalCfgByName[cfg.Name] = cfg
	}

	var b strings.Builder
	b.WriteString("You are an expert in AI evaluation rubric design.\n\n")
	b.WriteString("I ran an AI evaluation pipeline and then had a human score the same outputs. ")
	b.WriteString("Below is the calibration data showing where AI judges diverged from human judgment.\n\n")
	b.WriteString("For each evaluator, I'll show:\n")
	b.WriteString("1. The current rubric\n")
	b.WriteString("2. Calibration statistics\n")
	b.WriteString("3. The biggest disagreements (where AI and human scored differently)\n\n")
	b.WriteString("Please suggest specific, actionable rubric improvements for each evaluator. ")
	b.WriteString("Explain WHY each change would help align the AI judge with human expectations.\n\n")
	b.WriteString("---\n\n")

	for _, cr := range report.Evaluators {
		cfg := evalCfgByName[cr.EvaluatorName]

		b.WriteString(fmt.Sprintf("## Evaluator: %s\n\n", cr.EvaluatorName))

		if cfg.Rubric != "" {
			b.WriteString("### Current Rubric\n```\n")
			b.WriteString(cfg.Rubric)
			b.WriteString("```\n\n")
		}

		if len(cfg.Dimensions) > 0 {
			b.WriteString("### Dimensions\n")
			for _, d := range cfg.Dimensions {
				b.WriteString("- " + d + "\n")
			}
			b.WriteString("\n")
		}

		biasDir := "higher"
		if cr.MeanBias < 0 {
			biasDir = "lower"
		}
		b.WriteString(fmt.Sprintf("### Calibration Stats\n"))
		b.WriteString(fmt.Sprintf("- Pearson correlation: %.3f\n", cr.PearsonCorrelation))
		b.WriteString(fmt.Sprintf("- Mean bias: %+.3f (AI scores %s than humans)\n", cr.MeanBias, biasDir))
		b.WriteString(fmt.Sprintf("- Agreement rate: %.1f%%\n\n", cr.AgreementRate*100))

		// Find biggest divergences for this evaluator
		var divs []Divergence
		for _, result := range run.Results {
			he, ok := humanByTC[result.TestCaseID]
			if !ok {
				continue
			}
			score, ok := result.Scores[cr.EvaluatorName]
			if !ok {
				continue
			}
			humanNorm := (he.HumanScore - 1.0) / 4.0
			delta := score.Value - humanNorm
			if math.Abs(delta) > 0.1 { // only show meaningful divergences
				divs = append(divs, Divergence{
					TestCaseID:     result.TestCaseID,
					Input:          result.Input,
					AIScore:        score.Value,
					HumanScore:     humanNorm,
					HumanRaw:       he.HumanScore,
					Delta:          delta,
					AIReasoning:    score.Reasoning,
					HumanReasoning: he.HumanReasoning,
				})
			}
		}

		// Sort by absolute divergence
		sort.Slice(divs, func(i, j int) bool {
			return math.Abs(divs[i].Delta) > math.Abs(divs[j].Delta)
		})

		// Show top 5 divergences
		limit := 5
		if len(divs) < limit {
			limit = len(divs)
		}

		if limit > 0 {
			b.WriteString("### Biggest Disagreements\n\n")
			for _, d := range divs[:limit] {
				direction := "AI scored HIGHER"
				if d.Delta < 0 {
					direction = "AI scored LOWER"
				}
				b.WriteString(fmt.Sprintf("**%s** — %s (AI: %.2f, Human: %.0f/5 = %.2f, delta: %+.2f)\n",
					d.TestCaseID, direction, d.AIScore, d.HumanRaw, d.HumanScore, d.Delta))
				b.WriteString(fmt.Sprintf("- Prompt: %s\n", truncate(d.Input, 150)))
				if d.AIReasoning != "" {
					b.WriteString(fmt.Sprintf("- AI reasoning: %s\n", truncate(d.AIReasoning, 200)))
				}
				if d.HumanReasoning != "" {
					b.WriteString(fmt.Sprintf("- Human reasoning: %s\n", d.HumanReasoning))
				}
				b.WriteString("\n")
			}
		} else {
			b.WriteString("No significant divergences found.\n\n")
		}

		b.WriteString("---\n\n")
	}

	b.WriteString("## Your Task\n\n")
	b.WriteString("For each evaluator above, provide:\n")
	b.WriteString("1. **Diagnosis**: What pattern of disagreement do you see? Why might the AI judge be scoring differently from the human?\n")
	b.WriteString("2. **Suggested rubric changes**: Specific text additions, removals, or rewording. Show the exact new rubric text.\n")
	b.WriteString("3. **Reasoning**: Why this change would improve alignment with human judgment.\n")
	b.WriteString("4. **Confidence**: How confident are you this change will help? (high/medium/low)\n\n")
	b.WriteString("Be specific and actionable. Don't just say 'be stricter' — say exactly what criteria to add or how to reword existing ones.\n")

	return b.String()
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
