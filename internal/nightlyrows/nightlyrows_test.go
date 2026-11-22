package nightlyrows

import (
	"slices"
	"strings"
	"testing"
)

const registry = `# comment

strict-ruby | ruby | (^|/)schema[-_]ruby[-_]generated[-_]strict | make schema-ruby-generated-strict-gate-fast
hostile-c | c | ^internal/ci/hostilecorpus/ | make schema-c-runs-hostile-corpus SERIALIZE_C=../serialize.c
shared | - | ^tables/ | make something
`

func names(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestSelect(t *testing.T) {
	t.Parallel()
	rows, err := Parse(strings.NewReader(registry))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		files []string
		legs  []string
		broad bool
		want  []string
	}{
		{"a ruby codegen diff runs the ruby strict form", []string{"internal/codegen/ruby/emit.go"}, []string{"ruby"}, false, []string{"strict-ruby"}},
		{"the gate own make file selects it with no leg", []string{"make/schema-ruby-generated-strict-gate.mk"}, nil, false, []string{"strict-ruby"}},
		{"the shared corpus selects the c row", []string{"internal/ci/hostilecorpus/corpus/MANIFEST"}, nil, false, []string{"hostile-c"}},
		{"an unrelated diff selects nothing", []string{"docs/FAQ.md"}, nil, false, nil},
		{"the broad rule selects every row", []string{"ir/ir.go"}, nil, true, []string{"strict-ruby", "hostile-c", "shared"}},
		{"a diff to the registry selects every row", []string{RegistryPath}, nil, false, []string{"strict-ruby", "hostile-c", "shared"}},
		{"leg - is never selected by leg", []string{"x"}, []string{"-"}, false, nil},
	} {
		got := names(Select(rows, tc.files, tc.legs, tc.broad))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseRefusesByLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text, want string }{
		{"three fields", "a | b | c\n", ":1: want 4 fields"},
		{"empty field", "a |   | c | d\n", ":1: field 2 is empty"},
		{"duplicate", "a | b | c | d\n\na | b | c | d\n", ":3: row \"a\" is already named on line 1"},
		{"bad regexp", "a | b | ( | d\n", ":1: paths of \"a\""},
		{"no rows", "# only a comment\n", "no rows"},
	} {
		_, err := Parse(strings.NewReader(tc.text))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}
