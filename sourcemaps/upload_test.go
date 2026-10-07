package sourcemaps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The API key goes with every chunk: chunks go to the configured server,
// whatever upload URL its options name.
func TestChunksGoOnlyToTheConfiguredServer(t *testing.T) {
	var elsewhere, uploads atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/0/organizations/-/chunk-upload/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"url": other.URL + "/upload/", "chunkSize": 1 << 20, "chunksPerRequest": 4,
			"maxRequestSize": 32 << 20, "compression": []string{"gzip"}, "accept": []string{"artifact_bundles"}})
	})
	mux.HandleFunc("POST /api/0/organizations/-/chunk-upload/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fw_sk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		uploads.Add(1)
	})
	mux.HandleFunc("POST /api/0/organizations/-/artifactbundle/assemble/", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Chunks []string `json:"chunks"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if uploads.Load() == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "not_found", "missingChunks": in.Chunks})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	writeBuild(t, dir, "app.js", "a()", 0, 0)
	if _, err := Upload(context.Background(), dir, Options{URL: srv.URL, Token: "fw_sk_test", Poll: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if uploads.Load() == 0 || elsewhere.Load() != 0 {
		t.Fatalf("chunk requests: %d to the server, %d elsewhere", uploads.Load(), elsewhere.Load())
	}
}
