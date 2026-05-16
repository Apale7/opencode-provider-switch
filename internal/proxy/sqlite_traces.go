package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	UsageJSON    string
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
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open trace db: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteTraceStore{path: dsn}
	if err := store.init(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close initialized trace db: %w", err)
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
	usage_json TEXT NOT NULL DEFAULT '',
	attempts_json TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_request_traces_started_at ON request_traces(started_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_request_traces_alias ON request_traces(alias);
CREATE INDEX IF NOT EXISTS idx_request_traces_status_code ON request_traces(status_code);
CREATE INDEX IF NOT EXISTS idx_request_traces_attempt_count ON request_traces(attempt_count);
CREATE TABLE IF NOT EXISTS request_trace_attempts (
	trace_id INTEGER NOT NULL,
	attempt_index INTEGER NOT NULL,
	attempt INTEGER NOT NULL DEFAULT 0,
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	first_byte_ms INTEGER NOT NULL DEFAULT 0,
	status_code INTEGER NOT NULL DEFAULT 0,
	success INTEGER NOT NULL DEFAULT 0,
	retryable INTEGER NOT NULL DEFAULT 0,
	skipped INTEGER NOT NULL DEFAULT 0,
	result TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(trace_id, attempt_index)
);
CREATE INDEX IF NOT EXISTS idx_request_trace_attempts_provider ON request_trace_attempts(provider);
`)
	if err != nil {
		return fmt.Errorf("init trace db: %w", err)
	}
	if err := ensureSQLiteTraceColumn(ctx, db, "request_traces", "usage_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := backfillSQLiteTraceAttempts(ctx, db, 0); err != nil {
		return err
	}
	if err := seedSQLiteTraceCounter(ctx, db); err != nil {
		return err
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
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin trace insert: %w", err)
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `
INSERT INTO request_traces (
	id, started_at, finished_at, duration_ms, first_byte_ms, input_tokens, output_tokens,
	protocol, raw_model, alias, stream, success, status_code, error, final_provider,
	final_model, final_url, failover, attempt_count, request_headers_json,
	request_params_json, usage_json, attempts_json
 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
	usage_json=excluded.usage_json,
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
			row.UsageJSON,
			row.AttemptsJSON,
		)
		if err != nil {
			return fmt.Errorf("insert trace: %w", err)
		}
		if err := replaceSQLiteTraceAttempts(ctx, tx, row.Trace.ID, row.Trace.Attempts); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *SQLiteTraceStore) List(ctx context.Context, limit int) ([]RequestTrace, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = maxTracePageSize
	}
	items := []RequestTrace{}
	err := s.withDB(ctx, func(db *sql.DB) error {
		rows, err := db.QueryContext(ctx, `
SELECT id, started_at, finished_at, duration_ms, first_byte_ms, input_tokens, output_tokens,
	protocol, raw_model, alias, stream, success, status_code, error, final_provider,
	final_model, final_url, failover, attempt_count, request_headers_json,
	request_params_json, usage_json, attempts_json
FROM request_traces ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
		if err != nil {
			return fmt.Errorf("list traces: %w", err)
		}
		defer rows.Close()
		items = make([]RequestTrace, 0, limit)
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
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
	final_model, final_url, failover, attempt_count, usage_json
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
			trace, err := scanSQLiteTraceSummary(rows)
			if err != nil {
				return fmt.Errorf("scan trace summary: %w", err)
			}
			items = append(items, trace)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate traces: %w", err)
		}
		timeWhere, timeArgs := buildSQLiteTraceTimeWhere(query)
		aliases, failoverCounts, statusCodes, stats, err := querySQLiteTraceCatalog(ctx, db, timeWhere, timeArgs)
		if err != nil {
			return err
		}
		result = TraceQueryResult{
			Items:                   items,
			Total:                   total,
			Page:                    query.Page,
			PageSize:                query.PageSize,
			AvailableAliases:        aliases,
			AvailableFailoverCounts: failoverCounts,
			AvailableStatusCodes:    statusCodes,
			Stats:                   stats,
		}
		return nil
	})
	if err != nil {
		return TraceQueryResult{}, err
	}
	return result, nil
}

func (s *SQLiteTraceStore) QueryHealthTraces(ctx context.Context, query TraceQuery) ([]RequestTrace, error) {
	if s == nil {
		return nil, nil
	}
	query = normalizeTraceQuery(query)
	items := []RequestTrace{}
	err := s.withDB(ctx, func(db *sql.DB) error {
		where, args := buildSQLiteTraceWhere(query)
		listSQL := `
SELECT request_traces.id, request_traces.started_at, request_traces.finished_at,
	request_traces.duration_ms, request_traces.first_byte_ms,
	request_traces.input_tokens, request_traces.output_tokens,
	request_traces.protocol, request_traces.raw_model, request_traces.alias,
	request_traces.stream, request_traces.success, request_traces.status_code,
	request_traces.error, request_traces.final_provider, request_traces.final_model,
	request_traces.final_url, request_traces.failover, request_traces.attempt_count,
	json_extract(CASE WHEN trim(request_traces.usage_json) = '' THEN '{}' ELSE request_traces.usage_json END, '$.cacheReadTokens') AS cache_read_tokens,
	request_trace_attempts.attempt_index,
	request_trace_attempts.attempt,
	request_trace_attempts.provider,
	request_trace_attempts.model,
	request_trace_attempts.duration_ms,
	request_trace_attempts.first_byte_ms,
	request_trace_attempts.status_code,
	request_trace_attempts.success,
	request_trace_attempts.retryable,
	request_trace_attempts.skipped,
	request_trace_attempts.result
FROM request_traces
LEFT JOIN request_trace_attempts ON request_trace_attempts.trace_id = request_traces.id`
		if where != "" {
			listSQL += " WHERE " + where
		}
		listSQL += " ORDER BY started_at DESC, request_traces.id DESC, request_trace_attempts.attempt_index"
		rows, err := db.QueryContext(ctx, listSQL, args...)
		if err != nil {
			return fmt.Errorf("query health traces: %w", err)
		}
		defer rows.Close()
		items = []RequestTrace{}
		var current *RequestTrace
		for rows.Next() {
			row, err := scanSQLiteTraceHealthAttempt(rows)
			if err != nil {
				return err
			}
			if current == nil || current.ID != row.Trace.ID {
				items = append(items, row.Trace)
				current = &items[len(items)-1]
			}
			if row.HasAttempt {
				current.Attempts = append(current.Attempts, row.Attempt)
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate health traces: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteTraceStore) QueryAll(ctx context.Context, query TraceQuery) ([]RequestTrace, error) {
	if s == nil {
		return nil, nil
	}
	query = normalizeTraceQuery(query)
	items := []RequestTrace{}
	err := s.withDB(ctx, func(db *sql.DB) error {
		where, args := buildSQLiteTraceWhere(query)
		listSQL := `
SELECT id, started_at, finished_at, duration_ms, first_byte_ms, input_tokens, output_tokens,
	protocol, raw_model, alias, stream, success, status_code, error, final_provider,
	final_model, final_url, failover, attempt_count, request_headers_json,
	request_params_json, usage_json, attempts_json
FROM request_traces`
		if where != "" {
			listSQL += " WHERE " + where
		}
		listSQL += " ORDER BY started_at DESC, id DESC"
		rows, err := db.QueryContext(ctx, listSQL, args...)
		if err != nil {
			return fmt.Errorf("query all traces: %w", err)
		}
		defer rows.Close()
		items = []RequestTrace{}
		for rows.Next() {
			trace, err := scanSQLiteTrace(rows)
			if err != nil {
				return err
			}
			items = append(items, trace)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate all traces: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteTraceStore) Get(ctx context.Context, id uint64) (RequestTrace, bool, error) {
	if s == nil || id == 0 {
		return RequestTrace{}, false, nil
	}
	var trace RequestTrace
	err := s.withDB(ctx, func(db *sql.DB) error {
		row := db.QueryRowContext(ctx, `
SELECT id, started_at, finished_at, duration_ms, first_byte_ms, input_tokens, output_tokens,
	protocol, raw_model, alias, stream, success, status_code, error, final_provider,
	final_model, final_url, failover, attempt_count, request_headers_json,
	request_params_json, usage_json, attempts_json
FROM request_traces WHERE id = ?`, id)
		item, err := scanSQLiteTrace(row)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("scan trace detail: %w", err)
		}
		trace = item
		return nil
	})
	if err != nil {
		return RequestTrace{}, false, err
	}
	if trace.ID == 0 {
		return RequestTrace{}, false, nil
	}
	return trace, true, nil
}

func (s *SQLiteTraceStore) Close() error {
	return nil
}

func (s *SQLiteTraceStore) DBPath() string {
	if s == nil {
		return ""
	}
	path := s.path
	if index := strings.Index(path, "?"); index >= 0 {
		path = path[:index]
	}
	return path
}

func querySQLiteTraceCatalog(ctx context.Context, db *sql.DB, where string, args []any) ([]string, []int, []int, TraceStats, error) {
	aliases, err := querySQLiteTraceStringSet(ctx, db, "alias", where, args, "alias <> ''")
	if err != nil {
		return nil, nil, nil, TraceStats{}, err
	}
	statusCodes, err := querySQLiteTraceIntSet(ctx, db, "status_code", where, args, "status_code > 0")
	if err != nil {
		return nil, nil, nil, TraceStats{}, err
	}
	failoverCounts, err := querySQLiteTraceFailoverCounts(ctx, db, where, args)
	if err != nil {
		return nil, nil, nil, TraceStats{}, err
	}
	stats, err := querySQLiteTraceStats(ctx, db, where, args)
	if err != nil {
		return nil, nil, nil, TraceStats{}, err
	}
	return aliases, failoverCounts, statusCodes, stats, nil
}

func querySQLiteTraceStringSet(ctx context.Context, db *sql.DB, column string, where string, args []any, extraClause string) ([]string, error) {
	clauses := sqliteCatalogClauses(where, extraClause)
	query := fmt.Sprintf("SELECT DISTINCT %s FROM request_traces", column)
	if clauses != "" {
		query += " WHERE " + clauses
	}
	query += " ORDER BY " + column
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trace %s catalog: %w", column, err)
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan trace %s catalog: %w", column, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace %s catalog: %w", column, err)
	}
	return values, nil
}

func querySQLiteTraceIntSet(ctx context.Context, db *sql.DB, column string, where string, args []any, extraClause string) ([]int, error) {
	clauses := sqliteCatalogClauses(where, extraClause)
	query := fmt.Sprintf("SELECT DISTINCT %s FROM request_traces", column)
	if clauses != "" {
		query += " WHERE " + clauses
	}
	query += " ORDER BY " + column
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trace %s catalog: %w", column, err)
	}
	defer rows.Close()
	values := []int{}
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan trace %s catalog: %w", column, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace %s catalog: %w", column, err)
	}
	return values, nil
}

func querySQLiteTraceFailoverCounts(ctx context.Context, db *sql.DB, where string, args []any) ([]int, error) {
	query := "SELECT DISTINCT CASE WHEN attempt_count > 1 THEN attempt_count - 1 ELSE 0 END FROM request_traces"
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY 1"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trace failover catalog: %w", err)
	}
	defer rows.Close()
	values := []int{}
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan trace failover catalog: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace failover catalog: %w", err)
	}
	return values, nil
}

func querySQLiteTraceStats(ctx context.Context, db *sql.DB, where string, args []any) (TraceStats, error) {
	query := `SELECT
	COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN success = 1 THEN 0 ELSE 1 END), 0),
	COALESCE(SUM(CASE WHEN failover = 1 OR attempt_count > 1 THEN 1 ELSE 0 END), 0)
FROM request_traces`
	if where != "" {
		query += " WHERE " + where
	}
	var stats TraceStats
	if err := db.QueryRowContext(ctx, query, args...).Scan(&stats.Success, &stats.Failed, &stats.Failover); err != nil {
		return TraceStats{}, fmt.Errorf("query trace stats: %w", err)
	}
	return stats, nil
}

func sqliteCatalogClauses(where string, extraClause string) string {
	clauses := []string{}
	if strings.TrimSpace(where) != "" {
		clauses = append(clauses, where)
	}
	if strings.TrimSpace(extraClause) != "" {
		clauses = append(clauses, extraClause)
	}
	return strings.Join(clauses, " AND ")
}

func sortedStringSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedIntSet(values map[int]struct{}) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func buildSQLiteTraceWhere(query TraceQuery) (string, []any) {
	clauses := []string{}
	args := []any{}
	clauses, args = appendSQLiteTraceTimeWhere(query, clauses, args)
	if len(query.Aliases) > 0 {
		placeholders := make([]string, 0, len(query.Aliases))
		for _, alias := range query.Aliases {
			placeholders = append(placeholders, "?")
			args = append(args, alias)
		}
		clauses = append(clauses, "alias IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(query.FailoverCounts) > 0 {
		clauses, args = appendSQLiteFailoverWhere(query.FailoverCounts, clauses, args)
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

func buildSQLiteTraceTimeWhere(query TraceQuery) (string, []any) {
	clauses, args := appendSQLiteTraceTimeWhere(query, nil, nil)
	return strings.Join(clauses, " AND "), args
}

func appendSQLiteTraceTimeWhere(query TraceQuery, clauses []string, args []any) ([]string, []any) {
	if !query.StartedFrom.IsZero() {
		clauses = append(clauses, "started_at >= ?")
		args = append(args, formatSQLiteTime(query.StartedFrom))
	}
	if !query.StartedTo.IsZero() {
		clauses = append(clauses, "started_at <= ?")
		args = append(args, formatSQLiteTime(query.StartedTo))
	}
	return clauses, args
}

func appendSQLiteFailoverWhere(counts []int, clauses []string, args []any) ([]string, []any) {
	exactAttempts := []int{}
	includeZero := false
	for _, count := range counts {
		if count == 0 {
			includeZero = true
			continue
		}
		exactAttempts = append(exactAttempts, count+1)
	}
	parts := []string{}
	if includeZero {
		parts = append(parts, "attempt_count <= 1")
	}
	if len(exactAttempts) > 0 {
		placeholders := make([]string, 0, len(exactAttempts))
		for _, attemptCount := range exactAttempts {
			placeholders = append(placeholders, "?")
			args = append(args, attemptCount)
		}
		parts = append(parts, "attempt_count IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(parts) > 0 {
		clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
	}
	return clauses, args
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
	usageJSON, err := marshalTraceJSON(trace.Usage)
	if err != nil {
		return traceRow{}, fmt.Errorf("marshal usage: %w", err)
	}
	return traceRow{Trace: trace, HeadersJSON: headersJSON, ParamsJSON: paramsJSON, UsageJSON: usageJSON, AttemptsJSON: attemptsJSON}, nil
}

func ensureSQLiteTraceColumn(ctx context.Context, db *sql.DB, table string, column string, definition string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan %s schema: %w", table, err)
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s schema: %w", table, err)
	}
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

type sqliteTraceAttemptWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
}

func backfillSQLiteTraceAttempts(ctx context.Context, db *sql.DB, traceID uint64) error {
	query := `
SELECT request_traces.id
FROM request_traces
	LEFT JOIN request_trace_attempts ON request_trace_attempts.trace_id = request_traces.id
WHERE request_traces.attempts_json <> ''
	AND json_valid(request_traces.attempts_json)`
	args := []any{}
	if traceID > 0 {
		query += ` AND request_traces.id = ?`
		args = append(args, traceID)
	}
	query += `
GROUP BY request_traces.id, request_traces.attempts_json
HAVING COUNT(request_trace_attempts.trace_id) != json_array_length(request_traces.attempts_json)
ORDER BY request_traces.id`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query trace attempts backfill: %w", err)
	}
	defer rows.Close()
	type traceAttempts struct {
		id       uint64
		attempts []TraceAttempt
	}
	items := []traceAttempts{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan trace attempts backfill: %w", err)
		}
		items = append(items, traceAttempts{id: id})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate trace attempts backfill: %w", err)
	}
	for _, item := range items {
		attempts, err := readSQLiteTraceHealthAttempts(ctx, db, item.id)
		if err != nil {
			continue
		}
		item.attempts = attempts
		if err := replaceSQLiteTraceAttempts(ctx, db, item.id, item.attempts); err != nil {
			return err
		}
	}
	return nil
}

func readSQLiteTraceHealthAttempts(ctx context.Context, db *sql.DB, traceID uint64) ([]TraceAttempt, error) {
	var attemptsJSON string
	if err := db.QueryRowContext(ctx, "SELECT attempts_json FROM request_traces WHERE id = ?", traceID).Scan(&attemptsJSON); err != nil {
		return nil, fmt.Errorf("query trace attempts json: %w", err)
	}
	attempts, err := decodeSQLiteTraceHealthAttempts(attemptsJSON)
	if err != nil {
		return nil, err
	}
	return attempts, nil
}

func replaceSQLiteTraceAttempts(ctx context.Context, db sqliteTraceAttemptWriter, traceID uint64, attempts []TraceAttempt) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM request_trace_attempts WHERE trace_id = ?", traceID); err != nil {
		return fmt.Errorf("delete trace attempts: %w", err)
	}
	if len(attempts) == 0 {
		return nil
	}
	stmt, err := db.PrepareContext(ctx, `
INSERT INTO request_trace_attempts (
	trace_id, attempt_index, attempt, provider, model, duration_ms, first_byte_ms,
	status_code, success, retryable, skipped, result
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare trace attempts insert: %w", err)
	}
	defer stmt.Close()
	for index, attempt := range attempts {
		if _, err := stmt.ExecContext(ctx,
			traceID,
			index,
			attempt.Attempt,
			attempt.Provider,
			attempt.Model,
			attempt.DurationMs,
			attempt.FirstByteMs,
			attempt.StatusCode,
			boolToInt(attempt.Success),
			boolToInt(attempt.Retryable),
			boolToInt(attempt.Skipped),
			attempt.Result,
		); err != nil {
			return fmt.Errorf("insert trace attempt: %w", err)
		}
	}
	return nil
}

func seedSQLiteTraceCounter(ctx context.Context, db *sql.DB) error {
	var maxID sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM request_traces").Scan(&maxID); err != nil {
		return fmt.Errorf("query trace max id: %w", err)
	}
	if !maxID.Valid || maxID.Int64 <= 0 {
		return nil
	}
	target := uint64(maxID.Int64)
	for {
		current := atomic.LoadUint64(&reqCounter)
		if current >= target {
			return nil
		}
		if atomic.CompareAndSwapUint64(&reqCounter, current, target) {
			return nil
		}
	}
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
		trace        RequestTrace
		startedAt    string
		finishedAt   string
		stream       int
		success      int
		failover     int
		headersJSON  string
		paramsJSON   string
		usageJSON    string
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
		&usageJSON,
		&attemptsJSON,
	)
	if err != nil {
		return RequestTrace{}, err
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
	if usageJSON != "" {
		if err := json.Unmarshal([]byte(usageJSON), &trace.Usage); err != nil {
			return RequestTrace{}, fmt.Errorf("decode usage: %w", err)
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

func scanSQLiteTraceSummary(scanner interface{ Scan(dest ...any) error }) (RequestTrace, error) {
	var (
		trace      RequestTrace
		startedAt  string
		finishedAt string
		stream     int
		success    int
		failover   int
		usageJSON  string
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
		&usageJSON,
	)
	if err != nil {
		return RequestTrace{}, err
	}
	trace.StartedAt = parseSQLiteTime(startedAt)
	trace.FinishedAt = parseSQLiteTime(finishedAt)
	trace.Stream = stream == 1
	trace.Success = success == 1
	trace.Failover = failover == 1
	if usageJSON != "" {
		if err := json.Unmarshal([]byte(usageJSON), &trace.Usage); err != nil {
			return RequestTrace{}, fmt.Errorf("decode usage: %w", err)
		}
	}
	trace.Attempts = []TraceAttempt{}
	return trace, nil
}

func scanSQLiteTraceHealth(scanner interface{ Scan(dest ...any) error }) (RequestTrace, error) {
	var (
		trace        RequestTrace
		startedAt    string
		finishedAt   string
		stream       int
		success      int
		failover     int
		usageJSON    string
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
		&usageJSON,
		&attemptsJSON,
	)
	if err != nil {
		return RequestTrace{}, err
	}
	trace.StartedAt = parseSQLiteTime(startedAt)
	trace.FinishedAt = parseSQLiteTime(finishedAt)
	trace.Stream = stream == 1
	trace.Success = success == 1
	trace.Failover = failover == 1
	if usageJSON != "" {
		var usage struct {
			CacheReadTokens *int64 `json:"cacheReadTokens,omitempty"`
		}
		if err := json.Unmarshal([]byte(usageJSON), &usage); err != nil {
			return RequestTrace{}, fmt.Errorf("decode health usage: %w", err)
		}
		trace.Usage.CacheReadTokens = usage.CacheReadTokens
	}
	if attemptsJSON != "" {
		attempts, err := decodeSQLiteTraceHealthAttempts(attemptsJSON)
		if err != nil {
			return RequestTrace{}, err
		}
		trace.Attempts = attempts
	}
	if trace.Attempts == nil {
		trace.Attempts = []TraceAttempt{}
	}
	return trace, nil
}

type sqliteTraceHealthAttemptRow struct {
	Trace      RequestTrace
	HasAttempt bool
	Attempt    TraceAttempt
}

func scanSQLiteTraceHealthAttempt(scanner interface{ Scan(dest ...any) error }) (sqliteTraceHealthAttemptRow, error) {
	var (
		row             sqliteTraceHealthAttemptRow
		startedAt       string
		finishedAt      string
		stream          int
		success         int
		failover        int
		cacheReadTokens sql.NullInt64
		attemptKey      sql.NullInt64
		attempt         sql.NullInt64
		provider        sql.NullString
		model           sql.NullString
		durationMs      sql.NullInt64
		firstByteMs     sql.NullInt64
		statusCode      sql.NullInt64
		attemptSuccess  sql.NullBool
		retryable       sql.NullBool
		skipped         sql.NullBool
		result          sql.NullString
	)
	err := scanner.Scan(
		&row.Trace.ID,
		&startedAt,
		&finishedAt,
		&row.Trace.DurationMs,
		&row.Trace.FirstByteMs,
		&row.Trace.InputTokens,
		&row.Trace.OutputTokens,
		&row.Trace.Protocol,
		&row.Trace.RawModel,
		&row.Trace.Alias,
		&stream,
		&success,
		&row.Trace.StatusCode,
		&row.Trace.Error,
		&row.Trace.FinalProvider,
		&row.Trace.FinalModel,
		&row.Trace.FinalURL,
		&failover,
		&row.Trace.AttemptCount,
		&cacheReadTokens,
		&attemptKey,
		&attempt,
		&provider,
		&model,
		&durationMs,
		&firstByteMs,
		&statusCode,
		&attemptSuccess,
		&retryable,
		&skipped,
		&result,
	)
	if err != nil {
		return sqliteTraceHealthAttemptRow{}, err
	}
	row.Trace.StartedAt = parseSQLiteTime(startedAt)
	row.Trace.FinishedAt = parseSQLiteTime(finishedAt)
	row.Trace.Stream = stream == 1
	row.Trace.Success = success == 1
	row.Trace.Failover = failover == 1
	row.Trace.Attempts = []TraceAttempt{}
	if cacheReadTokens.Valid {
		value := cacheReadTokens.Int64
		row.Trace.Usage.CacheReadTokens = &value
	}
	if !attemptKey.Valid {
		return row, nil
	}
	row.HasAttempt = true
	if attempt.Valid {
		row.Attempt.Attempt = int(attempt.Int64)
	}
	if provider.Valid {
		row.Attempt.Provider = provider.String
	}
	if model.Valid {
		row.Attempt.Model = model.String
	}
	if durationMs.Valid {
		row.Attempt.DurationMs = durationMs.Int64
	}
	if firstByteMs.Valid {
		row.Attempt.FirstByteMs = firstByteMs.Int64
	}
	if statusCode.Valid {
		row.Attempt.StatusCode = int(statusCode.Int64)
	}
	if attemptSuccess.Valid {
		row.Attempt.Success = attemptSuccess.Bool
	}
	if retryable.Valid {
		row.Attempt.Retryable = retryable.Bool
	}
	if skipped.Valid {
		row.Attempt.Skipped = skipped.Bool
	}
	if result.Valid {
		row.Attempt.Result = result.String
	}
	return row, nil
}

func decodeSQLiteTraceHealthAttempts(value string) ([]TraceAttempt, error) {
	var rows []struct {
		Attempt     int    `json:"attempt"`
		Provider    string `json:"provider,omitempty"`
		Model       string `json:"model,omitempty"`
		APIKeyIndex int    `json:"apiKeyIndex,omitempty"`
		DurationMs  int64  `json:"durationMs"`
		FirstByteMs int64  `json:"firstByteMs,omitempty"`
		StatusCode  int    `json:"statusCode,omitempty"`
		Success     bool   `json:"success"`
		Retryable   bool   `json:"retryable"`
		Skipped     bool   `json:"skipped"`
		Result      string `json:"result,omitempty"`
	}
	if err := json.Unmarshal([]byte(value), &rows); err != nil {
		return nil, fmt.Errorf("decode health attempts: %w", err)
	}
	out := make([]TraceAttempt, 0, len(rows))
	for _, row := range rows {
		out = append(out, TraceAttempt{
			Attempt:     row.Attempt,
			Provider:    row.Provider,
			Model:       row.Model,
			APIKeyIndex: row.APIKeyIndex,
			DurationMs:  row.DurationMs,
			FirstByteMs: row.FirstByteMs,
			StatusCode:  row.StatusCode,
			Success:     row.Success,
			Retryable:   row.Retryable,
			Skipped:     row.Skipped,
			Result:      row.Result,
		})
	}
	return out, nil
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
	return fn(db)
}
