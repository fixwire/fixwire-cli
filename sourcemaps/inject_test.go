package sourcemaps

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sourcemap/sourcemap"
)

const original = "export function addToCart(id) {\n  if (!id) throw new Error(\"missing\");\n}\n"

// writeBuild writes a minified file whose code at (line, col) maps to
// cart.js line 2 (1-based) column 2, and returns its path.
func writeBuild(t *testing.T, dir, name, code string, line, col int) string {
	t.Helper()
	mappings := strings.Repeat(";", line) + encodeSegment([]int{col, 0, 1, 2})
	if line == 0 && col > 0 {
		// A segment at column 0 too, which an insertion after it must not move.
		mappings = encodeSegment([]int{0, 0, 0, 0}) + "," + encodeSegment([]int{col, 0, 1, 2})
	}
	m, _ := json.Marshal(map[string]any{"version": 3, "sources": []string{"src/cart.js"}, "sourcesContent": []string{original},
		"names": []string{}, "mappings": mappings, "x_custom": "kept"})
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(code+"\n//# sourceMappingURL="+name+".map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".map", m, 0o644); err != nil {
		t.Fatal(err)
	}
	// The original, outside the build directory's extensions, to measure
	// the insertion against.
	if err := os.WriteFile(p+".orig", []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// origin maps a generated position (1-based line, 0-based column) through
// the file's map as it is on disk.
func origin(t *testing.T, file string, line, col int) (string, int, int) {
	t.Helper()
	raw, err := os.ReadFile(file + ".map")
	if err != nil {
		t.Fatal(err)
	}
	// No URL: sources stay as the map names them (a Windows path, C:\…,
	// would read as a URL with the scheme c:).
	m, err := sourcemap.Parse("", raw)
	if err != nil {
		t.Fatal(err)
	}
	src, _, l, c, ok := m.Source(line, col)
	if !ok {
		t.Fatalf("%s:%d:%d maps nowhere", file, line, col)
	}
	return src, l, c
}

func TestInjectKeepsMappings(t *testing.T) {
	dir := t.TempDir()
	plain := writeBuild(t, dir, "plain.js", `function a(b){if(!b)throw new Error("missing")}`, 0, 14)
	strict := writeBuild(t, dir, "strict.js", `"use strict";'use client';function a(b){if(!b)throw new Error("missing")}`, 0, 40)
	bang := writeBuild(t, dir, "cli.mjs", "#!/usr/bin/env node\nfunction a(b){if(!b)throw new Error(\"missing\")}", 1, 14)
	notDirective := writeBuild(t, dir, "expr.js", `"abc".length;function a(b){}`, 0, 13)
	_ = os.WriteFile(filepath.Join(dir, "nomap.js"), []byte("var x=1"), 0o644)

	files, err := Inject(dir)
	if err != nil || len(files) != 4 {
		t.Fatalf("inject = %+v, %v", files, err)
	}
	ids := map[string]string{}
	for _, f := range files {
		ids[filepath.Base(f.File)] = f.DebugID.String()
	}

	for _, c := range []struct {
		file      string
		line, col int
		insertAt  int // where on that line the snippet went
		lineStart string
	}{
		{plain, 1, 14, 0, ";(function(){"},
		{strict, 1, 40, len(`"use strict";'use client';`), `"use strict";'use client';(function(){`[:13]},
		{bang, 2, 14, 0, ";(function(){"},
		{notDirective, 1, 13, 0, ";(function(){"},
	} {
		src, _ := os.ReadFile(c.file)
		lines := strings.Split(string(src), "\n")
		code := lines[c.line-1]
		if !strings.HasPrefix(code, c.lineStart) {
			t.Errorf("%s line %d = %.60q", c.file, c.line, code)
		}
		shift := len(code) - len(strings.Split(mustRead(t, c.file+".orig"), "\n")[c.line-1])
		if shift <= 0 {
			t.Errorf("%s: nothing inserted", c.file)
		}
		if s, l, col := origin(t, c.file, c.line, c.col+shift); s != "src/cart.js" || l != 2 || col != 2 {
			t.Errorf("%s: moved code maps to %s:%d:%d", c.file, s, l, col)
		}
		id := ids[filepath.Base(c.file)]
		if !strings.Contains(string(src), "//# debugId="+id+"\n//# sourceMappingURL=") || !strings.Contains(string(src), `g._fixwireDebugIds[s]="`+id+`"`) {
			t.Errorf("%s: debug id comment or snippet missing:\n%s", c.file, src)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(mustRead(t, c.file+".map")), &m)
		if m["debug_id"] != id || m["debugId"] != id || m["x_custom"] != "kept" {
			t.Errorf("%s map = %v", c.file, m)
		}
	}
	// The "use strict" segment at column 0 did not move.
	if s, l, _ := origin(t, strict, 1, 0); s != "src/cart.js" || l != 1 {
		t.Errorf("prologue segment maps to %s:%d", s, l)
	}

	// A second run changes nothing and reports the same ids.
	before := snapshot(t, dir)
	again, err := Inject(dir)
	if err != nil || len(again) != 4 || !again[0].Already {
		t.Fatalf("again = %+v, %v", again, err)
	}
	for _, f := range again {
		if ids[filepath.Base(f.File)] != f.DebugID.String() {
			t.Errorf("%s: id changed", f.File)
		}
	}
	if after := snapshot(t, dir); after != before {
		t.Error("a second run rewrote files")
	}
}

func TestDebugIDIsStable(t *testing.T) {
	a, b := debugIDFor([]byte("x")), debugIDFor([]byte("x"))
	if a != b || a == debugIDFor([]byte("y")) || a.Version() != 4 || a.Variant().String() != "RFC4122" {
		t.Fatalf("ids = %s %s", a, b)
	}
}

func TestShiftColumns(t *testing.T) {
	m := encodeSegment([]int{0, 0, 0, 0}) + "," + encodeSegment([]int{5, 0, 1, 0}) + "," + encodeSegment([]int{5, 0, 1, 4}) + ";" + encodeSegment([]int{3, 0, 1, 0})
	got, err := shiftColumns(m, 0, 3, 100)
	if err != nil {
		t.Fatal(err)
	}
	segs := strings.Split(strings.Split(got, ";")[0], ",")
	first, _ := decodeSegment(segs[0])
	second, _ := decodeSegment(segs[1])
	third, _ := decodeSegment(segs[2])
	if first[0] != 0 || second[0] != 105 || third[0] != 5 || strings.Split(got, ";")[1] != strings.Split(m, ";")[1] {
		t.Fatalf("shifted = %v %v %v", first, second, third)
	}
	if _, err := shiftColumns("!!", 0, 0, 1); err == nil {
		t.Fatal("bad VLQ accepted")
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		b.WriteString(e.Name() + "\n" + mustRead(t, filepath.Join(dir, e.Name())) + "\n")
	}
	return b.String()
}

// A build can't make inject rewrite anything outside it: not through a
// sourceMappingURL that climbs out, not through a symbolic link.
func TestInjectStaysInsideTheBuild(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dist")
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, p string) {
		t.Helper()
		if err := os.Symlink(target, p); err != nil {
			t.Skipf("symbolic links: %v", err)
		}
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside, lib := filepath.Join(root, "config.json"), filepath.Join(root, "lib.js")
	const config = `{"version":3,"mappings":"AAAA","token":"keep"}`
	write(outside, config)
	write(lib, "lib()\n")
	// A map named out of the build, stamped before or not.
	write(filepath.Join(dir, "a.js"), "a()\n//# debugId=0b2a1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d\n//# sourceMappingURL=../config.json\n")
	write(filepath.Join(dir, "b.js"), "b()\n//# sourceMappingURL=../config.json\n")
	// A file that links out, with a map in the build.
	link(lib, filepath.Join(dir, "c.js"))
	write(filepath.Join(dir, "c.js.map"), config)
	// A map that links out.
	write(filepath.Join(dir, "d.js"), "d()\n")
	link(outside, filepath.Join(dir, "d.js.map"))

	files, err := Inject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || mustRead(t, outside) != config || mustRead(t, lib) != "lib()\n" {
		t.Fatalf("injected %v; outside the build: %q, %q", files, mustRead(t, outside), mustRead(t, lib))
	}
}

// A build's precompressed copies follow the stamped file: servers such as
// SvelteKit's adapter-node send them in its place.
func TestInjectRefreshesCompressedCopies(t *testing.T) {
	dir := t.TempDir()
	app := writeBuild(t, dir, "app.js", `function a(b){if(!b)throw new Error("missing")}`, 0, 14)
	old, _ := os.ReadFile(app)
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(old)
	_ = w.Close()
	for name, body := range map[string][]byte{"app.js.gz": gz.Bytes(), "app.js.br": []byte("stale brotli")} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Inject(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("inject = %+v, %v", files, err)
	}
	if len(files[0].Removed) != 1 || filepath.Base(files[0].Removed[0]) != "app.js.br" {
		t.Errorf("removed = %v", files[0].Removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.js.br")); !os.IsNotExist(err) {
		t.Error("the out-of-date brotli copy is still there")
	}
	zipped, err := os.ReadFile(filepath.Join(dir, "app.js.gz"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		t.Fatal(err)
	}
	unzipped, _ := io.ReadAll(r)
	if stamped := mustRead(t, app); string(unzipped) != stamped || !strings.Contains(stamped, "debugId=") {
		t.Errorf("the gzip copy isn't the stamped file:\n%.200s", unzipped)
	}
}

// A build whose bundler wrote debug ids (esbuild in Angular's builder,
// Rollup's sourcemapDebugIds) gets the snippet that reports them at run
// time, with the bundler's id; the map's columns move with it.
func TestInjectAddsTheSnippetToBundlerDebugIDs(t *testing.T) {
	dir := t.TempDir()
	const bundlerID = "1998f4e9-a4f6-5bcd-8f5d-4fd90f62faf8"
	app := writeBuild(t, dir, "main.js", `function a(b){if(!b)throw new Error("missing")}`, 0, 14)
	src := strings.Replace(mustRead(t, app), "//# sourceMappingURL=", "//# debugId="+bundlerID+"\n//# sourceMappingURL=", 1)
	if err := os.WriteFile(app, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Inject(dir)
	if err != nil || len(files) != 1 || files[0].Already || files[0].DebugID.String() != bundlerID {
		t.Fatalf("inject = %+v, %v", files, err)
	}
	got := mustRead(t, app)
	if !strings.Contains(got, `g._fixwireDebugIds[s]="`+bundlerID+`"`) || strings.Count(got, "//# debugId=") != 1 {
		t.Fatalf("stamped:\n%s", got)
	}
	shift := len(strings.Split(got, "\n")[0]) - len(`function a(b){if(!b)throw new Error("missing")}`)
	if s, l, c := origin(t, app, 1, 14+shift); s != "src/cart.js" || l != 2 || c != 2 {
		t.Errorf("moved code maps to %s:%d:%d", s, l, c)
	}
	again, err := Inject(dir)
	if err != nil || len(again) != 1 || !again[0].Already {
		t.Fatalf("again = %+v, %v", again, err)
	}
}
