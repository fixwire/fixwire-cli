// Package sourcemaps prepares and uploads a build's source maps: inject
// stamps every JavaScript file and its map with a debug id, and upload
// sends them to Fixwire as an artifact bundle, over the standard chunked
// upload protocol.
package sourcemaps

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/google/uuid"
)

// jsExtensions are the files a build emits that maps can cover.
var jsExtensions = []string{".js", ".mjs", ".cjs"}

var (
	debugIDComment = regexp.MustCompile(`(?m)^//# debugId=([0-9a-fA-F-]{36})\s*$`)
	mapComment     = regexp.MustCompile(`(?m)^//[#@] sourceMappingURL=(\S+)\s*$`)
)

// snippet records, when the script runs, which debug id its stack belongs
// to. Fixwire's SDKs read the _fixwireDebugIds global to find each frame's
// debug id (protocol/PROTOCOL.md §10).
const snippet = `;(function(){try{var g=typeof globalThis!=="undefined"?globalThis:typeof window!=="undefined"?window:` +
	`typeof self!=="undefined"?self:{};var s=new g.Error().stack;if(s){g._fixwireDebugIds=g._fixwireDebugIds||{};` +
	`g._fixwireDebugIds[s]="%s"}}catch(e){}})();`

// Injected is a file inject looked at.
type Injected struct {
	File    string
	Map     string
	DebugID uuid.UUID
	// Already is true for a file stamped by an earlier run.
	Already bool
	// Removed are precompressed copies of the file that went out of date.
	Removed []string
}

// Inject stamps every JavaScript file under dir that has a source map
// (named by its sourceMappingURL comment, or <file>.map next to it) with a
// debug id derived from its content: a snippet that registers it, a
// //# debugId= comment, and debug_id in the map, whose mappings are
// shifted to keep pointing at the same code. Running it again changes
// nothing. Only regular files inside dir are rewritten: a symbolic link or
// a sourceMappingURL leading out of the build is left alone.
func Inject(dir string) ([]Injected, error) {
	var out []Injected
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !hasJSExtension(p) {
			return err
		}
		inj, ok, err := injectFile(dir, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if ok {
			out = append(out, inj)
		}
		return nil
	})
	return out, err
}

func hasJSExtension(p string) bool {
	for _, ext := range jsExtensions {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// mapFor finds a file's source map, or "" when it has none on disk: a
// regular file within root.
func mapFor(root, file string, src []byte) string {
	p := file + ".map"
	if m := mapComment.FindAllSubmatch(src, -1); len(m) > 0 {
		ref := string(m[len(m)-1][1])
		if strings.HasPrefix(ref, "data:") || strings.Contains(ref, "://") {
			return "" // inline or remote maps aren't uploaded
		}
		if i := strings.IndexAny(ref, "?#"); i >= 0 {
			ref = ref[:i]
		}
		p = filepath.Join(filepath.Dir(file), filepath.FromSlash(ref))
	}
	if rel, err := filepath.Rel(root, p); err != nil || !filepath.IsLocal(rel) {
		return ""
	}
	if info, err := os.Lstat(p); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return p
}

func injectFile(root, file string) (Injected, bool, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return Injected{}, false, err
	}
	mapPath := mapFor(root, file, src)
	if mapPath == "" {
		return Injected{}, false, nil
	}
	if m := debugIDComment.FindSubmatch(src); m != nil {
		id, err := uuid.Parse(string(m[1]))
		if err != nil {
			return Injected{}, false, err
		}
		// Stamped before; make sure the map carries the id too.
		if err := stampMap(mapPath, id, -1, 0, 0); err != nil {
			return Injected{}, false, err
		}
		return Injected{File: file, Map: mapPath, DebugID: id, Already: true}, true, nil
	}

	id := debugIDFor(src)
	code := fmt.Sprintf(snippet, id)
	at := insertionPoint(src)
	line, col := position(src, at)
	if err := stampMap(mapPath, id, line, col, len(code)); err != nil {
		return Injected{}, false, err
	}

	var b bytes.Buffer
	b.Grow(len(src) + len(code) + 64)
	b.Write(src[:at])
	b.WriteString(code)
	rest := src[at:]
	// The comment goes before the sourceMappingURL comment, which stays last.
	if loc := mapComment.FindAllIndex(rest, -1); len(loc) > 0 {
		last := loc[len(loc)-1][0]
		b.Write(rest[:last])
		fmt.Fprintf(&b, "//# debugId=%s\n", id)
		b.Write(rest[last:])
	} else {
		b.Write(rest)
		if len(rest) > 0 && rest[len(rest)-1] != '\n' {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "//# debugId=%s\n", id)
	}
	info, err := os.Stat(file)
	if err != nil {
		return Injected{}, false, err
	}
	if err := os.WriteFile(file, b.Bytes(), info.Mode().Perm()); err != nil {
		return Injected{}, false, err
	}
	removed, err := refreshCompressed(file, b.Bytes(), info.Mode().Perm())
	if err != nil {
		return Injected{}, false, err
	}
	return Injected{File: file, Map: mapPath, DebugID: id, Removed: removed}, true, nil
}

// refreshCompressed keeps the precompressed copies a build made of a file
// (SvelteKit's adapter-node, a CDN's preset) in step with it, as servers
// send them in its place: <file>.gz is rewritten, and <file>.br removed,
// as Go has no brotli encoder of its own; servers fall back to the gzip
// copy or the file. Only regular files are touched. Returns what it removed.
func refreshCompressed(file string, data []byte, perm fs.FileMode) ([]string, error) {
	var removed []string
	if info, err := os.Lstat(file + ".gz"); err == nil && info.Mode().IsRegular() {
		var b bytes.Buffer
		w, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(file+".gz", b.Bytes(), perm); err != nil {
			return nil, err
		}
	}
	if info, err := os.Lstat(file + ".br"); err == nil && info.Mode().IsRegular() {
		if err := os.Remove(file + ".br"); err != nil {
			return nil, err
		}
		removed = append(removed, file+".br")
	}
	return removed, nil
}

// debugIDFor derives a stable debug id (a version 4 UUID layout) from a
// file's content, so rebuilding the same code gives the same id.
func debugIDFor(src []byte) uuid.UUID {
	sum := sha256.Sum256(src)
	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// stampMap writes the debug id into a map and, when line >= 0, shifts its
// generated columns for the snippet inserted at (line, col). Other fields
// are kept as they are.
func stampMap(mapPath string, id uuid.UUID, line, col, delta int) error {
	raw, err := os.ReadFile(mapPath)
	if err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("source map %s: %w", mapPath, err)
	}
	if line >= 0 {
		if _, indexed := m["sections"]; indexed {
			return errors.New("indexed source maps (sections) are not supported yet")
		}
		var mappings string
		if err := json.Unmarshal(m["mappings"], &mappings); err != nil {
			return fmt.Errorf("source map %s: no mappings", mapPath)
		}
		if mappings, err = shiftColumns(mappings, line, col, delta); err != nil {
			return fmt.Errorf("source map %s: %w", mapPath, err)
		}
		m["mappings"], _ = json.Marshal(mappings)
	}
	q, _ := json.Marshal(id.String())
	m["debug_id"], m["debugId"] = q, q
	info, err := os.Stat(mapPath)
	if err != nil {
		return err
	}
	out, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(mapPath, out, info.Mode().Perm())
}

// insertionPoint is where the snippet goes: after a hashbang line and the
// directive prologue ("use strict"; …), which must stay first, else at the
// very start.
func insertionPoint(src []byte) int {
	i := 0
	if bytes.HasPrefix(src, []byte("#!")) {
		if nl := bytes.IndexByte(src, '\n'); nl >= 0 {
			i = nl + 1
		} else {
			return len(src)
		}
	}
	for {
		j, _ := skipBlank(src, i)
		if j >= len(src) || (src[j] != '"' && src[j] != '\'') {
			return i
		}
		end := scanString(src, j)
		if end < 0 {
			return i
		}
		k, newline := skipBlank(src, end)
		switch {
		case k < len(src) && src[k] == ';':
			i = k + 1
		case newline || k >= len(src) || src[k] == '}':
			i = end // ends by automatic semicolon insertion
		default:
			return i // a string starting an expression, not a directive
		}
	}
}

// skipBlank skips whitespace and comments, reporting whether it crossed a
// line break.
func skipBlank(src []byte, i int) (int, bool) {
	newline := false
	for i < len(src) {
		switch {
		case src[i] == '\n':
			newline = true
			i++
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\r':
			i++
		case bytes.HasPrefix(src[i:], []byte("//")):
			nl := bytes.IndexByte(src[i:], '\n')
			if nl < 0 {
				return len(src), newline
			}
			i += nl
		case bytes.HasPrefix(src[i:], []byte("/*")):
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return len(src), newline
			}
			if bytes.IndexByte(src[i:i+2+end], '\n') >= 0 {
				newline = true
			}
			i += end + 4
		default:
			return i, newline
		}
	}
	return i, newline
}

// scanString returns the index after a quoted string starting at i, or -1.
func scanString(src []byte, i int) int {
	quote := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '\n':
			return -1
		case quote:
			return j + 1
		}
	}
	return -1
}

// position converts a byte offset to a 0-based line and a column in UTF-16
// code units, the unit of source map columns.
func position(src []byte, at int) (int, int) {
	line := bytes.Count(src[:at], []byte("\n"))
	start := bytes.LastIndexByte(src[:at], '\n') + 1
	return line, len(utf16.Encode([]rune(string(src[start:at]))))
}

// urlPath joins a URL prefix and a relative file path with slashes.
func urlPath(prefix, rel string) string {
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(prefix, "/") {
		return prefix + rel
	}
	return path.Join(prefix, rel)
}
