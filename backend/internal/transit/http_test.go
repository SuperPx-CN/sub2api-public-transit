package transit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu          sync.Mutex
	err         error
	snapshotErr error
	loads       int
	snapshot    *PublicTransitSnapshot
}

func (f *fakeSource) Enabled(context.Context) error { f.mu.Lock(); defer f.mu.Unlock(); return f.err }
func (f *fakeSource) Snapshot(context.Context) (*PublicTransitSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	return f.snapshot, f.err
}
func testConfig() Config {
	return Config{PublicBaseURL: "https://transit.example", CacheTTL: time.Minute}
}
func request(h *Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "evil.example"
	r.Header.Set("X-Forwarded-Host", "evil.example")
	r.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestRoutesAndCache(t *testing.T) {
	f := &fakeSource{snapshot: assemble(testConfig(), nil, []PublicTransitGroup{}, []PublicTransitMonitor{}, nil, time.Unix(0, 0))}
	h := NewHandler(f, testConfig())
	for _, p := range []string{"/", "/health", "/public/transit", "/api/v1/public/transit/snapshot", "/api/v1/admin", "/login", "/api/v1/auth/login", "/api/v1/payment", "/v1/messages", "/v1/chat/completions", PublicTransitSnapshotPath + "/"} {
		if w := request(h, "GET", p); w.Code != 404 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	for _, p := range []string{PublicTransitWellKnownPath, PublicTransitSnapshotPath} {
		for _, m := range []string{"POST", "HEAD", "OPTIONS", "PUT", "DELETE"} {
			w := request(h, m, p)
			if w.Code != 405 || w.Header().Get("Allow") != "GET" {
				t.Fatal(m, p, w.Code)
			}
		}
		w := request(h, "GET", p)
		if w.Code != 200 || strings.Contains(w.Body.String(), "evil.example") {
			t.Fatal(w.Code, w.Body)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); request(h, "GET", PublicTransitSnapshotPath) }()
	}
	wg.Wait()
	if f.loads != 1 {
		t.Fatal(f.loads)
	}
	h.expires = time.Time{}
	request(h, "GET", PublicTransitSnapshotPath)
	if f.loads != 2 {
		t.Fatal(f.loads)
	}
	for _, err := range []error{ErrDisabled, errors.New("SELECT secret password=private")} {
		f.err = err
		for _, p := range []string{PublicTransitWellKnownPath, PublicTransitSnapshotPath} {
			w := request(h, "GET", p)
			want := 503
			if err == ErrDisabled {
				want = 404
			}
			if w.Code != want || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body)
			}
		}
	}
	f.err = nil
	request(h, "GET", PublicTransitSnapshotPath)
	if f.loads != 3 {
		t.Fatal("failed response was cached")
	}
}
func TestConfig(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://transit.example")
	t.Setenv("REDIS_HOST", "127.0.0.1")
	t.Setenv("REDIS_PASSWORD", "ignored-secret")
	c, e := LoadConfig()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(c.DSN, "default_transaction_read_only=on") || strings.Contains(c.DSN, "ignored-secret") {
		t.Fatal("unsafe DSN")
	}
	for _, v := range []string{"", "file:///tmp/x", "https://user:pass@example.com", "https://example.com/path", "https://example.com?x=y"} {
		t.Setenv("PUBLIC_BASE_URL", v)
		if _, e := LoadConfig(); e == nil {
			t.Fatal(v)
		}
	}
}
func TestPricingAndCatalog(t *testing.T) {
	p, e := LoadPricing("")
	if e != nil {
		t.Fatal(e)
	}
	if p.GetModelPricing("gpt-4o") == nil {
		t.Fatal("missing local pricing")
	}
	g := Group{ID: 1, Name: "public", Platform: "openai", Status: StatusActive, RateMultiplier: 2, ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{" X ", "x", "", "unknown-model"}}}
	groups := buildPublicTransitGroups([]Group{g, {ID: 2, IsExclusive: true}, {ID: 3, Status: "disabled"}}, nil, nil, p)
	if len(groups) != 1 || len(groups[0].Models) != 2 {
		t.Fatal(groups)
	}
	for _, m := range groups[0].Models {
		if m.CatalogSource != "group_models_list" {
			t.Fatal(m)
		}
	}
	zero := 0.0
	if pricingNeedsFallback(&ChannelModelPricing{InputPrice: &zero}) {
		t.Fatal("zero is an explicit price")
	}
	if pricingNeedsFallback(&ChannelModelPricing{Intervals: []PricingInterval{{InputPrice: &zero}}}) {
		t.Fatal("interval price ignored")
	}
}
func golden(t *testing.T, name string, v any) {
	t.Helper()
	data, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	data = append(data, '\n')
	p := filepath.Join("testdata", name+".json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if e = os.WriteFile(p, data, 0644); e != nil {
			t.Fatal(e)
		}
	}
	want, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if string(want) != string(data) {
		t.Fatalf("golden mismatch %s", name)
	}
}
func TestDiscoveryGolden(t *testing.T) {
	golden(t, "discovery", PublicTransitDiscovery{SchemaVersion: PublicTransitSchemaVersion, System: PublicTransitSystem, SnapshotURL: "https://transit.example" + PublicTransitSnapshotPath, GeneratedAt: "2026-01-01T00:00:00Z"})
}

func TestChannelMappingPricingSemantics(t *testing.T) {
	input, output, image := 3e-6, 15e-6, 0.125
	ch := Channel{ModelMapping: map[string]map[string]string{"openai": {"alias": "Text", "Text*": "*", "*": "*"}}, ModelPricing: []ChannelModelPricing{
		{Platform: "openai", Models: []string{"Text", "text"}, InputPrice: &input, OutputPrice: &output, Intervals: []PricingInterval{{MinTokens: 100, InputPrice: &input}}},
		{Platform: "openai", Models: []string{"Image"}, BillingMode: BillingModeImage, PerRequestPrice: &image},
	}}
	models := ch.SupportedModels()
	if len(models) != 3 {
		t.Fatal(models)
	}
	for _, m := range models {
		if strings.Contains(m.Name, "*") {
			t.Fatal(m)
		}
		g := Group{ImagePrice1K: &image}
		out := toPublicTransitModel(m, g)
		if m.Name == "alias" {
			if out.Price.InputUSDPerToken == nil || *out.Price.InputUSDPerToken != input || len(out.Intervals) != 1 {
				t.Fatal(out)
			}
		}
		if m.Name == "Image" {
			if out.BillingMode != "per_request" || *out.Price.ImageSizePrices["1k"] != image {
				t.Fatal(out)
			}
		}
	}
}
func TestExplicitPriceFileAndStationLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.json")
	if err := os.WriteFile(path, []byte("{\"custom\":{\"input_cost_per_token\":0.000001}}"), 0400); err != nil {
		t.Fatal(err)
	}
	p, e := LoadPricing(path)
	if e != nil {
		t.Fatal(e)
	}
	if p.GetModelPricing("custom").InputCostPerToken != 1e-6 {
		t.Fatal("explicit catalog not loaded")
	}
	t.Setenv("PUBLIC_BASE_URL", "https://transit.example")
	t.Setenv("STATION_MONITOR_URL", "https://station.example/status?view=monitoring")
	c, e := LoadConfig()
	if e != nil {
		t.Fatal(e)
	}
	snap := assemble(c, nil, nil, nil, nil, time.Unix(0, 0))
	if snap.Station.MonitorURL != c.MonitorURL || snap.Station.HomepageURL != "" {
		t.Fatal(snap.Station)
	}
}

func TestSnapshotFailuresAreNotCached(t *testing.T) {
	f := &fakeSource{snapshotErr: errors.New("private SQL"), snapshot: assemble(testConfig(), nil, nil, nil, nil, time.Unix(0, 0))}
	h := NewHandler(f, testConfig())
	for i := 0; i < 2; i++ {
		w := request(h, "GET", PublicTransitSnapshotPath)
		if w.Code != 503 || w.Body.String() != "service unavailable\n" {
			t.Fatal(w.Code, w.Body)
		}
	}
	f.snapshotErr = nil
	if w := request(h, "GET", PublicTransitSnapshotPath); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if f.loads != 3 {
		t.Fatal("failed snapshot was cached")
	}
}
