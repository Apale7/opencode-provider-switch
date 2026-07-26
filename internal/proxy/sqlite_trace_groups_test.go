package proxy

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteTraceStoreMigratesHistoricalGroupsWithoutDataLoss(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "ocswitch.json")
	dbPath, err := traceDBPath(configPath)
	if err != nil {
		t.Fatalf("traceDBPath() error = %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE request_traces (
 id INTEGER PRIMARY KEY, started_at TEXT NOT NULL, finished_at TEXT,
 duration_ms INTEGER NOT NULL DEFAULT 0, first_byte_ms INTEGER NOT NULL DEFAULT 0,
 first_token_ms INTEGER NOT NULL DEFAULT 0, input_tokens INTEGER NOT NULL DEFAULT 0,
 output_tokens INTEGER NOT NULL DEFAULT 0, generated_output_tokens INTEGER NOT NULL DEFAULT 0,
 protocol TEXT NOT NULL DEFAULT '', raw_model TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '',
 stream INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, status_code INTEGER NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '', final_provider TEXT NOT NULL DEFAULT '', final_model TEXT NOT NULL DEFAULT '',
 final_url TEXT NOT NULL DEFAULT '', failover INTEGER NOT NULL DEFAULT 0, attempt_count INTEGER NOT NULL DEFAULT 0,
 request_headers_json TEXT NOT NULL DEFAULT '', request_params_json TEXT NOT NULL DEFAULT '',
 usage_json TEXT NOT NULL DEFAULT '', attempts_json TEXT NOT NULL DEFAULT ''
);
CREATE TABLE request_trace_attempts (
 trace_id INTEGER NOT NULL, attempt_index INTEGER NOT NULL, attempt INTEGER NOT NULL DEFAULT 0,
 provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', duration_ms INTEGER NOT NULL DEFAULT 0,
 first_byte_ms INTEGER NOT NULL DEFAULT 0, first_token_ms INTEGER NOT NULL DEFAULT 0,
 status_code INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0,
 retryable INTEGER NOT NULL DEFAULT 0, skipped INTEGER NOT NULL DEFAULT 0, result TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(trace_id, attempt_index)
);
INSERT INTO request_traces (
 id, started_at, finished_at, success, status_code, final_provider, final_model, attempt_count, attempts_json
) VALUES (7, '2026-07-22T00:00:00Z', '', 1, 200, 'legacy-provider', 'legacy-model', 1,
 '[{"attempt":1,"provider":"legacy-provider","model":"legacy-model","success":true,"statusCode":200,"result":"success"}]');
`)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("seed historical database: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() migration error = %v", err)
	}
	defer store.Close()
	trace, ok, err := store.Get(context.Background(), 7)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("historical trace missing after migration")
	}
	if trace.FinalProvider != "legacy-provider" || trace.FinalGroup != DefaultTraceGroupID || len(trace.Attempts) != 1 || trace.Attempts[0].Group != DefaultTraceGroupID {
		t.Fatalf("migrated trace = %#v", trace)
	}

	now := time.Now().UTC()
	if err := store.Add(context.Background(), RequestTrace{
		ID: 8, StartedAt: now, Success: true, FinalProvider: "vendor", FinalGroup: "premium",
		Attempts: []TraceAttempt{{Attempt: 1, Provider: "vendor", Group: "premium", Model: "model", Success: true}},
	}); err != nil {
		t.Fatalf("Add(group trace) error = %v", err)
	}
	current, ok, err := store.Get(context.Background(), 8)
	if err != nil || !ok {
		t.Fatalf("Get(group trace) = ok %v, err %v", ok, err)
	}
	if current.FinalGroup != "premium" || len(current.Attempts) != 1 || current.Attempts[0].Group != "premium" {
		t.Fatalf("group trace round trip = %#v", current)
	}
}
