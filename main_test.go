package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const token = "fw_sk_do_not_print"

func env(url string) func(string) string {
	return func(name string) string {
		return map[string]string{"FIXWIRE_AUTH_TOKEN": token, "FIXWIRE_URL": url}[name]
	}
}

// Usage text prints flag defaults: the API key from the environment is
// not one, so -h or a mistyped flag in CI doesn't print it.
func TestUsageDoesNotPrintTheAPIKey(t *testing.T) {
	for _, args := range [][]string{
		{"sourcemaps", "upload", "-h"},
		{"sourcemaps", "upload", "--relase", "x", "dist"},
		{"releases", "set-commit", "-h"},
	} {
		var out bytes.Buffer
		_ = run(context.Background(), args, &out, env("https://api.fixwire.example"))
		if !strings.Contains(out.String(), "-auth-token") || strings.Contains(out.String(), token) {
			t.Errorf("%v: usage without the API key wanted:\n%s", args, out.String())
		}
	}
}

// The API key still comes from the environment.
func TestAPIKeyFromTheEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "web@1", "commit": "abc", "repository": "a/b", "projects": 1})
	}))
	defer srv.Close()
	var out bytes.Buffer
	args := []string{"releases", "set-commit", "--release", "web@1", "--commit", "abc", "--repository", "a/b"}
	if err := run(context.Background(), args, &out, env(srv.URL)); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
}

// migrate shows what it would change, and changes it with --write, flags
// before or after the directory: here, a fixture of the migrate package.
func TestMigrate(t *testing.T) {
	in, err := os.ReadFile("migrate/testdata/python/in/settings.py")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("migrate/testdata/python/want/settings.py")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.py")
	if err := os.WriteFile(settings, in, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"migrate", dir}, &out, env("")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(settings); !bytes.Equal(b, in) || !strings.Contains(out.String(), "+ import fixwire") {
		t.Fatalf("a dry run changed the file, or showed nothing:\n%s", out.String())
	}
	out.Reset()
	if err := run(context.Background(), []string{"migrate", dir, "--write"}, &out, env("")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(settings); !bytes.Equal(b, want) {
		t.Fatalf("after --write:\n%s", b)
	}
}

// --delete removes the source maps once they're uploaded, and nothing else.
func TestUploadDeletesTheMaps(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/0/organizations/-/chunk-upload/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "http://" + r.Host + "/api/0/organizations/-/chunk-upload/",
			"chunkSize": 1 << 20, "chunksPerRequest": 4, "maxRequestSize": 32 << 20, "accept": []string{"artifact_bundles"}})
	})
	mux.HandleFunc("POST /api/0/organizations/-/artifactbundle/assemble/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "chunks"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"chunks/app.js":     "a()\n//# sourceMappingURL=app.js.map\n",
		"chunks/app.js.map": `{"version":3,"sources":["app.ts"],"names":[],"mappings":"AAAA"}`,
		"chunks/data.map":   "not a source map",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"sourcemaps", "upload", "--inject", "--delete", dir}, &out, env(srv.URL)); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	for name, kept := range map[string]bool{"chunks/app.js": true, "chunks/app.js.map": false, "chunks/data.map": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != kept {
			t.Errorf("%s: kept = %v, want %v", name, err == nil, kept)
		}
	}
	if !strings.Contains(out.String(), "deleted 1 source maps") {
		t.Errorf("output:\n%s", out.String())
	}
}
