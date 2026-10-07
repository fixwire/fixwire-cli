package migrate

import (
	"fmt"
	"strings"
)

// --- package.json ----------------------------------------------------------

type jsonToken struct {
	start, end int
	str        bool // a string; else punctuation or a bare literal
}

func lexJSON(src []byte) []jsonToken {
	var toks []jsonToken
	for i := 0; i < len(src); {
		switch c := src[i]; c {
		case ' ', '\t', '\n', '\r':
			i++
		case '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			toks = append(toks, jsonToken{i, min(j+1, len(src)), true})
			i = j + 1
		case '{', '}', '[', ']', ':', ',':
			toks = append(toks, jsonToken{i, i + 1, false})
			i++
		default:
			j := i + 1
			for j < len(src) && !strings.ContainsRune(" \t\r\n{}[]:,\"", rune(src[j])) {
				j++
			}
			toks = append(toks, jsonToken{i, j, false})
			i = j
		}
	}
	return toks
}

var dependencyFields = set("dependencies", "devDependencies", "peerDependencies", "optionalDependencies")

// migratePackageJSON moves the incumbent's packages in a package.json to
// Fixwire's, and reports what's left to do by hand.
func migratePackageJSON(src []byte) ([]edit, []step) {
	toks := lexJSON(src)
	spans := make([]span, len(toks))
	for i, t := range toks {
		spans[i] = span{t.start, t.end}
	}
	ln := newLines(src)
	text := func(i int) string { return string(src[toks[i].start:toks[i].end]) }
	value := func(i int) string { s := text(i); return strings.Trim(s, `"`) }
	punct := func(i int, c byte) bool {
		return i >= 0 && i < len(toks) && !toks[i].str && src[toks[i].start] == c
	}
	var edits []edit
	var steps []step
	addStep := func(i int, format string, args ...any) {
		steps = append(steps, step{line: ln.of(toks[i].start), text: fmt.Sprintf(format, args...)})
	}

	// elements of the object at open: [first, last] token of each member.
	elements := func(open int) [][2]int {
		var out [][2]int
		depth, first := 0, -1
		for j := open + 1; j < len(toks); j++ {
			if !toks[j].str {
				switch src[toks[j].start] {
				case '{', '[':
					depth++
				case '}', ']':
					if depth == 0 {
						if first >= 0 {
							out = append(out, [2]int{first, j - 1})
						}
						return out
					}
					depth--
				case ',':
					if depth == 0 {
						if first >= 0 {
							out = append(out, [2]int{first, j - 1})
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
		return out
	}

	if len(toks) == 0 || !punct(0, '{') {
		return nil, nil
	}
	for _, field := range elements(0) {
		key := value(field[0])
		if key == "scripts" && punct(field[0]+2, '{') {
			for _, s := range elements(field[0] + 2) {
				v := value(s[1])
				if strings.Contains(v, "@sentry/") || strings.Contains(v, "sentry-cli") {
					addStep(s[0], "the %s script uses the other SDK: Fixwire's Node SDK needs no preload flags, and fixwire-cli uploads source maps", value(s[0]))
				}
			}
			continue
		}
		if !dependencyFields[key] || !punct(field[0]+2, '{') {
			continue
		}
		deps := elements(field[0] + 2)
		present := map[string]bool{}
		for _, d := range deps {
			present[value(d[0])] = true
		}
		var gone []int
		for k, d := range deps {
			name := value(d[0])
			pkg, ok := incumbentPackage(name)
			if !ok {
				continue
			}
			if why, ok := jsDropped[pkg]; ok {
				gone = append(gone, k)
				addStep(d[0], "removed %s from %s: %s", name, key, why)
				continue
			}
			if how, ok := jsByHand[pkg]; ok {
				addStep(d[0], "%s: %s", name, how)
				continue
			}
			target, ok := jsPackages[pkg]
			if !ok || name != pkg {
				addStep(d[0], "%s: move it by hand to the matching @fixwire package", name)
				continue
			}
			want := []string{target.pkg}
			if target.react {
				want = []string{"@fixwire/browser", "@fixwire/react"}
			}
			var add []string
			for _, w := range want {
				if !present[w] {
					add = append(add, w)
					present[w] = true
				}
			}
			if len(add) == 0 {
				gone = append(gone, k)
				continue
			}
			// "@x": "^9" → "@fixwire/y": "^0.1.2", one per line as the
			// file has them.
			indent := ""
			if ls := lineStart(src, toks[d[0]].start); blank(src[ls:toks[d[0]].start]) {
				indent = "\n" + string(src[ls:toks[d[0]].start])
			}
			var b strings.Builder
			for n, a := range add {
				if n > 0 {
					b.WriteString(",")
					if indent == "" {
						b.WriteString(" ")
					} else {
						b.WriteString(indent)
					}
				}
				fmt.Fprintf(&b, "%q: %q", a, "^"+jsVersion)
			}
			edits = append(edits, edit{start: toks[d[0]].start, end: toks[d[1]].end, text: b.String()})
		}
		edits = append(edits, removeElements(src, spans, deps, gone, func(i int) bool { return punct(i, ',') })...)
	}
	if len(edits) > 0 {
		steps = append(steps, step{line: 1, text: "install the new dependencies with your package manager (npm install, pnpm install, yarn)"})
	}
	return edits, steps
}

// --- Python requirements -----------------------------------------------------

// requirement rewrites a requirement on the incumbent's package to
// Fixwire's: extras of async frameworks become fixwire[async], and
// environment markers stay.
func requirement(req string) (string, bool) {
	s := strings.TrimSpace(req)
	n := 0
	for n < len(s) && (isIdentByte(s[n]) || s[n] == '-' || s[n] == '.') {
		n++
	}
	if n == 0 || !isIncumbentRequirement(s[:n]) {
		return "", false
	}
	rest := s[n:]
	async := false
	if strings.HasPrefix(strings.TrimSpace(rest), "[") {
		rest = strings.TrimSpace(rest)
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return "", false
		}
		for _, e := range strings.Split(rest[1:end], ",") {
			if asyncExtras[strings.ToLower(strings.TrimSpace(e))] {
				async = true
			}
		}
		rest = rest[end+1:]
	}
	markers := ""
	if i := strings.IndexByte(rest, ';'); i >= 0 {
		markers = "; " + strings.TrimSpace(rest[i+1:])
	}
	out := "fixwire"
	if async {
		out += "[async]"
	}
	return out + ">=" + pyVersion + markers, true
}

// migrateRequirements rewrites requirement lines: requirements*.txt,
// constraints files and setup.cfg's install_requires.
func migrateRequirements(src []byte) ([]edit, []step) {
	var edits []edit
	for i := 0; i < len(src); {
		end := lineEnd(src, i)
		line := string(src[i:end])
		body := line
		if c := strings.Index(body, " #"); c >= 0 {
			body = body[:c]
		} else if strings.HasPrefix(strings.TrimSpace(body), "#") {
			body = ""
		}
		trimmed := strings.TrimSpace(body)
		if trimmed != "" {
			if req, ok := requirement(trimmed); ok {
				at := i + strings.Index(line, trimmed)
				edits = append(edits, edit{start: at, end: at + len(trimmed), text: req})
			}
		}
		i = end + 1
	}
	return edits, nil
}

// migrateTOML rewrites pyproject.toml and Pipfile: requirement strings
// (PEP 621, PDM) and `sentry-sdk = …` keys (Poetry, Pipenv).
func migrateTOML(src []byte) ([]edit, []step) {
	var edits []edit
	for i := 0; i < len(src); {
		end := lineEnd(src, i)
		line := src[i:end]
		trimmed := strings.TrimSpace(string(line))
		indent := i + strings.Index(string(line), trimmed)
		if key, rest, ok := strings.Cut(trimmed, "="); ok && !strings.HasPrefix(trimmed, "#") {
			k := strings.Trim(strings.TrimSpace(key), `"'`)
			if isIncumbentRequirement(k) && !strings.ContainsAny(k, "[] ") {
				async := false
				for e := range asyncExtras {
					if strings.Contains(rest, `"`+e+`"`) || strings.Contains(rest, `'`+e+`'`) {
						async = true
					}
				}
				v := fmt.Sprintf(`fixwire = ">=%s"`, pyVersion)
				if async {
					v = fmt.Sprintf(`fixwire = { version = ">=%s", extras = ["async"] }`, pyVersion)
				}
				edits = append(edits, edit{start: indent, end: indent + len(trimmed), text: v})
				i = end + 1
				continue
			}
		}
		// Requirement strings anywhere on the line.
		for j := 0; j < len(line); j++ {
			q := line[j]
			if q != '"' && q != '\'' {
				continue
			}
			k := j + 1
			for k < len(line) && line[k] != q {
				k++
			}
			if k >= len(line) {
				break
			}
			if req, ok := requirement(string(line[j+1 : k])); ok {
				edits = append(edits, edit{start: i + j + 1, end: i + k, text: req})
			}
			j = k
		}
		i = end + 1
	}
	return edits, nil
}
