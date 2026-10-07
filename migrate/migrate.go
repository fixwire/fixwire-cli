// Package migrate moves a JavaScript, TypeScript or Python project from
// another error tracker's SDK to Fixwire's: it rewrites the imports, the
// calls whose names differ, the options and integrations Fixwire doesn't
// have, and the dependencies, and lists what's left to do by hand.
//
// That other SDK is Sentry's, which this package names only to find its
// code. Sentry is a trademark of Functional Software, Inc.; Fixwire isn't
// affiliated with it or endorsed by it.
package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxFile is the largest file read: source files are much smaller, and a
// bundle or a fixture this big isn't the project's code.
const maxFile = 2 << 20

// skipDirs are never walked: dependencies, builds, caches, environments.
var skipDirs = set("node_modules", ".git", "dist", "build", "out", ".next", ".nuxt", ".output", ".svelte-kit",
	".turbo", ".venv", "venv", "env", "__pycache__", ".tox", ".mypy_cache", ".pytest_cache", "vendor", "coverage",
	"site-packages")

type step struct {
	line int
	text string
}

// File is what migrating one file does.
type File struct {
	Path  string // relative to the project
	Hunks []Hunk
	Steps []Step
}

// Step is something left to do by hand.
type Step struct {
	Line int
	Text string
}

// Report is what migrating a project does.
type Report struct {
	Files   []File
	Written bool
}

// Edits counts the changed lines' hunks.
func (r Report) Edits() int {
	n := 0
	for _, f := range r.Files {
		n += len(f.Hunks)
	}
	return n
}

// Steps counts what's left to do.
func (r Report) Steps() int {
	n := 0
	for _, f := range r.Files {
		n += len(f.Steps)
	}
	return n
}

// Run migrates the project in dir, writing the files when write is set.
func Run(dir string, write bool) (Report, error) {
	var rep Report
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != ".github") {
				return filepath.SkipDir
			}
			return nil
		}
		migrate := migrator(d.Name())
		if migrate == nil || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFile {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		edits, steps := migrate(path, src)
		if len(edits) == 0 && len(steps) == 0 {
			return nil
		}
		out, hunks := apply(src, edits)
		rel, _ := filepath.Rel(dir, path)
		f := File{Path: filepath.ToSlash(rel), Hunks: hunks}
		sort.SliceStable(steps, func(a, b int) bool { return steps[a].line < steps[b].line })
		seen := map[step]bool{}
		for _, s := range steps {
			if !seen[s] {
				seen[s] = true
				f.Steps = append(f.Steps, Step{Line: s.line, Text: s.text})
			}
		}
		rep.Files = append(rep.Files, f)
		if write && !bytes.Equal(out, src) {
			if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
				return err
			}
		}
		return nil
	})
	rep.Written = write
	return rep, err
}

// migrator is what migrates a file, by its name; nil for files it leaves.
func migrator(name string) func(path string, src []byte) ([]edit, []step) {
	lower := strings.ToLower(name)
	switch {
	case lower == "package.json":
		return func(_ string, src []byte) ([]edit, []step) { return migratePackageJSON(src) }
	case lower == "pyproject.toml" || lower == "pipfile":
		return func(_ string, src []byte) ([]edit, []step) { return migrateTOML(src) }
	case lower == "setup.cfg" || strings.HasSuffix(lower, ".txt") && (strings.Contains(lower, "requirements") || strings.Contains(lower, "constraints")) ||
		strings.HasSuffix(lower, ".in") && strings.Contains(lower, "requirements"):
		return func(_ string, src []byte) ([]edit, []step) { return migrateRequirements(src) }
	case lower == ".env" || strings.HasPrefix(lower, ".env."):
		return migrateEnv
	}
	switch filepath.Ext(lower) {
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		if strings.HasSuffix(lower, ".d.ts") || strings.HasSuffix(lower, ".min.js") {
			return nil
		}
		return func(_ string, src []byte) ([]edit, []step) { return migrateJS(src) }
	case ".py":
		return migratePython
	}
	return nil
}

// migrateEnv reports the incumbent's variables in .env files: their values
// (a DSN, a token) are the other service's, so a person sets Fixwire's.
func migrateEnv(_ string, src []byte) ([]edit, []step) {
	var steps []step
	ln := newLines(src)
	for i := 0; i < len(src); {
		end := lineEnd(src, i)
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(src[i:end])), "export "))
		if name, _, ok := strings.Cut(line, "="); ok && strings.HasPrefix(name, "SENTRY_") {
			steps = append(steps, step{line: ln.of(i), text: fmt.Sprintf("%s: Fixwire reads %s; set it to your Fixwire project's value", name, envName(name))})
		}
		i = end + 1
	}
	return nil, steps
}

// Print writes the report: each file's changed lines, before and after,
// then what's left to do.
func Print(w io.Writer, rep Report) error {
	ew := &errWriter{w: w}
	if len(rep.Files) == 0 {
		ew.printf("Nothing to migrate: no imports of the other SDK, its packages or its settings were found.\n")
		return ew.err
	}
	edits, steps := rep.Edits(), rep.Steps()
	for _, f := range rep.Files {
		if len(f.Hunks) == 0 {
			continue
		}
		ew.printf("%s\n", f.Path)
		for _, h := range f.Hunks {
			for n, l := range strings.Split(h.Old, "\n") {
				ew.printf("  %4d - %s\n", h.Line+n, l)
			}
			if h.New == "" {
				continue
			}
			for _, l := range strings.Split(h.New, "\n") {
				ew.printf("       + %s\n", l)
			}
		}
	}
	if steps > 0 {
		ew.printf("\nLeft to do:\n")
		for _, f := range rep.Files {
			for _, s := range f.Steps {
				ew.printf("  %s:%d: %s\n", f.Path, s.Line, s.Text)
			}
		}
	}
	verb := "would change"
	if rep.Written {
		verb = "changed"
	}
	ew.printf("\n%s %d places in %d files; %d steps left.", verb, edits, len(rep.Files), steps)
	if !rep.Written && edits > 0 {
		ew.printf(" Run again with --write to make the changes.")
	}
	ew.printf("\n")
	return ew.err
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, args...)
	}
}

// ErrNotDir is returned for a path that isn't a directory.
var ErrNotDir = errors.New("migrate: name the project's directory")

func sortStrings(s []string) { sort.Strings(s) }
