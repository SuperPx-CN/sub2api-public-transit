package transit

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TEST_DATABASE_URL must name a disposable, empty PostgreSQL database. Never use a shared database.
func TestPostgresReadOnly(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to disposable PostgreSQL")
	}
	parsed, parseErr := url.Parse(dsn)
	if parseErr != nil || parsed.Path != "/transit_test" {
		t.Fatal("integration tests require disposable database named transit_test")
	}
	admin, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema, e := os.ReadFile("testdata/schema.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(string(schema)); e != nil {
		t.Fatal(e)
	}
	exec := func(q string) {
		t.Helper()
		if _, e := admin.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	exec("CREATE ROLE transit_fixture_reader LOGIN PASSWORD 'fixture-reader'; GRANT CONNECT ON DATABASE transit_test TO transit_fixture_reader; GRANT USAGE ON SCHEMA public TO transit_fixture_reader; GRANT SELECT ON settings,groups,channels,channel_groups,channel_model_pricing,channel_pricing_intervals,usage_logs,channel_monitor_histories,channel_monitor_v2_config,channel_monitor_v2_watermarks,channel_monitor_v2_metrics_rollup,channel_monitor_v2_latency_histograms_rollup,channel_monitor_v2_error_metrics_rollup TO transit_fixture_reader; GRANT SELECT(id,name,provider,group_name,primary_model,extra_models,enabled) ON channel_monitors TO transit_fixture_reader; ALTER ROLE transit_fixture_reader SET default_transaction_read_only=on")
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	u.User = url.UserPassword("transit_fixture_reader", "fixture-reader")
	q := u.Query()
	q.Set("default_transaction_read_only", "on")
	q.Set("statement_timeout", "5000")
	u.RawQuery = q.Encode()
	// Old Redis variables must cause no connection attempt.
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	t.Setenv("REDIS_HOST", host)
	t.Setenv("REDIS_PORT", port)
	connected := make(chan bool, 1)
	go func() {
		conn, e := listener.Accept()
		if e == nil {
			conn.Close()
			connected <- true
		}
	}()
	c := testConfig()
	c.DSN = u.String()
	s, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	ctx := context.Background()
	snap, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.Groups) != 1 || len(snap.Groups[0].Models) != 4 {
		t.Fatalf("groups/catalog: %+v", snap.Groups)
	}
	g := snap.Groups[0]
	if g.CacheUsage.Last24h.CacheHitRate != 20 || g.CacheUsage.Total.CacheHitRate != 50 {
		t.Fatal(g.CacheUsage)
	}
	if len(snap.Monitoring) != 1 || snap.Monitoring[0].Availability7d != 50 {
		t.Fatal(snap.Monitoring)
	}
	snap.GeneratedAt = "2026-01-01T00:00:00Z"
	for i := range snap.Monitoring {
		snap.Monitoring[i].LastCheckedAt = "2026-01-01T00:00:00Z"
		for j := range snap.Monitoring[i].Timeline {
			snap.Monitoring[i].Timeline[j].CheckedAt = "2026-01-01T00:00:00Z"
		}
	}
	golden(t, "snapshot-v1", snap)
	for _, sqlText := range []string{"CREATE TABLE forbidden (id int)", "UPDATE accounts SET status='disabled'", "SELECT credentials FROM accounts", "SELECT email FROM users", "SELECT api_key_encrypted FROM channel_monitors"} {
		if _, e := s.DB.Exec(sqlText); e == nil {
			t.Fatalf("reader allowed: %s", sqlText)
		}
	}
	// Even an admin DSN is forced read-only by the snapshot transaction.
	tx, e := admin.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec("CREATE TABLE forbidden (id int)"); e == nil {
		t.Fatal("read-only transaction allowed DDL")
	}
	tx.Rollback()
	exec("INSERT INTO settings VALUES('channel_monitor_mode','v2')")
	snap, e = s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.Monitoring) != 1 || snap.Monitoring[0].PrimaryModel != "Text" || snap.Monitoring[0].PrimaryStatus != "healthy" || snap.Monitoring[0].Availability7d != 0.9 {
		t.Fatalf("v2: %+v", snap.Monitoring)
	}
	snap.GeneratedAt = "2026-01-01T00:00:00Z"
	golden(t, "snapshot-v2", snap)
	h := NewHandler(s, c)
	w := request(h, "GET", PublicTransitSnapshotPath)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, secret := range []string{"SECRET-", "PRIVATE-EMAIL", "group_id", "channel_id", "account_id", "user_id", "public_page_url"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("privacy leak", secret)
		}
	}
	exec("INSERT INTO settings VALUES('public_transit_enabled','off')")
	for _, path := range []string{PublicTransitSnapshotPath, PublicTransitWellKnownPath} {
		if w = request(h, "GET", path); w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	exec("UPDATE settings SET value='true' WHERE key='public_transit_enabled'")
	exec("TRUNCATE channel_monitor_v2_metrics_rollup")
	snap, e = s.Snapshot(ctx)
	if e != nil || len(snap.Monitoring) != 0 {
		t.Fatal("empty samples", e)
	}
	exec("ALTER TABLE channel_monitor_v2_metrics_rollup RENAME TO missing_optional")
	snap, e = s.Snapshot(ctx)
	if e != nil || len(snap.Monitoring) != 0 || len(snap.Completeness.Warnings) == 0 {
		t.Fatal("optional missing", e)
	}
	exec("ALTER TABLE missing_optional RENAME TO channel_monitor_v2_metrics_rollup")
	exec("REVOKE SELECT ON usage_logs FROM transit_fixture_reader")
	if w = request(NewHandler(s, c), "GET", PublicTransitSnapshotPath); w.Code != 503 || w.Body.String() != "service unavailable\n" {
		t.Fatal("permissions", w.Code, w.Body)
	}
	exec("GRANT SELECT ON usage_logs TO transit_fixture_reader; ALTER TABLE groups RENAME TO missing_core")
	if w = request(NewHandler(s, c), "GET", PublicTransitSnapshotPath); w.Code != 503 {
		t.Fatal("core missing", w.Code)
	}
	for _, path := range []string{PublicTransitSnapshotPath, PublicTransitWellKnownPath} {
		if w = request(h, "GET", path); w.Code != 503 {
			t.Fatal("cached core failure", w.Code)
		}
	}
	exec("ALTER TABLE missing_core RENAME TO groups")
	exec("ALTER TABLE groups RENAME COLUMN models_list_config TO unsupported_models")
	if w = request(NewHandler(s, c), "GET", PublicTransitSnapshotPath); w.Code != 503 {
		t.Fatal("incompatible column", w.Code)
	}
	exec("ALTER TABLE groups RENAME COLUMN unsupported_models TO models_list_config")

	exec("ALTER TABLE settings RENAME TO missing_settings")
	for _, path := range []string{PublicTransitSnapshotPath, PublicTransitWellKnownPath} {
		if w = request(NewHandler(s, c), "GET", path); w.Code != 503 {
			t.Fatal("settings missing", w.Code)
		}
	}
	exec("ALTER TABLE missing_settings RENAME TO settings")
	var status string
	if e = admin.QueryRow("SELECT status FROM accounts WHERE id=1").Scan(&status); e != nil || status != "active" {
		t.Fatal("account modified", e)
	}
	s.DB.Close()
	for _, path := range []string{PublicTransitSnapshotPath, PublicTransitWellKnownPath} {
		if w = request(h, "GET", path); w.Code != 503 {
			t.Fatal("disconnect", w.Code)
		}
	}
	select {
	case <-connected:
		t.Fatal("Redis connection attempted")
	case <-time.After(50 * time.Millisecond):
	}
}
