package migrate

// A JavaScript and TypeScript lexer that knows just enough to rewrite code
// safely: where comments, strings, template literals and regular
// expressions are, so an edit only ever touches code tokens and module
// specifiers. It is linear in the file's size.

type jsKind uint8

const (
	jsIdent    jsKind = iota // identifiers and keywords
	jsString                 // '…' or "…"
	jsTemplate               // `…`, with its ${} expressions
	jsRegex                  // /…/flags
	jsNumber
	jsPunct // one byte of punctuation
)

type jsToken struct {
	kind       jsKind
	start, end int
}

// regexAfter are the words a regular expression may follow; after other
// words and identifiers, a slash divides.
var regexAfter = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true, "new": true, "delete": true,
	"void": true, "throw": true, "case": true, "do": true, "else": true, "yield": true, "await": true,
}

func lexJS(src []byte) []jsToken {
	l := jsLexer{src: src}
	l.run(0, false)
	return l.toks
}

type jsLexer struct {
	src  []byte
	toks []jsToken
	nest int // template literals inside template expressions, now
}

// maxNest bounds how deep templates nest inside ${}: past it, an
// expression is skipped as text, so no input recurses without end.
const maxNest = 100

// run lexes from i. Inside a template's ${} (expr) it stops after the
// closing brace and returns where it stopped; its tokens aren't kept.
func (l *jsLexer) run(i int, expr bool) int {
	src := l.src
	regexOK := true
	depth := 0
	emit := func(k jsKind, s, e int) {
		if !expr {
			l.toks = append(l.toks, jsToken{k, s, e})
		}
	}
	if !expr && len(src) > 1 && src[0] == '#' && src[1] == '!' {
		i = lineEnd(src, 0)
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i = lineEnd(src, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = blockCommentEnd(src, i+2)
		case c == '\'' || c == '"':
			j := quotedEnd(src, i)
			emit(jsString, i, j)
			i, regexOK = j, false
		case c == '`':
			j := l.templateEnd(i)
			emit(jsTemplate, i, j)
			i, regexOK = j, false
		case c == '/' && regexOK:
			j := regexEnd(src, i)
			emit(jsRegex, i, j)
			i, regexOK = j, false
		case isIdentByte(c) && !isDigit(c):
			j := i + 1
			for j < len(src) && isIdentByte(src[j]) {
				j++
			}
			emit(jsIdent, i, j)
			regexOK = regexAfter[string(src[i:j])]
			i = j
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			j := i + 1
			for j < len(src) && (isIdentByte(src[j]) || src[j] == '.') {
				j++
			}
			emit(jsNumber, i, j)
			i, regexOK = j, false
		default:
			if expr {
				switch c {
				case '{':
					depth++
				case '}':
					if depth == 0 {
						return i + 1
					}
					depth--
				}
			}
			emit(jsPunct, i, i+1)
			// After `<` a slash closes a JSX tag (</Tag>); it divides after
			// a closing bracket.
			regexOK = c != ')' && c != ']' && c != '}' && c != '<'
			i++
		}
	}
	return i
}

// templateEnd is where the template literal starting at i ends.
func (l *jsLexer) templateEnd(i int) int {
	src := l.src
	for j := i + 1; j < len(src); {
		switch src[j] {
		case '\\':
			j += 2
		case '`':
			return j + 1
		case '$':
			if j+1 < len(src) && src[j+1] == '{' && l.nest < maxNest {
				l.nest++
				j = l.run(j+2, true)
				l.nest--
			} else {
				j++
			}
		default:
			j++
		}
	}
	return len(src)
}

// quotedEnd is where the '…' or "…" string starting at i ends: its closing
// quote, else the end of its line (an unterminated string).
func quotedEnd(src []byte, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		case '\n':
			return j
		}
	}
	return len(src)
}

// regexEnd is where the regular expression starting at i ends, flags
// included.
func regexEnd(src []byte, i int) int {
	class := false
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				j++
				for j < len(src) && isIdentByte(src[j]) {
					j++
				}
				return j
			}
		case '\n':
			return j
		}
	}
	return len(src)
}

func blockCommentEnd(src []byte, i int) int {
	for j := i; j+1 < len(src); j++ {
		if src[j] == '*' && src[j+1] == '/' {
			return j + 2
		}
	}
	return len(src)
}

func lineEnd(src []byte, i int) int {
	for j := i; j < len(src); j++ {
		if src[j] == '\n' {
			return j
		}
	}
	return len(src)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isIdentByte: ASCII letters, digits, _ and $, and every byte of a
// multi-byte UTF-8 character (identifiers may use them).
func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || isDigit(c) || c == '_' || c == '$' || c >= 0x80
}
