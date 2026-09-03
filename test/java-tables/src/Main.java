/*
    THE JAVA TABLES LEG, under test (docs/SPEC-TABLES.md).

    Three gates the conformance harness does not hold, because none of them is a
    case — each is a SEARCH or a MEASUREMENT:

      fuzz  <block> <cook>     the READERS' oracle. Mutants of a block image and
                               a cooked file are handed to the generated Open,
                               and the answer must be a REFUSAL or a read that
                               stays inside the array it was given. An index out
                               of bounds is a refusal; an exception escaping into
                               a caller that asked a question is not, and this is
                               what says so.
      alloc <wiredir> <block> <cook>
                               the ALLOCATION gate: bytes per record on every
                               path, from the JVM's own per-thread allocation
                               counter, each held to a NAMED floor — exactly zero
                               on the tolerant wire's read and save and on both
                               accelerators' reads, a stated ceiling where the
                               language forces an allocation. SCHEMA_ALLOC_SABOTAGE
                               adds one allocation per record, which the
                               exact-zero floors must catch.
      soak  <seconds> <wiredir> <block> <cook>
                               the SOAK: read, save, print and parse the corpus in
                               a loop, opening a block and a cook each pass — with
                               the ALLOCATION TABLE re-measured at every sample,
                               not only the heap. A heap gate is a LEAK
                               instrument; it cannot see a per-iteration
                               allocation that is collected, which on a read path
                               is the defect that matters.
      order <le> <be>          the byte-order leg: a cook of THIS reader's order
                               opens and one of the other order refuses.

    Prints OK and exits 0 — no test framework, the exit code is the verdict.
*/

import java.io.IOException;
import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;

public final class Main {
    private Main() {}

    private static String site = "(none)";

    private static void describe(String what) {
        site = what;
    }

    private static void fail(String what) {
        System.out.flush();
        System.err.println();
        System.err.println("FAILED: " + what);
        System.err.println("  site  " + site);
        System.err.println();
        System.exit(1);
    }

    // ---- the reused reading state, which is the whole point of the wire's
    // contract: the value, the report and the reader all belong to the caller,
    // so a loop that reads a million records allocates nothing at all.

    private static final class Instance {
        String name;
        Object value;
        Reader read;      // (bytes) -> boolean, through the reused reader
        Measure measure;
        Save save;
        JsonMeasure jsonMeasure;
        Save json;
        JsonRead jsonRead;
        byte[] wire;
        byte[] scratch;
        byte[] text;
    }

    private interface Reader { boolean read(byte[] bytes); }
    private interface Measure { long of(); }
    private interface Save { long into(byte[] buffer); }
    private interface JsonMeasure { long of(); }
    private interface JsonRead { boolean of(byte[] text); }

    private static final tabledemo.TableReport report = new tabledemo.TableReport();
    private static final tabledemo.TableReader reader = new tabledemo.TableReader();
    private static final tabledemo.TableWriter writer = new tabledemo.TableWriter();

    private static Instance instance(String name, Object value, Reader read, Measure measure, Save save,
                                     JsonMeasure jsonMeasure, Save json, JsonRead jsonRead) {
        Instance i = new Instance();
        i.name = name;
        i.value = value;
        i.read = read;
        i.measure = measure;
        i.save = save;
        i.jsonMeasure = jsonMeasure;
        i.json = json;
        i.jsonRead = jsonRead;
        return i;
    }

    // an ARRAY and not a List, and the reason is the gate below: an enhanced
    // for over a java.util.List allocates an Iterator per pass, and a gate that
    // measured the harness's own iterator would be measuring the harness.
    private static Instance[] corpus(String wireDir) throws IOException {
        List<Instance> out = new ArrayList<>();

        tabledemo.TablesTable.RootConfig root = new tabledemo.TablesTable.RootConfig();
        out.add(instance("root_full", root,
                b -> { reader.reset(b, 0, b.length, report.clear());
                       return tabledemo.TablesTable.rootConfigLoadBody(reader, root); },
                () -> tabledemo.TablesTable.rootConfigMeasure(root),
                buf -> { writer.reset(buf, 0, buf.length);
                         return tabledemo.TablesTable.rootConfigSaveBody(writer, root) ? writer.offset : -1; },
                () -> tabledemo.TablesTable.rootConfigToJsonMeasure(root),
                buf -> tabledemo.TablesTable.rootConfigToJson(root, buf),
                t -> tabledemo.TablesTable.rootConfigFromJson(root, t, report.clear())));

        tabledemo.TablesTable.LoadoutConfig loadout = new tabledemo.TablesTable.LoadoutConfig();
        out.add(instance("loadout_full", loadout,
                b -> { reader.reset(b, 0, b.length, report.clear());
                       return tabledemo.TablesTable.loadoutConfigLoadBody(reader, loadout); },
                () -> tabledemo.TablesTable.loadoutConfigMeasure(loadout),
                buf -> { writer.reset(buf, 0, buf.length);
                         return tabledemo.TablesTable.loadoutConfigSaveBody(writer, loadout) ? writer.offset : -1; },
                () -> tabledemo.TablesTable.loadoutConfigToJsonMeasure(loadout),
                buf -> tabledemo.TablesTable.loadoutConfigToJson(loadout, buf),
                t -> tabledemo.TablesTable.loadoutConfigFromJson(loadout, t, report.clear())));

        tabledemo.TablesTable.ProfileConfig profile = new tabledemo.TablesTable.ProfileConfig();
        out.add(instance("profile_elide", profile,
                b -> { reader.reset(b, 0, b.length, report.clear());
                       return tabledemo.TablesTable.profileConfigLoadBody(reader, profile); },
                () -> tabledemo.TablesTable.profileConfigMeasure(profile),
                buf -> { writer.reset(buf, 0, buf.length);
                         return tabledemo.TablesTable.profileConfigSaveBody(writer, profile) ? writer.offset : -1; },
                () -> tabledemo.TablesTable.profileConfigToJsonMeasure(profile),
                buf -> tabledemo.TablesTable.profileConfigToJson(profile, buf),
                t -> tabledemo.TablesTable.profileConfigFromJson(profile, t, report.clear())));

        tabledemo.KeyedTable.KeyedConfig keyed = new tabledemo.KeyedTable.KeyedConfig();
        out.add(instance("keyed_config", keyed,
                b -> { reader.reset(b, 0, b.length, report.clear());
                       return tabledemo.KeyedTable.keyedConfigLoadBody(reader, keyed); },
                () -> tabledemo.KeyedTable.keyedConfigMeasure(keyed),
                buf -> { writer.reset(buf, 0, buf.length);
                         return tabledemo.KeyedTable.keyedConfigSaveBody(writer, keyed) ? writer.offset : -1; },
                () -> tabledemo.KeyedTable.keyedConfigToJsonMeasure(keyed),
                buf -> tabledemo.KeyedTable.keyedConfigToJson(keyed, buf),
                t -> tabledemo.KeyedTable.keyedConfigFromJson(keyed, t, report.clear())));

        tabledemo.NestedTable.ArchiveConfig archive = new tabledemo.NestedTable.ArchiveConfig();
        out.add(instance("archive", archive,
                b -> { reader.reset(b, 0, b.length, report.clear());
                       return tabledemo.NestedTable.archiveConfigLoadBody(reader, archive); },
                () -> tabledemo.NestedTable.archiveConfigMeasure(archive),
                buf -> { writer.reset(buf, 0, buf.length);
                         return tabledemo.NestedTable.archiveConfigSaveBody(writer, archive) ? writer.offset : -1; },
                () -> tabledemo.NestedTable.archiveConfigToJsonMeasure(archive),
                buf -> tabledemo.NestedTable.archiveConfigToJson(archive, buf),
                t -> tabledemo.NestedTable.archiveConfigFromJson(archive, t, report.clear())));

        tabledemo.WideTable.WideBlob wide = new tabledemo.WideTable.WideBlob();
        out.add(instance("wide_blob", wide,
                b -> { reader.reset(b, 0, b.length, report.clear());
                       return tabledemo.WideTable.wideBlobLoadBody(reader, wide); },
                () -> tabledemo.WideTable.wideBlobMeasure(wide),
                buf -> { writer.reset(buf, 0, buf.length);
                         return tabledemo.WideTable.wideBlobSaveBody(writer, wide) ? writer.offset : -1; },
                () -> tabledemo.WideTable.wideBlobToJsonMeasure(wide),
                buf -> tabledemo.WideTable.wideBlobToJson(wide, buf),
                t -> tabledemo.WideTable.wideBlobFromJson(wide, t, report.clear())));

        for (Instance i : out) {
            i.wire = Files.readAllBytes(Path.of(wireDir, i.name + ".bin"));
            describe("loading " + i.name);
            if (!i.read.read(i.wire)) {
                fail(i.name + " does not load");
            }
            long size = i.measure.of();
            if (size < 0) {
                fail(i.name + " measures as unsaveable");
            }
            i.scratch = new byte[(int) size];
            if (i.save.into(i.scratch) != size) {
                fail(i.name + " saves a size its measure did not name");
            }
            if (!java.util.Arrays.equals(i.scratch, i.wire)) {
                fail(i.name + " does not re-save byte-identically — the leg is gated before it is timed");
            }
            long text = i.jsonMeasure.of();
            if (text < 0) {
                fail(i.name + " holds a value the text form refuses");
            }
            i.text = new byte[(int) text];
            if (i.json.into(i.text) != text) {
                fail(i.name + " writes a text its measure did not name");
            }
        }
        return out.toArray(new Instance[0]);
    }

    // ---- the allocation counter, which is the JVM's own and not a heap guess

    private static com.sun.management.ThreadMXBean allocationCounter() {
        java.lang.management.ThreadMXBean bean = ManagementFactory.getThreadMXBean();
        if (!(bean instanceof com.sun.management.ThreadMXBean)) {
            return null;
        }
        com.sun.management.ThreadMXBean sun = (com.sun.management.ThreadMXBean) bean;
        if (!sun.isThreadAllocatedMemorySupported()) {
            return null;
        }
        sun.setThreadAllocatedMemoryEnabled(true);
        return sun;
    }

    private static long allocated(com.sun.management.ThreadMXBean bean) {
        return bean == null ? -1 : bean.getCurrentThreadAllocatedBytes();
    }

    // THE ALLOCATION GATE, and it is a MEASUREMENT of a COUNT rather than an
    // inference from a heap.
    //
    // A soak that gates on heap drift is a LEAK instrument: it sees storage that
    // is retained and is blind to a per-iteration allocation that is collected,
    // which on the read path is the defect that matters — a codec that allocates
    // a byte per field keeps a flat heap and ruins a frame budget. So the number
    // this gate holds is BYTES PER RECORD, from the JVM's own per-thread
    // allocation counter, over a window that follows a warm-up.
    //
    // EACH PATH IS MEASURED IN ITS OWN MONOMORPHIC LOOP, for the reason the
    // bench legs state: a shared loop behind an interface pools its type profile
    // across call sites and the JIT compiles it differently from the real one, so
    // a shared loop would be measuring the harness.
    //
    // THE FLOOR IS NAMED PER PATH, and the two kinds of floor are different
    // claims:
    //
    //   - the tolerant WIRE's read and save, and the accelerators' READS, are
    //     held at EXACTLY ZERO. That is the contract (docs/SPEC-TABLES.md §3):
    //     the caller owns the value, the buffer and the report, and this port
    //     adds the reader and the writer to that list — a nested body moves the
    //     reader's limit instead of slicing a sub-reader, so there is no
    //     per-body object to allocate. Exact, so a regression in either
    //     direction is visible.
    //   - the two accelerators' OPEN allocates ONE HANDLE, per FILE and never
    //     per row, and the text form allocates by nature. Those carry a stated
    //     CEILING rather than a pin, because the exact byte count of a String or
    //     a BigDecimal is a JDK internal and the pinned JDK is not the only one
    //     a consumer will run.

    private static final class Floor {
        final String name;
        final long bytes;      // the floor, per record
        final boolean exact;   // == the floor, or <= it
        final String why;

        Floor(String name, long bytes, boolean exact, String why) {
            this.name = name;
            this.bytes = bytes;
            this.exact = exact;
            this.why = why;
        }
    }

    // THE FLOORS THIS PORT HOLDS. Every non-zero one names what it is.
    private static final Floor WIRE_READ = new Floor("wire read", 0, true,
            "the value, the report and the READER are the caller's; a nested body moves the reader's limit");
    private static final Floor WIRE_SAVE = new Floor("wire save", 0, true,
            "the value, the buffer and the WRITER are the caller's");
    private static final Floor BLOCK_READ = new Floor("block row walk", 0, true,
            "a row is an offset into the caller's array and a field is one read at one offset");
    // THE TYPED FAST PATH — the generated accessor §19.2 calls "what a per-frame
    // job uses", which the descriptor walk above never touches. It was measured
    // at zero through the wrong path once; it is measured through its own now.
    private static final Floor BLOCK_TYPED = new Floor("block typed path", 0, true,
            "<field>Count() and <field>At(i) read the triple out of the instance and answer an int");
    // and the CONVENIENCE beside it, which carries the three numbers together in
    // a record and costs one per call. Stated rather than hidden: a call site
    // that wants zero uses the pair above.
    private static final Floor BLOCK_ROWS = new Floor("block rows()", 64, false,
            "one TableBlockRows record per call — the convenience, not the fast path");
    private static final Floor COOK_READ = new Floor("cook read", 0, true,
            "the same, plus `at`, which answers an offset and never a wrapper");
    private static final Floor BLOCK_OPEN = new Floor("block open", 64, false,
            "ONE handle per file — three fields, and never one per row");
    private static final Floor COOK_OPEN = new Floor("cook open", 64, false,
            "ONE handle per file — three fields, and never one per node");
    // The TEXT FORM (§16) is authoring-time and allocates by nature. What it
    // allocates, named: one In or Out per call, carrying the walk's scratch (a
    // 256-byte key buffer, a 512-char number token, a 4-byte UTF-8 unit); one
    // 64-byte duplicate-key frame per object read, which is the one piece of
    // scratch that is live across a recursion and so cannot be shared; and, per
    // FLOAT written, the BigDecimal and String of C's `%.*g` spelling, which is
    // where nearly all of it goes.
    //
    // THE CEILING IS A BOUND ON THE ORDER, not a pin on a JDK internal: the
    // exact byte count of a String or a BigDecimal moves between JDKs and the
    // pinned one is not the only one a consumer will run. On the pinned JDK the
    // corpus measures ~7.6 KB per record written and ~3.6 KB per record read, so
    // these sit at rather more than twice that — loose enough to survive a JDK,
    // tight enough that an accidental per-FIELD allocation, which would be an
    // order up, cannot hide under them.
    private static final Floor JSON_WRITE = new Floor("json write", 16384, false,
            "one Out per call, and per float written the BigDecimal and String of C's %.*g spelling");
    private static final Floor JSON_READ = new Floor("json read", 8192, false,
            "one In per call, one 64-byte duplicate-key frame per object, one String per number token");

    // the sabotage the negative control turns on: ONE extra allocation per
    // iteration, which the exact-zero floors must catch
    private static boolean sabotage;
    private static Object escape;

    private static long allocWireRead(com.sun.management.ThreadMXBean bean, Instance[] corpus, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            for (int n = 0; n < corpus.length; n++) {
                corpus[n].read.read(corpus[n].wire);
                if (sabotage) {
                    escape = new byte[1];
                }
            }
        }
        return allocated(bean) - before;
    }

    private static long allocWireSave(com.sun.management.ThreadMXBean bean, Instance[] corpus, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            for (int n = 0; n < corpus.length; n++) {
                corpus[n].save.into(corpus[n].scratch);
            }
        }
        return allocated(bean) - before;
    }

    private static long allocJsonWrite(com.sun.management.ThreadMXBean bean, Instance[] corpus,
                                       byte[] scratch, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            for (int n = 0; n < corpus.length; n++) {
                corpus[n].json.into(scratch);
            }
        }
        return allocated(bean) - before;
    }

    private static long allocJsonRead(com.sun.management.ThreadMXBean bean, Instance[] corpus, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            for (int n = 0; n < corpus.length; n++) {
                corpus[n].jsonRead.of(corpus[n].text);
            }
        }
        return allocated(bean) - before;
    }

    private static long allocBlockOpen(com.sun.management.ThreadMXBean bean, byte[] block, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            escape = blockdemo.RenderFrameBlock.open(block, 0, block.length);
        }
        return allocated(bean) - before;
    }

    // the TYPED accessors, exactly as USAGE and §19.2 spell them at a call site
    private static long allocBlockTyped(com.sun.management.ThreadMXBean bean,
                                        blockdemo.RenderFrameBlock handle, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            int rows = handle.shipsCount();
            for (int i = 0; i < rows; i++) {
                sink += blockdemo.RenderShipRow.objectId(handle.data(), handle.shipsAt(i));
            }
        }
        return allocated(bean) - before;
    }

    private static long allocBlockRows(com.sun.management.ThreadMXBean bean,
                                       blockdemo.RenderFrameBlock handle, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            escape = handle.ships();
        }
        return allocated(bean) - before;
    }

    private static long allocBlockRead(com.sun.management.ThreadMXBean bean,
                                       blockdemo.RenderFrameBlock handle, int passes) {
        blockdemo.TableBlockInfo type = blockdemo.RenderFrameBlock.type();
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            readBlock(handle.data(), handle.base(), type);
        }
        return allocated(bean) - before;
    }

    private static long allocCookOpen(com.sun.management.ThreadMXBean bean, byte[] cook, int passes) {
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            escape = graphdemo.SceneCook.open(cook, 0, cook.length);
        }
        return allocated(bean) - before;
    }

    // the READ, with no walker bookkeeping in it: the root's fields through the
    // descriptors, then every reference resolved through the generated `at`.
    // Descending is the WALKER's job and its visited set is the walker's
    // allocation, not the reader's.
    private static long allocCookRead(com.sun.management.ThreadMXBean bean,
                                      graphdemo.SceneCook cook, int passes) {
        graphdemo.TableCookInfo type = graphdemo.SceneCook.type();
        long before = allocated(bean);
        for (int pass = 0; pass < passes; pass++) {
            readCook(cook.data(), cook.region(), cook.regionLength(), cook.root(), type, 0, null);
            for (graphdemo.TableCookFieldInfo f : type.fields) {
                if (f.isPointer && f.record() != null) {
                    sink += cook.at(cook.root() + f.offset, f.record().size);
                }
            }
        }
        return allocated(bean) - before;
    }

    private static long sink;

    /** what a measured path costs over its window. */
    private interface Window { long bytes(); }

    // EVERY PATH IS MEASURED UNTIL TWO CONSECUTIVE WINDOWS AGREE, and that
    // count is the one reported — the bench's own "one warmup run, then the
    // measured ones" applied to allocation rather than to time. It earns its
    // place: the typed accessor loop is a tight int loop C2 compiles LATE, and
    // at a small window the compilation lands inside a measured span and reads
    // as a fixed ~576 bytes; a deoptimization and its recompile land the same
    // way and read as a few kilobytes. Those are one-offs and not per-record
    // costs (576 over 2500 records is 0.23 bytes; the smallest object Java can
    // allocate is sixteen), and no two windows carry the same one-off, so the
    // number that repeats is the loop's own rather than the compiler's. A path
    // that really allocates per record allocates the same amount in every
    // window and is reported on the second. The count is bounded: a path
    // whose allocation never settles is judged on its last window.
    private static final int SETTLE_WINDOWS = 8;

    private static long steady(Window w) {
        long last = w.bytes();
        for (int window = 1; window < SETTLE_WINDOWS; window++) {
            long next = w.bytes();
            if (next == last) {
                return next;
            }
            last = next;
        }
        return last;
    }

    // one measured row: the total over `records`, the per-record number, and the
    // verdict against the floor.
    private static boolean check(Floor floor, long bytes, long records) {
        double per = (double) bytes / records;
        String verdict;
        boolean ok;
        if (floor.exact) {
            ok = bytes == floor.bytes * records;
            verdict = ok ? "== 0" : "EXPECTED " + floor.bytes + "/record";
        } else {
            ok = bytes <= floor.bytes * records;
            verdict = ok ? "<= " + floor.bytes + "/record" : "OVER the " + floor.bytes + "/record ceiling";
        }
        System.out.printf("  %-16s %12d B over %7d records   %10.2f B/record   %s%n",
                floor.name, bytes, records, per, verdict);
        if (!ok) {
            System.out.println("      the floor is: " + floor.why);
        }
        return ok;
    }

    // the whole table, measured once. Returns false on any breach.
    // THE WINDOWS ARE SIZED PER PATH CLASS, and the reason is honest rather than
    // tidy: the tolerant wire's paths run at a million records a second and want
    // a long window to carry C2 past every tier; the text form spends its time in
    // BigDecimal and String, so the same window there would be minutes of wall
    // for a number that does not move. A path is warmed until its allocation is
    // steady, and then measured — the counts differ, the discipline does not.
    private static boolean allocationTable(com.sun.management.ThreadMXBean bean, Instance[] corpus,
                                           byte[] block, byte[] cook, byte[] jsonScratch,
                                           int scale) {
        int wirePasses = 100 * scale;
        int formPasses = 2 * scale;

        // WARM UP EVERY PATH FIRST: the first passes JIT-compile, resolve
        // constant pool entries and build the descriptor graphs, all of which
        // allocate once and never again.
        for (int pass = 0; pass < 2 * wirePasses; pass++) {
            for (int n = 0; n < corpus.length; n++) {
                corpus[n].read.read(corpus[n].wire);
                corpus[n].save.into(corpus[n].scratch);
            }
        }
        for (int pass = 0; pass < 2 * formPasses; pass++) {
            for (int n = 0; n < corpus.length; n++) {
                corpus[n].json.into(jsonScratch);
                corpus[n].jsonRead.of(corpus[n].text);
            }
        }
        blockdemo.RenderFrameBlock warmBlock = null;
        graphdemo.SceneCook warmCook = null;
        for (int pass = 0; pass < 2 * formPasses; pass++) {
            warmBlock = blockdemo.RenderFrameBlock.open(block, 0, block.length);
            warmCook = graphdemo.SceneCook.open(cook, 0, cook.length);
            readBlock(warmBlock.data(), warmBlock.base(), blockdemo.RenderFrameBlock.type());
            readCook(warmCook.data(), warmCook.region(), warmCook.regionLength(),
                    warmCook.root(), graphdemo.SceneCook.type(), 0, null);
        }
        if (warmBlock == null || warmCook == null) {
            fail("the fixtures do not open");
            return false;
        }
        final blockdemo.RenderFrameBlock blockHandle = warmBlock;
        final graphdemo.SceneCook cookHandle = warmCook;
        if (blockHandle == null || cookHandle == null) {
            fail("the fixtures do not open");
            return false;
        }

        describe("the measured allocation window");
        boolean ok = true;
        ok &= check(WIRE_READ, steady(() -> allocWireRead(bean, corpus, wirePasses)), (long) wirePasses * corpus.length);
        ok &= check(WIRE_SAVE, steady(() -> allocWireSave(bean, corpus, wirePasses)), (long) wirePasses * corpus.length);
        ok &= check(BLOCK_OPEN, steady(() -> allocBlockOpen(bean, block, wirePasses)), wirePasses);
        ok &= check(BLOCK_READ, steady(() -> allocBlockRead(bean, blockHandle, formPasses)), formPasses);
        ok &= check(BLOCK_TYPED, steady(() -> allocBlockTyped(bean, blockHandle, wirePasses)), wirePasses);
        ok &= check(BLOCK_ROWS, steady(() -> allocBlockRows(bean, blockHandle, wirePasses)), wirePasses);
        ok &= check(COOK_OPEN, steady(() -> allocCookOpen(bean, cook, wirePasses)), wirePasses);
        ok &= check(COOK_READ, steady(() -> allocCookRead(bean, cookHandle, formPasses)), formPasses);
        ok &= check(JSON_WRITE, allocJsonWrite(bean, corpus, jsonScratch, formPasses),
                (long) formPasses * corpus.length);
        ok &= check(JSON_READ, steady(() -> allocJsonRead(bean, corpus, formPasses)), (long) formPasses * corpus.length);
        return ok;
    }

    private static int modeAlloc(String wireDir, String blockFile, String cookFile) throws IOException {
        Instance[] corpus = corpus(wireDir);
        byte[] block = Files.readAllBytes(Paths.get(blockFile));
        byte[] cook = Files.readAllBytes(Paths.get(cookFile));
        byte[] jsonScratch = new byte[1 << 20];
        com.sun.management.ThreadMXBean bean = allocationCounter();
        if (bean == null) {
            System.out.println("SKIP: this JVM exposes no per-thread allocation counter");
            return 0;
        }
        int scale = 100;
        String env = System.getenv("SCHEMA_ALLOC_SCALE");
        if (env != null && !env.isEmpty()) {
            scale = Integer.parseInt(env);
        }
        System.out.println("allocation, bytes per record, after warm-up (scale " + scale + "):");
        if (!allocationTable(bean, corpus, block, cook, jsonScratch, scale)) {
            fail("a measured path allocated past the floor it is held to");
        }
        System.out.println("OK");
        return 0;
    }

    // ---- the readers' oracle
    //
    // A mutant is bytes. The generated Open answers a REFUSAL or a handle, and
    // when it answers a handle every read the reader's own bounds permit must
    // stay inside the array — which in Java means no exception reaches here.
    // That is the whole oracle, and it is the one the C++ leg's ASan build holds
    // with a redzone: the languages differ in the instrument, not in the claim.

    private static long seed = 0xc00c1e5eedL;

    private static long next() {
        seed ^= seed << 13;
        seed ^= seed >>> 7;
        seed ^= seed << 17;
        return seed;
    }

    private static int nextInt(int bound) {
        return (int) Long.remainderUnsigned(next() >>> 1, bound);
    }

    private static byte[] mutate(byte[] source) {
        byte[] out = source.clone();
        int edits = 1 + nextInt(6);
        for (int e = 0; e < edits; e++) {
            int at;
            if (nextInt(2) == 0 && out.length > 128) {
                at = nextInt(128); // the header and the prologue, where the checks are
            } else {
                at = nextInt(out.length);
            }
            switch (nextInt(3)) {
                case 0: out[at] = (byte) nextInt(256); break;
                case 1: out[at] ^= (byte) (1 << nextInt(8)); break;
                default: out[at] = (byte) (nextInt(2) == 0 ? 0x00 : 0xff); break;
            }
        }
        return out;
    }

    // every field of every row, through the DESCRIPTORS: the reflective read is
    // the one a walker makes, and it is where an out-of-bounds index would land.
    private static void readBlockRecord(byte[] data, int at, blockdemo.TableBlockInfo info) {
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.outOfLine) {
                continue;
            }
            if (f.counted) {
                blockdemo.TableBytes.i32(data, at + f.countOffset);
            }
            int slots = f.isArray ? f.arrayBound : 1;
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                if (f.element() != null) {
                    readBlockRecord(data, value, f.element());
                } else if (f.elemSize > 0) {
                    switch (f.elemSize) {
                        case 1: blockdemo.TableBytes.u8(data, value); break;
                        case 2: blockdemo.TableBytes.u16(data, value); break;
                        case 4: blockdemo.TableBytes.u32(data, value); break;
                        default: blockdemo.TableBytes.i64(data, value); break;
                    }
                }
            }
            if (f.optional) {
                blockdemo.TableBytes.bool(data, at + f.presentOffset);
            }
        }
    }

    private static void readBlock(byte[] data, int base, blockdemo.TableBlockInfo info) {
        readBlockRecord(data, base, info);
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (!f.outOfLine) {
                continue;
            }
            long offsetOf = blockdemo.TableBytes.i64(data, base + f.offsetOfOffset);
            long count = blockdemo.TableBytes.u32(data, base + f.countOffset);
            long stride = blockdemo.TableBytes.u32(data, base + f.strideOffset);
            for (long r = 0; r < count; r++) {
                readBlockRecord(data, base + (int) (offsetOf + r * stride), f.element());
            }
        }
    }

    // the cook's read: the root, then every reference the reader's own `at`
    // resolves, bounded as the reader bounds it and as a walker must bound the
    // RECORD — `at` answers where a target starts and the region says whether it
    // fits (§7.4).
    private static void readCook(byte[] data, int region, long regionLength, int at,
                                 graphdemo.TableCookInfo info, int depth, boolean[] seen) {
        if (depth > 64) {
            return;
        }
        for (graphdemo.TableCookFieldInfo f : info.fields) {
            if (f.countOffset >= 0) {
                graphdemo.TableBytes.i32(data, at + f.countOffset);
            }
            if (f.isPointer) {
                continue; // resolved by the caller below, through the reader's own `at`
            }
            int slots = f.isArray ? f.arrayBound : 1;
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                if (f.record() != null) {
                    readCook(data, region, regionLength, value, f.record(), depth + 1, seen);
                } else {
                    switch (f.elemSize) {
                        case 1: graphdemo.TableBytes.u8(data, value); break;
                        case 2: graphdemo.TableBytes.u16(data, value); break;
                        case 4: graphdemo.TableBytes.u32(data, value); break;
                        default: graphdemo.TableBytes.i64(data, value); break;
                    }
                }
            }
            if (f.presentOffset >= 0) {
                graphdemo.TableBytes.bool(data, at + f.presentOffset);
            }
        }
    }

    private static void walkCook(graphdemo.SceneCook cook, int at, graphdemo.TableCookInfo info,
                                 int depth, java.util.Set<Integer> seen) {
        if (depth > 512 || !seen.add(at)) {
            return;
        }
        readCook(cook.data(), cook.region(), cook.regionLength(), at, info, 0, null);
        for (graphdemo.TableCookFieldInfo f : info.fields) {
            if (!f.isPointer) {
                continue;
            }
            if (f.record() == null) {
                continue;
            }
            // THE WALKER CARRIES NO BOUND OF ITS OWN, and that is the whole point
            // of this loop: an oracle that re-checked what the reader is supposed
            // to check would certify a reader that checks nothing. `at` refuses a
            // delta whose whole record does not lie inside the region, so a -1 is
            // the reader's answer and anything else is an offset the reader has
            // vouched for — and if it has not, the read below throws and the
            // oracle says so.
            int target = cook.at(at + f.offset, f.record().size);
            if (target < 0) {
                continue; // null, or a delta the reader refused — both are answers
            }
            walkCook(cook, target, f.record(), depth + 1, seen);
        }
    }

    private static int modeFuzz(String blockFile, String cookFile, long mutants) throws IOException {
        byte[] block = Files.readAllBytes(Paths.get(blockFile));
        byte[] cook = Files.readAllBytes(Paths.get(cookFile));
        long opened = 0;
        long refused = 0;
        for (long n = 0; n < mutants; n++) {
            byte[] image = mutate(block);
            describe("block mutant " + n);
            // the CLAIM is varied too: a caller may claim less than the file or
            // more, and Open must answer for the claim rather than for the file
            long claim = image.length;
            if (nextInt(4) == 0) {
                claim = nextInt(image.length + 1);
            }
            byte[] buffer = new byte[(int) Math.max(claim, 1)];
            System.arraycopy(image, 0, buffer, 0, (int) Math.min(claim, image.length));
            try {
                blockdemo.RenderFrameBlock handle =
                        blockdemo.RenderFrameBlock.open(buffer, 0, claim);
                if (handle == null) {
                    refused++;
                } else {
                    opened++;
                    readBlock(handle.data(), handle.base(), blockdemo.RenderFrameBlock.type());
                }
            } catch (RuntimeException e) {
                fail("a block mutant escaped an exception rather than refusing: " + e);
            }

            byte[] region = mutate(cook);
            describe("cook mutant " + n);
            try {
                graphdemo.SceneCook handle = graphdemo.SceneCook.open(region, 0, region.length);
                if (handle == null) {
                    refused++;
                } else {
                    opened++;
                    walkCook(handle, handle.root(), graphdemo.SceneCook.type(), 0, new java.util.HashSet<>());
                }
            } catch (RuntimeException e) {
                fail("a cook mutant escaped an exception rather than refusing: " + e);
            }
        }
        System.out.println("fuzz: " + (opened + refused) + " mutants, " + opened + " opened, " + refused + " refused");
        if (opened == 0) {
            fail("no mutant opened at all — the fuzzer is only exercising the first check, not the readers");
        }
        if (refused == 0) {
            fail("no mutant was refused at all — the oracle is watching nothing");
        }
        System.out.println("OK");
        return 0;
    }

    // ---- the REFERENCE EXTENT gate (§6.3, §7.4)
    //
    // The forged delta the blind read of #356 found, kept as a gate. §7.1 blesses
    // a cook that carries data alone (attribution_length 0 — "a build that ships
    // no tooling need not carry it at all"), so the region ends at the array's
    // end and there are no directory bytes to absorb an overrun. Forge a root
    // pointer's delta so the target STARTS inside the region and its RECORD does
    // not fit, and the reader must refuse.
    //
    // Bounding the start alone passes this and then throws one call later, on the
    // first field read past the end — which is why the bound is over the whole
    // record and why this gate exists.
    private static int modeExtent(String cookFile) throws IOException {
        byte[] file = Files.readAllBytes(Paths.get(cookFile));
        long alignment = graphdemo.TableBytes.i64(file, 40);
        long dataLength = graphdemo.TableBytes.i64(file, 24);
        long dataOffset = (64 + alignment - 1) & ~(alignment - 1);

        // the same file with the attribution part stripped, which §7.1 allows
        byte[] bare = java.util.Arrays.copyOf(file, (int) (dataOffset + dataLength));
        for (int i = 0; i < 8; i++) { bare[32 + i] = 0; }
        describe("opening a cook that carries data alone");
        graphdemo.SceneCook cook = graphdemo.SceneCook.open(bare, 0, bare.length);
        if (cook == null) {
            fail("a cook carrying no attribution part did not open — §7.1 blesses one");
        }

        // the first pointer field of the root, through the descriptors
        graphdemo.TableCookFieldInfo edge = null;
        for (graphdemo.TableCookFieldInfo f : graphdemo.SceneCook.type().fields) {
            if (f.isPointer && f.record() != null) { edge = f; break; }
        }
        if (edge == null) {
            fail("the root names no pointer, so there is nothing to forge");
            return 1;
        }
        int slot = cook.root() + edge.offset;
        int size = edge.record().size;
        int regionEnd = cook.region() + (int) cook.regionLength();

        // the CONTROL first: a delta whose record fits is still resolved, so a
        // gate that refused everything could not pass this
        describe("a delta whose record fits");
        int good = regionEnd - size;
        writeDelta(bare, slot, good - slot);
        if (cook.at(slot, size) != good) {
            fail("a reference whose record ends exactly at the region's end was refused");
        }

        // and the forgery: one byte further, so the record overruns by one
        describe("a delta whose record overruns the region by one byte");
        writeDelta(bare, slot, (good + 1) - slot);
        int answer = cook.at(slot, size);
        if (answer != -1) {
            fail("at answered " + answer + " for a target whose record ends at " +
                 (answer + size) + ", past the region's " + regionEnd +
                 " — the bound is on the START and not on the RECORD");
        }
        // every byte from there to the end, so the gate is not one lucky offset
        for (int start = good + 1; start < regionEnd; start++) {
            writeDelta(bare, slot, start - slot);
            if (cook.at(slot, size) != -1) {
                fail("at accepted a target starting at " + start + ", whose record needs " +
                     size + " bytes and the region ends at " + regionEnd);
            }
        }
        System.out.println("extent: at refuses every delta whose record leaves the region, and resolves the one that fits");
        System.out.println("OK");
        return 0;
    }

    private static void writeDelta(byte[] data, int slot, long delta) {
        for (int i = 0; i < 8; i++) { data[slot + i] = (byte) (delta >>> (8 * i)); }
    }

    // ---- the byte-order leg
    //
    // Java reads a block and a cook EXPLICITLY LITTLE-ENDIAN, so this reader's
    // order is a constant rather than the host's — and a file of the other order
    // is refused twice: its magic reads back byte-swapped, and its order word is
    // not this reader's.
    private static int modeOrder(String little, String big) throws IOException {
        byte[] le = Files.readAllBytes(Paths.get(little));
        byte[] be = Files.readAllBytes(Paths.get(big));
        describe("a cook of this reader's order");
        if (graphdemo.SceneCook.open(le, 0, le.length) == null) {
            fail("a cook of this reader's own byte order did not open");
        }
        describe("a cook of the other order");
        if (graphdemo.SceneCook.open(be, 0, be.length) != null) {
            fail("a cook of the OTHER byte order opened — the magic and the order word both had to refuse it");
        }
        System.out.println("OK");
        return 0;
    }

    // ---- the soak

    private static long usedHeap() {
        Runtime runtime = Runtime.getRuntime();
        System.gc();
        try {
            Thread.sleep(50);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        System.gc();
        return runtime.totalMemory() - runtime.freeMemory();
    }

    private static long collections() {
        long n = 0;
        for (GarbageCollectorMXBean bean : ManagementFactory.getGarbageCollectorMXBeans()) {
            long count = bean.getCollectionCount();
            if (count > 0) {
                n += count;
            }
        }
        return n;
    }

    private static int modeSoak(long seconds, String wireDir, String blockFile, String cookFile)
            throws IOException {
        Instance[] corpus = corpus(wireDir);
        byte[] block = Files.readAllBytes(Paths.get(blockFile));
        byte[] cook = Files.readAllBytes(Paths.get(cookFile));
        byte[] jsonScratch = new byte[1 << 20];

        com.sun.management.ThreadMXBean bean = allocationCounter();

        // WARM UP first, and sample AFTER it: a JVM's first seconds allocate
        // class metadata, compiled code and the descriptor graphs, and a gate
        // that sampled before them would be measuring start-up.
        long warmupEnd = System.nanoTime() + 20L * 1000 * 1000 * 1000;
        long passes = 0;
        while (System.nanoTime() < warmupEnd) {
            passes += onePass(corpus, block, cook, jsonScratch);
        }
        long baseline = usedHeap();
        long gcBefore = collections();
        System.out.println("soak: warm-up done, " + passes + " passes, heap " + (baseline >> 20) + " MiB");

        // THE FIRST SAMPLE, immediately after warm-up: the allocation table is
        // what this soak is FOR, and the heap is the second instrument beside it.
        int samples = 0;
        int breaches = 0;
        if (bean != null) {
            System.out.println("soak: allocation at t=0");
            if (!allocationTable(bean, corpus, block, cook, jsonScratch, 20)) {
                breaches++;
            }
            samples++;
        }

        long end = System.nanoTime() + seconds * 1000L * 1000 * 1000;
        long nextSample = System.nanoTime() + 300L * 1000 * 1000 * 1000;
        long worst = baseline;
        passes = 0;
        while (System.nanoTime() < end) {
            passes += onePass(corpus, block, cook, jsonScratch);
            if (System.nanoTime() >= nextSample) {
                long used = usedHeap();
                if (used > worst) {
                    worst = used;
                }
                System.out.println("soak: " + passes + " passes, heap " + (used >> 20) + " MiB");
                if (bean != null) {
                    // RE-MEASURED, not assumed: an allocation that appears an
                    // hour in — a descriptor rebuilt, a cache that grew a
                    // wrapper — is exactly what a heap-flat gate cannot see.
                    if (!allocationTable(bean, corpus, block, cook, jsonScratch, 20)) {
                        breaches++;
                    }
                    samples++;
                }
                nextSample = System.nanoTime() + 300L * 1000 * 1000 * 1000;
            }
        }
        long after = usedHeap();
        long gcAfter = collections();
        System.out.println("soak: " + passes + " passes over " + seconds + "s");
        System.out.println("soak: heap after warm-up " + baseline + " B, at the end " + after +
                " B, worst sample " + worst + " B");
        System.out.println("soak: collections during the measured window " + (gcAfter - gcBefore));
        System.out.println("soak: allocation table sampled " + samples + " times, " + breaches + " breach(es)");
        if (breaches != 0) {
            fail("a measured path allocated past its floor during the soak — " + breaches +
                 " of " + samples + " samples");
        }
        // FLAT means flat, not "did not run out": a live set that grew by more
        // than a megabyte over an hour of the same six records is a leak. It is
        // the SECOND instrument here, and the weaker one: the allocation table
        // above is what sees a per-record allocation that is collected.
        if (after - baseline > (1L << 20)) {
            fail("the live heap grew by " + (after - baseline) + " bytes over the soak — the read path retains");
        }
        System.out.println("OK");
        return 0;
    }

    private static long onePass(Instance[] corpus, byte[] block, byte[] cook, byte[] jsonScratch) {
        long n = 0;
        for (int index = 0; index < corpus.length; index++) {
            Instance i = corpus[index];
            describe("soak: " + i.name);
            if (!i.read.read(i.wire)) {
                fail(i.name + " stopped loading mid-soak");
            }
            if (i.save.into(i.scratch) != i.scratch.length) {
                fail(i.name + " stopped re-saving at its measured size mid-soak");
            }
            long text = i.json.into(jsonScratch);
            if (text < 0) {
                fail(i.name + " stopped printing mid-soak");
            }
            if (!i.jsonRead.of(java.util.Arrays.copyOf(jsonScratch, (int) text))) {
                fail(i.name + " stopped parsing its own text mid-soak");
            }
            n++;
        }
        describe("soak: the block");
        blockdemo.RenderFrameBlock handle = blockdemo.RenderFrameBlock.open(block, 0, block.length);
        if (handle == null) {
            fail("the block image stopped opening mid-soak");
        }
        readBlock(handle.data(), handle.base(), blockdemo.RenderFrameBlock.type());
        describe("soak: the cook");
        graphdemo.SceneCook opened = graphdemo.SceneCook.open(cook, 0, cook.length);
        if (opened == null) {
            fail("the cook stopped opening mid-soak");
        }
        walkCook(opened, opened.root(), graphdemo.SceneCook.type(), 0, new java.util.HashSet<>());
        return n;
    }

    public static void main(String[] args) throws IOException {
        if (args.length < 1) {
            System.err.println("usage: Main fuzz <block> <cook> | alloc <wiredir> <block> <cook> | " +
                    "soak <seconds> <wiredir> <block> <cook> | order <le> <be> | extent <cook>");
            System.exit(1);
        }
        // THE PLANTED ALLOCATION IS READ HERE, not inside one mode. It lived in
        // modeAlloc, which made SCHEMA_ALLOC_SABOTAGE a silent no-op in the SOAK
        // — the gate the whole port leads with had a control that could not fire,
        // and by this file's own words a gate that has never gone red is watching
        // nothing. Every mode that measures allocation reads it now.
        sabotage = System.getenv("SCHEMA_ALLOC_SABOTAGE") != null;
        if (sabotage) {
            System.out.println("SABOTAGED: one extra allocation per record on the wire read path");
        }

        String mode = args[0];
        switch (mode) {
            case "fuzz": {
                long mutants = 20000;
                String env = System.getenv("SEED");
                if (env != null && !env.isEmpty()) {
                    seed = Long.parseUnsignedLong(env.startsWith("0x") ? env.substring(2) : env,
                            env.startsWith("0x") ? 16 : 10);
                }
                env = System.getenv("N");
                if (env != null && !env.isEmpty()) {
                    mutants = Long.parseLong(env);
                }
                System.exit(modeFuzz(args[1], args[2], mutants));
                break;
            }
            case "alloc":
                System.exit(modeAlloc(args[1], args[2], args[3]));
                break;
            case "soak":
                System.exit(modeSoak(Long.parseLong(args[1]), args[2], args[3], args[4]));
                break;
            case "order":
                System.exit(modeOrder(args[1], args[2]));
                break;
            case "extent":
                System.exit(modeExtent(args[1]));
                break;
            default:
                System.err.println("FAILED: unknown mode " + mode);
                System.exit(1);
        }
    }
}
