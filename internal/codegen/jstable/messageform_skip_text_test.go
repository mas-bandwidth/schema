package jstable

import (
	"strings"
	"testing"
)

func TestJsMessageSkipTextIsBytes(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package probe

fixed table Msg
{
    keep uint32
    text string(16)
}
`
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	src := string(files["ProbeTable.js"])
	if src == "" {
		t.Fatal("missing ProbeTable.js")
	}

	skipFn := jsFn(src, "tableMessageSkip")
	_, after, ok := strings.Cut(skipFn, "case 12:")
	if !ok {
		t.Fatalf("tableMessageSkip has no kind 12 case:\n%s", skipFn)
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, "case ")
	if !ok0 {
		t.Fatalf("kind 12 case has no following case:\n%s", skipFn)
	}
	textCase := before0

	if !strings.Contains(textCase, "r.Align()") {
		t.Errorf("kind 12 skip must align:\n%s", textCase)
	}
	if !strings.Contains(textCase, "r.Skip(Number(n) * 8)") {
		t.Errorf("kind 12 skip must skip count bytes after the alignment:\n%s", textCase)
	}
	if strings.Contains(textCase, "s.Kind") {
		t.Errorf("kind 12 skip must not test the kind inside its own case:\n%s", textCase)
	}
	if strings.Contains(textCase, "16") {
		t.Errorf("kind 12 skip must use width 8, not a wider width:\n%s", textCase)
	}
}
