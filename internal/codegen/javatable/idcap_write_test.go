package javatable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Java. The save-side id table (the Saver's) used to grow to
// 65536 entries; every reader refuses more than 4096.
//
// THE UNIT CANNOT BE THE SHARED tablewire.IdCapSchema HERE: javac refuses the
// generated Java of a unit that spells 4094 variants ("code too large": the
// JVM's 64 KB method limit hits writeKeyedPayload, readFields and the JSON
// walks), so no Java program can drive a Saver to 4097 ids through a schema.
// The test drives the id table the writer interns through, which is where the
// cap lives: a growable table takes 4096 distinct ids, answers the 4097th
// with a refusal (overflow, reference 0) and appends nothing; a repeat of a
// held id still answers its reference.
func TestJavaIdTableCapIs4096(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	const schema = `package probe

table Root { x int32 }
`
	const driver = `import probe.*;

public final class Main {
    private static void check(boolean ok, String msg) {
        if (!ok) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        TableVariable.Ids ids = new TableVariable.Ids(64);
        ids.growable = true;
        for (int i = 1; i <= 4096; i++) {
            check(ids.ref(0x1000L + i) == i, "id " + i + " took the wrong reference");
        }
        check(!ids.overflow && ids.count == 4096, "4096 ids must fit");
        check(ids.ref(0x1000L + 7) == 7, "a held id answers its reference at the bound");
        check(!ids.overflow, "a repeat at the bound must not overflow");
        check(ids.ref(0x9999L) == 0 && ids.overflow, "the 4097th id must be refused");
        check(ids.count == 4096, "a refused id must not be appended");
    }
}
`
	compileAndRunJavaProbe(t, schema, "probe", driver)
}
