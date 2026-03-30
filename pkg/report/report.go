package report

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
)

//go:embed templates/report.html
var reportTemplate string

// ReportData is the data structure passed to the HTML template.
type ReportData struct {
	Run        *models.Run
	Comparison *models.Comparison
	Title      string
	Generated  string
	Histograms map[string][]int // evaluator name -> bin counts (10 bins)
}

// GenerateReport builds an HTML report from a run and optional comparison,
// writing the result to outputPath. Media content is extracted to a sibling
// directory (<report_name>_media/) to keep the HTML file small.
func GenerateReport(run *models.Run, comparison *models.Comparison, outputPath string) error {
	// Extract any remaining media that wasn't flushed by the pipeline
	// (e.g. when generating a report from a stored run)
	if run != nil {
		extractMedia(run, outputPath)
	}

	data := buildReportData(run, comparison)

	funcMap := template.FuncMap{
		"truncate":     truncate,
		"formatFloat":  formatFloat,
		"scoreColor":   scoreColor,
		"scoreColorBg": scoreColorBg,
		"multiply":     multiply,
		"add":          add,
		"subtract":     subtract,
		"divFloat":     divFloat,
		"maxBin":       maxBin,
		"binColor":     binColor,
		"allPassed":    allPassed,
		"isMediaType":  isMediaType,
		"derefBool":    derefBool,
		"metricDesc":   metricDesc,
		"evalConfig":   evalConfigLookup(run),
		"deltaStatus":  deltaStatus,
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportTemplate)
	if err != nil {
		return fmt.Errorf("parsing report template: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("executing report template: %w", err)
	}

	return nil
}

func buildReportData(run *models.Run, comparison *models.Comparison) ReportData {
	title := "JudgeJudy Evaluation Report"
	if run != nil && run.Metadata != nil {
		if t, ok := run.Metadata["title"].(string); ok && t != "" {
			title = t
		}
	}

	histograms := make(map[string][]int)
	if run != nil {
		for _, evalName := range run.EvaluatorNames {
			bins := make([]int, 10)
			for _, result := range run.Results {
				if score, ok := result.Scores[evalName]; ok {
					idx := int(math.Floor(score.Value * 10))
					if idx < 0 {
						idx = 0
					}
					if idx > 9 {
						idx = 9
					}
					bins[idx]++
				}
			}
			histograms[evalName] = bins
		}
	}

	return ReportData{
		Run:        run,
		Comparison: comparison,
		Title:      title,
		Generated:  time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Histograms: histograms,
	}
}

// Template helper functions

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func formatFloat(f float64, prec int) string {
	return fmt.Sprintf("%.*f", prec, f)
}

func scoreColor(v float64) string {
	if v >= 0.8 {
		return "color-green"
	}
	if v >= 0.5 {
		return "color-yellow"
	}
	return "color-red"
}

func scoreColorBg(v float64) string {
	if v >= 0.8 {
		return "bg-green"
	}
	if v >= 0.5 {
		return "bg-yellow"
	}
	return "bg-red"
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func multiply(a, b any) float64 {
	return toFloat64(a) * toFloat64(b)
}

func add(a, b any) int {
	return int(toFloat64(a) + toFloat64(b))
}

func subtract(a, b any) float64 {
	return toFloat64(a) - toFloat64(b)
}

func divFloat(a, b any) float64 {
	bv := toFloat64(b)
	if bv == 0 {
		return 0
	}
	return toFloat64(a) / bv
}

func maxBin(bins []int) int {
	m := 0
	for _, v := range bins {
		if v > m {
			m = v
		}
	}
	return m
}

func binColor(binStart float64) string {
	if binStart >= 0.8 {
		return "#16a34a"
	}
	if binStart >= 0.5 {
		return "#ca8a04"
	}
	return "#dc2626"
}

func allPassed(scores map[string]models.Score) bool {
	for _, s := range scores {
		if s.Passed != nil && !*s.Passed {
			return false
		}
	}
	return true
}

// extractMedia writes base64 media content from results to files in a sibling
// directory, replacing Content with a relative file path (MediaPath).
func extractMedia(run *models.Run, reportPath string) {
	// Build media dir path: report.html -> report_media/
	base := strings.TrimSuffix(reportPath, filepath.Ext(reportPath))
	mediaDir := base + "_media"

	created := false
	for i := range run.Results {
		r := &run.Results[i]
		if r.GeneratedOutput.MediaPath != "" {
			continue // already flushed by pipeline
		}
		ct := r.GeneratedOutput.ContentType
		if ct == "" || r.GeneratedOutput.Content == "" {
			continue
		}
		if !strings.HasPrefix(ct, "audio") && !strings.HasPrefix(ct, "image") && !strings.HasPrefix(ct, "video") {
			continue
		}

		if !created {
			if err := os.MkdirAll(mediaDir, 0755); err != nil {
				return // cannot write media files
			}
			created = true
		}

		ext := extForMedia(ct)
		// Sanitize TestCaseID to prevent path traversal
		filename := fmt.Sprintf("%s%s", filepath.Base(r.TestCaseID), ext)
		filePath := filepath.Join(mediaDir, filename)

		data, err := base64.StdEncoding.DecodeString(r.GeneratedOutput.Content)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			continue
		}

		// Replace base64 content with relative path for the template
		relDir := filepath.Base(mediaDir)
		r.GeneratedOutput.MediaPath = relDir + "/" + filename
		r.GeneratedOutput.Content = "" // free the memory
	}
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

func isMediaType(contentType, prefix string) bool {
	return strings.HasPrefix(contentType, prefix+"/")
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// evalConfigLookup returns a function that retrieves evaluator config by name.
func evalConfigLookup(run *models.Run) func(string) models.EvaluatorConfig {
	m := make(map[string]models.EvaluatorConfig)
	if run != nil {
		for _, cfg := range run.EvaluatorConfigs {
			m[cfg.Name] = cfg
		}
	}
	return func(name string) models.EvaluatorConfig {
		return m[name]
	}
}

var metricDescriptions = map[string]string{
	"bertscore":            "Semantic similarity between generated and reference text using contextual embeddings (0-1, higher is better)",
	"rouge":                "ROUGE-L F1 overlap between generated and reference text, measuring recall of key phrases (0-1, higher is better)",
	"bleu":                 "N-gram precision between generated and reference text, common in translation evaluation (0-1, higher is better)",
	"clip_score":           "CLIP cosine similarity between generated image and text prompt — measures how well the image matches the description (0-1, higher is better)",
	"fid":                  "Frechet Inception Distance between generated and reference image distributions — lower FID means higher quality (normalized to 0-1, higher is better)",
	"lpips":                "Learned Perceptual Image Patch Similarity — perceptual distance between generated and reference images (normalized to 0-1, higher is better)",
	"ssim":                 "Structural Similarity Index between generated and reference images — measures luminance, contrast, and structure (0-1, higher is better)",
	"pesq":                 "Perceptual Evaluation of Speech Quality — ITU standard for voice quality, comparing generated vs reference audio (normalized to 0-1, higher is better)",
	"stoi":                 "Short-Time Objective Intelligibility — measures how understandable generated speech is vs reference (0-1, higher is better)",
	"utmos":                "UTMOS Mean Opinion Score — neural prediction of human-perceived speech quality, no reference needed (normalized to 0-1, higher is better)",
	"temporal_consistency": "Frame-to-frame SSIM across consecutive video frames — measures visual stability over time (0-1, higher is better)",
	"clip_temporal":        "CLIP embedding similarity between consecutive video frames — measures semantic consistency over time (0-1, higher is better)",
}

// deltaStatus returns "improved", "regressed", or "stable" based on the percent delta,
// matching the CLI's +/- 5% threshold logic.
func deltaStatus(pctDelta float64) string {
	if pctDelta > 5 {
		return "improved"
	}
	if pctDelta < -5 {
		return "regressed"
	}
	return "stable"
}

func metricDesc(name string) string {
	if desc, ok := metricDescriptions[name]; ok {
		return desc
	}
	// Try normalizing: "clip-score" -> "clip_score", "bertscore" -> "bertscore"
	normalized := strings.ReplaceAll(strings.ToLower(name), "-", "_")
	if desc, ok := metricDescriptions[normalized]; ok {
		return desc
	}
	return "AI judge score (0-1, higher is better)"
}

