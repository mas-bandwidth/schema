package cpptable

// TestCppFixedLoadWalkHasNoUnresetEntryPoint pins ruling R4 on the SHAPE of the
// emitted C++ fixed read: the walk lives inside <T>FixedLoadWalk as a lambda, so
// every `return -1` it takes lands in the one reset that puts ALL `capacity`
// rows back at their declared defaults. The old header also emitted a public
// inline <T>FixedLoadWalkBody — the same walk with no reset — which a caller
// could call directly and keep a partial value after a refusal; its name was in
// no reserved list either (internal/check/check.go, docs/SPEC-TABLES.md). No
// header may emit a second entry point to the walk again.

import (
	"strings"
	"testing"
)

func TestCppFixedLoadWalkHasNoUnresetEntryPoint(t *testing.T) {
	t.Parallel()
	files := generateCPPAll(t, "package probe\nfixed table Fox\n{\n    x int32\n    y float32\n}\n")
	var header string
	for name, b := range files {
		if strings.HasSuffix(name, "Table.h") {
			header = string(b)
		}
	}
	if header == "" {
		t.Fatalf("no Table.h emitted: %v", cppKeysOf(files))
	}
	if strings.Contains(header, "FixedLoadWalkBody") {
		t.Fatalf("the header emits FoxFixedLoadWalkBody, a public walk with no R4 reset; the walk must live inside FoxFixedLoadWalk")
	}
	start := strings.Index(header, "inline int64_t FoxFixedLoadWalk(")
	if start < 0 {
		t.Fatalf("no FoxFixedLoadWalk in the header")
	}
	end := strings.Index(header[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("FoxFixedLoadWalk has no end")
	}
	fn := header[start : start+end+3]
	open := strings.Index(fn, "const int64_t walked = [&]() -> int64_t")
	closeAt := strings.LastIndex(fn, "}();\n")
	if open < 0 || closeAt < open {
		t.Fatalf("FoxFixedLoadWalk does not walk inside a lambda:\n%s", fn)
	}
	if strings.Contains(fn[:open], "return") {
		t.Fatalf("FoxFixedLoadWalk returns before the walk, past the reset:\n%s", fn[:open])
	}
	want := "}();\n    if ( walked < 0 || report->damaged || report->refused )\n    {\n" +
		"        if ( values != NULL )\n        {\n            for ( int64_t k = 0; k < capacity; ++k ) { FoxReset( values[k] ); }\n        }\n" +
		"        return -1;\n    }\n    return walked;\n}\n"
	if fn[closeAt:] != want {
		t.Fatalf("after the walk FoxFixedLoadWalk must reset every row on any refusal; got:\n%s", fn[closeAt:])
	}
}
