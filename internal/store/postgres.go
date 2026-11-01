// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements the Store interface using PostgreSQL with pgxpool.
type PostgresStore struct {
	pool *pgxpool.Pool
	cfg  Config
}

// pgxTxStore wraps a pgx.Tx to provide Store methods within a single transaction.
type pgxTxStore struct {
	tx pgx.Tx
}

// NewPostgresStore establishes a connection pool to PostgreSQL and runs pending migrations.
func NewPostgresStore(ctx context.Context, cfg Config) (*PostgresStore, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("store: database URL is empty")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: failed to parse database URL: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		poolCfg.MaxConns = cfg.MaxOpenConns
	}
	if cfg.MinIdleConns > 0 {
		poolCfg.MinConns = cfg.MinIdleConns
	}
	if cfg.ConnMaxLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	}
	if cfg.ConnMaxIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.ConnMaxIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("store: failed to create pgx pool: %w", err)
	}

	// Verify connectivity
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping failed for %s: %w", SanitizeDatabaseURL(cfg.DatabaseURL), err)
	}

	store := &PostgresStore{
		pool: pool,
		cfg:  cfg,
	}

	// Run migrations
	tx, err := pool.Begin(ctx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: failed to start migration transaction: %w", err)
	}
	defer tx.Rollback(ctx) // Safe to call if already committed

	if err := Migrate(ctx, tx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: migration failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: failed to commit migrations: %w", err)
	}

	return store, nil
}

// Close gracefully closes all database connections in the pool.
func (p *PostgresStore) Close() error {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
	return nil
}

// Pool returns the underlying pgxpool.Pool (for health checks / metrics).
func (p *PostgresStore) Pool() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	return p.pool
}

// Ping checks database connectivity.
func (p *PostgresStore) Ping(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}
	return p.pool.Ping(ctx)
}

func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" { // unique_violation
			return ErrConflict
		}
	}
	return err
}

// CreateRun inserts a new run entity into PostgreSQL.
func (p *PostgresStore) CreateRun(ctx context.Context, run *Run) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}
	return createRun(ctx, p.pool, run)
}

func (p *PostgresStore) GetRun(ctx context.Context, id string) (*Run, error) {
	if p == nil || p.pool == nil {
		return nil, ErrStoreClosed
	}
	return getRun(ctx, p.pool, id)
}

func (p *PostgresStore) ListRuns(ctx context.Context, filter RunFilter) ([]*Run, error) {
	if p == nil || p.pool == nil {
		return nil, ErrStoreClosed
	}
	return listRuns(ctx, p.pool, filter)
}

func (p *PostgresStore) UpdateRunStatus(ctx context.Context, id string, status RunStatus, runErr error) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}
	return updateRunStatus(ctx, p.pool, id, status, runErr)
}

func (p *PostgresStore) RecordStep(ctx context.Context, step *Step) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}
	return recordStep(ctx, p.pool, step)
}

func (p *PostgresStore) ListStepsByRun(ctx context.Context, runID string) ([]*Step, error) {
	if p == nil || p.pool == nil {
		return nil, ErrStoreClosed
	}
	return listStepsByRun(ctx, p.pool, runID)
}

func (p *PostgresStore) RecordPendingGate(ctx context.Context, gate *PendingGate) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}
	return recordPendingGate(ctx, p.pool, gate)
}

func (p *PostgresStore) RecordGateAudit(ctx context.Context, entry *GateAudit) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}
	return recordGateAudit(ctx, p.pool, entry)
}

func (p *PostgresStore) ListPendingGates(ctx context.Context) ([]*PendingGate, error) {
	if p == nil || p.pool == nil {
		return nil, ErrStoreClosed
	}
	return listPendingGates(ctx, p.pool)
}

func (p *PostgresStore) ResolveGate(ctx context.Context, token string, resolution GateResolution) (*GateAudit, error) {
	if p == nil || p.pool == nil {
		return nil, ErrStoreClosed
	}
	var audit *GateAudit
	err := p.WithTx(ctx, func(tx Store) error {
		var err error
		audit, err = tx.ResolveGate(ctx, token, resolution)
		return err
	})
	return audit, err
}

func (p *PostgresStore) WithTx(ctx context.Context, fn func(tx Store) error) error {
	if p == nil || p.pool == nil {
		return ErrStoreClosed
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txStore := &pgxTxStore{tx: tx}
	if err := fn(txStore); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// pgxTxStore implementation

func (tx *pgxTxStore) CreateRun(ctx context.Context, run *Run) error {
	return createRun(ctx, tx.tx, run)
}

func (tx *pgxTxStore) GetRun(ctx context.Context, id string) (*Run, error) {
	return getRun(ctx, tx.tx, id)
}

func (tx *pgxTxStore) ListRuns(ctx context.Context, filter RunFilter) ([]*Run, error) {
	return listRuns(ctx, tx.tx, filter)
}

func (tx *pgxTxStore) UpdateRunStatus(ctx context.Context, id string, status RunStatus, runErr error) error {
	return updateRunStatus(ctx, tx.tx, id, status, runErr)
}

func (tx *pgxTxStore) RecordStep(ctx context.Context, step *Step) error {
	return recordStep(ctx, tx.tx, step)
}

func (tx *pgxTxStore) ListStepsByRun(ctx context.Context, runID string) ([]*Step, error) {
	return listStepsByRun(ctx, tx.tx, runID)
}

func (tx *pgxTxStore) RecordPendingGate(ctx context.Context, gate *PendingGate) error {
	return recordPendingGate(ctx, tx.tx, gate)
}

func (tx *pgxTxStore) RecordGateAudit(ctx context.Context, entry *GateAudit) error {
	return recordGateAudit(ctx, tx.tx, entry)
}

func (tx *pgxTxStore) ListPendingGates(ctx context.Context) ([]*PendingGate, error) {
	return listPendingGates(ctx, tx.tx)
}

func (tx *pgxTxStore) ResolveGate(ctx context.Context, token string, resolution GateResolution) (*GateAudit, error) {
	if token == "" {
		return nil, ErrInvalidInput
	}

	tokenHash := HashToken(token)

	// Select for update to prevent concurrent race condition resolutions
	querySelect := `
		SELECT id, run_id, token_hash, gate_type, status, expires_at
		FROM pending_gates
		WHERE token_hash = $1 OR token_hash = $2
		FOR UPDATE
	`
	var (
		gateID    string
		runID     string
		hash      string
		gateType  string
		status    string
		expiresAt time.Time
	)

	row := tx.tx.QueryRow(ctx, querySelect, tokenHash, token)
	if err := row.Scan(&gateID, &runID, &hash, &gateType, &status, &expiresAt); err != nil {
		return nil, mapPgError(err)
	}

	now := time.Now().UTC()
	if status == string(GateStatusResolved) {
		return nil, ErrGateAlreadyResolved
	}
	if !expiresAt.IsZero() && now.After(expiresAt) {
		_, _ = tx.tx.Exec(ctx, "UPDATE pending_gates SET status = $1 WHERE id = $2", string(GateStatusExpired), gateID)
		return nil, ErrGateExpired
	}

	updateSQL := `UPDATE pending_gates SET status = $1 WHERE id = $2`
	if _, err := tx.tx.Exec(ctx, updateSQL, string(GateStatusResolved), gateID); err != nil {
		return nil, fmt.Errorf("failed to update gate status: %w", err)
	}

	audit := &GateAudit{
		RunID:     runID,
		TokenHash: hash,
		GateType:  gateType,
		Action:    resolution.Decision,
		Actor:     resolution.Actor,
		Reason:    resolution.Reason,
		Metadata:  resolution.Metadata,
		CreatedAt: now,
	}

	if err := recordGateAudit(ctx, tx.tx, audit); err != nil {
		return nil, fmt.Errorf("failed to record gate audit: %w", err)
	}

	return audit, nil
}

func (tx *pgxTxStore) WithTx(ctx context.Context, fn func(tx Store) error) error {
	// Nested transactions inside existing transaction
	return fn(tx)
}

func (tx *pgxTxStore) Close() error {
	return nil
}

// Database query executors common to pool and tx

type dbQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (commandTag pgconn.CommandTag, err error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func createRun(ctx context.Context, db dbQuerier, run *Run) error {
	if run == nil || run.ID == "" {
		return ErrInvalidInput
	}

	now := time.Now().UTC()
	createdAt := run.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := run.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}

	status := run.Status
	if status == "" {
		status = RunStatusQueued
	}

	metaJSON, err := json.Marshal(run.Metadata)
	if err != nil {
		return fmt.Errorf("store: failed to serialize metadata: %w", err)
	}
	if len(run.Metadata) == 0 {
		metaJSON = []byte("{}")
	}

	sql := `
		INSERT INTO runs (id, template_name, status, initiated_by, started_at, completed_at, error_message, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = db.Exec(ctx, sql,
		run.ID,
		run.TemplateName,
		string(status),
		run.InitiatedBy,
		run.StartedAt,
		run.CompletedAt,
		run.ErrorMessage,
		metaJSON,
		createdAt,
		updatedAt,
	)

	return mapPgError(err)
}

func getRun(ctx context.Context, db dbQuerier, id string) (*Run, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}

	sql := `
		SELECT id, template_name, status, initiated_by, started_at, completed_at, error_message, metadata, created_at, updated_at
		FROM runs
		WHERE id = $1
	`
	row := db.QueryRow(ctx, sql, id)

	var (
		r        Run
		status   string
		metaJSON []byte
	)

	err := row.Scan(
		&r.ID,
		&r.TemplateName,
		&status,
		&r.InitiatedBy,
		&r.StartedAt,
		&r.CompletedAt,
		&r.ErrorMessage,
		&metaJSON,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		return nil, mapPgError(err)
	}

	r.Status = RunStatus(status)
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &r.Metadata)
	}
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}

	return &r, nil
}

func listRuns(ctx context.Context, db dbQuerier, filter RunFilter) ([]*Run, error) {
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`
		SELECT id, template_name, status, initiated_by, started_at, completed_at, error_message, metadata, created_at, updated_at
		FROM runs
		WHERE 1=1
	`)

	args := make([]any, 0)
	argIdx := 1

	if filter.TemplateName != "" {
		queryBuilder.WriteString(fmt.Sprintf(" AND template_name = $%d", argIdx))
		args = append(args, filter.TemplateName)
		argIdx++
	}
	if filter.Status != "" {
		queryBuilder.WriteString(fmt.Sprintf(" AND status = $%d", argIdx))
		args = append(args, string(filter.Status))
		argIdx++
	}
	if filter.InitiatedBy != "" {
		queryBuilder.WriteString(fmt.Sprintf(" AND initiated_by = $%d", argIdx))
		args = append(args, filter.InitiatedBy)
		argIdx++
	}

	queryBuilder.WriteString(" ORDER BY created_at DESC")

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	queryBuilder.WriteString(fmt.Sprintf(" LIMIT $%d", argIdx))
	args = append(args, limit)
	argIdx++

	if filter.Offset > 0 {
		queryBuilder.WriteString(fmt.Sprintf(" OFFSET $%d", argIdx))
		args = append(args, filter.Offset)
	}

	rows, err := db.Query(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	var runs []*Run
	for rows.Next() {
		var (
			r        Run
			status   string
			metaJSON []byte
		)
		err := rows.Scan(
			&r.ID,
			&r.TemplateName,
			&status,
			&r.InitiatedBy,
			&r.StartedAt,
			&r.CompletedAt,
			&r.ErrorMessage,
			&metaJSON,
			&r.CreatedAt,
			&r.UpdatedAt,
		)
		if err != nil {
			return nil, mapPgError(err)
		}
		r.Status = RunStatus(status)
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &r.Metadata)
		}
		if r.Metadata == nil {
			r.Metadata = make(map[string]any)
		}
		runs = append(runs, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	return runs, nil
}

func updateRunStatus(ctx context.Context, db dbQuerier, id string, status RunStatus, runErr error) error {
	if id == "" {
		return ErrInvalidInput
	}

	now := time.Now().UTC()
	var (
		setClauses []string
		args       []any
		argIdx     = 1
	)

	setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
	args = append(args, string(status))
	argIdx++

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, now)
	argIdx++

	if runErr != nil {
		setClauses = append(setClauses, fmt.Sprintf("error_message = $%d", argIdx))
		args = append(args, runErr.Error())
		argIdx++
	}

	if status == RunStatusRunning {
		setClauses = append(setClauses, fmt.Sprintf("started_at = COALESCE(started_at, $%d)", argIdx))
		args = append(args, now)
		argIdx++
	}

	if status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled {
		setClauses = append(setClauses, fmt.Sprintf("completed_at = COALESCE(completed_at, $%d)", argIdx))
		args = append(args, now)
		argIdx++
	}

	sql := fmt.Sprintf("UPDATE runs SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argIdx)
	args = append(args, id)

	tag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func recordStep(ctx context.Context, db dbQuerier, step *Step) error {
	if step == nil || step.ID == "" || step.RunID == "" {
		return ErrInvalidInput
	}

	now := time.Now().UTC()
	createdAt := step.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	status := step.Status
	if status == "" {
		status = StepStatusPending
	}

	outJSON, err := json.Marshal(step.Output)
	if err != nil {
		return fmt.Errorf("store: failed to serialize step output: %w", err)
	}
	if len(step.Output) == 0 {
		outJSON = []byte("{}")
	}

	sql := `
		INSERT INTO run_steps (id, run_id, step_name, status, duration_ms, error_message, output, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = db.Exec(ctx, sql,
		step.ID,
		step.RunID,
		step.StepName,
		string(status),
		step.DurationMS,
		step.ErrorMessage,
		outJSON,
		createdAt,
	)

	return mapPgError(err)
}

func listStepsByRun(ctx context.Context, db dbQuerier, runID string) ([]*Step, error) {
	if runID == "" {
		return nil, ErrInvalidInput
	}

	sql := `
		SELECT id, run_id, step_name, status, duration_ms, error_message, output, created_at
		FROM run_steps
		WHERE run_id = $1
		ORDER BY created_at ASC
	`
	rows, err := db.Query(ctx, sql, runID)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	var steps []*Step
	for rows.Next() {
		var (
			s       Step
			status  string
			outJSON []byte
		)
		err := rows.Scan(
			&s.ID,
			&s.RunID,
			&s.StepName,
			&status,
			&s.DurationMS,
			&s.ErrorMessage,
			&outJSON,
			&s.CreatedAt,
		)
		if err != nil {
			return nil, mapPgError(err)
		}
		s.Status = StepStatus(status)
		if len(outJSON) > 0 {
			_ = json.Unmarshal(outJSON, &s.Output)
		}
		if s.Output == nil {
			s.Output = make(map[string]any)
		}
		steps = append(steps, &s)
	}

	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	return steps, nil
}

func recordPendingGate(ctx context.Context, db dbQuerier, gate *PendingGate) error {
	if gate == nil || gate.RunID == "" {
		return ErrInvalidInput
	}

	now := time.Now().UTC()
	id := gate.ID
	if id == "" {
		id = fmt.Sprintf("gate-%d", now.UnixNano())
	}

	tokenHash := gate.TokenHash
	if tokenHash == "" && gate.Token != "" {
		tokenHash = HashToken(gate.Token)
	}
	if tokenHash == "" {
		tokenHash = HashToken(id)
	}

	status := gate.Status
	if status == "" {
		status = GateStatusPending
	}

	createdAt := gate.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	expiresAt := gate.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(10 * time.Minute)
	}

	argsJSON, err := json.Marshal(gate.Arguments)
	if err != nil {
		return fmt.Errorf("store: failed to serialize gate arguments: %w", err)
	}
	if len(gate.Arguments) == 0 {
		argsJSON = []byte("{}")
	}

	sql := `
		INSERT INTO pending_gates (id, run_id, token_hash, gate_type, tool_name, arguments, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err = db.Exec(ctx, sql,
		id,
		gate.RunID,
		tokenHash,
		gate.GateType,
		gate.ToolName,
		argsJSON,
		string(status),
		createdAt,
		expiresAt,
	)

	return mapPgError(err)
}

func recordGateAudit(ctx context.Context, db dbQuerier, entry *GateAudit) error {
	if entry == nil || entry.RunID == "" {
		return ErrInvalidInput
	}

	now := time.Now().UTC()
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	metaJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("store: failed to serialize gate audit metadata: %w", err)
	}
	if len(entry.Metadata) == 0 {
		metaJSON = []byte("{}")
	}

	sql := `
		INSERT INTO gate_audits (run_id, token_hash, gate_type, action, actor, reason, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`
	row := db.QueryRow(ctx, sql,
		entry.RunID,
		entry.TokenHash,
		entry.GateType,
		entry.Action,
		entry.Actor,
		entry.Reason,
		metaJSON,
		createdAt,
	)

	if err := row.Scan(&entry.ID); err != nil {
		return mapPgError(err)
	}

	return nil
}

func listPendingGates(ctx context.Context, db dbQuerier) ([]*PendingGate, error) {
	now := time.Now().UTC()
	sql := `
		SELECT id, run_id, token_hash, gate_type, tool_name, arguments, status, created_at, expires_at
		FROM pending_gates
		WHERE status = $1 AND expires_at > $2
		ORDER BY created_at ASC
	`
	rows, err := db.Query(ctx, sql, string(GateStatusPending), now)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	var gates []*PendingGate
	for rows.Next() {
		var (
			g        PendingGate
			status   string
			argsJSON []byte
		)
		err := rows.Scan(
			&g.ID,
			&g.RunID,
			&g.TokenHash,
			&g.GateType,
			&g.ToolName,
			&argsJSON,
			&status,
			&g.CreatedAt,
			&g.ExpiresAt,
		)
		if err != nil {
			return nil, mapPgError(err)
		}
		g.Status = GateStatus(status)
		if len(argsJSON) > 0 {
			_ = json.Unmarshal(argsJSON, &g.Arguments)
		}
		if g.Arguments == nil {
			g.Arguments = make(map[string]any)
		}
		gates = append(gates, &g)
	}

	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	return gates, nil
}
