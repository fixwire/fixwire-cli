package sourcemaps

import (
	"errors"
	"strings"
)

const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var errVLQ = errors.New("sourcemaps: invalid VLQ in mappings")

// decodeSegment reads the fields of one mappings segment.
func decodeSegment(seg string) ([]int, error) {
	var out []int
	value, shift := 0, 0
	for i := 0; i < len(seg); i++ {
		digit := strings.IndexByte(base64Chars, seg[i])
		if digit < 0 {
			return nil, errVLQ
		}
		value |= (digit & 31) << shift
		if digit&32 != 0 {
			shift += 5
			continue
		}
		if value&1 != 0 {
			out = append(out, -(value >> 1))
		} else {
			out = append(out, value>>1)
		}
		value, shift = 0, 0
	}
	if shift != 0 {
		return nil, errVLQ
	}
	return out, nil
}

// encodeSegment writes the fields of one mappings segment.
func encodeSegment(fields []int) string {
	var b strings.Builder
	for _, v := range fields {
		u := v << 1
		if v < 0 {
			u = (-v << 1) | 1
		}
		for {
			digit := u & 31
			u >>= 5
			if u > 0 {
				digit |= 32
			}
			b.WriteByte(base64Chars[digit])
			if u == 0 {
				break
			}
		}
	}
	return b.String()
}

// shiftColumns moves the generated columns at or after col on a line
// (both 0-based) right by delta, as inserting delta characters there does.
// Columns in a line are relative to the previous segment, so only the
// first segment at or after col changes.
func shiftColumns(mappings string, line, col, delta int) (string, error) {
	groups := strings.Split(mappings, ";")
	if line >= len(groups) || groups[line] == "" {
		return mappings, nil
	}
	segs := strings.Split(groups[line], ",")
	abs := 0
	for i, seg := range segs {
		fields, err := decodeSegment(seg)
		if err != nil || len(fields) == 0 {
			return "", errVLQ
		}
		abs += fields[0]
		if abs >= col {
			fields[0] += delta
			segs[i] = encodeSegment(fields)
			groups[line] = strings.Join(segs, ",")
			return strings.Join(groups, ";"), nil
		}
	}
	return mappings, nil
}
