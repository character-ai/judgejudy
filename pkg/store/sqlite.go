package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path.
// If path is empty, the default ~/.judgejudy/judgejudy.db is used.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		dir := filepath.Join(home, ".judgejudy")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
		path = filepath.Join(dir, "judgejudy.db")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			dataset_version TEXT NOT NULL,
			generator_config TEXT NOT NULL,
			evaluator_names TEXT NOT NULL,
			results TEXT NOT NULL,
			aggregate TEXT NOT NULL,
			total_cost_usd REAL NOT NULL DEFAULT 0,
			duration_seconds REAL NOT NULL DEFAULT 0,
			metadata TEXT,
			is_baseline INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS comparisons (
			id TEXT PRIMARY KEY,
			baseline_run_id TEXT NOT NULL,
			candidate_run_id TEXT NOT NULL,
			deltas TEXT NOT NULL,
			regressions TEXT NOT NULL,
			improvements TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (baseline_run_id) REFERENCES runs(id),
			FOREIGN KEY (candidate_run_id) REFERENCES runs(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_dataset ON runs(dataset_id)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_baseline ON runs(is_baseline)`,
		`CREATE TABLE IF NOT EXISTS human_evaluations (
			run_id TEXT NOT NULL,
			test_case_id TEXT NOT NULL,
			evaluator_name TEXT NOT NULL,
			human_score REAL NOT NULL,
			human_reasoning TEXT,
			scored_at TEXT NOT NULL,
			PRIMARY KEY (run_id, test_case_id, evaluator_name),
			FOREIGN KEY (run_id) REFERENCES runs(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_human_evals_run ON human_evaluations(run_id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("exec %q: %w", s[:40], err)
		}
	}
	return nil
}

func (s *SQLiteStore) SaveRun(ctx context.Context, run *models.Run) error {
	genCfg, err := json.Marshal(run.GeneratorConfig)
	if err != nil {
		return fmt.Errorf("marshal generator_config: %w", err)
	}
	evalNames, err := json.Marshal(run.EvaluatorNames)
	if err != nil {
		return fmt.Errorf("marshal evaluator_names: %w", err)
	}
	results, err := json.Marshal(run.Results)
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	agg, err := json.Marshal(run.Aggregate)
	if err != nil {
		return fmt.Errorf("marshal aggregate: %w", err)
	}
	var meta *string
	if run.Metadata != nil {
		b, err := json.Marshal(run.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		s := string(b)
		meta = &s
	}

	isBaseline := 0
	if run.IsBaseline {
		isBaseline = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO runs
		(id, timestamp, dataset_id, dataset_version, generator_config, evaluator_names,
		 results, aggregate, total_cost_usd, duration_seconds, metadata, is_baseline)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.Timestamp.Format(time.RFC3339Nano),
		run.DatasetID,
		run.DatasetVersion,
		string(genCfg),
		string(evalNames),
		string(results),
		string(agg),
		run.TotalCostUSD,
		run.DurationSeconds,
		meta,
		isBaseline,
	)
	return err
}

func (s *SQLiteStore) GetRun(ctx context.Context, id string) (*models.Run, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, timestamp, dataset_id, dataset_version, generator_config,
		        evaluator_names, results, aggregate, total_cost_usd, duration_seconds,
		        metadata, is_baseline
		 FROM runs WHERE id = ?`, id)
	return scanRunFull(row)
}

func (s *SQLiteStore) ListRuns(ctx context.Context, opts ListOpts) ([]models.Run, error) {
	query := `SELECT id, timestamp, dataset_id, dataset_version, generator_config,
	                  evaluator_names, aggregate, total_cost_usd, duration_seconds,
	                  metadata, is_baseline
	           FROM runs`
	var args []any

	if opts.DatasetID != "" {
		query += " WHERE dataset_id = ?"
		args = append(args, opts.DatasetID)
	}
	query += " ORDER BY timestamp DESC"
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
		if opts.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, opts.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []models.Run
	for rows.Next() {
		var (
			run        models.Run
			ts         string
			genCfg     string
			evalNames  string
			agg        string
			meta       sql.NullString
			isBaseline int
		)
		if err := rows.Scan(&run.ID, &ts, &run.DatasetID, &run.DatasetVersion,
			&genCfg, &evalNames, &agg, &run.TotalCostUSD, &run.DurationSeconds,
			&meta, &isBaseline); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			run.Timestamp = t
		}
		if err := json.Unmarshal([]byte(genCfg), &run.GeneratorConfig); err != nil {
			return nil, fmt.Errorf("unmarshal generator_config for run %s: %w", run.ID, err)
		}
		if err := json.Unmarshal([]byte(evalNames), &run.EvaluatorNames); err != nil {
			return nil, fmt.Errorf("unmarshal evaluator_names for run %s: %w", run.ID, err)
		}
		if err := json.Unmarshal([]byte(agg), &run.Aggregate); err != nil {
			return nil, fmt.Errorf("unmarshal aggregate for run %s: %w", run.ID, err)
		}
		if meta.Valid {
			_ = json.Unmarshal([]byte(meta.String), &run.Metadata)
		}
		run.IsBaseline = isBaseline != 0
		// Results intentionally left nil for list queries.
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *SQLiteStore) GetBaseline(ctx context.Context, datasetID string) (*models.Run, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, timestamp, dataset_id, dataset_version, generator_config,
		        evaluator_names, results, aggregate, total_cost_usd, duration_seconds,
		        metadata, is_baseline
		 FROM runs WHERE dataset_id = ? AND is_baseline = 1 LIMIT 1`, datasetID)
	return scanRunFull(row)
}

func (s *SQLiteStore) SetBaseline(ctx context.Context, runID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get the dataset_id for this run.
	var datasetID string
	err = tx.QueryRowContext(ctx, `SELECT dataset_id FROM runs WHERE id = ?`, runID).Scan(&datasetID)
	if err != nil {
		return fmt.Errorf("find run %s: %w", runID, err)
	}

	// Unset existing baseline for this dataset.
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET is_baseline = 0 WHERE dataset_id = ? AND is_baseline = 1`, datasetID); err != nil {
		return err
	}

	// Set new baseline.
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET is_baseline = 1 WHERE id = ?`, runID); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) SaveComparison(ctx context.Context, comp *models.Comparison) error {
	deltas, err := json.Marshal(comp.Deltas)
	if err != nil {
		return fmt.Errorf("marshal deltas: %w", err)
	}
	regressions, err := json.Marshal(comp.Regressions)
	if err != nil {
		return fmt.Errorf("marshal regressions: %w", err)
	}
	improvements, err := json.Marshal(comp.Improvements)
	if err != nil {
		return fmt.Errorf("marshal improvements: %w", err)
	}

	id := models.NewID()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO comparisons (id, baseline_run_id, candidate_run_id, deltas, regressions, improvements, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id,
		comp.BaselineRunID,
		comp.CandidateRunID,
		string(deltas),
		string(regressions),
		string(improvements),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanRunFull scans a single row (with results column) into a Run.
func scanRunFull(row *sql.Row) (*models.Run, error) {
	var (
		run        models.Run
		ts         string
		genCfg     string
		evalNames  string
		results    string
		agg        string
		meta       sql.NullString
		isBaseline int
	)

	err := row.Scan(&run.ID, &ts, &run.DatasetID, &run.DatasetVersion,
		&genCfg, &evalNames, &results, &agg, &run.TotalCostUSD, &run.DurationSeconds,
		&meta, &isBaseline)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("run not found")
		}
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		run.Timestamp = t
	}
	if err := json.Unmarshal([]byte(genCfg), &run.GeneratorConfig); err != nil {
		return nil, fmt.Errorf("unmarshal generator_config: %w", err)
	}
	if err := json.Unmarshal([]byte(evalNames), &run.EvaluatorNames); err != nil {
		return nil, fmt.Errorf("unmarshal evaluator_names: %w", err)
	}
	if err := json.Unmarshal([]byte(results), &run.Results); err != nil {
		return nil, fmt.Errorf("unmarshal results: %w", err)
	}
	if err := json.Unmarshal([]byte(agg), &run.Aggregate); err != nil {
		return nil, fmt.Errorf("unmarshal aggregate: %w", err)
	}
	if meta.Valid {
		_ = json.Unmarshal([]byte(meta.String), &run.Metadata)
	}
	run.IsBaseline = isBaseline != 0

	return &run, nil
}

func (s *SQLiteStore) SaveHumanEvaluations(ctx context.Context, evals []models.HumanEvaluation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO human_evaluations
		(run_id, test_case_id, evaluator_name, human_score, human_reasoning, scored_at)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, e := range evals {
		_, err := stmt.ExecContext(ctx, e.RunID, e.TestCaseID, e.EvaluatorName,
			e.HumanScore, e.HumanReasoning, e.ScoredAt.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("insert human eval %s/%s: %w", e.TestCaseID, e.EvaluatorName, err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetHumanEvaluations(ctx context.Context, runID string) ([]models.HumanEvaluation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, test_case_id, evaluator_name,
		human_score, human_reasoning, scored_at FROM human_evaluations WHERE run_id = ?`, runID)
	if err != nil {
		return nil, fmt.Errorf("query human evals: %w", err)
	}
	defer rows.Close()

	var evals []models.HumanEvaluation
	for rows.Next() {
		var e models.HumanEvaluation
		var scoredAt string
		var reasoning sql.NullString
		if err := rows.Scan(&e.RunID, &e.TestCaseID, &e.EvaluatorName,
			&e.HumanScore, &reasoning, &scoredAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if reasoning.Valid {
			e.HumanReasoning = reasoning.String
		}
		e.ScoredAt, _ = time.Parse(time.RFC3339, scoredAt)
		evals = append(evals, e)
	}
	return evals, rows.Err()
}
