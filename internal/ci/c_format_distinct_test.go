package ci

import (
	"slices"
	"strings"
	"testing"
)

// TestCFormatFormatsEachDistinctFileOnce: the C format gate formats one file
// per (content, base name) in a run and carries that verdict to every path with
// the same content and name, and only to those. Three copies of one drifting
// header under one name are one formatter run and three verdicts: the listed
// copy rides the ratchet and the two unlisted copies are each named red. The
// same bytes under another name are formatted on their own, and a listed copy
// of a clean file is told to leave the ratchet while its unlisted twin is not
// named at all.
func TestCFormatFormatsEachDistinctFileOnce(t *testing.T) {
	t.Parallel()
	g := newCFormatGate(t)
	g.write("tree/u1/SchemaRuntime.h", "int DRIFT;\n")
	g.write("tree/u2/SchemaRuntime.h", "int DRIFT;\n")
	g.write("tree/u3/SchemaRuntime.h", "int DRIFT;\n")
	g.write("tree/u3/Other.h", "int DRIFT;\n")
	g.write("tree/u1/Clean.c", "int clean;\n")
	g.write("tree/u2/Clean.c", "int clean;\n")
	g.write("allow.txt", "tree/u1/SchemaRuntime.h\ntree/u2/Clean.c\n")

	out, green, calls := g.run("", "allow.txt", "tree")
	if green {
		t.Fatalf("the gate is green over two unlisted drifting copies:\n%s", out)
	}
	keys := map[string]int{}
	for _, c := range calls {
		name := c[strings.LastIndex(c, "/")+1:]
		keys[name]++
	}
	if len(calls) != 3 || keys["SchemaRuntime.h"] != 1 || keys["Other.h"] != 1 || keys["Clean.c"] != 1 {
		t.Fatalf("formatter runs %v, want one each for SchemaRuntime.h, Other.h and Clean.c\n%s", calls, out)
	}
	for _, want := range []string{
		"clang-format drift in tree/u2/SchemaRuntime.h",
		"clang-format drift in tree/u3/SchemaRuntime.h",
		"clang-format drift in tree/u3/Other.h",
		"tree/u2/Clean.c is clean now",
		"tables-c-clean: 6 files, 3 distinct",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the gate does not say %q:\n%s", want, out)
		}
	}
	for _, never := range []string{"tree/u1/SchemaRuntime.h", "tree/u1/Clean.c", "no verdict"} {
		if strings.Contains(out, never) {
			t.Errorf("the gate names %q, which it must not:\n%s", never, out)
		}
	}

	// the plan's C shards over the same tree: each key is formatted by exactly
	// one row, and each of the unsharded gate's red lines is said by exactly
	// one row (the row holding that path's key)
	specs := shardsOf(t, planCleanRows(t, "internal/codegen/ctable/wire.go"), "c")
	formatted := map[string]int{}
	said := map[string]int{}
	for _, spec := range specs {
		out, _, calls := g.run(spec, "allow.txt", "tree")
		for _, c := range calls {
			formatted[c[strings.LastIndex(c, "/")+1:]]++
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "tables-c-clean: clang-format drift") || strings.Contains(line, "is clean now") {
				said[line]++
			}
		}
	}
	if len(formatted) != 3 || formatted["SchemaRuntime.h"] != 1 || formatted["Other.h"] != 1 || formatted["Clean.c"] != 1 {
		t.Errorf("the %d shards formatted %v, want each of the three keys once", len(specs), formatted)
	}
	if len(said) != 4 {
		t.Errorf("the shards said %d distinct red lines, want the unsharded gate's 4: %v", len(said), said)
	}
	for line, n := range said {
		if n != 1 || !strings.Contains(out, line) {
			t.Errorf("%q was said by %d shards (want 1) or not by the unsharded gate", line, n)
		}
	}

	// the same trees, every drifting path listed and the clean one not: green,
	// with all four drifting paths on the ratchet count
	g.write("allow.txt", "tree/u1/SchemaRuntime.h\ntree/u2/SchemaRuntime.h\ntree/u3/SchemaRuntime.h\ntree/u3/Other.h\n")
	out, green, calls = g.run("", "allow.txt", "tree")
	if !green || len(calls) != 3 || !strings.Contains(out, "4 table-emitter files still on the ratchet") {
		t.Fatalf("all drift listed: green=%v with %d formatter runs, want green with 3 and 4 on the ratchet\n%s", green, len(calls), out)
	}
	if !slices.ContainsFunc(calls, func(c string) bool { return strings.HasSuffix(c, "Other.h") }) {
		t.Errorf("Other.h, the same bytes as SchemaRuntime.h under another name, was not formatted: %v", calls)
	}
}
