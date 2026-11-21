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
		{"TestGated/roundtrip", "TestGated/roundtrip", true},
		{"TestGated/roundtrip", "TestGated/other", false},
		{"TestGated", "TestGated/any", true},
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

// TestPatternsAreRewrittenAsGoRewritesThem is the reviewer's F-219: Go
// rewrites each element of a selector before compiling it, a space character
// to `_`, so `-skip 'TestWin Codec'` and `-skip TestWin_Codec` both remove the
// top-level TestWin_Codec. Compiled raw, the spelling with a space matched
// nothing, and a line that skips the test was credited with running it. The
// control: a selector naming another test leaves TestWin_Codec running.
func TestPatternsAreRewrittenAsGoRewritesThem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		pattern, name string
		skips, runs   bool
	}{
		{"TestWin Codec", "TestWin_Codec", true, true},
		{"TestWin_Codec", "TestWin_Codec", true, true},
		{"^TestWin Codec$", "TestWin_Codec", true, true},
		{"TestWin\tCodec", "TestWin_Codec", true, true},
		{"TestWin Codec", "TestWin_Codec", true, true},
		{"TestWin Codec", "TestWin_Codec", true, true},
		{"TestA|TestWin Codec", "TestWin_Codec", true, true},
		{"TestWin Other", "TestWin_Codec", false, false},
		{"TestOther", "TestWin_Codec", false, false},
		{"TestWin/Codec x", "TestWin/Codec_x", true, true},
	} {
		p, err := Compile(tc.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		if got := p.Skips(tc.name); got != tc.skips {
			t.Errorf("-skip %q removes %s = %v, want %v (go test rewrites spaces to _)", tc.pattern, tc.name, got, tc.skips)
		}
		if got := p.Selects(tc.name); got != tc.runs {
			t.Errorf("-run %q selects %s = %v, want %v (go test rewrites spaces to _)", tc.pattern, tc.name, got, tc.runs)
		}
	}
	for in, want := range map[string]string{"a b": "a_b", "a\x01b": `a\x01b`, "plain": "plain", "　": "_"} {
		if got := Rewrite(in); got != want {
			t.Errorf("Rewrite(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSkipsFollowsGo: -skip removes a test only on a match that is not partial.
func TestSkipsFollowsGo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"TestFast", "TestFast", true},
		{"TestFast", "TestFast/sub", true},
		{"TestFast/sub", "TestFast", false},
		{"TestFast/sub", "TestFast/sub", true},
		{"TestFast/sub", "TestFast/other", false},
		{"TestA|TestFast", "TestFast", true},
		{"TestA/x|TestFast/y", "TestFast", false},
		{"TestGated", "TestFast", false},
		{"^TestPythonReflectionStrictTypes$", "TestPythonReflectionStrictTypes", true},
	} {
		p, err := Compile(tc.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		if got := p.Skips(tc.name); got != tc.want {
			t.Errorf("-skip %q removes %s = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}
