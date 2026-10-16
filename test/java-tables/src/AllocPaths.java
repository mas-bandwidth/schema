/*
    THE JAVA ALLOCATION GATE'S ROWS (docs/PORTING.md I1, M1, I14).

    Main's `allocator` mode decides WHETHER this JDK may certify (the pin, the
    counter, the observation escape); this file is WHAT is measured. Every row is
    a real path through the generated code over a committed corpus file, counted
    with the JVM's per-thread allocation counter
    (com.sun.management.ThreadMXBean#getCurrentThreadAllocatedBytes) — the
    instrument the emitter cannot influence. The bytes are counted, not sampled:
    the counter is exact to the byte for the measuring thread, including inside a
    TLAB.

    Each row is WARMED until its per-iteration byte count settles (the same
    count over SETTLE_RUNS consecutive blocks: the interpreter, C1 and C2 each
    allocate differently, escape analysis in particular can only remove an
    allocation once C2 has compiled the loop) and then measured over a stated
    number of iterations.

      1 block-walk   open the committed block image, then every row of every
                     table through the generated <table>Count/<table>At and the
                     row's descriptor, every inline field read through
                     TableBytes. Ceiling: ONE handle per open (its size read
                     off the handle class's own fields), 0 bytes per row.
      2 cook-walk    open the committed cook, then every node the root reaches
                     through the reader's own `at`, every field read. Ceiling:
                     ONE handle per open, 0 bytes per node.
      3 load-measure the variable form's loadMeasure over committed wires.
                     M1 wants 0 and it is NOT 0 (a defect in the emitter,
                     named at measureCeiling); the ceiling is its floor.
      4 load, save   the variable form's load and save over the same wires,
                     each at its measured floor with its reason (loadCeiling,
                     saveCeiling): load answers a fresh Value tree by
                     contract; save's MiB scratch per node is a defect. A
                     regression of one byte past a floor reds the row.

    THE NEGATIVE CONTROL (make tables-java-alloc-negative-control):
    SCHEMA_JAVA_ALLOC_PLANT_ROW_WALK=1 plants ONE allocation per row in row 1's
    walk — here, in the harness, never in generated code — and the gate must go
    red on row 1 by name while row 2 stays green. An instrument that cannot
    localize a planted allocation is not watching the paths it names.

    The handles' per-open bytes are held EXACT by storing each handle in a static
    field: an escaping handle cannot be scalar-replaced, so the row measures the
    handle the library allocates rather than whatever escape analysis left.
*/

import java.io.IOException;
import java.lang.management.ManagementFactory;
import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import java.nio.file.Files;
import java.nio.file.Paths;

final class AllocPaths {
    private AllocPaths() {}

    // the escape sinks: what a row keeps so nothing it reads is dead code and
    // nothing it allocates can be scalar-replaced out of the count
    static volatile Object keep;
    static long sink;

    static final boolean PLANT_ROW_WALK = "1".equals(System.getenv("SCHEMA_JAVA_ALLOC_PLANT_ROW_WALK"));

    static final int BLOCK_ITERATIONS = 2_000;
    static final int COOK_ITERATIONS = 50_000;
    static final int VARIABLE_ITERATIONS = 20_000;
    // save allocates a MiB-scale scratch per node (see saveCeiling), so it is
    // measured over fewer calls: the count is exact either way
    static final int SAVE_ITERATIONS = 50;
    static final int SETTLE_RUNS = 3;
    static final int SETTLE_MAX_BLOCKS = 400;

    interface Body { void run(); }

    private static com.sun.management.ThreadMXBean bean;

    private static long counter() {
        return bean == null ? 0L : bean.getCurrentThreadAllocatedBytes();
    }

    private static long measure(Body body, int iterations) {
        long before = counter();
        for (int i = 0; i < iterations; i++) {
            body.run();
        }
        return counter() - before;
    }

    // WARM until the block total is the same SETTLE_RUNS blocks running (exact
    // bytes, not a rounded rate), then measure FLOOR_RUNS times over
    // `iterations` and answer the least per-iteration count: the FLOOR, the
    // bytes the compiled path allocates on every call. A compile landing inside
    // one measurement adds bytes to that one only; an allocation the path
    // makes on every call is in all of them, so the least still carries it.
    // Answers a negative number when the row never settled.
    static final int FLOOR_RUNS = 5;

    private static double settled(Body body, int iterations, int[] blocksOut) {
        int block = Math.max(iterations / 10, 1);
        long last = Long.MIN_VALUE;
        int same = 0;
        int blocks = 0;
        while (same < SETTLE_RUNS) {
            if (blocks++ >= SETTLE_MAX_BLOCKS) {
                blocksOut[0] = blocks;
                return -1;
            }
            long total = measure(body, block);
            if (total == last) {
                same++;
            } else {
                last = total;
                same = 1;
            }
        }
        blocksOut[0] = blocks;
        long least = Long.MAX_VALUE;
        for (int r = 0; r < FLOOR_RUNS; r++) {
            least = Math.min(least, measure(body, iterations));
        }
        return (double) least / iterations;
    }

    // ---- a handle's size, read off its class: the header and every instance
    // field at this VM's reference width, rounded to the object alignment
    private static long handleBytes(Class<?> type) {
        boolean compressedOops = true;
        boolean compressedClass = true;
        long alignment = 8;
        try {
            com.sun.management.HotSpotDiagnosticMXBean hs =
                    ManagementFactory.getPlatformMXBean(com.sun.management.HotSpotDiagnosticMXBean.class);
            compressedOops = Boolean.parseBoolean(hs.getVMOption("UseCompressedOops").getValue());
            compressedClass = Boolean.parseBoolean(hs.getVMOption("UseCompressedClassPointers").getValue());
            alignment = Long.parseLong(hs.getVMOption("ObjectAlignmentInBytes").getValue());
        } catch (RuntimeException e) {
            // the defaults above are HotSpot's on a heap under 32 GB
        }
        long bytes = compressedClass ? 12 : 16;
        for (Class<?> c = type; c != null && c != Object.class; c = c.getSuperclass()) {
            for (Field f : c.getDeclaredFields()) {
                if (Modifier.isStatic(f.getModifiers())) {
                    continue;
                }
                Class<?> t = f.getType();
                if (t == long.class || t == double.class) { bytes += 8; }
                else if (t == int.class || t == float.class) { bytes += 4; }
                else if (t == short.class || t == char.class) { bytes += 2; }
                else if (t == byte.class || t == boolean.class) { bytes += 1; }
                else { bytes += compressedOops ? 4 : 8; }
            }
        }
        return (bytes + alignment - 1) / alignment * alignment;
    }

    // ---- row 1: the block walk

    private static void readBlockRecord(byte[] data, int at, blockdemo.TableBlockInfo info) {
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.outOfLine) {
                continue;
            }
            if (f.counted) {
                sink += blockdemo.TableBytes.i32(data, at + f.countOffset);
            }
            int slots = f.isArray ? f.arrayBound : 1;
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                blockdemo.TableBlockInfo element = f.element();
                if (element != null) {
                    readBlockRecord(data, value, element);
                } else if (f.elemSize > 0) {
                    switch (f.elemSize) {
                        case 1: sink += blockdemo.TableBytes.u8(data, value); break;
                        case 2: sink += blockdemo.TableBytes.u16(data, value); break;
                        case 4: sink += blockdemo.TableBytes.u32(data, value); break;
                        default: sink += blockdemo.TableBytes.i64(data, value); break;
                    }
                }
            }
            if (f.optional) {
                sink += blockdemo.TableBytes.bool(data, at + f.presentOffset) ? 1 : 0;
            }
        }
    }

    // THE PLANT, and the only place it can fire: one array per row of the walk
    private static void row(byte[] data, int at, blockdemo.TableBlockInfo info) {
        if (PLANT_ROW_WALK) {
            keep = new long[1];
        }
        readBlockRecord(data, at, info);
    }

    // every row of every table, through the generated count and row accessors
    private static long walkBlock(byte[] image) {
        blockdemo.RenderFrameBlock h = blockdemo.RenderFrameBlock.open(image, 0, image.length);
        if (h == null) {
            return -1;
        }
        keep = h;
        byte[] d = h.data();
        long rows = 0;
        int n;
        n = h.camerasCount();
        for (int i = 0; i < n; i++) { row(d, h.camerasAt(i), blockdemo.RenderCameraRow.blockInfo()); }
        rows += n;
        n = h.shipsCount();
        for (int i = 0; i < n; i++) { row(d, h.shipsAt(i), blockdemo.RenderShipRow.blockInfo()); }
        rows += n;
        n = h.turretsCount();
        for (int i = 0; i < n; i++) { row(d, h.turretsAt(i), blockdemo.RenderTurretRow.blockInfo()); }
        rows += n;
        n = h.missilesCount();
        for (int i = 0; i < n; i++) { row(d, h.missilesAt(i), blockdemo.RenderMissileRow.blockInfo()); }
        rows += n;
        n = h.dynamicPropsCount();
        for (int i = 0; i < n; i++) { row(d, h.dynamicPropsAt(i), blockdemo.RenderDynamicPropRow.blockInfo()); }
        rows += n;
        n = h.staticPropsCount();
        for (int i = 0; i < n; i++) { row(d, h.staticPropsAt(i), blockdemo.RenderStaticPropRow.blockInfo()); }
        rows += n;
        n = h.cosmeticPropsCount();
        for (int i = 0; i < n; i++) { row(d, h.cosmeticPropsAt(i), blockdemo.RenderCosmeticPropRow.blockInfo()); }
        rows += n;
        n = h.lasersCount();
        for (int i = 0; i < n; i++) { row(d, h.lasersAt(i), blockdemo.RenderLaserRow.blockInfo()); }
        rows += n;
        n = h.explosionsCount();
        for (int i = 0; i < n; i++) { row(d, h.explosionsAt(i), blockdemo.RenderExplosionRow.blockInfo()); }
        rows += n;
        return rows;
    }

    // ---- row 2: the cook walk
    //
    // No HashSet and no recursion over pointers: the visited set is a stamp per
    // region byte and the work list is an array, both allocated ONCE before the
    // row is measured, so the walk itself owns no memory and what the counter
    // sees is the library's.

    private static int[] stamp = new int[0];
    private static int epoch;
    private static int[] stackAt = new int[0];
    private static graphdemo.TableCookInfo[] stackInfo = new graphdemo.TableCookInfo[0];
    private static int top;

    private static void push(int at, graphdemo.TableCookInfo info) {
        if (top == stackAt.length) {
            throw new IllegalStateException("the cook walk's work list is full; size it from the region");
        }
        stackAt[top] = at;
        stackInfo[top] = info;
        top++;
    }

    // one record's fields: scalars read, inline records descended into, and
    // every pointer slot resolved through the reader's `at` and queued
    private static void readCookRecord(graphdemo.SceneCook cook, int at, graphdemo.TableCookInfo info, int depth) {
        byte[] data = cook.data();
        if (depth > 64) {
            return;
        }
        for (graphdemo.TableCookFieldInfo f : info.fields) {
            int slots = f.isArray ? f.arrayBound : 1;
            if (f.countOffset >= 0) {
                int used = graphdemo.TableBytes.i32(data, at + f.countOffset);
                sink += used;
                if (f.isArray && used >= 0 && used < slots) {
                    slots = used;
                }
            }
            graphdemo.TableCookInfo record = f.record();
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                if (f.isPointer) {
                    if (record == null) {
                        continue;
                    }
                    int target = cook.at(value, record.size);
                    if (target >= 0) {
                        push(target, record);
                    }
                } else if (record != null) {
                    readCookRecord(cook, value, record, depth + 1);
                } else {
                    switch (f.elemSize) {
                        case 1: sink += graphdemo.TableBytes.u8(data, value); break;
                        case 2: sink += graphdemo.TableBytes.u16(data, value); break;
                        case 4: sink += graphdemo.TableBytes.u32(data, value); break;
                        default: sink += graphdemo.TableBytes.i64(data, value); break;
                    }
                }
            }
            if (f.presentOffset >= 0) {
                sink += graphdemo.TableBytes.bool(data, at + f.presentOffset) ? 1 : 0;
            }
        }
    }

    private static long walkCook(byte[] file) {
        graphdemo.SceneCook cook = graphdemo.SceneCook.open(file, 0, file.length);
        if (cook == null) {
            return -1;
        }
        keep = cook;
        epoch++;
        top = 0;
        long nodes = 0;
        push(cook.root(), graphdemo.SceneCook.type());
        while (top > 0) {
            top--;
            int at = stackAt[top];
            graphdemo.TableCookInfo info = stackInfo[top];
            if (stamp[at] == epoch) {
                continue;
            }
            stamp[at] = epoch;
            nodes++;
            readCookRecord(cook, at, info, 0);
        }
        return nodes;
    }

    // ---- the rows

    private static boolean failed;
    private static boolean certify;
    private static final StringBuilder failures = new StringBuilder();

    private static void verdict(String row, double perIteration, double ceiling, String unit,
                                int iterations, int blocks, String detail, String reason) {
        boolean ok = perIteration >= 0 && perIteration <= ceiling;
        String line = String.format("alloc %-22s %10.3f bytes/iter  ceiling %8.0f  least of %d runs of %d iterations"
                        + " (settled after %d blocks)  %s  — %s%s",
                row, perIteration, ceiling, FLOOR_RUNS, iterations, blocks, detail, reason,
                ok ? "  OK" : "  RED");
        System.out.println(line);
        if (!ok) {
            failed = true;
            failures.append("  row ").append(row).append(": ")
                    .append(perIteration < 0 ? "never settled" : String.format("%.3f bytes/iter", perIteration))
                    .append(" over a ceiling of ").append((long) ceiling).append(' ').append(unit).append('\n');
        }
    }

    private static void blockRow(byte[] image) {
        long rows = walkBlock(image);
        if (rows <= 0) {
            fail("1 block-walk: the committed block image did not open, or holds no rows");
            return;
        }
        long handle = handleBytes(blockdemo.RenderFrameBlock.class);
        int[] blocks = new int[1];
        double per = settled(() -> sink += walkBlock(image), BLOCK_ITERATIONS, blocks);
        double perRow = per < 0 ? -1 : (per - handle) / rows;
        verdict("1 block-walk", per, handle, "bytes/iter", BLOCK_ITERATIONS, blocks[0],
                String.format("[%d rows/open: %.4f bytes/row beyond the handle]", rows, perRow),
                "ONE RenderFrameBlock handle (" + handle + " bytes) per open, 0 per row");
    }

    private static void cookRow(byte[] file) {
        int region = file.length;
        stamp = new int[region];
        stackAt = new int[region];
        stackInfo = new graphdemo.TableCookInfo[region];
        long nodes = walkCook(file);
        if (nodes <= 1) {
            fail("2 cook-walk: the committed cook did not open, or its root reaches no node");
            return;
        }
        long handle = handleBytes(graphdemo.SceneCook.class);
        int[] blocks = new int[1];
        double per = settled(() -> sink += walkCook(file), COOK_ITERATIONS, blocks);
        double perNode = per < 0 ? -1 : (per - handle) / nodes;
        verdict("2 cook-walk", per, handle, "bytes/iter", COOK_ITERATIONS, blocks[0],
                String.format("[%d nodes/open: %.4f bytes/node beyond the handle]", nodes, perNode),
                "ONE SceneCook handle (" + handle + " bytes) per open, 0 per node");
    }

    // the variable form's rows: one wire, one codec, a ceiling per path
    static final class Wire {
        final String name;
        final byte[] bytes;
        final java.util.function.ToLongFunction<byte[]> loadMeasure;
        final java.util.function.Function<byte[], Object> load;
        final java.util.function.Function<Object, byte[]> save;

        Wire(String name, byte[] bytes, java.util.function.ToLongFunction<byte[]> loadMeasure,
             java.util.function.Function<byte[], Object> load, java.util.function.Function<Object, byte[]> save) {
            this.name = name;
            this.bytes = bytes;
            this.loadMeasure = loadMeasure;
            this.load = load;
            this.save = save;
        }
    }

    // THE VARIABLE FORM'S CEILINGS, bytes per call, set at the floor measured
    // on the pinned JDK (Temurin 21.0.12.1+1, hetzner2, 2026-10-16: the least
    // of FLOOR_RUNS settled runs, over 34 JVM launches) and held there: each
    // ceiling is the highest floor seen plus one byte, rounded up to 64 —
    // C2's escape analysis moves a floor between launches by a Writer or a
    // Reader (32 to 64 bytes) on some rows, and a gate that flakes is a gate
    // nobody reads. The floors seen are beside each case; a steady new
    // allocation of one object per call on the small rows, or one per node on
    // the deep ones, still clears the margin.
    // NONE IS ZERO, and each says why:
    //
    //   loadMeasure  M1 says 0 and this is NOT 0 at BASE: it is a DEFECT in
    //                internal/codegen/javatable/variableform.go (outside this
    //                card's PATHS; repaired and lowered to 0 in the LAND-AFTER
    //                card java-variable-form-save-and-load-measure-do-not-allocate-per-node)
    //                that this gate holds at its measured floor so it cannot grow.
    //                Every call allocates a Report, a Reader and a NodeTable, and
    //                TableVariable.nodesRaw copies every node body into a fresh
    //                byte[] (internal/codegen/javatable/variableform.go,
    //                writeLoadMeasure). C2's escape analysis removes some of
    //                the first three on some launches and not others.
    //   load         the contract: load answers a fresh Value tree, the node
    //                table it resolves through and its Strings. Zero needs the
    //                caller-owned-storage form (docs/PORTING.md M1), which this
    //                port does not emit.
    //   save         a DEFECT held at its floor: save allocates 1 MiB scratch
    //                arrays, two per call and one per node written, and one
    //                per nested body (variableform.go, `new byte[1 << 20]`), then answers a
    //                fresh byte[] of the wire's size.
    private static long measureCeiling(String wire) {
        switch (wire) {
            case "graph_tree.bin": return 832;     // floors 736..776 over 34 launches
            case "graph_deep.bin": return 24512;   // floors 24432..24496
            case "graph_shared.bin": return 640;   // floors 600..624.2
            case "blob_small.bin": return 832;     // floors 704..768
            default: return 0;
        }
    }

    private static long loadCeiling(String wire) {
        switch (wire) {
            case "graph_tree.bin": return 1728;     // floors 1696..1725.1
            case "graph_deep.bin": return 43456;    // floor 43424 on every launch
            case "graph_shared.bin": return 1344;   // floor 1280 on every launch
            case "blob_small.bin": return 1408;     // floor 1376 on every launch
            default: return 0;
        }
    }

    private static long saveCeiling(String wire) {
        switch (wire) {
            case "graph_tree.bin": return 10488832;   // floor 10488808 on every launch
            case "graph_deep.bin": return 276881344;  // floor 276881312 on every launch
            case "graph_shared.bin": return 8391424;  // floors 8391352..8391384
            case "blob_small.bin": return 4196992;    // floors 4196912..4196976
            default: return 0;
        }
    }

    private static Wire wire(String path) throws IOException {
        byte[] bytes = Files.readAllBytes(Paths.get(path));
        String name = Paths.get(path).getFileName().toString();
        if (name.startsWith("blob_")) {
            return new Wire(name, bytes, blobdemo.CatalogVariable::loadMeasure,
                    b -> blobdemo.CatalogVariable.load(b, new blobdemo.TableVariable.Report()),
                    v -> blobdemo.CatalogVariable.save((blobdemo.CatalogVariable.Value) v));
        }
        return new Wire(name, bytes, graphdemo.SceneVariable::loadMeasure,
                b -> graphdemo.SceneVariable.load(b, new graphdemo.TableVariable.Report()),
                v -> graphdemo.SceneVariable.save((graphdemo.SceneVariable.Value) v));
    }

    private static void variableRows(Wire w) {
        if (w.loadMeasure.applyAsLong(w.bytes) < 0) {
            fail("3 load-measure: " + w.name + " is refused by loadMeasure");
            return;
        }
        Object value = w.load.apply(w.bytes);
        if (value == null) {
            fail("4 load: " + w.name + " does not load");
            return;
        }
        byte[] saved = w.save.apply(value);
        if (!java.util.Arrays.equals(saved, w.bytes)) {
            fail("4 save: " + w.name + " does not save back to its committed bytes");
            return;
        }
        int[] blocks = new int[1];
        double per = settled(() -> sink += w.loadMeasure.applyAsLong(w.bytes), VARIABLE_ITERATIONS, blocks);
        verdict("3 load-measure " + w.name, per, measureCeiling(w.name), "bytes/call", VARIABLE_ITERATIONS, blocks[0],
                "[1 record/call]", "DEFECT in variableform.go (outside PATHS; lowered to 0 by java-variable-form-save-and-load-measure-do-not-allocate-per-node) held at its floor, M1 wants 0: a Report, Reader, NodeTable and a copy of every node body per call");
        per = settled(() -> keep = w.load.apply(w.bytes), VARIABLE_ITERATIONS, blocks);
        verdict("4 load " + w.name, per, loadCeiling(w.name), "bytes/call", VARIABLE_ITERATIONS, blocks[0],
                "[1 record/call]", "the floor, by contract: load answers a fresh Value tree (no caller-owned form, M1)");
        per = settled(() -> keep = w.save.apply(value), SAVE_ITERATIONS, blocks);
        verdict("4 save " + w.name, per, saveCeiling(w.name), "bytes/call", SAVE_ITERATIONS, blocks[0],
                "[1 record/call]", "DEFECT held at its floor: 1 MiB scratch arrays per call and per node, then a fresh byte[" + saved.length + "]");
    }

    private static void fail(String what) {
        failed = true;
        failures.append("  ").append(what).append('\n');
        System.out.println("alloc FAILED: " + what);
    }

    /** runs every row; answers the exit code. `counter` is null only in the
     *  observation mode, which reports and never certifies. */
    static int run(com.sun.management.ThreadMXBean counter, boolean certifying, String blockFile,
                   String cookFile, String[] wires) throws IOException {
        bean = counter;
        certify = certifying;
        if (PLANT_ROW_WALK) {
            System.out.println("alloc PLANTED: one allocation per row in row 1's walk (the negative control)");
        }
        // the instrument's own floor, so a non-zero row is the path's and not the counter's
        int[] blocks = new int[1];
        double empty = settled(() -> sink++, 100_000, blocks);
        verdict("0 instrument", empty, 0, "bytes/iter", 100_000, blocks[0], "[an empty body]",
                "the counter reads allocate nothing");
        blockRow(Files.readAllBytes(Paths.get(blockFile)));
        cookRow(Files.readAllBytes(Paths.get(cookFile)));
        for (String w : wires) {
            variableRows(wire(w));
        }
        if (sink == Long.MIN_VALUE) {
            System.out.println(sink);
        }
        if (failed) {
            if (!certify) {
                System.out.println("NOT CERTIFIED: rows over their ceiling in observation mode:");
                System.out.print(failures);
                return 0;
            }
            System.out.flush();
            System.err.println();
            System.err.println("FAILED: the java allocation gate is RED:");
            System.err.print(failures);
            return 1;
        }
        return 0;
    }
}
