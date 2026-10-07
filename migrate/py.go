package migrate

import (
	"fmt"
	"path/filepath"
	"strings"
)

// pyFile rewrites one Python file.
type pyFile struct {
	path  string
	src   []byte
	toks  []pyToken
	spans []span
	edits []edit
	steps []step
	lines lines

	// What the file binds from the incumbent's SDK.
	module     bool                     // `import sentry_sdk`: the name is used as is
	aliases    map[string]bool          // `import sentry_sdk as x`
	named      map[string]string        // local name → Fixwire's name for it
	kept       map[string]pyIntegration // integration classes Fixwire has
	dropped    map[string]string        // integration classes it doesn't: what to do
	djangoGone bool
}

// pyNotes say what to use instead of the incumbent's names Fixwire lacks.
var pyNotes = map[string]string{
	"monitor":           "wrap the job in fixwire.capture_check_in calls (in_progress, then ok or error)",
	"trace":             "use `with fixwire.start_span(name=...)` in the function",
	"start_transaction": "use fixwire.start_span: a span without a parent starts a trace",
	"push_scope":        "use `with fixwire.new_scope() as scope`",
	"configure_scope":   "use fixwire.get_current_scope()",
	"Hub":               "Fixwire has no hubs: use scopes",
	"get_current_hub":   "Fixwire has no hubs: use scopes",
	"set_measurement":   "set an attribute on the span instead",
	"metrics":           "Fixwire has no metrics yet",
	"get_traceparent":   "use fixwire.trace_headers()",
	"get_baggage":       "use fixwire.trace_headers()",
	"logger":            "use the logging module with fixwire.integrations.logging.LoggingIntegration",
}

func migratePython(path string, src []byte) ([]edit, []step) {
	f := &pyFile{
		path: path, src: src, toks: lexPython(src), lines: newLines(src),
		aliases: map[string]bool{}, named: map[string]string{}, kept: map[string]pyIntegration{},
		dropped: map[string]string{},
	}
	f.spans = make([]span, len(f.toks))
	for i, t := range f.toks {
		f.spans[i] = span{t.start, t.end}
	}
	f.imports()
	if f.module || len(f.aliases) > 0 || len(f.named) > 0 || len(f.kept) > 0 || len(f.dropped) > 0 {
		f.code()
		f.middleware()
	}
	f.strings()
	return f.edits, f.steps
}

func (f *pyFile) text(i int) string { return string(f.src[f.toks[i].start:f.toks[i].end]) }

func (f *pyFile) name(i int, s string) bool {
	return i >= 0 && i < len(f.toks) && f.toks[i].kind == pyName && f.text(i) == s
}

func (f *pyFile) op(i int, c byte) bool {
	return i >= 0 && i < len(f.toks) && f.toks[i].kind == pyOp && f.src[f.toks[i].start] == c
}

func (f *pyFile) step(i int, format string, args ...any) {
	off := 0
	if i >= 0 && i < len(f.toks) {
		off = f.toks[i].start
	}
	f.steps = append(f.steps, step{line: f.lines.of(off), text: fmt.Sprintf(format, args...)})
}

func (f *pyFile) replace(i int, text string) {
	f.edits = append(f.edits, edit{start: f.toks[i].start, end: f.toks[i].end, text: text})
}

// statements yields each logical statement's token range [from, to).
func (f *pyFile) statements(yield func(from, to int)) {
	from := 0
	for i, t := range f.toks {
		if t.kind == pyNewline {
			if i > from {
				yield(from, i)
			}
			from = i + 1
		}
	}
}

// dotted reads a dotted name starting at toks[i]: its parts and the index
// after it.
func (f *pyFile) dotted(i int) ([]string, int) {
	var parts []string
	for i < len(f.toks) && f.toks[i].kind == pyName {
		parts = append(parts, f.text(i))
		if !f.op(i+1, '.') {
			return parts, i + 1
		}
		i += 2
	}
	return parts, i
}

func (f *pyFile) imports() {
	f.statements(func(from, to int) {
		switch {
		case f.name(from, "import"):
			f.importStatement(from, to)
		case f.name(from, "from"):
			f.fromStatement(from, to)
		}
	})
}

// importStatement: import sentry_sdk [as x], …
func (f *pyFile) importStatement(from, to int) {
	for i := from + 1; i < to; i++ {
		if !f.name(i, "sentry_sdk") || f.op(i-1, '.') {
			continue
		}
		if f.op(i+1, '.') {
			f.step(i, "import a Fixwire module here instead of the other SDK's %s", f.text(i))
			continue
		}
		f.replace(i, "fixwire")
		if f.name(i+1, "as") && i+2 < to {
			f.aliases[f.text(i+2)] = true
		} else {
			f.module = true
		}
	}
}

// fromStatement: from sentry_sdk[.x.y] import a, b as c
func (f *pyFile) fromStatement(from, to int) {
	parts, after := f.dotted(from + 1)
	if len(parts) == 0 || parts[0] != "sentry_sdk" || !f.name(after, "import") {
		return
	}
	names := f.importedNames(after+1, to)
	switch {
	case len(parts) == 1 || len(parts) == 2 && parts[1] == "crons":
		// sentry_sdk and sentry_sdk.crons: fixwire has their names.
		f.edits = append(f.edits, edit{start: f.toks[from+1].start, end: f.toks[after-1].end, text: "fixwire"})
		for _, n := range names {
			to := n.imported
			if r, ok := pyRenamed[n.imported]; ok {
				to = r
				if n.local == n.imported {
					f.replace(n.at, r+" as "+n.imported)
				} else {
					f.replace(n.at, r)
				}
			} else if !pyExports[n.imported] {
				f.step(n.at, "%s isn't in fixwire%s", n.imported, noteFor(n.imported))
			}
			f.named[n.local] = to
		}
	case len(parts) >= 3 && parts[1] == "integrations":
		integ, ok := pyIntegrations[parts[2]]
		if ok && integ.module != "" {
			// sentry_sdk.integrations.X → fixwire.integrations.X
			f.edits = append(f.edits, edit{start: f.toks[from+1].start, end: f.toks[from+1+2*2].end,
				text: "fixwire.integrations." + integ.module})
			for _, n := range names {
				f.kept[n.local] = integ
			}
			return
		}
		note := integ.note
		if note == "" {
			note = "Fixwire has no " + parts[2] + " integration"
		}
		for _, n := range names {
			f.dropped[n.local] = note
		}
		f.removeStatement(from, to)
		if parts[2] == "django" {
			// middleware() adds Fixwire's middleware, or says to.
			f.djangoGone = true
			return
		}
		f.step(from, "removed the %s integration: %s", parts[2], note)
	default:
		f.step(from, "%s has no counterpart in fixwire: replace what this imports", strings.Join(parts, "."))
	}
}

type pyImported struct {
	imported, local string
	at              int // the imported name's token
}

func (f *pyFile) importedNames(from, to int) []pyImported {
	var out []pyImported
	for i := from; i < to; i++ {
		if f.toks[i].kind != pyName || f.name(i, "as") || f.name(i-1, "as") {
			continue
		}
		n := pyImported{imported: f.text(i), local: f.text(i), at: i}
		if f.name(i+1, "as") && i+2 < to {
			n.local = f.text(i + 2)
		}
		out = append(out, n)
	}
	return out
}

func noteFor(name string) string {
	if n, ok := pyNotes[name]; ok {
		return ": " + n
	}
	return ""
}

func (f *pyFile) removeStatement(from, to int) {
	start, end := f.toks[from].start, f.toks[to-1].end
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

// isNamespace reports whether toks[i] names the SDK's module.
func (f *pyFile) isNamespace(i int) bool {
	if f.toks[i].kind != pyName || f.op(i-1, '.') {
		return false
	}
	n := f.text(i)
	return (n == "sentry_sdk" && f.module) || f.aliases[n]
}

// code renames the module where it's used, and edits calls.
func (f *pyFile) code() {
	inImport := map[int]bool{}
	f.statements(func(from, to int) {
		if f.name(from, "import") || f.name(from, "from") {
			for i := from; i < to; i++ {
				inImport[i] = true
			}
		}
	})
	for i := range f.toks {
		if inImport[i] || !f.isNamespace(i) {
			continue
		}
		if f.text(i) == "sentry_sdk" {
			f.replace(i, "fixwire")
		}
		if !f.op(i+1, '.') || i+2 >= len(f.toks) || f.toks[i+2].kind != pyName {
			continue
		}
		member := f.text(i + 2)
		if r, ok := pyRenamed[member]; ok {
			f.replace(i+2, r)
			member = r
		} else if !pyExports[member] {
			f.step(i, "%s.%s isn't in fixwire%s", f.text(i), member, noteFor(member))
		}
		if f.op(i+3, '(') {
			f.call(i+3, member)
		}
	}
	for i, t := range f.toks {
		if t.kind != pyName || inImport[i] || f.op(i-1, '.') || !f.op(i+1, '(') {
			continue
		}
		if fixwireName, ok := f.named[f.text(i)]; ok {
			f.call(i+1, fixwireName)
		}
	}
}

// elements lists the comma-separated elements inside the bracket at open.
func (f *pyFile) elements(open int) [][2]int {
	var elems [][2]int
	depth := 0
	first := -1
	for j := open + 1; j < len(f.toks); j++ {
		t := f.toks[j]
		if t.kind == pyOp {
			switch f.src[t.start] {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				if depth == 0 {
					if first >= 0 {
						elems = append(elems, [2]int{first, j - 1})
					}
					return elems
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
	return elems
}

func (f *pyFile) removeAll(elems [][2]int, idx []int) {
	f.edits = append(f.edits, removeElements(f.src, f.spans, elems, idx, func(i int) bool { return f.op(i, ',') })...)
}

// kwarg is an element's keyword (`name=…`), else "".
func (f *pyFile) kwarg(e [2]int) string {
	if f.toks[e[0]].kind == pyName && f.op(e[0]+1, '=') && !f.op(e[0]+2, '=') {
		return f.text(e[0])
	}
	return ""
}

// call edits a call to one of the SDK's functions; toks[open] is its "(".
func (f *pyFile) call(open int, fn string) {
	elems := f.elements(open)
	switch fn {
	case "init":
		var gone []int
		for k, e := range elems {
			kw := f.kwarg(e)
			switch {
			case kw == "":
				f.dsn(e)
			case kw == "dsn":
				f.dsn(e)
			case kw == "integrations" && f.op(e[0]+2, '['):
				f.integrations(e[0] + 2)
			case !pyOptions[kw]:
				gone = append(gone, k)
				note := pyOptionNotes[kw]
				if note != "" {
					note = ": " + note
				}
				f.step(e[0], "removed the %s option; fixwire.init doesn't take it (it would turn the SDK off)%s", kw, note)
			}
		}
		f.removeAll(elems, gone)
	case "capture_check_in":
		for _, e := range elems {
			if f.kwarg(e) == "monitor_slug" {
				f.replace(e[0], "monitor")
			}
		}
	}
}

func (f *pyFile) dsn(e [2]int) {
	for j := e[0]; j <= e[1]; j++ {
		if f.toks[j].kind == pyString && strings.Contains(f.text(j), "sentry.io") {
			f.step(j, "the DSN is the other service's: use your Fixwire project's DSN (FIXWIRE_DSN)")
		}
	}
}

// integrations edits the list at toks[open]: integrations Fixwire doesn't
// have go; the arguments Fixwire's don't take go too.
func (f *pyFile) integrations(open int) {
	elems := f.elements(open)
	var gone []int
	for k, e := range elems {
		if f.toks[e[0]].kind != pyName {
			continue
		}
		parts, after := f.dotted(e[0])
		local := parts[len(parts)-1]
		if len(parts) == 1 {
			local = parts[0]
		}
		if _, ok := f.dropped[local]; ok {
			gone = append(gone, k)
			continue
		}
		integ, ok := f.kept[local]
		if !ok || !f.op(after, '(') {
			continue
		}
		args := f.elements(after)
		var extra []int
		for a, arg := range args {
			kw := f.kwarg(arg)
			if !integ.args[kw] {
				extra = append(extra, a)
				name := kw
				if name == "" {
					name = "a positional argument"
				}
				f.step(arg[0], "removed %s from %s(): Fixwire's takes %s", name, local, argsList(integ.args))
			}
		}
		f.removeAll(args, extra)
	}
	f.removeAll(elems, gone)
}

func argsList(args map[string]bool) string {
	if len(args) == 0 {
		return "none"
	}
	var out []string
	for a := range args {
		out = append(out, a)
	}
	sortStrings(out)
	return strings.Join(out, " and ")
}

// middleware adds Fixwire's Django middleware where the Django integration
// went, when the same file lists MIDDLEWARE.
func (f *pyFile) middleware() {
	if !f.djangoGone {
		return
	}
	const mw = `"fixwire.integrations.django.FixwireMiddleware",`
	done := false
	f.statements(func(from, to int) {
		if done || !f.name(from, "MIDDLEWARE") || !f.op(from+1, '=') || !f.op(from+2, '[') {
			return
		}
		done = true
		open := from + 2
		elems := f.elements(open)
		if len(elems) == 0 {
			f.edits = append(f.edits, edit{start: f.toks[open].end, end: f.toks[open].end, text: mw[:len(mw)-1]})
			return
		}
		first := f.toks[elems[0][0]].start
		ls := lineStart(f.src, first)
		if blank(f.src[ls:first]) && ls > f.toks[open].end {
			indent := string(f.src[ls:first])
			f.edits = append(f.edits, edit{start: ls, end: ls, text: indent + mw + "\n"})
			return
		}
		f.edits = append(f.edits, edit{start: first, end: first, text: mw + " "})
	})
	if !done {
		f.step(0, "removed the django integration: add \"fixwire.integrations.django.FixwireMiddleware\" first in MIDDLEWARE in your settings")
	}
}

// strings handles string literals: requirements in setup.py, the
// incumbent's environment variables and DSNs.
func (f *pyFile) strings() {
	setup := filepath.Base(f.path) == "setup.py"
	for i, t := range f.toks {
		switch t.kind {
		case pyString:
			v, ok := pyStringValue(f.src[t.start:t.end])
			if !ok {
				continue
			}
			if setup {
				if req, ok := requirement(v); ok {
					q := string(f.src[t.end-1])
					f.edits = append(f.edits, edit{start: t.start, end: t.end, text: q + req + q})
					continue
				}
			}
			if strings.Contains(v, "SENTRY_") && !strings.ContainsAny(v, " /") {
				f.step(i, "%s: Fixwire reads %s; set that instead", v, envName(v))
			}
		case pyName:
			if n := f.text(i); strings.Contains(n, "SENTRY_") {
				f.step(i, "%s: Fixwire reads %s; set that instead", n, envName(n))
			}
		}
	}
}
