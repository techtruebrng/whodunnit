// Package sourcemap reads JavaScript source maps (revision 3) and maps
// positions in generated code back to original sources. It supports the
// subset needed for stack traces: plain maps and indexed maps with sections.
package sourcemap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type segment struct {
	genCol  int
	src     int // -1 when the segment has no source
	srcLine int
	srcCol  int
	name    int // -1 when absent
}

// Map is a decoded source map.
type Map struct {
	File     string
	sources  []string
	names    []string
	lines    [][]segment // indexed by generated line (0-based)
	sections []section
}

type section struct {
	line, col int
	m         *Map
}

// Position is an original source location. Line and Col are 1-based.
type Position struct {
	Source string
	Line   int
	Col    int
	Name   string
}

type rawMap struct {
	Version    int      `json:"version"`
	File       string   `json:"file"`
	SourceRoot string   `json:"sourceRoot"`
	Sources    []string `json:"sources"`
	Names      []string `json:"names"`
	Mappings   string   `json:"mappings"`
	Sections   []struct {
		Offset struct {
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"offset"`
		Map *rawMap `json:"map"`
	} `json:"sections"`
}

// ParseFile reads and decodes a source map from disk.
func ParseFile(name string) (*Map, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes a source map.
func Parse(data []byte) (*Map, error) {
	// Some servers prefix maps with )]}' to prevent XSSI.
	if s := string(data); strings.HasPrefix(s, ")]}'") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	var raw rawMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("sourcemap: %w", err)
	}
	return fromRaw(&raw)
}

func fromRaw(raw *rawMap) (*Map, error) {
	if raw.Version != 0 && raw.Version != 3 {
		return nil, fmt.Errorf("sourcemap: unsupported version %d", raw.Version)
	}
	m := &Map{File: raw.File, names: raw.Names}
	if len(raw.Sections) > 0 {
		for _, s := range raw.Sections {
			if s.Map == nil {
				continue // remote "url" sections are not supported
			}
			sub, err := fromRaw(s.Map)
			if err != nil {
				return nil, err
			}
			m.sections = append(m.sections, section{s.Offset.Line, s.Offset.Column, sub})
		}
		return m, nil
	}
	root := raw.SourceRoot
	if root != "" && !strings.HasSuffix(root, "/") {
		root += "/"
	}
	for _, s := range raw.Sources {
		m.sources = append(m.sources, root+s)
	}
	lines, err := decodeMappings(raw.Mappings)
	if err != nil {
		return nil, err
	}
	m.lines = lines
	return m, nil
}

// Lookup maps a 1-based generated line and column (as printed in stack
// traces) to the original position. ok is false when nothing maps there.
func (m *Map) Lookup(line, col int) (Position, bool) {
	l, c := line-1, col-1
	if c < 0 {
		c = 0
	}
	if len(m.sections) > 0 {
		i := sort.Search(len(m.sections), func(i int) bool {
			s := m.sections[i]
			return s.line > l || (s.line == l && s.col > c)
		}) - 1
		if i < 0 {
			return Position{}, false
		}
		s := m.sections[i]
		if l == s.line {
			c -= s.col
		}
		return s.m.Lookup(l-s.line+1, c+1)
	}
	if l < 0 || l >= len(m.lines) {
		return Position{}, false
	}
	segs := m.lines[l]
	i := sort.Search(len(segs), func(i int) bool { return segs[i].genCol > c }) - 1
	if i < 0 {
		// Stack traces sometimes point before the first mapped column
		// (e.g. at leading whitespace); use the first segment on the line.
		if len(segs) == 0 {
			return Position{}, false
		}
		i = 0
	}
	seg := segs[i]
	if seg.src < 0 || seg.src >= len(m.sources) {
		return Position{}, false
	}
	p := Position{Source: m.sources[seg.src], Line: seg.srcLine + 1, Col: seg.srcCol + 1}
	if seg.name >= 0 && seg.name < len(m.names) {
		p.Name = m.names[seg.name]
	}
	return p, true
}

func decodeMappings(s string) ([][]segment, error) {
	var (
		lines                         [][]segment
		cur                           []segment
		src, srcLine, srcCol, nameIdx int
	)
	for _, group := range strings.Split(s, ";") {
		cur = nil
		genCol := 0
		for _, seg := range strings.Split(group, ",") {
			if seg == "" {
				continue
			}
			vals, err := decodeVLQ(seg)
			if err != nil {
				return nil, err
			}
			genCol += vals[0]
			sg := segment{genCol: genCol, src: -1, name: -1}
			if len(vals) >= 4 {
				src += vals[1]
				srcLine += vals[2]
				srcCol += vals[3]
				sg.src, sg.srcLine, sg.srcCol = src, srcLine, srcCol
			}
			if len(vals) >= 5 {
				nameIdx += vals[4]
				sg.name = nameIdx
			}
			cur = append(cur, sg)
		}
		sort.SliceStable(cur, func(i, j int) bool { return cur[i].genCol < cur[j].genCol })
		lines = append(lines, cur)
	}
	return lines, nil
}

const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var b64Index = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(b64); i++ {
		t[b64[i]] = int8(i)
	}
	return t
}()

var errVLQ = errors.New("sourcemap: invalid VLQ mapping")

func decodeVLQ(s string) ([]int, error) {
	var out []int
	value, shift := 0, 0
	for i := 0; i < len(s); i++ {
		d := b64Index[s[i]]
		if d < 0 {
			return nil, errVLQ
		}
		value += int(d&31) << shift
		if d&32 != 0 {
			shift += 5
			continue
		}
		neg := value&1 == 1
		value >>= 1
		if neg {
			value = -value
		}
		out = append(out, value)
		value, shift = 0, 0
	}
	if shift != 0 || len(out) == 0 {
		return nil, errVLQ
	}
	return out, nil
}
