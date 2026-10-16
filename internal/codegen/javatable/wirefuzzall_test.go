package javatable

// THE ALL-ROOT JAVA WIRE-FUZZ GATE (java-wire-fuzz-every-root).
//
// tables-java-wire-fuzz-all runs the harness's wire-fuzz over the DEFAULT
// roster (testdata/conformance/tables/MANIFEST.txt, the manifest the harness
// uses when no --manifest is passed), carries the zero-coverage greps, and is
// a prerequisite of test-java. This test reads make/java.mk and pins that
// wiring, so a target that regresses to the card manifest, drops a grep, or is
// not run by test-java is refused.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const allRootFuzzTarget = "tables-java-wire-fuzz-all"

func targetBlock(mk, name string) string {
	lines := strings.Split(mk, "\n")
	header := name + ":"
	for i, l := range lines {
		if strings.TrimRight(l, " ") == header || strings.HasPrefix(l, header+" ") {
			var buf []string
			for _, x := range lines[i+1:] {
				if x != "" && !strings.HasPrefix(x, "\t") && !strings.HasPrefix(x, " ") && strings.Contains(x, ":") {
					break
				}
				buf = append(buf, x)
			}
			return strings.Join(buf, "\n")
		}
	}
	return ""
}

// TestWireFuzzAllTargetJava pins that tables-java-wire-fuzz-all is the all-root
// Java wire-fuzz gate: the default roster (no --manifest), the zero-coverage
// greps, and a prerequisite of test-java (java-wire-fuzz-every-root, DONE-WHEN
// 0).
func TestWireFuzzAllTargetJava(t *testing.T) {
	t.Parallel()
	mk, err := os.ReadFile("../../../make/java.mk")
	if err != nil {
		t.Fatal(err)
	}
	block := targetBlock(string(mk), allRootFuzzTarget)
	if block == "" {
		t.Fatalf("%s target is missing from make/java.mk", allRootFuzzTarget)
	}
	if strings.Contains(block, "--manifest") {
		t.Fatalf("%s passes --manifest: it must run the harness's default roster", allRootFuzzTarget)
	}
	for _, want := range []string{
		"wire-fuzz",
		"'0 divergences'",
		"seeds over",
		"mutants'",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("%s is missing the zero-coverage grep %q", allRootFuzzTarget, want)
		}
	}
	for _, line := range strings.Split(string(mk), "\n") {
		if strings.HasPrefix(line, "test-java:") && strings.Contains(line, allRootFuzzTarget) {
			return
		}
	}
	t.Fatalf("test-java does not require %s", allRootFuzzTarget)
}

// TestVariableUnionArmNamedTagCompiles pins the collision the all-root gate
// found at tables/stream/Stream.schema: a union arm named tag was emitted as a
// second tag member of the union Value beside the discriminator, which javac
// refuses ("variable tag is already defined in class Value"), so the default
// roster could not be compiled. The discriminator keeps its name (the retain,
// keyed and field writers read it as .tag), the arm takes another member name,
// and the wire id still hashes the arm's own name (docs/SPEC-TABLES.md §2.6, §3).
func TestVariableUnionArmNamedTagCompiles(t *testing.T) {
	t.Parallel()
	files := generate(t, `package probe

table Chunk
{
    data bytes(8)
    next *Chunk
}

union Frame
{
    chunk Chunk
    tag   uint32
}

table Feed
{
    id    uint32
    frame Frame
}
`)
	src := string(files["FrameVariable.java"])
	if src == "" {
		t.Fatalf("no FrameVariable.java among %d generated files", len(files))
	}
	decl := regexp.MustCompile(`(?m)^\s*public [A-Za-z0-9_<>\[\]]+ tag\b`)
	if n := len(decl.FindAllString(src, -1)); n != 1 {
		t.Fatalf("union Value declares %d members named tag, want only the discriminator", n)
	}
	if !strings.Contains(src, "tagArm") {
		t.Fatalf("the arm named tag has no member of its own in FrameVariable.java")
	}
}
