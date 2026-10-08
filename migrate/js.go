package migrate

import (
	"fmt"
	"sort"
	"strings"
)

// jsFile rewrites one JavaScript or TypeScript file.
type jsFile struct {
	src   []byte
	toks  []jsToken
	spans []span
	edits []edit
	steps []step
	lines lines

	// What the file imports from the incumbent's packages: namespace
	// aliases and named bindings, with the Fixwire package each now comes
	// from, and bindings from dropped packages.
	namespaces map[string]jsTarget
	named      map[string]jsTarget // local name → target
	dropped    map[string]string   // local name → why it's gone
	reactNeed  map[string]bool     // React names the namespace alias used
	reactAfter int                 // where to add @fixwire/react's import (-1: nowhere)
	cjs        bool                // the namespace came from require()
	noted      map[string]bool     // names whose jsNotes step was given
}

func migrateJS(src []byte) ([]edit, []step) {
	f := &jsFile{
		src: src, toks: lexJS(src), lines: newLines(src),
		namespaces: map[string]jsTarget{}, named: map[string]jsTarget{}, dropped: map[string]string{},
		reactNeed: map[string]bool{}, reactAfter: -1, noted: map[string]bool{},
	}
	f.spans = make([]span, len(f.toks))
	for i, t := range f.toks {
		f.spans[i] = span{t.start, t.end}
	}
	f.imports()
	if len(f.namespaces) == 0 && len(f.named) == 0 && len(f.dropped) == 0 {
		f.environment()
		return f.edits, f.steps
	}
	f.members()
	f.objects()
	f.environment()
	f.reactImport()
	return f.edits, f.steps
}

func (f *jsFile) text(i int) string { return string(f.src[f.toks[i].start:f.toks[i].end]) }

func (f *jsFile) is(i int, kind jsKind, s string) bool {
	return i >= 0 && i < len(f.toks) && f.toks[i].kind == kind && f.text(i) == s
}

func (f *jsFile) punct(i int, c byte) bool {
	return i >= 0 && i < len(f.toks) && f.toks[i].kind == jsPunct && f.src[f.toks[i].start] == c
}

func (f *jsFile) step(i int, format string, args ...any) {
	off := 0
	if i >= 0 && i < len(f.toks) {
		off = f.toks[i].start
	}
	f.steps = append(f.steps, step{line: f.lines.of(off), text: fmt.Sprintf(format, args...)})
}

// note gives name's jsNotes step, once per file.
func (f *jsFile) note(i int, name string) {
	if n, ok := jsNotes[name]; ok && !f.noted[name] {
		f.noted[name] = true
		f.step(i, "%s", n)
	}
}

// missing reports a name the target package doesn't have (used as `used`),
// with what replaces it when jsMissing knows.
func (f *jsFile) missing(i int, used, name, pkg string) {
	advice := "replace it, or drop it"
	if a, ok := jsMissing[name]; ok {
		advice = a
	}
	f.step(i, "%s isn't in %s: %s", used, pkg, advice)
}

func (f *jsFile) replace(i int, text string) {
	f.edits = append(f.edits, edit{start: f.toks[i].start, end: f.toks[i].end, text: text})
}

// stringValue is a '…' or "…" token's value (no escapes in package names).
func (f *jsFile) stringValue(i int) string {
	s := f.text(i)
	if len(s) < 2 {
		return ""
	}
	return s[1 : len(s)-1]
}

func (f *jsFile) quoted(i int, value string) string {
	q := f.text(i)[0]
	return string(q) + value + string(q)
}

// incumbent says whether a module specifier is one of the incumbent's
// packages (a deep import counts as its package).
func incumbentPackage(spec string) (string, bool) {
	if !strings.HasPrefix(spec, "@sentry/") && !strings.HasPrefix(spec, "@sentry-internal/") {
		return "", false
	}
	parts := strings.SplitN(spec, "/", 3)
	return parts[0] + "/" + parts[1], true
}

// imports finds the module specifiers naming the incumbent's packages and
// rewrites them with what they bind.
func (f *jsFile) imports() {
	for i, t := range f.toks {
		if t.kind != jsString {
			continue
		}
		spec := f.stringValue(i)
		pkg, ok := incumbentPackage(spec)
		if !ok {
			continue
		}
		switch {
		case f.is(i-1, jsIdent, "from"):
			f.fromImport(i, pkg, spec)
		case f.punct(i-1, '(') && f.punct(i+1, ')') && f.is(i-2, jsIdent, "require"):
			f.require(i, pkg, spec)
		default:
			// import "pkg", import("pkg"), a mock or a bundler's list.
			f.plainSpecifier(i, pkg, spec)
		}
	}
}

func (f *jsFile) plainSpecifier(i int, pkg, spec string) {
	if t, ok := jsPackages[pkg]; ok && spec == pkg && !t.react {
		f.replace(i, f.quoted(i, t.pkg))
		return
	}
	if why, ok := jsDropped[pkg]; ok {
		f.step(i, "%s: %s; remove it", spec, why)
		return
	}
	if how, ok := jsByHand[pkg]; ok {
		f.step(i, "%s: %s", spec, how)
		return
	}
	f.step(i, "%s: move this by hand to the matching @fixwire package", spec)
}

// fromImport handles `import … from "pkg"` and `export … from "pkg"`.
func (f *jsFile) fromImport(i int, pkg, spec string) {
	// The statement's first token: import or export, at the clause's start.
	start := -1
	for j := i - 2; j >= 0 && j >= i-200; j-- {
		if f.toks[j].kind == jsIdent && (f.text(j) == "import" || f.text(j) == "export") {
			start = j
			break
		}
		if f.punct(j, ';') {
			break
		}
	}
	if start < 0 {
		f.plainSpecifier(i, pkg, spec)
		return
	}
	end := i
	if f.punct(i+1, ';') {
		end = i + 1
	}
	if why, ok := jsDropped[pkg]; ok {
		for _, name := range f.bindings(start+1, i-2) {
			f.dropped[name.local] = why
		}
		f.removeStatement(start, end)
		f.step(start, "removed the import of %s: %s", spec, why)
		return
	}
	target, ok := jsPackages[pkg]
	if !ok || spec != pkg {
		f.plainSpecifier(i, pkg, spec)
		return
	}
	isExport := f.text(start) == "export"
	clause := start + 1
	typeOnly := false
	if f.is(clause, jsIdent, "type") && !f.punct(clause+1, ',') && !f.is(clause+1, jsIdent, "from") {
		typeOnly = true
		clause++
	}
	// import * as NS from "pkg"
	if f.punct(clause, '*') && f.is(clause+1, jsIdent, "as") && f.toks[clause+2].kind == jsIdent && clause+3 == i-1 {
		if !isExport {
			f.namespaces[f.text(clause+2)] = target
			if target.react {
				f.reactAfter = end
			}
		}
		f.replace(i, f.quoted(i, target.pkg))
		return
	}
	if !f.punct(clause, '{') {
		f.replace(i, f.quoted(i, target.pkg))
		if !target.react {
			return
		}
		f.step(start, "%s: check this import; its React parts come from @fixwire/react and the rest from @fixwire/browser", spec)
		return
	}
	names := f.bindings(clause, i-2)
	var main, react []string
	changed := false
	for _, b := range names {
		imported := b.imported
		if to, ok := jsRenamed[imported]; ok {
			imported, changed = to, true
		}
		item := imported
		if b.typeOnly {
			item = "type " + item
		}
		if imported != b.local {
			item += " as " + b.local
		}
		if target.react && jsReactExports[imported] {
			react = append(react, item)
			changed = true
		} else {
			main = append(main, item)
			if !jsExports[target.pkg][imported] && !b.typeOnly {
				f.missing(clause, b.imported, b.imported, target.pkg)
			}
			f.note(clause, imported)
		}
		if !isExport {
			t := target
			if target.react && jsReactExports[imported] {
				t = jsTarget{pkg: "@fixwire/react"}
			}
			f.named[b.local] = t
		}
	}
	if !changed {
		f.replace(i, f.quoted(i, target.pkg))
		return
	}
	// Write the statement anew: names renamed, React's split off.
	kw := f.text(start)
	if typeOnly {
		kw += " type"
	}
	semi := ""
	if end > i {
		semi = ";"
	}
	q := string(f.text(i)[0])
	var b strings.Builder
	if len(main) > 0 {
		fmt.Fprintf(&b, "%s { %s } from %s%s%s%s", kw, strings.Join(main, ", "), q, target.pkg, q, semi)
	}
	if len(react) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s { %s } from %s@fixwire/react%s%s", kw, strings.Join(react, ", "), q, q, semi)
	}
	f.edits = append(f.edits, edit{start: f.toks[start].start, end: f.toks[end].end, text: b.String()})
}

type jsBinding struct {
	imported, local string
	typeOnly        bool
}

// bindings reads `{ a, b as c, type d }` (or `a: c` in destructuring)
// between toks[from] and toks[to]; a default or namespace name binds itself.
func (f *jsFile) bindings(from, to int) []jsBinding {
	var out []jsBinding
	var cur []string
	typeOnly := false
	flush := func() {
		switch len(cur) {
		case 1:
			out = append(out, jsBinding{cur[0], cur[0], typeOnly})
		case 2:
			out = append(out, jsBinding{cur[0], cur[1], typeOnly})
		}
		cur, typeOnly = nil, false
	}
	for j := from; j <= to && j < len(f.toks); j++ {
		switch {
		case f.toks[j].kind == jsIdent:
			w := f.text(j)
			switch {
			case w == "as":
			case w == "type" && len(cur) == 0 && f.toks[j+1].kind == jsIdent && !f.is(j+1, jsIdent, "as"):
				typeOnly = true
			default:
				cur = append(cur, w)
			}
		case f.punct(j, ','), f.punct(j, '}'):
			flush()
		}
	}
	flush()
	return out
}

// require handles `const X = require("pkg")` and `const { a, b: c } =
// require("pkg")`.
func (f *jsFile) require(i int, pkg, spec string) {
	// require ( "pkg" ) at i-2..i+1; before it, `=` and the binding.
	eq := i - 3
	if why, ok := jsDropped[pkg]; ok {
		decl := f.declarationStart(eq)
		if decl >= 0 {
			for _, b := range f.bindings(decl+1, eq-1) {
				f.dropped[b.local] = why
			}
			end := i + 1
			if f.punct(end+1, ';') {
				end++
			}
			f.removeStatement(decl, end)
			f.step(decl, "removed the require of %s: %s", spec, why)
			return
		}
		f.plainSpecifier(i, pkg, spec)
		return
	}
	target, ok := jsPackages[pkg]
	if !ok || spec != pkg {
		f.plainSpecifier(i, pkg, spec)
		return
	}
	if !f.punct(eq, '=') {
		if target.react {
			f.step(i, "%s: its React parts come from @fixwire/react and the rest from @fixwire/browser", spec)
			return
		}
		f.replace(i, f.quoted(i, target.pkg))
		return
	}
	if f.toks[eq-1].kind == jsIdent && !f.punct(eq-2, '}') {
		f.namespaces[f.text(eq-1)] = target
		f.cjs = true
		if target.react {
			end := i + 1
			if f.punct(end+1, ';') {
				end++
			}
			f.reactAfter = end
		}
		f.replace(i, f.quoted(i, target.pkg))
		return
	}
	if !f.punct(eq-1, '}') {
		f.replace(i, f.quoted(i, target.pkg))
		return
	}
	open := eq - 1
	for open >= 0 && !f.punct(open, '{') {
		open--
	}
	for j := open + 1; j < eq-1; j++ {
		if f.toks[j].kind != jsIdent || f.punct(j-1, ':') {
			continue
		}
		name := f.text(j)
		local := name
		if f.punct(j+1, ':') && f.toks[j+2].kind == jsIdent {
			local = f.text(j + 2)
		}
		if to, ok := jsRenamed[name]; ok {
			if local == name {
				f.replace(j, to+": "+name)
			} else {
				f.replace(j, to)
			}
			name = to
		}
		t := target
		if target.react && jsReactExports[name] {
			f.step(j, "%s comes from @fixwire/react: require it from there", name)
			t = jsTarget{pkg: "@fixwire/react"}
		} else if !jsExports[target.pkg][name] {
			f.missing(j, name, name, target.pkg)
		}
		f.note(j, name)
		f.named[local] = t
	}
	f.replace(i, f.quoted(i, target.pkg))
}

// declarationStart is the const/let/var before a binding ending at toks[eq].
func (f *jsFile) declarationStart(eq int) int {
	for j := eq - 1; j >= 0 && j >= eq-100; j-- {
		if f.toks[j].kind == jsIdent {
			switch f.text(j) {
			case "const", "let", "var":
				return j
			}
		}
		if f.punct(j, ';') {
			return -1
		}
	}
	return -1
}

// removeStatement deletes toks[from..to], with its lines when it has them
// to itself.
func (f *jsFile) removeStatement(from, to int) {
	start, end := f.toks[from].start, f.toks[to].end
	ls, le := lineStart(f.src, start), lineEnd(f.src, end)
	if blank(f.src[ls:start]) && blank(f.src[end:le]) {
		if le < len(f.src) {
			le++
		}
		f.edits = append(f.edits, edit{start: ls, end: le})
		return
	}
	f.edits = append(f.edits, edit{start: start, end: end})
}

// members renames and checks what the namespace aliases are used for.
func (f *jsFile) members() {
	for i := 0; i+2 < len(f.toks); i++ {
		if f.toks[i].kind != jsIdent || !f.punct(i+1, '.') || f.toks[i+2].kind != jsIdent || f.punct(i-1, '.') {
			continue
		}
		target, ok := f.namespaces[f.text(i)]
		if !ok {
			continue
		}
		name := f.text(i + 2)
		if to, ok := jsRenamed[name]; ok {
			f.replace(i+2, to)
			f.note(i, to)
			continue
		}
		if target.react && jsReactExports[name] {
			// NS.ErrorBoundary → ErrorBoundary, imported from @fixwire/react.
			f.edits = append(f.edits, edit{start: f.toks[i].start, end: f.toks[i+2].start})
			f.reactNeed[name] = true
			continue
		}
		if !jsExports[target.pkg][name] && !f.inIntegrations(i) && name != "init" {
			f.missing(i, f.text(i)+"."+name, name, target.pkg)
		}
		f.note(i, name)
	}
}

// inIntegrations reports whether toks[i] starts an element of an
// integrations array, which objects() takes care of.
func (f *jsFile) inIntegrations(i int) bool {
	for j := i - 1; j >= 0 && j >= i-4000; j-- {
		switch {
		case f.punct(j, '['):
			return f.punct(j-1, ':') && f.is(j-2, jsIdent, "integrations")
		case f.punct(j, ']'), f.punct(j, ';'), f.punct(j, '{'), f.punct(j, '}'):
			return false
		}
	}
	return false
}

// objects edits the options passed to init (and to withFixwire's options
// callback): options Fixwire doesn't take, integrations it doesn't have.
func (f *jsFile) objects() {
	seen := map[int]bool{}
	for i := 0; i+1 < len(f.toks); i++ {
		// An integrations array anywhere (an options object built apart
		// from the init call, too).
		if f.is(i, jsIdent, "integrations") && f.punct(i+1, ':') && f.punct(i+2, '[') && !seen[i+2] {
			seen[i+2] = true
			f.integrations(i + 2)
		}
		if f.toks[i].kind != jsIdent || !f.punct(i+1, '(') {
			continue
		}
		name := f.text(i)
		var target jsTarget
		switch {
		case f.punct(i-1, '.') && i >= 2:
			t, ok := f.namespaces[f.text(i-2)]
			if !ok {
				continue
			}
			target = t
		default:
			t, ok := f.named[name]
			if !ok {
				continue
			}
			target = t
		}
		pkg := target.pkg
		var obj int
		switch name {
		case "init":
			obj = i + 2
		case "withSentry", "withFixwire":
			// withFixwire((env) => ({ … }), handler): the object the
			// callback returns.
			obj = -1
			for j := i + 2; j < len(f.toks) && j < i+40; j++ {
				if f.punct(j, '{') && f.punct(j-1, '(') && f.isArrow(j-2) {
					obj = j
					break
				}
				if f.punct(j, ',') {
					break
				}
			}
		default:
			continue
		}
		if obj < 0 || !f.punct(obj, '{') {
			continue
		}
		f.options(obj, pkg, seen)
	}
}

// isArrow reports a `=>` ending at toks[j].
func (f *jsFile) isArrow(j int) bool { return f.punct(j, '>') && f.punct(j-1, '=') }

// elements lists the comma-separated elements inside the bracket at open:
// their first and last token indexes.
func (f *jsFile) elements(open int) (elems [][2]int, close int) {
	depth := 0
	first := -1
	for j := open + 1; j < len(f.toks); j++ {
		t := f.toks[j]
		if t.kind == jsPunct {
			switch f.src[t.start] {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				if depth == 0 {
					if first >= 0 {
						elems = append(elems, [2]int{first, j - 1})
					}
					return elems, j
				}
				depth--
			case ',':
				if depth == 0 {
					if first >= 0 {
						elems = append(elems, [2]int{first, j - 1})
					}
					first = -1
					continue
				}
			}
		}
		if first < 0 {
			first = j
		}
	}
	return elems, len(f.toks) - 1
}

func (f *jsFile) removeAll(elems [][2]int, idx []int) {
	f.edits = append(f.edits, removeElements(f.src, f.spans, elems, idx, func(i int) bool { return f.punct(i, ',') })...)
}

// options edits the options object literal at toks[obj].
func (f *jsFile) options(obj int, pkg string, seen map[int]bool) {
	known := jsOptions[pkg]
	elems, _ := f.elements(obj)
	var gone []int
	for k, e := range elems {
		key := f.text(e[0])
		if f.toks[e[0]].kind == jsString {
			key = f.stringValue(e[0])
		} else if f.toks[e[0]].kind != jsIdent {
			continue // a spread or a computed key
		}
		if key == "integrations" && f.punct(e[0]+1, ':') && f.punct(e[0]+2, '[') && !seen[e[0]+2] {
			seen[e[0]+2] = true
			f.integrations(e[0] + 2)
		}
		if key == "dsn" {
			for j := e[0]; j <= e[1]; j++ {
				if f.toks[j].kind == jsString && strings.Contains(f.stringValue(j), "sentry.io") {
					f.step(j, "the DSN is the other service's: use your Fixwire project's DSN (FIXWIRE_DSN)")
				}
			}
		}
		if known != nil && !known[key] {
			gone = append(gone, k)
			note := jsOptionNotes[key]
			if note != "" {
				note = ": " + note
			}
			f.step(e[0], "removed the %s option; Fixwire's init doesn't take it%s", key, note)
		}
	}
	f.removeAll(elems, gone)
}

// integrations edits the integrations array at toks[open]: an integration
// Fixwire doesn't have, or one from a dropped package, goes.
func (f *jsFile) integrations(open int) {
	elems, _ := f.elements(open)
	var gone []int
	defer func() { f.removeAll(elems, gone) }()
	for k, e := range elems {
		callee, ns := "", ""
		switch {
		case f.toks[e[0]].kind == jsIdent && f.punct(e[0]+1, '.') && e[0]+2 <= e[1]:
			ns, callee = f.text(e[0]), f.text(e[0]+2)
		case f.toks[e[0]].kind == jsIdent:
			callee = f.text(e[0])
		default:
			continue
		}
		if ns != "" {
			target, ok := f.namespaces[ns]
			if !ok || jsExports[target.pkg][callee] || jsRenamed[callee] != "" {
				continue
			}
			gone = append(gone, k)
			f.step(e[0], "removed the %s integration: Fixwire doesn't have it (its defaults cover errors, requests and tracing)", callee)
			continue
		}
		if why, ok := f.dropped[callee]; ok {
			gone = append(gone, k)
			f.step(e[0], "removed the %s integration: %s", callee, why)
			continue
		}
		if target, ok := f.named[callee]; ok && !jsExports[target.pkg][callee] {
			gone = append(gone, k)
			f.step(e[0], "removed the %s integration: Fixwire doesn't have it (its defaults cover errors, requests and tracing)", callee)
		}
	}
}

// environment reports the incumbent's environment variables: Fixwire reads
// FIXWIRE_DSN, FIXWIRE_RELEASE and FIXWIRE_ENVIRONMENT.
func (f *jsFile) environment() {
	for i, t := range f.toks {
		if t.kind != jsIdent && t.kind != jsString {
			continue
		}
		name := f.text(i)
		if t.kind == jsString {
			name = f.stringValue(i)
		}
		if strings.Contains(name, "SENTRY_") {
			f.step(i, "%s: Fixwire reads %s; set that instead", name, envName(name))
		}
	}
}

// envName is the Fixwire variable for one of the incumbent's. A
// framework's public prefix (NEXT_PUBLIC_, VITE_, PUBLIC_…) stays, as the
// bundle reads the variable through it.
func envName(name string) string {
	if i := strings.Index(name, "SENTRY_"); i > 0 {
		prefix, rest := name[:i], name[i:]
		switch rest {
		case "SENTRY_DSN", "SENTRY_RELEASE", "SENTRY_ENVIRONMENT":
			return prefix + "FIXWIRE_" + strings.TrimPrefix(rest, "SENTRY_")
		}
	}
	switch name {
	case "SENTRY_DSN", "SENTRY_RELEASE", "SENTRY_ENVIRONMENT":
		return "FIXWIRE_" + strings.TrimPrefix(name, "SENTRY_")
	case "SENTRY_AUTH_TOKEN":
		return "FIXWIRE_AUTH_TOKEN (fixwire-cli)"
	case "SENTRY_ORG", "SENTRY_PROJECT", "SENTRY_URL":
		return "FIXWIRE_" + strings.TrimPrefix(name, "SENTRY_") + " (fixwire-cli)"
	}
	return "FIXWIRE_DSN, FIXWIRE_RELEASE or FIXWIRE_ENVIRONMENT"
}

// reactImport adds @fixwire/react's import for the React names a namespace
// alias used.
func (f *jsFile) reactImport() {
	if len(f.reactNeed) == 0 || f.reactAfter < 0 {
		return
	}
	names := make([]string, 0, len(f.reactNeed))
	for n := range f.reactNeed {
		names = append(names, n)
	}
	sort.Strings(names)
	line := fmt.Sprintf("\nimport { %s } from \"@fixwire/react\";", strings.Join(names, ", "))
	if f.cjs {
		line = fmt.Sprintf("\nconst { %s } = require(\"@fixwire/react\");", strings.Join(names, ", "))
	}
	end := f.toks[f.reactAfter].end
	f.edits = append(f.edits, edit{start: end, end: end, text: line})
}
