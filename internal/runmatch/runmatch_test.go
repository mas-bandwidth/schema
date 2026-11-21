package runmatch

import (
	"slices"
	"testing"
)

// TestTopLevelIsGosSplit pins the port of testing/match.go's splitRegexp.
func TestTopLevelIsGosSplit(t *testing.T) {
	t.Parallel()
	for pattern, want := range map[string][]string{
		"TestA":           {"TestA"},
		"TestA/b":         {"TestA"},
		"TestA/b|TestC/d": {"TestA", "TestC"},
		"TestA|TestB":     {"TestA", "TestB"},
		"TestA/(b|TestC)": {"TestA"},
		"Test(A|B)/x":     {"Test(A|B)"},
		"Test[/|]A/x":     {"Test[/|]A"},
		`Test\/A/x`:       {`Test\/A`},
		"TestA/b/c|TestD": {"TestA", "TestD"},
		"":                {""},
		"/sub":            {""},
		"TestA|":          {"TestA", ""},
	} {
		if got := TopLevel(pattern); !slices.Equal(got, want) {
			t.Errorf("TopLevel(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// TestSelectsFollowsGo holds the selection to what `go test -run` runs.
func TestSelectsFollowsGo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"TestGated", "TestGated", true},
		{"TestGated/roundtrip", "TestGated", true},
		{"TestGated/roundtrip", "TestFast", false},
		{"TestFast/unused|TestGated/roundtrip", "TestGated", true},
		{"TestFast/unused|TestGated/roundtrip", "TestFast", true},
		{"TestFast/(a|TestGated)", "TestGated", false},
		{"^TestFast$", "TestFastest", false},
		{"TestFast", "TestFastest", true},
		{"/roundtrip", "TestAnything", true},
		{"TestMissing", "TestFast", false},
	} {
		s, err := Compile(tc.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		if got := s.Selects(tc.name); got != tc.want {
			t.Errorf("-run %q selects %s = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
	if _, err := Compile("TestA|("); err == nil {
		t.Error("a pattern with an element that does not compile returned no error")
	}
}
