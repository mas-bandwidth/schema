package elixir

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirLoopHelperPerOwnerAndField holds the naming rule for the packet
// emitter's array loops: a loop helper belongs to exactly one (owner, field)
// pair, so every array field is written and read at its own element type.
func TestElixirLoopHelperPerOwnerAndField(t *testing.T) {
	t.Parallel()

	dboundary := `package Probe

type FooBar
{
    baz [..4]uint8
}

type Foo
{
    bar_baz [..4]uint32
}
`
	fastSuffix := `package Probe

type Foo
{
    x [..4]uint8
    x_fast [..4]uint32
}
`
	alignSuffix := `package Probe

type Foo
{
    x [..4]uint8
    x_align [..4]uint32
}
`

	cases := []struct {
		name   string
		src    string
		fields []loopHelperField
	}{
		{
			name: "owner and field boundary",
			src:  dboundary,
			fields: []loopHelperField{
				{owner: "FooBar", field: "baz"},
				{owner: "Foo", field: "bar_baz"},
			},
		},
		{
			name: "fast entry suffix",
			src:  fastSuffix,
			fields: []loopHelperField{
				{owner: "Foo", field: "x"},
				{owner: "Foo", field: "x_fast"},
			},
		},
		{
			name: "align entry suffix",
			src:  alignSuffix,
			fields: []loopHelperField{
				{owner: "Foo", field: "x"},
				{owner: "Foo", field: "x_align"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := emitUnit(t, map[string]string{"Case.schema": tc.src})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			src := string(out["Case.ex"])
			if src == "" {
				t.Fatalf("Case.ex missing from generated output")
			}

			defs := loopHelperDefs(src)
			owned := map[string]map[string]bool{}
			for _, f := range tc.fields {
				writeBody := functionBody(t, src, "  def write_"+elixirName(f.owner)+"!(")
				readBody := functionBody(t, src, "  def read_"+elixirName(f.owner)+"_bits(")

				w := calledWriteHelper(t, writeBody, f)
				r := calledReadHelper(t, readBody, f)
				if _, ok := defs[w]; !ok {
					t.Errorf("write helper %s called for %s.%s is not defined", w, f.owner, f.field)
				}
				if _, ok := defs[r]; !ok {
					t.Errorf("read helper %s called for %s.%s is not defined", r, f.owner, f.field)
				}

				names := map[string]bool{}
				loopHelperClosure(w, defs, names)
				loopHelperClosure(r, defs, names)
				owned[f.owner+"."+f.field] = names
			}

			keys := make([]string, 0, len(owned))
			for key := range owned {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for i := 0; i < len(keys); i++ {
				for j := i + 1; j < len(keys); j++ {
					for name := range owned[keys[i]] {
						if owned[keys[j]][name] {
							t.Errorf("loop helper %s belongs to both %s and %s", name, keys[i], keys[j])
						}
					}
				}
			}
		})
	}

	t.Run("a claimed helper name rejects another pair", func(t *testing.T) {
		t.Parallel()

		g := &gen{helperPairs: map[string]helperPair{}}
		g.helperOwner = "FooBar"
		first := &ir.Field{Name: "baz"}
		name := g.helperName("w", first)
		if !g.claimHelper(name, first) {
			t.Fatalf("first claim of %s failed: %v", name, g.err)
		}
		if !g.claimHelper(name, first) {
			t.Fatalf("the same pair must reuse %s: %v", name, g.err)
		}

		g.helperOwner = "Foo"
		second := &ir.Field{Name: "bar_baz"}
		if g.claimHelper(name, second) {
			t.Fatalf("helper name %s must not be shared by Foo.bar_baz and FooBar.baz", name)
		}
		if g.err == nil {
			t.Fatalf("reusing %s for another pair must fail generation", name)
		}
	})
}

type loopHelperField struct {
	owner string
	field string
}

func calledWriteHelper(t *testing.T, body string, f loopHelperField) string {
	t.Helper()
	field := regexp.QuoteMeta(elixirName(f.field))
	re := regexp.MustCompile(`(?s)=\s*(w[A-Za-z0-9_]+)\(\s*(?:value\.` + field + `|value_` + field + `)\b`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no write helper call for %s.%s in:\n%s", f.owner, f.field, body)
	}
	return m[1]
}

func calledReadHelper(t *testing.T, body string, f loopHelperField) string {
	t.Helper()
	field := regexp.QuoteMeta(elixirName(f.field))
	re := regexp.MustCompile(`(?s)\{bits_read, v_` + field + `\}\s*=\s*(r[A-Za-z0-9_]+)\(`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no read helper call for %s.%s in:\n%s", f.owner, f.field, body)
	}
	return m[1]
}

// functionBody returns the text of the function whose definition line starts
// with marker, up to the next top-level def or defp.
func functionBody(t *testing.T, src, marker string) string {
	t.Helper()
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("definition %q missing from:\n%s", marker, src)
	}
	body := src[start:]
	end := -1
	for _, next := range []string{"\n  def ", "\n  defp "} {
		if i := strings.Index(body, next); i >= 0 && (end < 0 || i < end) {
			end = i
		}
	}
	if end >= 0 {
		body = body[:end]
	}
	return body
}

// loopHelperDefs maps each helper name to the helper names its definitions
// mention, so a field owns every helper reachable from the entries it calls.
func loopHelperDefs(src string) map[string]map[string]bool {
	defRe := regexp.MustCompile(`(?m)^  defp ([wr][A-Za-z0-9_]+)\(`)
	locs := defRe.FindAllStringSubmatchIndex(src, -1)
	names := map[string]bool{}
	for _, loc := range locs {
		names[src[loc[2]:loc[3]]] = true
	}
	toks := make(map[string]*regexp.Regexp, len(names))
	for name := range names {
		toks[name] = regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	}
	defs := make(map[string]map[string]bool, len(names))
	for i, loc := range locs {
		name := src[loc[2]:loc[3]]
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		block := src[loc[0]:end]
		if defs[name] == nil {
			defs[name] = map[string]bool{}
		}
		for other, re := range toks {
			if re.MatchString(block) {
				defs[name][other] = true
			}
		}
	}
	return defs
}

func loopHelperClosure(root string, defs map[string]map[string]bool, seen map[string]bool) {
	if seen[root] {
		return
	}
	seen[root] = true
	for next := range defs[root] {
		loopHelperClosure(next, defs, seen)
	}
}
