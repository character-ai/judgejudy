package store

import (
	"context"

	"github.com/character-ai/judgejudy/internal/models"
)

// Store is the persistence interface for evaluation runs and comparisons.
type Store interface {
	SaveRun(ctx context.Context, run *models.Run) error
	GetRun(ctx context.Context, id string) (*models.Run, error)
	ListRuns(ctx context.Context, opts ListOpts) ([]models.Run, error)
	GetBaseline(ctx context.Context, datasetID string) (*models.Run, error)
	SetBaseline(ctx context.Context, runID string) error
	SaveComparison(ctx context.Context, comp *models.Comparison) error
	Close() error
}

// ListOpts configures run listing queries.
type ListOpts struct {
	DatasetID string
	Limit     int
	Offset    int
}
