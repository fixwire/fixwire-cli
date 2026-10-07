package migrate

import (
	"bytes"
	"sort"
	"strings"
)

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

// Hunk is a run of a file's lines before and after its edits.
type Hunk struct {
	Line     int // the first line, from 1
	Old, New string
}

// apply returns src with the edits made and the hunks they make, line by
// line. Edits must not overlap; one that does is dropped.
func apply(src []byte, edits []edit) ([]byte, []Hunk) {
	if len(edits) == 0 {
		return src, nil
	}
	sort.SliceStable(edits, func(a, b int) bool { return edits[a].start < edits[b].start })
	kept := make([]edit, 0, len(edits))
	last := -1
	for _, e := range edits {
		if e.start < last {
			continue
		}
		kept = append(kept, e)
		last = e.end
	}

	// Group the edits by the whole lines they touch.
	type group struct {
		from, to int // byte range of whole lines
		edits    []edit
	}
	var groups []group
	for _, e := range kept {
		from := lineStart(src, e.start)
		// An edit deleting whole lines ends where the next line starts;
		// the hunk ends there too, not at the next line's end.
		to := e.end
		if e.end == e.start || src[e.end-1] != '\n' {
			to = lineEnd(src, e.end)
		}
		if n := len(groups); n > 0 && from <= groups[n-1].to {
			groups[n-1].to = max(groups[n-1].to, to)
			groups[n-1].edits = append(groups[n-1].edits, e)
			continue
		}
		groups = append(groups, group{from, to, []edit{e}})
	}

	ln := newLines(src)
	var out bytes.Buffer
	hunks := make([]Hunk, 0, len(groups))
	pos := 0
	for _, g := range groups {
		out.Write(src[pos:g.from])
		var nw bytes.Buffer
		p := g.from
		for _, e := range g.edits {
			nw.Write(src[p:e.start])
			nw.WriteString(e.text)
			p = e.end
		}
		nw.Write(src[p:g.to])
		out.Write(nw.Bytes())
		hunks = append(hunks, Hunk{
			Line: ln.of(g.from),
			Old:  strings.TrimSuffix(string(src[g.from:g.to]), "\n"),
			New:  strings.TrimSuffix(nw.String(), "\n"),
		})
		pos = g.to
	}
	out.Write(src[pos:])
	return out.Bytes(), hunks
}

func lineStart(src []byte, i int) int {
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	return i
}

// lines numbers offsets in a file: one pass over it, then a binary search
// per offset.
type lines []int

func newLines(src []byte) lines {
	l := lines{0}
	for i, c := range src {
		if c == '\n' {
			l = append(l, i+1)
		}
	}
	return l
}

// of is the line, from 1, of offset i.
func (l lines) of(i int) int { return sort.SearchInts(l, i+1) }

// span is a token's byte range, so element removal works on JavaScript's
// and Python's tokens alike.
type span struct{ start, end int }

// removeElement deletes the element spanning toks[first..last] from a
// comma-separated list, call or object whose separators and brackets are
// tokens too: toks[first-1] is a comma or the opening bracket, toks[last+1]
// a comma or the closing bracket. An element on lines of its own goes with
// its lines; one sharing a line goes with its comma.
func removeElement(src []byte, toks []span, first, last int, prevIsComma, nextIsComma bool) []edit {
	start, end := toks[first].start, toks[last].end
	if nextIsComma {
		end = toks[last+1].end
	}
	ls, le := lineStart(src, start), lineEnd(src, end)
	if blank(src[ls:start]) && blank(src[end:le]) {
		if le < len(src) {
			le++
		}
		lines := edit{start: ls, end: le}
		if !nextIsComma && prevIsComma {
			// The last element, without a trailing comma: the comma before
			// it goes too, so the list stays well formed.
			comma := toks[first-1]
			return []edit{{start: comma.start, end: comma.end}, lines}
		}
		return []edit{lines}
	}
	switch {
	case nextIsComma && last+2 < len(toks):
		return []edit{{start: start, end: toks[last+2].start}} // "a, b" → "b"
	case prevIsComma:
		return []edit{{start: toks[first-1].start, end: toks[last].end}} // "a, b" → "a"
	default:
		return []edit{{start: start, end: end}}
	}
}

// removeElements deletes the elements at idx (ascending) of elems, each a
// first and last token index; neighbours go as one run, so the commas
// between them aren't deleted twice.
func removeElements(src []byte, toks []span, elems [][2]int, idx []int, isComma func(int) bool) []edit {
	var out []edit
	for k := 0; k < len(idx); k++ {
		a, b := idx[k], idx[k]
		for k+1 < len(idx) && idx[k+1] == b+1 {
			k++
			b = idx[k]
		}
		first, last := elems[a][0], elems[b][1]
		out = append(out, removeElement(src, toks, first, last, isComma(first-1), isComma(last+1))...)
	}
	return out
}

func blank(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}
