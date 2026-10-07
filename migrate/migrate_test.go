package migrate

import (
	"strings"
	"testing"
)

func jsTexts(src string) []string {
	var out []string
	for _, t := range lexJS([]byte(src)) {
		out = append(out, src[t.start:t.end])
	}
	return out
}

func TestLexJS(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string // tokens joined by spaces
	}{
		{`a / b / c`, `a / b / c`},
		{`x = /"(a)"/g.test(s)`, `x = /"(a)"/g . test ( s )`},
		{`return /a\/b[/]/.source`, `return /a\/b[/]/ . source`},
		{"const s = `a ${ {b: \"}\"}.b } c`; d", "const s = `a ${ {b: \"}\"}.b } c` ; d"},
		{"// \"@x\" in a comment\n'@y' /* \"@z\" */", `'@y'`},
		{`<A.B x={1}>t</A.B>`, `< A . B x = { 1 } > t < / A . B >`},
		{"#!/usr/bin/env node\nf()", `f ( )`},
		{`f(a) / 2`, `f ( a ) / 2`},
		{`"unterminated` + "\n" + `next`, `"unterminated next`},
	} {
		if got := strings.Join(jsTexts(tc.src), " "); got != tc.want {
			t.Errorf("lexJS(%q)\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
}

func TestLexPython(t *testing.T) {
	src := "x = rb'\\''  # c\ny = f\"\"\"a\n\"b\"\n\"\"\"\nz = (1,\n  2); w = 3\n"
	var got []string
	for _, tk := range lexPython([]byte(src)) {
		if tk.kind == pyNewline {
			got = append(got, "NL")
			continue
		}
		got = append(got, src[tk.start:tk.end])
	}
	want := "x = rb'\\'' NL y = f\"\"\"a\n\"b\"\n\"\"\" NL z = ( 1 , 2 ) NL w = 3 NL"
	if strings.Join(got, " ") != want {
		t.Errorf("lexPython\n got %q\nwant %q", strings.Join(got, " "), want)
	}
}

// Code the other SDK's names appear in, but only in comments and strings,
// is left as it is.
func TestCommentsAndStringsStay(t *testing.T) {
	js := "// import * as S from \"@sentry/node\"\nconst msg = \"see @sentry/node docs\";\nconst t = `${'@sentry/node'}`;\n"
	if edits, _ := migrateJS([]byte(js)); len(edits) != 0 {
		t.Errorf("JavaScript edits %v", edits)
	}
	py := "# import sentry_sdk\nmsg = 'sentry_sdk.init'\ndoc = \"\"\"\nimport sentry_sdk\n\"\"\"\n"
	if edits, _ := migratePython("app.py", []byte(py)); len(edits) != 0 {
		t.Errorf("Python edits %v", edits)
	}
}

func TestRemoveOptions(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{
			`import * as S from "@sentry/node"; S.init({ dsn: "", tunnel: "/t", debug: true });`,
			`import * as S from "@fixwire/node"; S.init({ dsn: "", debug: true });`,
		},
		{
			`import * as S from "@sentry/node"; S.init({ dsn: "", tunnel: "/t", profilesSampleRate: 1 });`,
			`import * as S from "@fixwire/node"; S.init({ dsn: "" });`,
		},
		{
			`import * as S from "@sentry/node"; S.init({ profilesSampleRate: 1, tunnel: "/t", dsn: "" });`,
			`import * as S from "@fixwire/node"; S.init({ dsn: "" });`,
		},
		{
			"import * as S from \"@sentry/node\";\nS.init({\n  dsn: \"\",\n  tunnel: \"/t\"\n});\n",
			"import * as S from \"@fixwire/node\";\nS.init({\n  dsn: \"\"\n});\n",
		},
		{
			"import * as S from \"@sentry/node\";\nS.init({\n  tunnel: \"/t\",\n  dsn: \"\",\n});\n",
			"import * as S from \"@fixwire/node\";\nS.init({\n  dsn: \"\",\n});\n",
		},
	} {
		edits, _ := migrateJS([]byte(tc.in))
		got, _ := apply([]byte(tc.in), edits)
		if string(got) != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.in, got, tc.want)
		}
	}
}

func TestRequirement(t *testing.T) {
	for in, want := range map[string]string{
		"sentry-sdk":                              "fixwire>=0.1.2",
		"sentry_sdk==2.39.0":                      "fixwire>=0.1.2",
		"Sentry-SDK[django,celery]>=2":            "fixwire>=0.1.2",
		"sentry-sdk[fastapi] ~= 2.0":              "fixwire[async]>=0.1.2",
		"sentry-sdk>=2; python_version >= '3.10'": "fixwire>=0.1.2; python_version >= '3.10'",
		"sentry-sdk-extra":                        "",
		"requests>=2":                             "",
	} {
		got, ok := requirement(in)
		if want == "" && ok || want != "" && got != want {
			t.Errorf("requirement(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

// Nothing a migration reads can make it slow or crash: deep nesting, long
// lines and cut-off files are linear work.
func TestHostileInput(t *testing.T) {
	big := strings.Repeat("(", 50_000) + `import * as S from "@sentry/node"; S.init({ tunnel: "` + strings.Repeat("x", 100_000)
	_, _ = migrateJS([]byte(big))
	_, _ = migratePython("x.py", []byte("import sentry_sdk\nsentry_sdk.init("+strings.Repeat("[", 50_000)))
	_, _ = migratePackageJSON([]byte(`{"dependencies": {"@sentry/node": "`))
	_, _ = migrateTOML([]byte("sentry-sdk = {"))
	_ = lexJS([]byte(strings.Repeat("`${", 200_000)))
}
