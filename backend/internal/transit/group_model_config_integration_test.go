package transit

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Called only within TestPostgresReadOnly's disposable transit_test database.
func testGroupModelCompatibility(t *testing.T, admin *sql.DB, s *Store, c Config) {
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	check := func(h *Handler, status int, names []string, missing bool) PublicTransitSnapshot {
		t.Helper()
		var snap PublicTransitSnapshot
		for _, path := range []string{PublicTransitWellKnownPath, PublicTransitSnapshotPath} {
			w := request(h, "GET", path)
			if w.Code != status {
				t.Fatalf("%s: status %d, want %d: %s", path, w.Code, status, w.Body)
			}
			if status == 503 {
				if w.Body.String() != "service unavailable\n" {
					t.Fatalf("error disclosure: %s", w.Body)
				}
				continue
			}
			if path == PublicTransitSnapshotPath {
				if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
					t.Fatal(err)
				}
				if len(snap.Groups) != 1 || snap.Groups[0].Models == nil {
					t.Fatalf("groups or empty array shape: %+v", snap.Groups)
				}
				var got []string
				for _, m := range snap.Groups[0].Models {
					got = append(got, m.StandardModel)
				}
				if !slices.Equal(got, names) {
					t.Fatalf("models = %v, want %v", got, names)
				}
				if slices.Contains(snap.Completeness.Warnings, missingGroupModelConfigWarning) != missing {
					t.Fatalf("missing configuration warning: %v", snap.Completeness.Warnings)
				}
			}
		}
		return snap
	}
	legacy := []string{"Alias", "catalog-model", "Image", "Text"}
	catalog := []string{"Alias", "Image", "Text"}
	h := NewHandler(s, c)
	baseline := check(h, 200, legacy, false)
	byName := make(map[string]PublicTransitModel)
	for _, m := range baseline.Groups[0].Models {
		byName[m.StandardModel] = m
	}

	t.Run("rename during cached lifetime", func(t *testing.T) {
		exec("ALTER TABLE groups RENAME COLUMN models_list_config TO model_allowlist")
		check(h, 200, legacy, false) // Existing snapshot retains its documented TTL.
		h.mu.Lock()
		h.expires = time.Now().Add(-time.Second)
		h.mu.Unlock()
		check(h, 200, []string{"Text"}, false)
	})
	t.Run("new column semantics", func(t *testing.T) {
		for _, tc := range []struct {
			name, raw string
			want      []string
		}{
			{"disabled", `{"enabled":false,"models":["unknown-model"]}`, catalog},
			{"empty object", `{}`, catalog},
			{"null configuration", `null`, catalog},
			{"empty enabled", `{"enabled":true,"models":[]}`, nil},
			{"missing enabled list", `{"enabled":true}`, nil},
			{"blank entries", `{"enabled":true,"models":[" "]}`, nil},
			{"exact case insensitive", `{"enabled":true,"models":[" tExT ","TEXT"]}`, []string{"Text"}},
			{"prefix wildcard", `{"enabled":true,"models":["te*"]}`, []string{"Text"}},
			{"bare wildcard", `{"enabled":true,"models":["*"]}`, catalog},
			{"unsupported entries add nothing", `{"enabled":true,"models":["catalog-model","unknown-*","gpt-4o"]}`, nil},
			{"client alias retains target price", `{"enabled":true,"models":["alias"]}`, []string{"Alias"}},
			{"target does not allow alias", `{"enabled":true,"models":["Text"]}`, []string{"Text"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				exec("UPDATE groups SET model_allowlist=$1 WHERE id=1", tc.raw)
				snap := check(NewHandler(s, c), 200, tc.want, false)
				for _, m := range snap.Groups[0].Models {
					if !reflect.DeepEqual(m, byName[m.StandardModel]) {
						t.Fatalf("filter changed price or provenance: %+v", m)
					}
				}
			})
		}
	})
	t.Run("both columns prefer new even disabled or empty", func(t *testing.T) {
		exec(`ALTER TABLE groups ADD COLUMN models_list_config jsonb DEFAULT '{"enabled":true,"models":["legacy-only"]}'`)
		// The unselected legacy column is deliberately not granted.
		for _, raw := range []string{`{}`, `{"enabled":false,"models":["Text"]}`} {
			exec("UPDATE groups SET model_allowlist=$1 WHERE id=1", raw)
			check(NewHandler(s, c), 200, catalog, false)
		}
		exec(`UPDATE groups SET model_allowlist='{"enabled":true,"models":[]}' WHERE id=1`)
		check(NewHandler(s, c), 200, nil, false)
		exec(`UPDATE groups SET model_allowlist='{"enabled":true,"models":["Text"]}' WHERE id=1`)
		check(NewHandler(s, c), 200, []string{"Text"}, false)
		// A broken, unselected old column must not affect the selected new one.
		exec("ALTER TABLE groups ALTER COLUMN models_list_config DROP DEFAULT; ALTER TABLE groups ALTER COLUMN models_list_config TYPE text USING models_list_config::text")
		check(NewHandler(s, c), 200, []string{"Text"}, false)
		exec("ALTER TABLE groups ALTER COLUMN models_list_config TYPE jsonb USING models_list_config::jsonb")
	})
	t.Run("selected column privilege checked on cache hits", func(t *testing.T) {
		h := NewHandler(s, c)
		check(h, 200, []string{"Text"}, false)
		exec("GRANT SELECT(models_list_config) ON groups TO transit_fixture_reader; REVOKE SELECT(model_allowlist) ON groups FROM transit_fixture_reader")
		check(h, 503, nil, false)
		check(NewHandler(s, c), 503, nil, false)
		exec("GRANT SELECT(model_allowlist) ON groups TO transit_fixture_reader")
		check(h, 200, []string{"Text"}, false)
	})
	t.Run("invalid selected type and payload fail closed", func(t *testing.T) {
		h := NewHandler(s, c)
		check(h, 200, []string{"Text"}, false)
		exec("ALTER TABLE groups ALTER COLUMN model_allowlist DROP DEFAULT; ALTER TABLE groups ALTER COLUMN model_allowlist TYPE text USING model_allowlist::text")
		check(h, 503, nil, false)
		exec("ALTER TABLE groups ALTER COLUMN model_allowlist TYPE json USING model_allowlist::json")
		check(h, 200, []string{"Text"}, false)
		for _, raw := range []string{`[]`, `true`, `"invalid"`, `{"enabled":"true"}`, `{"enabled":true,"models":[1]}`, `{"models":{}}`} {
			exec("UPDATE groups SET model_allowlist=$1 WHERE id=1", raw)
			check(h, 503, nil, false)
		}
		exec("ALTER TABLE groups ALTER COLUMN model_allowlist TYPE jsonb USING model_allowlist::jsonb")
	})
	t.Run("neither column preserves channel catalog", func(t *testing.T) {
		exec("ALTER TABLE groups DROP COLUMN models_list_config; ALTER TABLE groups RENAME COLUMN model_allowlist TO unavailable_config")
		check(NewHandler(s, c), 200, catalog, true)
		exec(`UPDATE groups SET unavailable_config='{"enabled":true,"models":["Text","catalog-model"," ","CATALOG-MODEL"]}' WHERE id=1`)
		exec("ALTER TABLE groups RENAME COLUMN unavailable_config TO models_list_config")
		check(NewHandler(s, c), 200, legacy, false)
		exec("REVOKE SELECT(models_list_config) ON groups FROM transit_fixture_reader")
		check(NewHandler(s, c), 503, nil, false)
		exec("GRANT SELECT(models_list_config) ON groups TO transit_fixture_reader")
	})
	t.Run("probe follows search path", func(t *testing.T) {
		exec("CREATE SCHEMA transit_alternate; CREATE TABLE transit_alternate.groups (LIKE public.groups INCLUDING ALL); INSERT INTO transit_alternate.groups SELECT * FROM public.groups; ALTER TABLE transit_alternate.groups RENAME COLUMN models_list_config TO model_allowlist; GRANT USAGE ON SCHEMA transit_alternate TO transit_fixture_reader; GRANT SELECT(id,name,platform,subscription_type,rate_multiplier,image_price_1k,image_price_2k,image_price_4k,status,is_exclusive,deleted_at,model_allowlist) ON transit_alternate.groups TO transit_fixture_reader")
		t.Cleanup(func() { exec("DROP SCHEMA transit_alternate CASCADE") })
		u, err := url.Parse(c.DSN)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", "transit_alternate,public")
		u.RawQuery = q.Encode()
		altConfig := c
		altConfig.DSN = u.String()
		alt, err := Open(altConfig)
		if err != nil {
			t.Fatal(err)
		}
		defer alt.DB.Close()
		check(NewHandler(alt, altConfig), 200, []string{"Text"}, false)
		if strings.Contains(altConfig.DSN, "postgres:fixture-admin") {
			t.Fatal("test must use reader")
		}
	})
	check(NewHandler(s, c), 200, legacy, false)
}
