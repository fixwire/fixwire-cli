package migrate

// A Python lexer that knows where comments and strings (with their
// prefixes, and triple-quoted) are, and where each logical statement ends,
// so an edit only touches code tokens. It is linear in the file's size.

type pyKind uint8

const (
	pyName pyKind = iota
	pyString
	pyNumber
	pyOp      // one byte of punctuation
	pyNewline // the end of a logical line (not inside brackets)
)

type pyToken struct {
	kind       pyKind
	start, end int
}

func lexPython(src []byte) []pyToken {
	var toks []pyToken
	depth := 0
	newline := func(i int) {
		if n := len(toks); n > 0 && toks[n-1].kind != pyNewline {
			toks = append(toks, pyToken{pyNewline, i, i})
		}
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			if depth == 0 {
				newline(i)
			}
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			i++
		case c == '\\' && i+1 < len(src) && (src[i+1] == '\n' || src[i+1] == '\r'):
			i += 2 // a continued line
		case c == '#':
			i = lineEnd(src, i)
		case c == '\'' || c == '"':
			j := pyStringEnd(src, i)
			toks = append(toks, pyToken{pyString, i, j})
			i = j
		case isIdentByte(c) && !isDigit(c):
			j := i + 1
			for j < len(src) && isIdentByte(src[j]) {
				j++
			}
			// A string prefix (r, b, f, u, t, and pairs) right before a quote.
			if j < len(src) && (src[j] == '\'' || src[j] == '"') && j-i <= 2 && isStringPrefix(src[i:j]) {
				k := pyStringEnd(src, j)
				toks = append(toks, pyToken{pyString, i, k})
				i = k
				continue
			}
			toks = append(toks, pyToken{pyName, i, j})
			i = j
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			j := i + 1
			for j < len(src) && (isIdentByte(src[j]) || src[j] == '.') {
				j++
			}
			toks = append(toks, pyToken{pyNumber, i, j})
			i = j
		default:
			switch c {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				if depth > 0 {
					depth--
				}
			case ';':
				if depth == 0 {
					newline(i)
					i++
					continue
				}
			}
			toks = append(toks, pyToken{pyOp, i, i + 1})
			i++
		}
	}
	newline(len(src))
	return toks
}

func isStringPrefix(p []byte) bool {
	for _, c := range p {
		switch c | 0x20 { // lower case
		case 'r', 'b', 'f', 'u', 't':
		default:
			return false
		}
	}
	return true
}

// pyStringEnd is where the string whose quote is at i ends. A backslash
// escapes the next character, in raw strings too (it keeps a quote from
// ending one); a one-line string also ends at its line's end.
func pyStringEnd(src []byte, i int) int {
	q := src[i]
	triple := i+2 < len(src) && src[i+1] == q && src[i+2] == q
	j := i + 1
	if triple {
		j = i + 3
	}
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case '\n':
			if !triple {
				return j
			}
		case q:
			if !triple {
				return j + 1
			}
			if j+2 < len(src) && src[j+1] == q && src[j+2] == q {
				return j + 3
			}
		}
		j++
	}
	return len(src)
}

// pyStringValue is a simple string literal's value (no escapes, no
// prefix but r/u), else false.
func pyStringValue(lit []byte) (string, bool) {
	i := 0
	for i < len(lit) && lit[i] != '\'' && lit[i] != '"' {
		if c := lit[i] | 0x20; c != 'r' && c != 'u' {
			return "", false
		}
		i++
	}
	if len(lit)-i < 2 || lit[i] != lit[len(lit)-1] {
		return "", false
	}
	v := lit[i+1 : len(lit)-1]
	for _, c := range v {
		if c == '\\' || c == lit[i] || c == '\n' {
			return "", false
		}
	}
	return string(v), true
}
