package javatable

import (
	"strings"
	"testing"
)

// fixedLoadAllocSchema is the smallest fixed table with no text or collection
// fields: every byte a load lands is a scalar store, so the only allocation the
// measured loop can see is one the load path itself makes.
const fixedLoadAllocSchema = `package allocprobe

fixed table Counted
{
    id int32
    weight int32
}
`

// TestFixedJavaLoadAllocatesOnce pins docs/FIXED-FORM-ALGORITHM.md §4.2 ("the
// codec never allocates") and §4.3 ("the hole list and the default image are
// PER PLAN and computed ONCE ... never per call"): after a first load warms the
// plan, repeated loads of the same file allocate nothing. The JVM's per-thread
// allocation counter measures the generated load over a committed project, not
// a copy of the emitter's text.
func TestFixedJavaLoadAllocatesOnce(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)

	_, out := compileAndRunJavaProbe(t, fixedLoadAllocSchema, "allocprobe", fixedLoadAllocDriver)
	if !strings.Contains(out, "ALLOC 0 CALLS ") {
		t.Fatalf("the generated load did not settle at zero allocation:\n%s", out)
	}
}

const fixedLoadAllocDriver = `import java.lang.management.ManagementFactory;

public final class Main {
    private static com.sun.management.ThreadMXBean bean;

    static void fail(String why) {
        System.out.println("FAILED: " + why);
        System.exit(1);
    }

    public static void main(String[] args) {
        bean = (com.sun.management.ThreadMXBean) ManagementFactory.getThreadMXBean();
        if (!bean.isThreadAllocatedMemorySupported()) {
            fail("the JVM has no per-thread allocation counter");
        }
        if (!bean.isThreadAllocatedMemoryEnabled()) {
            bean.setThreadAllocatedMemoryEnabled(true);
        }

        final allocprobe.CountedFixed.Value written = new allocprobe.CountedFixed.Value();
        written.id = 41;
        written.weight = -7;
        final byte[] wire = new byte[allocprobe.CountedFixed.measure(1)];
        if (allocprobe.CountedFixed.save(new allocprobe.CountedFixed.Value[] { written }, 1, wire) < 0) {
            fail("save");
        }

        final allocprobe.CountedFixed.Value[] values =
                new allocprobe.CountedFixed.Value[] { new allocprobe.CountedFixed.Value() };
        final allocprobe.TableFixed.Report report = new allocprobe.TableFixed.Report();
        final allocprobe.TableFixed.Entry[] plan = allocprobe.TableFixed.plan(64);
        final short[] remap = new short[64];
        final byte[] image = allocprobe.CountedFixed.image();

        int n = allocprobe.CountedFixed.load(values, values.length, wire, plan, remap, image, report);
        if (n != 1 || report.refused || report.damaged || report.unknown != 0 || report.widened != 0) {
            fail("first load n=" + n + " refused=" + report.refused + " damaged=" + report.damaged);
        }
        if (values[0].id != 41 || values[0].weight != -7) {
            fail("first load value");
        }

        // WARM the plan, the JIT and the per-thread scratch.
        for (int i = 0; i < 200000; i++) {
            n += allocprobe.CountedFixed.load(values, values.length, wire, plan, remap, image, report);
        }
        if (n != 200001) {
            fail("warm load count n=" + n);
        }

        // MEASURE the FLOOR over several runs: a one-time compile inside one
        // run adds bytes to that run alone, while bytes a load makes on every
        // call are in all of them, so the least run carries any per-call cost.
        final int iterations = 100000;
        long allocated = Long.MAX_VALUE;
        for (int r = 0; r < 8; r++) {
            int sum = 0;
            final long before = bean.getCurrentThreadAllocatedBytes();
            for (int i = 0; i < iterations; i++) {
                sum += allocprobe.CountedFixed.load(values, values.length, wire, plan, remap, image, report);
            }
            final long run = bean.getCurrentThreadAllocatedBytes() - before;
            if (sum != iterations) {
                fail("measured load count sum=" + sum);
            }
            allocated = Math.min(allocated, run);
        }
        System.out.println("ALLOC " + allocated + " CALLS " + iterations);
        if (allocated != 0) {
            fail("a load allocated " + allocated + " bytes over " + iterations + " calls");
        }
    }
}
`
