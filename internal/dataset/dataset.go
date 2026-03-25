package dataset

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/character-ai/judgejudy/internal/config"
	"github.com/character-ai/judgejudy/internal/models"
	"gopkg.in/yaml.v3"
)

// LoadDataset loads a dataset from a config reference (file path or inline).
func LoadDataset(ref config.DatasetRef) (*models.Dataset, error) {
	var ds *models.Dataset

	switch {
	case ref.Path != "":
		loaded, err := loadFromFile(ref.Path)
		if err != nil {
			return nil, fmt.Errorf("load dataset from file: %w", err)
		}
		ds = loaded
	case ref.Inline != nil:
		// Make a copy so we don't mutate the config.
		cp := *ref.Inline
		cp.TestCases = make([]models.TestCase, len(ref.Inline.TestCases))
		copy(cp.TestCases, ref.Inline.TestCases)
		ds = &cp
	default:
		return nil, fmt.Errorf("dataset ref must have either path or inline set")
	}

	// Sample if requested.
	if ref.Sample > 0 && ref.Sample < len(ds.TestCases) {
		rand.Shuffle(len(ds.TestCases), func(i, j int) {
			ds.TestCases[i], ds.TestCases[j] = ds.TestCases[j], ds.TestCases[i]
		})
		ds.TestCases = ds.TestCases[:ref.Sample]
	}

	// Validate and assign IDs.
	if len(ds.TestCases) == 0 {
		return nil, fmt.Errorf("dataset must have at least 1 test case")
	}
	for i := range ds.TestCases {
		if ds.TestCases[i].Input == "" {
			return nil, fmt.Errorf("test case at index %d has empty input", i)
		}
		if ds.TestCases[i].ID == "" {
			ds.TestCases[i].ID = models.NewID()
		}
	}

	return ds, nil
}

func loadFromFile(path string) (*models.Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var ds models.Dataset
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &ds); err != nil {
			return nil, fmt.Errorf("unmarshal YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &ds); err != nil {
			return nil, fmt.Errorf("unmarshal JSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported dataset file extension: %s", ext)
	}

	return &ds, nil
}
