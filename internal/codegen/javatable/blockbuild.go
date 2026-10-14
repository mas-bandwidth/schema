// The BLOCK FORM's BUILD half in Java (docs/SPEC-TABLES.md §19.1), emitted
// beside the read half into the same <Table>Block.java, plus the counts record
// of each block table and the shared refusal type.
//
// This is the Java spelling of internal/codegen/gotable/blockbuild.go: Begin
// writes ONLY the prologue and the triples, never rows or padding, and checks
// storage and every count before any byte of the extent changes. Java has no
// pointer to align, so the storage's alignment is its OFFSET's — a fresh
// byte[] begins at index 0, which is 64-aligned in the only sense Open checks
// (docs/SPEC-TABLES.md §19.2) — and the allocation is the declared maximum
// extent with no +63 tail the C and Go legs need for their address rounding.
package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableBlockRefusalSource is the producer refusal §19.1 names: the array whose
// count violated its declared bound, with the count and the maximum. It is the
// Java spelling of Go's TableBlockRefusal (internal/codegen/gotable/blockbuild.go).
const tableBlockRefusalSource = `// The producer count that violates its declared bound (docs/SPEC-TABLES.md
// §19.1). Begin fills it on a count refusal and clears it on every call, so a
// caller that reuses one refusal sees only the last call's answer.
public final class TableBlockRefusal {
    /** the array whose count violated its declared bound; empty when none did. */
    public String array = "";
    public long count;
    public long maximum;

    public void clear() { array = ""; count = 0; maximum = 0; }

    public void set(String array, long count, long maximum) {
        this.array = array;
        this.count = count;
        this.maximum = maximum;
    }
}
`

// emitCountsFile emits <Table>Counts, the record Begin takes: one int per
// out-of-line array, gathered before Begin — clamping is the caller's policy
// (docs/SPEC-TABLES.md §19.1), the Java spelling of Go's <Table>Counts.
func emitCountsFile(u *ir.Unit, bl *ir.BlockLayout) []byte {
	var b strings.Builder
	n := bl.Table.Name
	fmt.Fprintf(&b, "// %sCounts is gathered before begin; clamping is the caller's policy\n", n)
	fmt.Fprintf(&b, "// (docs/SPEC-TABLES.md §19.1): one count per out-of-line array.\n")
	fmt.Fprintf(&b, "public record %sCounts(", n)
	for i, a := range bl.Arrays {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "int %s", member(a.Field))
	}
	b.WriteString(") {}\n")
	return javaFile(u, "the counts a block begin takes (docs/SPEC-TABLES.md §19.1).", b.String())
}

// blockStartAlignment is the alignment one array's start must meet: the block's
// own 64, widened by the row's (§19.1) — the same number <Table>Block.open
// checks an instance's offset_of against.
func blockStartAlignment(a ir.BlockArray) int64 {
	alignment := int64(ir.BlockAlign)
	if a.ElemAlign() > alignment {
		alignment = a.ElemAlign()
	}
	return alignment
}

// emitBlockBuild emits §19.1's build surface inside <Table>Block.java: the
// storage, begin and the used-extent measure. Cite: docs/SPEC-TABLES.md §19.1
// (Begin writes only the prologue and triples; every count is checked before
// any byte changes; a refusal names the array, the count and the maximum).
func (g *blockGen) emitBlockBuild() {
	bl := g.bl
	name := bl.Table.Name
	g.f("\n    // ---- the BUILD half (docs/SPEC-TABLES.md §19.1) ----\n")
	g.f("    // The storage a PRODUCER owns: one allocation of the maximum-sized extent,\n")
	g.f("    // made exactly once per lifetime. Java's alignment is the OFFSET's — a fresh\n")
	g.f("    // byte[] starts at index 0, which is the only alignment open() checks.\n")
	g.f("    public static final class Storage {\n")
	g.f("        byte[] allocation;\n\n")
	g.f("        /** one allocation per lifetime; a second create refuses (docs/SPEC-TABLES.md §19.1). */\n")
	g.f("        public boolean create() {\n")
	g.f("            if (allocation != null) { return false; }\n")
	g.f("            if (blockMaxBytes > Integer.MAX_VALUE - 8L) { return false; }\n")
	g.f("            allocation = new byte[(int) blockMaxBytes];\n")
	g.f("            return true;\n")
	g.f("        }\n\n")
	g.f("        public void destroy() { allocation = null; }\n\n")
	g.f("        public byte[] data() { return allocation; }\n")
	g.f("    }\n\n")
	g.f("    // Begin writes ONLY the prologue and the triples, never rows or padding\n")
	g.f("    // (docs/SPEC-TABLES.md §19.1). Storage and all counts are checked before any\n")
	g.f("    // byte of the extent changes; a count past its declared maximum refuses and\n")
	g.f("    // names the array, the count and the maximum in the refusal.\n")
	g.f("    public static %sBlock begin(Storage storage, %sCounts counts, TableBlockRefusal refusal) {\n", name, name)
	g.f("        if (refusal != null) { refusal.clear(); }\n")
	g.f("        if (storage == null || storage.allocation == null) { return null; }\n")
	for _, a := range bl.Arrays {
		field := member(a.Field)
		g.f("        if (counts.%s() < 0 || counts.%s() > %d) {\n", field, field, a.Max)
		g.f("            if (refusal != null) { refusal.set(%q, counts.%s(), %d); }\n", a.Field.Name, field, a.Max)
		g.f("            return null;\n")
		g.f("        }\n")
	}
	g.f("        byte[] data = storage.allocation;\n")
	g.f("        putI64(data, 0, TableBlockInfo.magic);\n")
	g.f("        putI64(data, 8, BuildVersion.value);\n")
	g.f("        putI64(data, 16, TableBlockInfo.byteOrder);\n")
	g.f("        long offset = projectionSize;\n")
	for _, a := range bl.Arrays {
		field := member(a.Field)
		alignment := blockStartAlignment(a)
		g.f("        offset = (offset + %d) & ~%dL;\n", alignment-1, alignment-1)
		g.f("        putI64(data, %d, offset);\n", a.OffsetOfOffset)
		g.f("        putI32(data, %d, counts.%s());\n", a.CountOffset, field)
		g.f("        putI32(data, %d, %d);\n", a.StrideOffset, a.Stride)
		g.f("        offset += (long) counts.%s() * %d;\n", field, a.Stride)
	}
	g.f("        return new %sBlock(data, 0, (offset + 63) & ~63L);\n", name)
	g.f("    }\n\n")
	g.f("    // The used extent, measured from the TRIPLES the instance carries rather\n")
	g.f("    // than remembered from begin, rounded to 64 (docs/SPEC-TABLES.md §19.1).\n")
	g.f("    public long blockBytes() {\n")
	g.f("        long used = projectionSize;\n")
	for _, a := range bl.Arrays {
		g.f("        {\n")
		g.f("            long end = TableBytes.i64(data, base + %d)\n", a.OffsetOfOffset)
		g.f("                     + TableBytes.u32(data, base + %d) * TableBytes.u32(data, base + %d);\n", a.CountOffset, a.StrideOffset)
		g.f("            if (end > used) { used = end; }\n")
		g.f("        }\n")
	}
	g.f("        return (used + 63) & ~63L;\n")
	g.f("    }\n\n")
	g.f("    // the build half's explicit little-endian writes — TableBytes' twin (§19:\n")
	g.f("    // the byte order is the writer's own, never the host's)\n")
	g.f("    private static void putI64(byte[] d, int at, long v) {\n")
	g.f("        for (int i = 0; i < 8; i++) { d[at + i] = (byte) (v >>> (i * 8)); }\n")
	g.f("    }\n\n")
	g.f("    private static void putI32(byte[] d, int at, int v) {\n")
	g.f("        for (int i = 0; i < 4; i++) { d[at + i] = (byte) (v >>> (i * 8)); }\n")
	g.f("    }\n")
}
