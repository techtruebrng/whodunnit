package sourcemap

import (
	"reflect"
	"testing"
)

func TestDecodeVLQ(t *testing.T) {
	for in, want := range map[string][]int{
		"AAAA":  {0, 0, 0, 0},
		"SAAS":  {9, 0, 0, 9},
		"AACA":  {0, 0, 1, 0},
		"D":     {-1},
		"gB":    {16},
		"AAAAA": {0, 0, 0, 0, 0},
	} {
		got, err := decodeVLQ(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("decodeVLQ(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "g", "A!"} {
		if _, err := decodeVLQ(bad); err == nil {
			t.Errorf("decodeVLQ(%q): expected error", bad)
		}
	}
}

const testMap = `{
  "version": 3,
  "file": "main.js",
  "sourceRoot": "webpack:///",
  "sources": ["./src/discount.ts", "./src/invoice.ts"],
  "names": ["applyDiscount", "total"],
  "mappings": "AAAAA,SAAS;ACCAC,IAAI"
}`

func TestLookup(t *testing.T) {
	m, err := Parse([]byte(testMap))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		line, col int
		want      Position
	}{
		{1, 1, Position{"webpack:///./src/discount.ts", 1, 1, "applyDiscount"}},
		{1, 5, Position{"webpack:///./src/discount.ts", 1, 1, "applyDiscount"}},
		{1, 10, Position{"webpack:///./src/discount.ts", 1, 10, ""}},
		{1, 99, Position{"webpack:///./src/discount.ts", 1, 10, ""}},
		{2, 1, Position{"webpack:///./src/invoice.ts", 2, 10, "total"}},
		{2, 6, Position{"webpack:///./src/invoice.ts", 2, 14, ""}},
	}
	for _, c := range cases {
		got, ok := m.Lookup(c.line, c.col)
		if !ok || got != c.want {
			t.Errorf("Lookup(%d,%d) = %+v, %v; want %+v", c.line, c.col, got, ok, c.want)
		}
	}
	if _, ok := m.Lookup(3, 1); ok {
		t.Error("Lookup past last line should fail")
	}
}

func TestIndexedMap(t *testing.T) {
	data := `{"version":3,"sections":[
	  {"offset":{"line":0,"column":0},"map":{"version":3,"sources":["a.ts"],"names":[],"mappings":"AAAA"}},
	  {"offset":{"line":10,"column":0},"map":{"version":3,"sources":["b.ts"],"names":[],"mappings":"AAAA;AACA"}}
	]}`
	m, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := m.Lookup(1, 1); !ok || p.Source != "a.ts" {
		t.Errorf("section 0: %+v %v", p, ok)
	}
	if p, ok := m.Lookup(12, 1); !ok || p.Source != "b.ts" || p.Line != 2 {
		t.Errorf("section 1: %+v %v", p, ok)
	}
}
