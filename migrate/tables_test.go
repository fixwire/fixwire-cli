package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The SDKs' sources, next to the CLI in fixwire/fixwire; the CLI's own
// repository doesn't have them, so these tests skip there.
var sdks = filepath.Join("..", "..", "sdks")

func readSDK(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sdks, path))
	if os.IsNotExist(err) {
		t.Skip("the SDKs aren't next to the CLI here")
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var (
	exportDecl  = regexp.MustCompile(`(?m)^export (?:declare )?(?:async )?(?:function\*?|const|let|class|interface|type|enum) ([A-Za-z0-9_$]+)`)
	exportBlock = regexp.MustCompile(`(?s)export (?:type )?\{([^}]*)\}`)
	exportStar  = regexp.MustCompile(`(?m)^export (?:type )?\* from "([^"]+)"`)
)

// jsNames are what a module of a JavaScript package exports, following its
// `export * from` re-exports.
func jsNames(t *testing.T, pkg, file string, seen map[string]bool) map[string]bool {
	key := pkg + "/" + file
	if seen[key] {
		return nil
	}
	seen[key] = true
	src := readSDK(t, filepath.Join("js", "packages", pkg, "src", file))
	names := map[string]bool{}
	for _, m := range exportDecl.FindAllStringSubmatch(src, -1) {
		names[m[1]] = true
	}
	for _, m := range exportBlock.FindAllStringSubmatch(src, -1) {
		for _, item := range strings.Split(m[1], ",") {
			f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(item), "type "))
			if len(f) > 0 {
				names[f[len(f)-1]] = true
			}
		}
	}
	for _, m := range exportStar.FindAllStringSubmatch(src, -1) {
		var more map[string]bool
		if strings.HasPrefix(m[1], "@fixwire/") {
			more = jsNames(t, strings.TrimPrefix(m[1], "@fixwire/"), "index.ts", seen)
		} else {
			more = jsNames(t, pkg, strings.TrimPrefix(m[1], "./"), seen)
		}
		for n := range more {
			names[n] = true
		}
	}
	return names
}

func TestJSNamesAreFixwires(t *testing.T) {
	for pkg, names := range jsExports {
		exported := jsNames(t, strings.TrimPrefix(pkg, "@fixwire/"), "index.ts", map[string]bool{})
		for n := range names {
			if !exported[n] {
				t.Errorf("%s doesn't export %s", pkg, n)
			}
		}
	}
	for _, to := range jsRenamed {
		if !jsExports["@fixwire/edge"][to] && !jsExports["@fixwire/node"][to] {
			t.Errorf("%s is no Fixwire name", to)
		}
	}
}

var tsField = regexp.MustCompile(`(?m)^  ([A-Za-z_]+)\??:`)

// interfaceFields are the fields of an exported interface in a file.
func interfaceFields(t *testing.T, file, name string) []string {
	src := readSDK(t, file)
	i := strings.Index(src, "export interface "+name)
	if i < 0 {
		t.Fatalf("%s has no interface %s", file, name)
	}
	body := src[i:]
	body = body[:strings.Index(body, "\n}")]
	var out []string
	for _, m := range tsField.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestJSOptionsAreFixwires(t *testing.T) {
	client := interfaceFields(t, "js/packages/core/src/client.ts", "ClientOptions")
	for pkg, extra := range map[string][2]string{
		"@fixwire/browser": {"js/packages/browser/src/index.ts", "BrowserOptions"},
		"@fixwire/node":    {"js/packages/node/src/index.ts", "NodeOptions"},
		"@fixwire/edge":    {"js/packages/edge/src/index.ts", "EdgeOptions"},
	} {
		want := append(append([]string{}, client...), interfaceFields(t, extra[0], extra[1])...)
		sameNames(t, pkg+" options", jsOptions[pkg], want)
	}
	sameNames(t, "@fixwire/core options", jsOptions["@fixwire/core"], client)
}

func TestPythonNamesAreFixwires(t *testing.T) {
	init := readSDK(t, "python/src/fixwire/__init__.py")
	all := regexp.MustCompile(`(?s)__all__ = \[(.*?)\]`).FindStringSubmatch(init)
	if all == nil {
		t.Fatal("no __all__")
	}
	var names []string
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(all[1], -1) {
		names = append(names, m[1])
	}
	exports := map[string]bool{}
	for n := range pyExports {
		if n != "integrations" { // a subpackage, imported by name
			exports[n] = true
		}
	}
	sameNames(t, "fixwire's names", exports, names)

	types := readSDK(t, "python/src/fixwire/types.py")
	i := strings.Index(types, "class ClientOptions(")
	if i < 0 {
		t.Fatal("no ClientOptions")
	}
	body := types[i:]
	if end := strings.Index(body[1:], "\nclass "); end > 0 {
		body = body[:end+1]
	}
	var options []string
	for _, m := range regexp.MustCompile(`(?m)^    ([a-z_]+):`).FindAllStringSubmatch(body, -1) {
		options = append(options, m[1])
	}
	opts := map[string]bool{}
	for n := range pyOptions {
		if n != "dsn" { // init's own argument
			opts[n] = true
		}
	}
	sameNames(t, "fixwire.init's options", opts, options)

	for name, integ := range pyIntegrations {
		if integ.module == "" {
			continue
		}
		src := readSDK(t, filepath.Join("python", "src", "fixwire", "integrations", integ.module+".py"))
		if !strings.Contains(src, "Integration") {
			t.Errorf("fixwire.integrations.%s (for %s) has no integration", integ.module, name)
		}
	}
}

func TestVersionsAreFixwires(t *testing.T) {
	if pkg := readSDK(t, "js/packages/core/package.json"); !strings.Contains(pkg, `"version": "`+jsVersion+`"`) {
		t.Errorf("@fixwire/core isn't %s", jsVersion)
	}
	if v := readSDK(t, "python/src/fixwire/_version.py"); !strings.Contains(v, `"`+pyVersion+`"`) {
		t.Errorf("fixwire (Python) isn't %s", pyVersion)
	}
}

func sameNames(t *testing.T, what string, have map[string]bool, want []string) {
	t.Helper()
	w := map[string]bool{}
	for _, n := range want {
		w[n] = true
	}
	var missing, extra []string
	for n := range w {
		if !have[n] {
			missing = append(missing, n)
		}
	}
	for n := range have {
		if !w[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("%s: the tables lack %v and have %v the SDK doesn't", what, missing, extra)
	}
}
