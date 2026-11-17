// The schema-spec-pin-s0 pins' compiler half (docs/SPEC-TABLES.md, the
// opening): the table-declaration backend every language carries (TA-2), Go's
// enum-keyed storage and TableKeyed helper (TA-11), Go's generated layout
// refusal (TA-12), and Java's `open` refusing a foreign block or cook (TA-14).
//
// These live in a file of their own so the s0 card adds its assertions and
// edits no shared test. Each reads the emitted source for the spelling the
// sentence names, which is what the earlier pins failed to do.
package compiler

import (
	"strings"
	"testing"
)

// TestEveryTargetHasATableBackend: a unit declaring an ordinary table is
// accepted by EVERY generated language — refusal is scoped to a construct a
// backend does not carry, never to the table declaration itself.
func TestEveryTargetHasATableBackend(t *testing.T) {
	t.Parallel()
	c := New()
	u := unitFromSource(t, tableSrc)
	for _, target := range c.Targets() {
		if _, err := c.Generate(u, target, Options{}); err != nil {
			t.Errorf("--lang %s refused a variable table declaration: %v", target, err)
		}
	}
}

// TestGoTableKeyedShiftsAndRefusesBothEnds: an enum-keyed array's Go storage
// IS the plain [E.Max]T the schema means, and the shift and both refusals live
// in one TableKeyed helper — None at or below zero, and anything past E.Max.
func TestGoTableKeyedShiftsAndRefusesBothEnds(t *testing.T) {
	t.Parallel()
	files := goFiles(t, runtimeSrc)
	src := string(files["ProbeTable.go"])
	if src == "" {
		t.Fatal("--lang go emitted no ProbeTable.go for a unit with an enum-keyed array")
	}
	for _, want := range []string{
		"[KindMax]int32",
		"func TableKeyed[T any](slots []T, key int) *T {",
		"if key <= 0 || key > len(slots) {",
		"return &slots[key-1]",
		"None keys no slot, and neither does a value past E.Max",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("ProbeTable.go is missing %q — the enum-keyed storage and its one helper are the sentence's claim", want)
		}
	}
}

// TestGoLayoutCheckRefusesNamingRecordFieldAndBothNumbers: Go's layout
// contract is a generated init() that REFUSES, naming the record, the field
// and both numbers. It covers the block and cook closures, each with a size
// check (the record and the two byte counts) and an offset check (the field
// and the two offsets).
func TestGoLayoutCheckRefusesNamingRecordFieldAndBothNumbers(t *testing.T) {
	t.Parallel()
	files := goFiles(t, pointerSrc)
	block := string(files["ProbeBlock.go"])
	cook := string(files["ProbeCook.go"])
	if block == "" || cook == "" {
		t.Fatalf("--lang go emitted block=%d cook=%d bytes; both closures carry a layout check", len(block), len(cook))
	}
	for _, tc := range []struct {
		file string
		src  string
		want []string
	}{
		{
			file: "ProbeBlock.go",
			src:  block,
			want: []string{
				"func init() {",
				`tableBlockLayoutSize("LeafBlockProjection", unsafe.Sizeof(LeafBlockProjection{}),`,
				`tableBlockLayoutOffset("LeafBlockProjection.`,
				`"schema block layout: %s is %d bytes in this runtime and %d in the schema "`,
				`"schema block layout: %s sits at %d in this runtime and %d in the schema "`,
			},
		},
		{
			file: "ProbeCook.go",
			src:  cook,
			want: []string{
				"func init() {",
				`tableCookLayoutSize("NodeRow", unsafe.Sizeof(NodeRow{}),`,
				`tableCookLayoutOffset("NodeRow.`,
				`"schema cook layout: %s is %d bytes in this runtime and %d in the layout "`,
				`"schema cook layout: %s sits at %d in this runtime and %d in the layout "`,
			},
		},
	} {
		for _, want := range tc.want {
			if !strings.Contains(tc.src, want) {
				t.Errorf("%s is missing %q — the layout contract names the record, the field and both numbers", tc.file, want)
			}
		}
	}
}

// TestJavaOpenRefusesAForeignBlockOrCook: what refuses a foreign block or cook
// is `open`, and it compares every number the instance carries against this
// build's. The bodies below are the generated open methods, not the file's
// prose: the magic and byte order that establish the file's reading, the
// build version this reader was generated for, and every offset, pitch, count
// and extent the region carries.
func TestJavaOpenRefusesAForeignBlockOrCook(t *testing.T) {
	t.Parallel()
	files := javaFiles(t, tableSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	blocks, cooks := 0, 0
	for name, data := range files {
		if strings.HasPrefix(name, "Table") {
			continue // the runtime types, not a table's generated reader
		}
		switch {
		case strings.HasSuffix(name, "Block.java"):
			blocks++
			src := javaOpenBody(string(data), "public static "+strings.TrimSuffix(name, ".java")+" open(byte[] data, int offset, long bytes) {")
			if src == "" {
				t.Fatalf("%s has no generated open method", name)
			}
			src = javaOpenWithCheck(t, name, string(data), src, "private static long openCheck(byte[] data, int offset, long bytes) {")
			for _, want := range []string{
				"TableBlockInfo.magic", "BuildVersion.value", "TableBlockInfo.byteOrder",
				"stride !=", "count >", "Long.compareUnsigned(offsetOf,", "bytes",
			} {
				if !strings.Contains(src, want) {
					t.Errorf("%s open does not compare %q against this build's", name, want)
				}
			}
		case strings.HasSuffix(name, "Cook.java"):
			cooks++
			src := javaOpenBody(string(data), "public static "+strings.TrimSuffix(name, ".java")+" open(byte[] data, int offset, long length) {")
			if src == "" {
				t.Fatalf("%s has no generated open method", name)
			}
			src = javaOpenWithCheck(t, name, string(data), src, "private static long openCheck(byte[] data, int offset, long length) {")
			for _, want := range []string{
				"TableCookInfo.magic", "BuildVersion.value", "TableCookInfo.byteOrder",
				"dataLength", "attribution", "alignment", "rootSize",
			} {
				if !strings.Contains(src, want) {
					t.Errorf("%s open does not compare %q against this build's", name, want)
				}
			}
		}
	}
	if blocks == 0 || cooks == 0 {
		t.Fatalf("the scan found %d block and %d cook readers — it is the scan, not the emitter, that is broken", blocks, cooks)
	}
}

// javaOpenWithCheck follows `open` into the openCheck it delegates to: the
// Java reader keeps every comparison in one private openCheck so that `open`
// and `openReason` refuse on the same checks. When open calls openCheck the
// claim is about both bodies together; an open that calls it but whose file
// has no such method is a broken scan, not a pass.
func javaOpenWithCheck(t *testing.T, name, file, open, checkSignature string) string {
	t.Helper()
	if !strings.Contains(open, "openCheck(") {
		return open
	}
	check := javaOpenBody(file, checkSignature)
	if check == "" {
		t.Fatalf("%s open calls openCheck but the file has no %q", name, checkSignature)
	}
	return open + "\n" + check
}

// javaOpenBody returns the text of a Java method from its signature through its
// matching closing brace, so a claim about `open` reads the method and not a
// comment elsewhere in the file.
func javaOpenBody(src, signature string) string {
	at := strings.Index(src, signature)
	if at < 0 {
		return ""
	}
	open := strings.Index(src[at:], "{")
	if open < 0 {
		return ""
	}
	start := at + open
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	return ""
}
