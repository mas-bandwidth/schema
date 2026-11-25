package main

import (
	"strings"
	"testing"
)

// TestPinSelectsItsSurfaces holds `harness pin -surfaces`: empty pins every
// dump, a subset pins only those, and a name that is not a pin surface (or one
// named twice) is an error rather than a run that pins nothing and passes.
func TestPinSelectsItsSurfaces(t *testing.T) {
	t.Parallel()
	cases := []struct {
		list string
		want []string
		err  string
	}{
		{"", []string{"cook", "block-dump"}, ""},
		{"cook", []string{"cook"}, ""},
		{"block-dump", []string{"block-dump"}, ""},
		{"block-dump, cook", []string{"cook", "block-dump"}, ""},
		{"block", nil, `"block", which is not a pin surface`},
		{"cook,,", nil, `"", which is not a pin surface`},
		{"cook,cook", nil, `"cook" twice`},
	}
	for _, c := range cases {
		got, err := pinSelection(c.list)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("pinSelection(%q) = %v, %v; want an error naming %s", c.list, got, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("pinSelection(%q): %v", c.list, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("pinSelection(%q) = %v, want %v", c.list, got, c.want)
		}
		for _, s := range c.want {
			if !got[s] {
				t.Errorf("pinSelection(%q) = %v, want %s selected", c.list, got, s)
			}
		}
	}
}
