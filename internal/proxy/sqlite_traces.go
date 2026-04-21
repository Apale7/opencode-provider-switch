package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const traceDBFileName = "traces.db"

type SQLiteTraceStore struct {
	mu   sync.Mutex
	path string
}

type traceRow struct {
	Trace        RequestTrace
	AttemptsJSON string
	HeadersJSON  string
	ParamsJSON   string
}

func NewSQLiteTraceStore(configPath string) (*SQLiteTraceStore, error) {
	dbPath, err := traceDBPath(configPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir trace db dir: %w", err)
	}
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	store := &SQLiteTraceStore{path: dsn}
	if err := store.withDB(context.Background(), func(db *sql.DB) error {
		return store.init(context.Background(), db)
	}); err != nil {
		return nil, err
	}
	return store, nil
}

func traceDBPath(configPath string) (string, error) {
	resolved := strings.TrimSpace(configPath)
	if resolved == "" {
		return "", fmt.Errorf("config path is required")
	}
	return filepath.Join(filepath.Dir(resolved), traceDBFileName), nil
}

func (s *SQLiteTraceStore) init(ctx context.Context, db *sql.DB) error {
	if s == nil || db == nil {
		return fmt.Errorf("trace store unavailable")
	}
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS request_traces (
	id INTEGER PRIMARY KEY,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	first_byte_ms INTEGER NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	protocol TEXT NOT NULL DEFAULT '',
	raw_model TEXT NOT NULL DEFAULT '',
	alias TEXT NOT NULL DEFAULT '',
	stream INTEGER NOT NULL DEFAULT 0,
	success INTEGER NOT NULL DEFAULT 0,
	status_code INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	final_provider TEXT NOT NULL DEFAULT '',
	final_model TEXT NOT NULL DEFAULT '',
	final_url TEXT NOT NULL DEFAULT '',
	failover INTEGER NOT NULL DEFAULT 0,
	attempt_count INTEGER NOT NULL DEFAULT 0,
	request_headers_json TEXT NOT NULL DEFAULT '',
	request_params_json TEXT NOT NULL DEFAULT '',
	attempts_json TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_request_traces_started_at ON request_traces(started_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_request_traces_alias ON request_traces(alias);
CREATE INDEX IF NOT EXISTS idx_request_traces_status_code ON request_traces(status_code);
CREATE INDEX IF NOT EXISTS idx_request_traces_attempt_count ON request_traces(attempt_count);
`)
	if err != nil {
		return fmt.Errorf("init trace db: %w", err)
	}
	return nil
}

func (s *SQLiteTraceStore) Add(ctx context.Context, trace RequestTrace) error {
	if s == nil {
		return nil
	}
	row, err := encodeTraceRow(trace)
	if err != nil {
		return err
	}
	return s.withDB(ctx, func(db *sql.DB) error {
		_, err = db.ExecContext(ctx, `
INSERT INTO request_traces (
	id, started_at, finished_at, duration_ms, first_byte_ms, input_tokens, output_tokens,
	protocol, raw_model, alias, stream, success, status_code, error, final_provider,
	final_model, final_url, failover, attempt_count, request_headers_json,
	request_params_json, attempts_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	finished_at=excluded.finished_at,
	duration_ms=excluded.duration_ms,
	first_byte_ms=excluded.first_byte_ms,
	input_tokens=excluded.input_tokens,
	output_tokens=excluded.output_tokens,
	protocol=excluded.protocol,
	raw_model=excluded.raw_model,
	alias=excluded.alias,
	stream=excluded.stream,
	success=excluded.success,
	status_code=excluded.status_code,
	error=excluded.error,
	final_provider=excluded.final_provider,
	final_model=excluded.final_model,
	final_url=excluded.final_url,
	failover=excluded.failover,
	attempt_count=excluded.attempt_count,
	request_headers_json=excluded.request_headers_json,
	request_params_json=excluded.request_params_json,
	attempts_json=excluded.attempts_json
`,
			row.Trace.ID,
			formatSQLiteTime(row.Trace.StartedAt),
			formatSQLiteTime(row.Trace.FinishedAt),
			row.Trace.DurationMs,
			row.Trace.FirstByteMs,
			row.Trace.InputTokens,
			row.Trace.OutputTokens,
			row.Trace.Protocol,
			row.Trace.RawModel,
			row.Trace.Alias,
			boolToInt(row.Trace.Stream),
			boolToInt(row.Trace.Success),
			row.Trace.StatusCode,
			row.Trace.Error,
			row.Trace.FinalProvider,
			row.Trace.FinalModel,
			row.Trace.FinalURL,
			boolToInt(row.Trace.Failover),
			row.Trace.AttemptCount,
			row.HeadersJSON,
			row.ParamsJSON,
			row.AttemptsJSON,
		)
		if err != nil {
			return fmt.Errorf("insert trace: %w", err)
		}
		return nil
	})
}

func (s *SQLiteTraceStore) List(ctx context.Context, limit int) ([]RequestTrace, error) {
	query := TraceQuery{Page: 1, PageSize: limit}
	if limit <= 0 {
		query.PageSize = maxTracePageSize
	}
	result, err := s.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (s *SQLiteTraceStore) Query(ctx context.Context, query TraceQuery) (TraceQueryResult, error) {
	if s == nil {
		query = normalizeTraceQuery(query)
		return TraceQueryResult{Page: query.Page, PageSize: query.PageSize}, nil
	}
	query = normalizeTraceQuery(query)
	result := TraceQueryResult{}
	err := s.withDB(ctx, func(db *sql.DB) error {
		where, args := buildSQLiteTraceWhere(query)
		countSQL := "SELECT COUNT(*) FROM request_traces"
		if where != "" {
			countSQL += " WHERE " + where
		}
		var total int
		if err := db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return fmt.Errorf("count traces: %w", err)
		}
		offset := (query.Page - 1) * query.PageSize
		listSQL := `
SELECT id, started_at, finished_at, duration_ms, first_byte_ms, input_tokens, output_tokens,
	protocol, raw_model, alias, stream, success, status_code, error, final_provider,
	final_model, final_url, failover, attempt_count, request_headers_json,
	request_params_json, attempts_json
FROM request_traces`
		if where != "" {
			listSQL += " WHERE " + where
		}
		listSQL += " ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?"
		listArgs := append(append([]any(nil), args...), query.PageSize, offset)
		rows, err := db.QueryContext(ctx, listSQL, listArgs...)
		if err != nil {
			return fmt.Errorf("query traces: %w", err)
		}
		defer rows.Close()
		items := make([]RequestTrace, 0, query.PageSize)
		for rows.Next() {
			trace, err := scanSQLiteTrace(rows)
			if err != nil {
				return err
			}
			items = append(items, trace)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate traces: %w", err)
		}
		aliases, err := s.listDistinctStrings(ctx, db, "alias", "alias <> ''", "alias ASC")
		if err != nil {
			return err
		}
		statusCodes, err := s.listDistinctInts(ctx, db, "status_code", "status_code > 0", "status_code ASC")
		if err != nil {
			return err
		}
		attemptCounts, err := s.listDistinctInts(ctx, db, "attempt_count", "attempt_count >= 0", "attempt_count ASC")
		if err != nil {
			return err
		}
		failoverCounts := make([]int, 0, len(attemptCounts))
		for _, attemptCount := range attemptCounts {
			count := attemptCount - 1
			if count < 0 {
				count = 0
			}
			if len(failoverCounts) > 0 && failoverCounts[len(failoverCounts)-1] == count {
				continue
			}
			failoverCounts = append(failoverCounts, count)
		}
		result = TraceQueryResult{
			Items:                   items,
			Total:                   total,
			Page:                    query.Page,
			PageSize:                query.PageSize,
			AvailableAliases:        aliases,
			AvailableFailoverCounts: failoverCounts,
			AvailableStatusCodes:    statusCodes,
		}
		return nil
	})
	if err != nil {
		return TraceQueryResult{}, err
	}
	return result, nil
}

func (s *SQLiteTraceStore) Close() error {
	return nil
}

func (s *SQLiteTraceStore) listDistinctStrings(ctx context.Context, db *sql.DB, column string, where string, orderBy string) ([]string, error) {
	query := fmt.Sprintf("SELECT DISTINCT %s FROM request_traces", column)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY " + orderBy
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list distinct %s: %w", column, err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan distinct %s: %w", column, err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate distinct %s: %w", column, err)
	}
	return out, nil
}

func (s *SQLiteTraceStore) listDistinctInts(ctx context.Context, db *sql.DB, column string, where string, orderBy string) ([]int, error) {
	query := fmt.Sprintf("SELECT DISTINCT %s FROM request_traces", column)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY " + orderBy
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list distinct %s: %w", column, err)
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan distinct %s: %w", column, err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate distinct %s: %w", column, err)
	}
	return out, nil
}

func buildSQLiteTraceWhere(query TraceQuery) (string, []any) {
	clauses := []string{}
	args := []any{}
	if len(query.Aliases) > 0 {
		placeholders := make([]string, 0, len(query.Aliases))
		for _, alias := range query.Aliases {
			placeholders = append(placeholders, "?")
			args = append(args, alias)
		}
		clauses = append(clauses, "alias IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(query.FailoverCounts) > 0 {
		placeholders := make([]string, 0, len(query.FailoverCounts))
		for _, count := range query.FailoverCounts {
			placeholders = append(placeholders, "?")
			args = append(args, count+1)
		}
		clauses = append(clauses, "CASE WHEN attempt_count <= 1 THEN 1 ELSE attempt_count END IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(query.StatusCodes) > 0 {
		placeholders := make([]string, 0, len(query.StatusCodes))
		for _, code := range query.StatusCodes {
			placeholders = append(placeholders, "?")
			args = append(args, code)
		}
		clauses = append(clauses, "status_code IN ("+strings.Join(placeholders, ",")+")")
	}
	return strings.Join(clauses, " AND "), args
}

func encodeTraceRow(trace RequestTrace) (traceRow, error) {
	trace = cloneTrace(trace)
	headersJSON, err := marshalTraceJSON(trace.RequestHeaders)
	if err != nil {
		return traceRow{}, fmt.Errorf("marshal request headers: %w", err)
	}
	paramsJSON, err := marshalTraceJSON(trace.RequestParams)
	if err != nil {
		return traceRow{}, fmt.Errorf("marshal request params: %w", err)
	}
	attemptsJSON, err := marshalTraceJSON(trace.Attempts)
	if err != nil {
		return traceRow{}, fmt.Errorf("marshal attempts: %w", err)
	}
	return traceRow{Trace: trace, HeadersJSON: headersJSON, ParamsJSON: paramsJSON, AttemptsJSON: attemptsJSON}, nil
}

func marshalTraceJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func scanSQLiteTrace(scanner interface{ Scan(dest ...any) error }) (RequestTrace, error) {
	var (
		trace       RequestTrace
		startedAt   string
		finishedAt  string
		stream      int
		success     int
		failover    int
		headersJSON string
		paramsJSON  string
		attemptsJSON string
	)
	err := scanner.Scan(
		&trace.ID,
		&startedAt,
		&finishedAt,
		&trace.DurationMs,
		&trace.FirstByteMs,
		&trace.InputTokens,
		&trace.OutputTokens,
		&trace.Protocol,
		&trace.RawModel,
		&trace.Alias,
		&stream,
		&success,
		&trace.StatusCode,
		&trace.Error,
		&trace.FinalProvider,
		&trace.FinalModel,
		&trace.FinalURL,
		&failover,
		&trace.AttemptCount,
		&headersJSON,
		&paramsJSON,
		&attemptsJSON,
	)
	if err != nil {
		return RequestTrace{}, fmt.Errorf("scan trace row: %w", err)
	}
	trace.StartedAt = parseSQLiteTime(startedAt)
	trace.FinishedAt = parseSQLiteTime(finishedAt)
	trace.Stream = stream == 1
	trace.Success = success == 1
	trace.Failover = failover == 1
	if headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &trace.RequestHeaders); err != nil {
			return RequestTrace{}, fmt.Errorf("decode request headers: %w", err)
		}
	}
	if paramsJSON != "" {
		if err := json.Unmarshal([]byte(paramsJSON), &trace.RequestParams); err != nil {
			return RequestTrace{}, fmt.Errorf("decode request params: %w", err)
		}
	}
	if attemptsJSON != "" {
		if err := json.Unmarshal([]byte(attemptsJSON), &trace.Attempts); err != nil {
			return RequestTrace{}, fmt.Errorf("decode attempts: %w", err)
		}
	}
	if trace.Attempts == nil {
		trace.Attempts = []TraceAttempt{}
	}
	return trace, nil
}

func formatSQLiteTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseSQLiteTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *SQLiteTraceStore) withDB(ctx context.Context, fn func(*sql.DB) error) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return fmt.Errorf("open trace db: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := s.init(ctx, db); err != nil {
		return err
	}
	return fn(db)
}
