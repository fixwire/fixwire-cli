package migrate

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*/want with what migrate does now")

// TestGolden migrates each project in testdata/<name>/in and compares the
// result, and the report, with testdata/<name>/want. A second run must
// change nothing.
func TestGolden(t *testing.T) {
	for _, name := range []string{"js", "frameworks", "python"} {
		t.Run(name, func(t *testing.T) {
			work := t.TempDir()
			copyTree(t, filepath.Join("testdata", name, "in"), work)
			rep, err := Run(work, true)
			if err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			if err := Print(&report, rep); err != nil {
				t.Fatal(err)
			}
			want := filepath.Join("testdata", name, "want")
			if *update {
				if err := os.RemoveAll(want); err != nil {
					t.Fatal(err)
				}
				copyTree(t, work, want)
				if err := os.WriteFile(filepath.Join(want, "report.txt"), report.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got := readTree(t, work)
			wanted := readTree(t, want)
			if r := wanted["report.txt"]; r != report.String() {
				t.Errorf("report:\n%s\nwant:\n%s", report.String(), r)
			}
			delete(wanted, "report.txt")
			for path, w := range wanted {
				if g, ok := got[path]; !ok {
					t.Errorf("%s is missing", path)
				} else if g != w {
					t.Errorf("%s:\n%s\nwant:\n%s", path, g, w)
				}
			}
			for path := range got {
				if _, ok := wanted[path]; !ok {
					t.Errorf("%s is unexpected", path)
				}
			}

			again, err := Run(work, false)
			if err != nil {
				t.Fatal(err)
			}
			if n := again.Edits(); n != 0 {
				var b bytes.Buffer
				_ = Print(&b, again)
				t.Errorf("a second run changes %d places:\n%s", n, b.String())
			}
		})
	}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	for path, content := range readTree(t, from) {
		dst := filepath.Join(to, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
