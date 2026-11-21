package gotestmatch

import (
	"reflect"
	"testing"
)

// TestRunsAsGoTestDoes holds the matcher to cases whose answers were read off
// a real `go test` binary (go1.26): the last column is whether the named test
// ran.
func TestRunsAsGoTestDoes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		run, skip string
		name      []string
		want      bool
	}{
		{"", "", []string{"TestA"}, true},
		{"TestA", "", []string{"TestA"}, true},
		{"TestA", "", []string{"TestAB"}, true}, // unanchored
		{"^TestA$", "", []string{"TestAB"}, false},
		{"TestA/sub", "", []string{"TestA"}, true}, // the parent runs for its subtest
		{"TestA/sub", "", []string{"TestA", "sub"}, true},
		{"TestA/sub", "", []string{"TestA", "other"}, false},
		{"TestA/sub", "", []string{"TestB"}, false},
		{"TestA|TestB", "", []string{"TestB"}, true},
		{"TestX/child|TestB", "", []string{"TestB"}, true},
		{"TestX/child|TestB", "", []string{"TestX"}, true},
		{"TestX/child|TestB", "", []string{"TestC"}, false},
		{"^(TestA|TestB)$", "", []string{"TestB"}, true}, // `|` inside parentheses is the regexp's own
		{"Test[A/|B]", "", []string{"TestB"}, true},      // and inside brackets
		{"/sub", "", []string{"TestA"}, true},            // an empty level matches every name
		{"", "TestA", []string{"TestA"}, false},
		{"", "TestA", []string{"TestB"}, true},
		{"", "TestA/sub", []string{"TestA"}, true}, // only the subtest is skipped
		{"", "TestA/sub", []string{"TestA", "sub"}, false},
		{"", "TestMissing/child|TestWin", []string{"TestWin"}, false},
		{"", "TestWin/child|TestMissing", []string{"TestWin"}, true},
		{"", "^(TestA|TestB)$", []string{"TestB"}, false},
		{"Test[A-C]", "TestB", []string{"TestB"}, false},
		{"Test[A-C]", "TestB", []string{"TestC"}, true},
		{"TestA", "", []string{"TestA", "has space"}, true},
		{"TestA/has_space", "", []string{"TestA", "has space"}, true},
		// F-219: the selector is rewritten as names are, so a space in it is
		// an underscore. The last row is the control: an unrelated selector.
		{"", "TestWin Codec", []string{"TestWin_Codec"}, false},
		{"", "TestWin_Codec", []string{"TestWin_Codec"}, false},
		{"", "Different Name", []string{"TestWin_Codec"}, true},
		{"TestWin Codec", "", []string{"TestWin_Codec"}, true},
		{"TestA/has space", "", []string{"TestA", "has space"}, true},
		{"TestA/has\tspace", "", []string{"TestA", "has space"}, true},
	} {
		m, err := New(c.run, c.skip)
		if err != nil {
			t.Fatalf("New(%q, %q): %v", c.run, c.skip, err)
		}
		if got := m.Runs(c.name...); got != c.want {
			t.Errorf("-run %q -skip %q: Runs(%q) = %v, want %v", c.run, c.skip, c.name, got, c.want)
		}
	}
}

func TestSplitIsTestingSplitRegexp(t *testing.T) {
	t.Parallel()
	for in, want := range map[string][][]string{
		"":                    {{""}},
		"a":                   {{"a"}},
		"a/b":                 {{"a", "b"}},
		"a|b":                 {{"a"}, {"b"}},
		"a/b|c":               {{"a", "b"}, {"c"}},
		"(a|b)/[/|]":          {{"(a|b)", "[/|]"}},
		`a\/b`:                {{`a\/b`}},
		"a]/b":                {{"a]", "b"}},
		"^TestA$|^TestB$/x/y": {{"^TestA$"}, {"^TestB$", "x", "y"}},
	} {
		if got := Split(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Split(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPatternThatDoesNotCompileIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := New("Test(", ""); err == nil {
		t.Error("-run 'Test(' compiled")
	}
	if _, err := New("", "TestA/[z-a]"); err == nil {
		t.Error("-skip 'TestA/[z-a]' compiled")
	}
}
