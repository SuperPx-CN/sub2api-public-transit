package transit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

type Source interface {
	Enabled(context.Context) error
	Snapshot(context.Context) (*PublicTransitSnapshot, error)
}
type Handler struct {
	source  Source
	config  Config
	mu      sync.Mutex
	cached  []byte
	expires time.Time
}

func NewHandler(s Source, c Config) *Handler {
	if c.CacheTTL <= 0 {
		c.CacheTTL = 60 * time.Second
	}
	return &Handler{source: s, config: c}
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != PublicTransitWellKnownPath && r.URL.Path != PublicTransitSnapshotPath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", 405)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// Check the source and kill switch even while the assembled snapshot is cached.
	if err := h.source.Enabled(ctx); err != nil {
		h.mu.Lock()
		h.cached = nil
		h.mu.Unlock()
		writeError(w, r, err)
		return
	}
	var data []byte
	var err error
	if r.URL.Path == PublicTransitWellKnownPath {
		data, err = json.Marshal(PublicTransitDiscovery{SchemaVersion: PublicTransitSchemaVersion, System: PublicTransitSystem, SnapshotURL: absoluteURL(h.config.PublicBaseURL, PublicTransitSnapshotPath), HomepageURL: h.config.HomeURL, GeneratedAt: time.Now().UTC().Format(time.RFC3339)})
	} else {
		data, err = h.snapshot(ctx)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}
func (h *Handler) snapshot(ctx context.Context) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached != nil && time.Now().Before(h.expires) {
		return h.cached, nil
	}
	snap, err := h.source.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	h.cached = data
	h.expires = time.Now().Add(h.config.CacheTTL)
	return data, nil
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrDisabled) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, "service unavailable", 503)
}
