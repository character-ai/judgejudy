package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/character-ai/judgejudy/pkg/config"
	"github.com/character-ai/judgejudy/pkg/models"
	"google.golang.org/api/iterator"
	"gopkg.in/yaml.v3"
)

// LoadDataset loads a dataset from a config reference (file path, GCS path, or inline).
func LoadDataset(ref config.DatasetRef) (*models.Dataset, error) {
	var ds *models.Dataset

	switch {
	case ref.Path != "":
		var err error
		if strings.HasPrefix(ref.Path, "gs://") {
			ds, err = loadFromGCS(ref.Path)
		} else {
			ds, err = loadFromFile(ref.Path)
		}
		if err != nil {
			return nil, fmt.Errorf("load dataset: %w", err)
		}
	case ref.Inline != nil:
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
	return parseDataset(data, filepath.Ext(path))
}

func parseDataset(data []byte, ext string) (*models.Dataset, error) {
	var ds models.Dataset
	switch strings.ToLower(ext) {
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

// parseGCSPath splits gs://bucket/path into bucket and object prefix.
func parseGCSPath(gcsPath string) (bucket, prefix string) {
	trimmed := strings.TrimPrefix(gcsPath, "gs://")
	parts := strings.SplitN(trimmed, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		prefix = parts[1]
	}
	return
}

// loadFromGCS loads a dataset from a GCS path.
// If the path points to a single file (.yaml/.json), it's parsed as a dataset.
// If it points to a prefix (directory), each object becomes a test case
// where the filename (without extension) is the test case ID and the content is the input.
func loadFromGCS(gcsPath string) (*models.Dataset, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w (ensure GOOGLE_APPLICATION_CREDENTIALS is set or run gcloud auth application-default login)", err)
	}
	defer client.Close()

	bucket, prefix := parseGCSPath(gcsPath)
	bkt := client.Bucket(bucket)

	// First, try to read it as a single file
	ext := strings.ToLower(filepath.Ext(prefix))
	if ext == ".yaml" || ext == ".yml" || ext == ".json" {
		data, err := readGCSObject(ctx, bkt, prefix)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", gcsPath, err)
		}
		return parseDataset(data, ext)
	}

	// Treat as a directory: list objects under the prefix and build test cases
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	it := bkt.Objects(ctx, &storage.Query{Prefix: prefix})
	var testCases []models.TestCase

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list objects under %s: %w", gcsPath, err)
		}

		// Skip "directory" markers
		if strings.HasSuffix(attrs.Name, "/") {
			continue
		}

		name := filepath.Base(attrs.Name)
		ext := filepath.Ext(name)
		id := strings.TrimSuffix(name, ext)

		data, err := readGCSObject(ctx, bkt, attrs.Name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", attrs.Name, err)
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		// Try to parse as YAML/JSON test case first
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			var tc models.TestCase
			if ext == ".json" {
				json.Unmarshal(data, &tc)
			} else {
				yaml.Unmarshal(data, &tc)
			}
			if tc.Input != "" {
				if tc.ID == "" {
					tc.ID = id
				}
				testCases = append(testCases, tc)
				continue
			}
		}

		// Otherwise treat the raw content as the input
		testCases = append(testCases, models.TestCase{
			ID:    id,
			Input: content,
		})
	}

	if len(testCases) == 0 {
		return nil, fmt.Errorf("no test cases found under %s", gcsPath)
	}

	return &models.Dataset{
		ID:        bucket + "/" + prefix,
		Name:      "GCS Dataset: " + gcsPath,
		Version:   "1.0.0",
		TestCases: testCases,
	}, nil
}

func readGCSObject(ctx context.Context, bkt *storage.BucketHandle, name string) ([]byte, error) {
	reader, err := bkt.Object(name).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
