package sourcemaps

import (
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha1" //nolint:gosec // the upload protocol addresses chunks by SHA-1
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Options configure an upload.
type Options struct {
	// URL is the Fixwire API, e.g. https://api.fixwire.example.
	URL string
	// Token is an API key with the artifacts:write scope.
	Token string
	// Org is the organization slug in the API paths; the key decides the
	// organization, so any value works.
	Org string
	// Projects are project slugs; a project key needs none.
	Projects []string
	// Release and Dist tie the files to a release, for SDKs that don't
	// report debug ids.
	Release, Dist string
	// URLPrefix is how the files are served, "~/" (any host) by default:
	// dist/assets/app.js uploaded from dist is ~/assets/app.js.
	URLPrefix string
	// HTTP is the client to use (default: 60 s timeout).
	HTTP *http.Client
	// Poll is how often to ask for the assembly state (default 1 s), for
	// up to Wait (default 5 min).
	Poll, Wait time.Duration
}

// Result describes an upload.
type Result struct {
	Files    int
	DebugIDs int
	Bytes    int
	Checksum string
	// Uploaded is the chunks sent; zero when the server had them all.
	Uploaded int
	// Maps are the source maps in the bundle, relative to its directory
	// (slash-separated).
	Maps []string
}

// chunkOptions is the server's answer to GET chunk-upload.
type chunkOptions struct {
	URL              string   `json:"url"`
	ChunkSize        int      `json:"chunkSize"`
	ChunksPerRequest int      `json:"chunksPerRequest"`
	MaxFileSize      int64    `json:"maxFileSize"`
	MaxRequestSize   int      `json:"maxRequestSize"`
	Compression      []string `json:"compression"`
	Accept           []string `json:"accept"`
}

type assembleResponse struct {
	State         string   `json:"state"`
	MissingChunks []string `json:"missingChunks"`
	Detail        *string  `json:"detail"`
}

// Upload bundles the JavaScript files and source maps under dir and
// uploads them.
func Upload(ctx context.Context, dir string, o Options) (Result, error) {
	if o.URL == "" || o.Token == "" {
		return Result{}, errors.New("sourcemaps: set the Fixwire URL and an API key with the artifacts:write scope")
	}
	if o.Org == "" {
		o.Org = "-"
	}
	if o.URLPrefix == "" {
		o.URLPrefix = "~/"
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if o.Wait <= 0 {
		o.Wait = 5 * time.Minute
	}
	zipped, res, err := Bundle(dir, o)
	if err != nil {
		return Result{}, err
	}
	if res.Files == 0 {
		return res, errors.New("sourcemaps: no JavaScript files or source maps found")
	}
	c := client{o: o}
	opts, err := c.options(ctx)
	if err != nil {
		return res, err
	}
	if !slices.Contains(opts.Accept, "artifact_bundles") {
		return res, errors.New("sourcemaps: the server does not accept artifact bundles")
	}
	if opts.MaxFileSize > 0 && int64(len(zipped)) > opts.MaxFileSize {
		return res, fmt.Errorf("sourcemaps: the bundle is %d MiB, over the server's %d MiB", len(zipped)>>20, opts.MaxFileSize>>20)
	}
	size := opts.ChunkSize
	if size <= 0 {
		size = 8 << 20
	}
	var chunks [][]byte
	for i := 0; i < len(zipped); i += size {
		chunks = append(chunks, zipped[i:min(i+size, len(zipped))])
	}
	sums := make([]string, len(chunks))
	byHash := make(map[string][]byte, len(chunks))
	for i, ch := range chunks {
		sums[i] = sha1Hex(ch)
		byHash[sums[i]] = ch
	}

	deadline := time.Now().Add(o.Wait)
	uploaded := false
	for {
		a, err := c.assemble(ctx, res.Checksum, sums)
		if err != nil {
			return res, err
		}
		switch a.State {
		case "ok":
			return res, nil
		case "error":
			detail := "assembly failed"
			if a.Detail != nil {
				detail = *a.Detail
			}
			return res, fmt.Errorf("sourcemaps: %s", detail)
		case "not_found":
			if uploaded {
				return res, errors.New("sourcemaps: the server lost the uploaded chunks")
			}
			missing := make([][]byte, 0, len(a.MissingChunks))
			for _, h := range a.MissingChunks {
				if ch, ok := byHash[h]; ok {
					missing = append(missing, ch)
				}
			}
			if err := c.upload(ctx, opts, missing); err != nil {
				return res, err
			}
			res.Uploaded, uploaded = len(missing), true
			continue
		}
		if time.Now().After(deadline) {
			return res, fmt.Errorf("sourcemaps: still %q after %s", a.State, o.Wait)
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(o.Poll):
		}
	}
}

// Bundle builds the artifact bundle for dir: a zip with each JavaScript
// file and source map and a manifest naming their URLs and debug ids. The
// zip is deterministic, so the same build gives the same checksum and the
// server stores it once.
func Bundle(dir string, o Options) ([]byte, Result, error) {
	if o.URLPrefix == "" {
		o.URLPrefix = "~/"
	}
	type file struct {
		rel  string
		data []byte
	}
	var files []file
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		// Regular files only: a symbolic link can lead out of the build.
		if err != nil || !d.Type().IsRegular() || (!hasJSExtension(p) && !strings.HasSuffix(p, ".map")) {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files = append(files, file{rel: filepath.ToSlash(rel), data: data})
		return nil
	})
	if err != nil {
		return nil, Result{}, err
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.rel, b.rel) })

	type entry struct {
		URL     string            `json:"url"`
		Type    string            `json:"type"`
		Headers map[string]string `json:"headers,omitempty"`
	}
	manifest := struct {
		Files   map[string]entry `json:"files"`
		Release string           `json:"release,omitempty"`
		Dist    string           `json:"dist,omitempty"`
	}{Files: map[string]entry{}, Release: o.Release, Dist: o.Dist}
	var res Result
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	for _, f := range files {
		e := entry{URL: urlPath(o.URLPrefix, f.rel), Headers: map[string]string{}}
		var id string
		if strings.HasSuffix(f.rel, ".map") {
			e.Type = "source_map"
			var m struct {
				DebugID  string `json:"debug_id"`
				DebugID2 string `json:"debugId"`
			}
			if json.Unmarshal(f.data, &m) != nil {
				continue // not a source map
			}
			id = cmp.Or(m.DebugID, m.DebugID2)
			res.Maps = append(res.Maps, f.rel)
		} else {
			e.Type = "minified_source"
			if m := debugIDComment.FindSubmatch(f.data); m != nil {
				id = string(m[1])
			}
			if refs := mapComment.FindAllSubmatch(f.data, -1); len(refs) > 0 {
				if ref := string(refs[len(refs)-1][1]); !strings.HasPrefix(ref, "data:") {
					e.Headers["sourcemap"] = ref
				}
			}
		}
		if parsed, err := uuid.Parse(id); err == nil {
			e.Headers["debug-id"] = parsed.String()
			res.DebugIDs++
		}
		name := "files/" + f.rel
		manifest.Files[name] = e
		if err := add(name, f.data); err != nil {
			return nil, Result{}, err
		}
		res.Files++
	}
	m, err := json.Marshal(manifest)
	if err != nil {
		return nil, Result{}, err
	}
	if err := add("manifest.json", m); err != nil {
		return nil, Result{}, err
	}
	if err := zw.Close(); err != nil {
		return nil, Result{}, err
	}
	res.Bytes, res.Checksum = buf.Len(), sha1Hex(buf.Bytes())
	return buf.Bytes(), res, nil
}

type client struct{ o Options }

func (c client) api(p string) string {
	return strings.TrimRight(c.o.URL, "/") + "/api/0/organizations/" + url.PathEscape(c.o.Org) + p
}

func (c client) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.o.Token)
	req.Header.Set("User-Agent", "fixwire-cli")
	res, err := c.o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return fmt.Errorf("sourcemaps: %s %s: %d %s", req.Method, req.URL.Path, res.StatusCode, msg)
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c client) options(ctx context.Context) (chunkOptions, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api("/chunk-upload/"), nil)
	if err != nil {
		return chunkOptions{}, err
	}
	var o chunkOptions
	return o, c.do(req, &o)
}

func (c client) assemble(ctx context.Context, checksum string, chunks []string) (assembleResponse, error) {
	body, _ := json.Marshal(map[string]any{
		"checksum": checksum, "chunks": chunks, "projects": c.o.Projects, "version": c.o.Release, "dist": c.o.Dist,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api("/artifactbundle/assemble/"), bytes.NewReader(body))
	if err != nil {
		return assembleResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var a assembleResponse
	return a, c.do(req, &a)
}

// upload sends chunks in requests within the server's limits, gzipped when
// it accepts gzip. The API key goes with them, so they go only to the
// origin of the configured URL, whatever URL the server names.
func (c client) upload(ctx context.Context, o chunkOptions, chunks [][]byte) error {
	target := c.api("/chunk-upload/")
	if o.URL != "" {
		base, err1 := url.Parse(c.o.URL)
		u, err2 := url.Parse(o.URL)
		if err1 == nil && err2 == nil {
			if u = base.ResolveReference(u); u.Scheme == base.Scheme && u.Host == base.Host {
				target = u.String()
			}
		}
	}
	gz := slices.Contains(o.Compression, "gzip")
	perRequest := max(o.ChunksPerRequest, 1)
	maxBytes := o.MaxRequestSize
	if maxBytes <= 0 {
		maxBytes = 32 << 20
	}
	for len(chunks) > 0 {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		n := 0
		for n < len(chunks) && n < perRequest && (n == 0 || body.Len()+len(chunks[n]) <= maxBytes) {
			field, data := "file", chunks[n]
			if gz {
				field, data = "file_gzip", gzipBytes(data)
			}
			w, err := mw.CreateFormFile(field, sha1Hex(chunks[n]))
			if err != nil {
				return err
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
			n++
		}
		if err := mw.Close(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if err := c.do(req, nil); err != nil {
			return err
		}
		chunks = chunks[n:]
	}
	return nil
}

func sha1Hex(b []byte) string {
	s := sha1.Sum(b) //nolint:gosec // the protocol's checksum
	return hex.EncodeToString(s[:])
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}
