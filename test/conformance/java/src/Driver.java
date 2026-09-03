// THE JAVA CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp and test/conformance/cs/src/Program
// .java's C# sibling, and it is deliberately the same shape: one process per
// surface, every expectation in the data, nothing literal here.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// It answers ALL TEN surfaces from ONE binary, cook and cook-forgery included.
// The C# leg splits those two off into a second project because its cook side
// lives in another assembly; Java's generated units are packages of one
// classpath, so the wire units, the block unit and the pointered unit compile
// together and one JVM start-up answers every surface.

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;

public final class Driver {
    private Driver() {}

    // ---- the manifest, exactly as testdata/conformance/tables/FORMAT.md states it

    private static final List<String[]> lines = new ArrayList<>();

    private static void readManifest(String path) throws IOException {
        for (String raw : Files.readAllLines(Paths.get(path), StandardCharsets.UTF_8)) {
            String text = raw.trim();
            if (text.isEmpty() || text.charAt(0) == '#') {
                continue;
            }
            lines.add(text.split("[ \t]+"));
        }
    }

    private static List<String[]> kind(String what) {
        List<String[]> out = new ArrayList<>();
        for (String[] f : lines) {
            if (f[0].equals(what)) {
                out.add(f);
            }
        }
        return out;
    }

    // ---- the codec table: one row per (unit, root) the corpus names

    private static final class Report {
        int unknown, kindMismatch, clamped, duplicate;
        boolean malformed;
    }

    private interface Maker<V> { V make(); }
    private interface Reporter<R> { R make(); }
    private interface Copier<R> { void copy(R from, Report to); }
    private interface Loader<V, R> { boolean load(V value, byte[] bytes, R report); }
    private interface Measurer<V> { long measure(V value); }
    private interface Saver<V> { long save(V value, byte[] buffer); }

    private interface LoadInto { Object load(byte[] bytes, Report report); }
    private interface MeasureOf { long measure(Object value); }
    private interface SaveOf { long save(Object value, byte[] buffer); }

    private static final class Codec {
        String unit;
        String root;
        LoadInto load;
        MeasureOf measure;
        SaveOf save;
        LoadInto fromJson;
        MeasureOf toJsonMeasure;
        SaveOf toJson;
    }

    // Each unit declares its own TableReport, so the driver carries one report
    // shape and every row copies into it — five counters is the whole of §4.
    private static void copyDemo(tabledemo.TableReport r, Report to) {
        to.unknown = r.unknown; to.kindMismatch = r.kindMismatch; to.clamped = r.clamped;
        to.duplicate = r.duplicate; to.malformed = r.malformed;
    }
    private static void copyV1(tblv1.TableReport r, Report to) {
        to.unknown = r.unknown; to.kindMismatch = r.kindMismatch; to.clamped = r.clamped;
        to.duplicate = r.duplicate; to.malformed = r.malformed;
    }
    private static void copyV2(tblv2.TableReport r, Report to) {
        to.unknown = r.unknown; to.kindMismatch = r.kindMismatch; to.clamped = r.clamped;
        to.duplicate = r.duplicate; to.malformed = r.malformed;
    }
    private static void copyP1(tblp1.TableReport r, Report to) {
        to.unknown = r.unknown; to.kindMismatch = r.kindMismatch; to.clamped = r.clamped;
        to.duplicate = r.duplicate; to.malformed = r.malformed;
    }
    private static void copyP3(tblp3.TableReport r, Report to) {
        to.unknown = r.unknown; to.kindMismatch = r.kindMismatch; to.clamped = r.clamped;
        to.duplicate = r.duplicate; to.malformed = r.malformed;
    }

    // ONE row of the codec table, for any unit. The value type and the unit's own
    // TableReport are the two type parameters; `make` and `copy` are the two
    // things that cannot be generic, because each unit declares its own report
    // class and Java has no structural typing to unify five identical shapes.
    private static <V, R> Codec row(
            String unit, String root,
            Maker<V> make, Reporter<R> newReport, Copier<R> copy,
            Loader<V, R> load, Measurer<V> measure, Saver<V> save,
            Loader<V, R> fromJson, Measurer<V> toJsonMeasure, Saver<V> toJson) {
        Codec c = new Codec();
        c.unit = unit;
        c.root = root;
        c.load = (bytes, report) -> {
            V value = make.make();
            R inner = newReport.make();
            boolean ok = load.load(value, bytes, inner);
            copy.copy(inner, report);
            return ok ? value : null;
        };
        c.measure = v -> measure.measure(cast(v));
        c.save = (v, buffer) -> save.save(cast(v), buffer);
        c.fromJson = (text, report) -> {
            V value = make.make();
            R inner = newReport.make();
            boolean ok = fromJson.load(value, text, inner);
            copy.copy(inner, report);
            return ok ? value : null;
        };
        c.toJsonMeasure = v -> toJsonMeasure.measure(cast(v));
        c.toJson = (v, buffer) -> toJson.save(cast(v), buffer);
        return c;
    }

    @SuppressWarnings("unchecked")
    private static <V> V cast(Object v) { return (V) v; }

    private static final List<Codec> codecs = new ArrayList<>();

    static {
        codecs.add(row("tabledemo", "RootConfig",
                tabledemo.TablesTable.RootConfig::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.TablesTable::rootConfigLoad, tabledemo.TablesTable::rootConfigMeasure,
                tabledemo.TablesTable::rootConfigSave, tabledemo.TablesTable::rootConfigFromJson,
                tabledemo.TablesTable::rootConfigToJsonMeasure, tabledemo.TablesTable::rootConfigToJson));
        codecs.add(row("tabledemo", "ProfileConfig",
                tabledemo.TablesTable.ProfileConfig::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.TablesTable::profileConfigLoad, tabledemo.TablesTable::profileConfigMeasure,
                tabledemo.TablesTable::profileConfigSave, tabledemo.TablesTable::profileConfigFromJson,
                tabledemo.TablesTable::profileConfigToJsonMeasure, tabledemo.TablesTable::profileConfigToJson));
        codecs.add(row("tabledemo", "LoadoutConfig",
                tabledemo.TablesTable.LoadoutConfig::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.TablesTable::loadoutConfigLoad, tabledemo.TablesTable::loadoutConfigMeasure,
                tabledemo.TablesTable::loadoutConfigSave, tabledemo.TablesTable::loadoutConfigFromJson,
                tabledemo.TablesTable::loadoutConfigToJsonMeasure, tabledemo.TablesTable::loadoutConfigToJson));
        codecs.add(row("tabledemo", "WideBlob",
                tabledemo.WideTable.WideBlob::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.WideTable::wideBlobLoad, tabledemo.WideTable::wideBlobMeasure,
                tabledemo.WideTable::wideBlobSave, tabledemo.WideTable::wideBlobFromJson,
                tabledemo.WideTable::wideBlobToJsonMeasure, tabledemo.WideTable::wideBlobToJson));
        codecs.add(row("tabledemo", "ArchiveConfig",
                tabledemo.NestedTable.ArchiveConfig::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.NestedTable::archiveConfigLoad, tabledemo.NestedTable::archiveConfigMeasure,
                tabledemo.NestedTable::archiveConfigSave, tabledemo.NestedTable::archiveConfigFromJson,
                tabledemo.NestedTable::archiveConfigToJsonMeasure, tabledemo.NestedTable::archiveConfigToJson));
        codecs.add(row("tabledemo", "KeyedConfig",
                tabledemo.KeyedTable.KeyedConfig::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.KeyedTable::keyedConfigLoad, tabledemo.KeyedTable::keyedConfigMeasure,
                tabledemo.KeyedTable::keyedConfigSave, tabledemo.KeyedTable::keyedConfigFromJson,
                tabledemo.KeyedTable::keyedConfigToJsonMeasure, tabledemo.KeyedTable::keyedConfigToJson));
        codecs.add(row("tabledemo", "PackConfig",
                tabledemo.PackTable.PackConfig::new, tabledemo.TableReport::new, Driver::copyDemo,
                tabledemo.PackTable::packConfigLoad, tabledemo.PackTable::packConfigMeasure,
                tabledemo.PackTable::packConfigSave, tabledemo.PackTable::packConfigFromJson,
                tabledemo.PackTable::packConfigToJsonMeasure, tabledemo.PackTable::packConfigToJson));
        codecs.add(row("tblv1", "Cfg",
                tblv1.V1Table.Cfg::new, tblv1.TableReport::new, Driver::copyV1,
                tblv1.V1Table::cfgLoad, tblv1.V1Table::cfgMeasure, tblv1.V1Table::cfgSave,
                tblv1.V1Table::cfgFromJson, tblv1.V1Table::cfgToJsonMeasure, tblv1.V1Table::cfgToJson));
        codecs.add(row("tblv2", "Cfg",
                tblv2.V2Table.Cfg::new, tblv2.TableReport::new, Driver::copyV2,
                tblv2.V2Table::cfgLoad, tblv2.V2Table::cfgMeasure, tblv2.V2Table::cfgSave,
                tblv2.V2Table::cfgFromJson, tblv2.V2Table::cfgToJsonMeasure, tblv2.V2Table::cfgToJson));
        codecs.add(row("tblp1", "Chain",
                tblp1.P1Table.Chain::new, tblp1.TableReport::new, Driver::copyP1,
                tblp1.P1Table::chainLoad, tblp1.P1Table::chainMeasure, tblp1.P1Table::chainSave,
                tblp1.P1Table::chainFromJson, tblp1.P1Table::chainToJsonMeasure, tblp1.P1Table::chainToJson));
        codecs.add(row("tblp3", "Chain",
                tblp3.P3Table.Chain::new, tblp3.TableReport::new, Driver::copyP3,
                tblp3.P3Table::chainLoad, tblp3.P3Table::chainMeasure, tblp3.P3Table::chainSave,
                tblp3.P3Table::chainFromJson, tblp3.P3Table::chainToJsonMeasure, tblp3.P3Table::chainToJson));
    }

    private static Codec find(String unit, String root) {
        for (Codec c : codecs) {
            if (c.unit.equals(unit) && c.root.equals(root)) {
                return c;
            }
        }
        return null;
    }

    private static void spill(String out, String name, byte[] body) throws IOException {
        Files.write(Path.of(out, name), body);
    }

    private static void spill(String out, String name, String body) throws IOException {
        spill(out, name, body.getBytes(StandardCharsets.UTF_8));
    }

    // spillAbsent says this backend cannot answer THIS CASE — a feature it
    // lacks, not a test it failed. The harness counts it and the matrix prints
    // it beside what the leg did answer (test/conformance/README.md).
    private static void spillAbsent(String out, String name) throws IOException {
        spill(out, name + ".absent", new byte[0]);
    }

    // noText marks an instance the corpus carries on the WIRE only: the
    // variable class has no text form yet (docs/SPEC-TABLES.md 16.2), so the
    // TEXT surfaces skip it rather than reporting a form nobody has.
    private static boolean noText(String[] f) {
        return f.length > 5 && f[5].equals("no-text");
    }

    private static String counters(Report r, boolean malformed) {
        if (malformed) {
            return r.unknown + "," + r.kindMismatch + "," + r.clamped + "," + r.duplicate + ",true\n";
        }
        return r.unknown + "," + r.kindMismatch + "," + r.clamped + "," + r.duplicate + ",false\n";
    }

    // ---- the surfaces

    private static int surfaceWire(String out) throws IOException {
        for (String[] f : kind("instance")) {
            Codec codec = find(f[2], f[3]);
            if (codec == null) {
                // Java refuses a pointered unit's wire by name (11), so it has
                // no codec here and says so per case
                spillAbsent(out, f[1]);
                continue;
            }
            byte[] wire = Files.readAllBytes(Paths.get(f[4]));
            Report report = new Report();
            Object value = codec.load.load(wire, report);
            if (value == null) {
                System.err.println("driver: " + f[1] + " does not load");
                return 1;
            }
            long size = codec.measure.measure(value);
            if (size < 0) {
                System.err.println("driver: " + f[1] + " measures as unsaveable");
                return 1;
            }
            byte[] buffer = new byte[(int) size];
            if (codec.save.save(value, buffer) != size) {
                System.err.println("driver: " + f[1] + " saves a size its measure did not name");
                return 1;
            }
            spill(out, f[1], buffer);
        }
        return 0;
    }

    // json-read: the text is the input and the WIRE is the answer, so the pass
    // proves the reader against bytes this driver did not write.
    private static int surfaceJsonRead(String out) throws IOException {
        for (String[] f : kind("instance")) {
            if (noText(f)) {
                continue;
            }
            Codec codec = find(f[2], f[3]);
            if (codec == null) {
                // Java refuses a pointered unit's wire by name (11), so it has
                // no codec here and says so per case
                spillAbsent(out, f[1]);
                continue;
            }
            byte[] text = Files.readAllBytes(Paths.get("testdata", "conformance", "tables", "json", f[1] + ".json"));
            Report report = new Report();
            Object value = codec.fromJson.load(text, report);
            if (value == null) {
                System.err.println("driver: " + f[1] + " does not read as JSON");
                return 1;
            }
            long size = codec.measure.measure(value);
            if (size < 0) {
                System.err.println("driver: " + f[1] + " measures as unsaveable after a clean read");
                return 1;
            }
            byte[] buffer = new byte[(int) size];
            if (codec.save.save(value, buffer) != size) {
                System.err.println("driver: " + f[1] + " saves a size its measure did not name");
                return 1;
            }
            spill(out, f[1], buffer);
        }
        return 0;
    }

    // json-write: the wire is the input and the TEXT is the answer, compared
    // against a text a third implementation wrote.
    private static int surfaceJsonWrite(String out) throws IOException {
        for (String[] f : kind("instance")) {
            if (noText(f)) {
                continue;
            }
            Codec codec = find(f[2], f[3]);
            if (codec == null) {
                // Java refuses a pointered unit's wire by name (11), so it has
                // no codec here and says so per case
                spillAbsent(out, f[1]);
                continue;
            }
            byte[] wire = Files.readAllBytes(Paths.get(f[4]));
            Report report = new Report();
            Object value = codec.load.load(wire, report);
            if (value == null) {
                System.err.println("driver: " + f[1] + " does not load");
                return 1;
            }
            long size = codec.toJsonMeasure.measure(value);
            if (size < 0) {
                System.err.println("driver: " + f[1] + " holds a value ToJson refuses");
                return 1;
            }
            byte[] text = new byte[(int) size];
            if (codec.toJson.save(value, text) != size) {
                System.err.println("driver: " + f[1] + " writes a text its measure did not name");
                return 1;
            }
            spill(out, f[1] + ".json", text);
        }
        return 0;
    }

    // json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
    // The answer is the REPORT the read produces, or `refused`.
    private static int surfaceJsonHostile(String out) throws IOException {
        for (String[] f : kind("json-hostile")) {
            Codec codec = find(f[2], f[3]);
            if (codec == null) {
                // Java refuses a pointered unit's wire by name (11), so it has
                // no codec here and says so per case
                spillAbsent(out, f[1]);
                continue;
            }
            // the tree is what `schema pack` reads, so the text is
            // <tree>/<root>.json (§17)
            byte[] text = Files.readAllBytes(Path.of(f[4], f[3] + ".json"));
            Report report = new Report();
            Object value = codec.fromJson.load(text, report);
            String verdict = value == null || report.malformed ? "refused\n" : counters(report, false);
            spill(out, f[1], verdict);
        }
        return 0;
    }

    private static int surfaceReport(String out) throws IOException {
        for (String[] f : kind("report")) {
            Codec codec = find(f[2], f[3]);
            if (codec == null) {
                // Java refuses a pointered unit's wire by name (11), so it has
                // no codec here and says so per case
                spillAbsent(out, f[1]);
                continue;
            }
            byte[] wire = Files.readAllBytes(Paths.get(f[4]));
            Report report = new Report();
            Object value = codec.load.load(wire, report);
            spill(out, f[1], counters(report, report.malformed || value == null));
        }
        return 0;
    }

    // ---- the block surfaces
    //
    // A block's base is 64-byte aligned by construction (§19.1) and `extent` is
    // the length the CALLER claims, which a forgery may set past the bytes the
    // image carries. The allocation is the claim, so a reader that walks past
    // what it was given walks into an array this process owns and nothing else's
    // — and the ALIGNMENT the Java reader checks is the base OFFSET's, which is
    // zero here because a Java array's own address is not the caller's business.

    private static final class Placed {
        byte[] data;
        int base;
        long length;
    }

    // A copy of `source`, `claim` bytes long, at a base `lead` bytes into the
    // array — the twin of the C# leg's mmap placement, in the currency Java has:
    // the base's residue modulo an alignment is the LEAD's, so an unaligned base
    // is `lead` not divisible by the alignment, exactly as it is there.
    private static Placed place(byte[] source, long claim, int lead) {
        Placed p = new Placed();
        long total = claim + lead;
        p.data = new byte[(int) total];
        p.base = lead;
        p.length = claim;
        int copy = (int) Math.min(claim, source.length);
        System.arraycopy(source, 0, p.data, lead, copy);
        return p;
    }

    private static String openBlock(String name, byte[] bytes, long extent) {
        long claim = extent < 0 || extent < bytes.length ? bytes.length : extent;
        Placed p = place(bytes, claim, 0);
        boolean opened;
        if (name.startsWith("block_render")) {
            opened = blockdemo.RenderFrameBlock.open(p.data, p.base, p.length) != null;
        } else if (name.startsWith("block_padded")) {
            opened = blockdemo.PaddedFrameBlock.open(p.data, p.base, p.length) != null;
        } else {
            System.err.println("driver: no block named " + name);
            System.exit(1);
            return "";
        }
        return opened ? "open\n" : "refuse\n";
    }

    private static int surfaceBlock(String out) throws IOException {
        for (String[] f : kind("block")) {
            spill(out, f[1], openBlock(f[1], Files.readAllBytes(Paths.get(f[3])), -1));
        }
        return 0;
    }

    // THE FOREIGN SURFACES (§7.1, §19.1). The magic is the one word read
    // without assuming the order the rest of the file is in, so byte-reversing
    // it IS a file of the other byte order as far as any reader is concerned,
    // and every leg on every host must refuse one.
    //
    // Java refuses it twice over, and neither refusal depends on the host: every
    // multi-byte read goes through TableBytes, which is explicitly
    // little-endian, so the magic reads back byte-swapped AND the order word is
    // not this reader's.
    private static byte[] swapMagic(byte[] file) {
        byte[] out = file.clone();
        for (int i = 0; i < 4 && i < out.length; i++) {
            byte t = out[i];
            out[i] = out[7 - i];
            out[7 - i] = t;
        }
        return out;
    }

    private static int surfaceBlockForeign(String out) throws IOException {
        for (String[] f : kind("block")) {
            spill(out, f[1], openBlock(f[1], swapMagic(Files.readAllBytes(Paths.get(f[3]))), -1));
        }
        return 0;
    }

    private static int surfaceCookForeign(String out) throws IOException {
        for (String[] f : kind("cook")) {
            Root r = rootNamed(f[3]);
            byte[] file = swapMagic(Files.readAllBytes(Paths.get(f[4])));
            spill(out, f[1], r.open.open(file, 0, file.length) != null ? "open\n" : "refuse\n");
        }
        return 0;
    }

    private static int surfaceForgery(String out) throws IOException {
        for (String[] f : kind("forgery")) {
            if (!f[2].equals("block")) {
                continue; // the cook's battery is below
            }
            spill(out, f[1], openBlock(f[3], Files.readAllBytes(Paths.get(f[4])), Long.parseLong(f[5])));
        }
        return 0;
    }

    // ---- the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md)
    //
    // The twin of the C++ leg's walk, and like it, written against §8's
    // descriptors and NOTHING ELSE: no generated row accessor, no field named in
    // this file. That is the claim §19.2 makes for the descriptors, and a walk
    // that reached for an accessor would be proving something else. A FLOAT is
    // its IEEE-754 bit pattern, because a block row is a byte-identical
    // projection and its bits are the fact.

    private static void dumpScalar(StringBuilder into, byte[] data, int at, int kind, int width) {
        switch (kind) {
            case 1:
                into.append(data[at] != 0 ? "true" : "false");
                return;
            case 10:
                into.append("0x").append(String.format("%08x", blockdemo.TableBytes.i32(data, at)));
                return;
            case 11:
                into.append("0x").append(String.format("%016x", blockdemo.TableBytes.i64(data, at)));
                return;
            case 2: case 3: case 4: case 5: {
                long v = width == 1 ? blockdemo.TableBytes.i8(data, at)
                       : width == 2 ? blockdemo.TableBytes.i16(data, at)
                       : width == 4 ? blockdemo.TableBytes.i32(data, at)
                       : blockdemo.TableBytes.i64(data, at);
                into.append(v);
                return;
            }
            default: {
                if (width == 8) {
                    into.append(Long.toUnsignedString(blockdemo.TableBytes.i64(data, at)));
                    return;
                }
                long v = width == 1 ? blockdemo.TableBytes.u8(data, at)
                       : width == 2 ? blockdemo.TableBytes.u16(data, at)
                       : blockdemo.TableBytes.u32(data, at);
                into.append(v);
            }
        }
    }

    private static void dumpText(StringBuilder into, byte[] data, int at, int used) {
        if (used < 0) {
            used = 0;
        }
        into.append('"');
        for (int i = 0; i < used; i++) {
            int c = data[at + i] & 0xff;
            if (c >= 0x20 && c < 0x7f && c != '"' && c != '\\') {
                into.append((char) c);
            } else {
                into.append("\\x").append(String.format("%02x", c));
            }
        }
        into.append('"').append(" len=").append(used);
    }

    private static String join(String prefix, String name) {
        return prefix.isEmpty() ? name : prefix + "." + name;
    }

    private static boolean dumpRecord(StringBuilder into, byte[] data, int storage,
                                      blockdemo.TableBlockInfo info, String path) {
        if (info == null) {
            System.err.println("driver: a descriptor names no record");
            return false;
        }
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.outOfLine) {
                continue;
            }
            String name = join(path, f.name);
            if (f.counted) {
                int used = blockdemo.TableBytes.i32(data, storage + f.countOffset);
                if (used < 0 || used > f.arrayBound) {
                    System.err.println("driver: " + info.name + "." + f.name +
                            " carries a used length of " + used + ", outside [ 0, " + f.arrayBound + " ]");
                    return false;
                }
                into.append("  ").append(name).append(" = ");
                dumpText(into, data, storage + f.offset, used);
                into.append('\n');
            } else {
                int slots = f.isArray ? f.arrayBound : 1;
                for (int s = 0; s < slots; s++) {
                    String at = f.isArray ? name + "[" + s + "]" : name;
                    int value = storage + f.offset + s * f.elemSize;
                    if (f.element() != null) {
                        if (!dumpRecord(into, data, value, f.element(), at)) {
                            return false;
                        }
                    } else {
                        into.append("  ").append(at).append(" = ");
                        dumpScalar(into, data, value, f.kind, f.elemSize);
                        into.append('\n');
                    }
                }
            }
            if (f.optional) {
                into.append("  ").append(name).append("#present = ")
                        .append(data[storage + f.presentOffset] != 0 ? "true" : "false").append('\n');
            }
        }
        return true;
    }

    private static boolean dumpBlock(StringBuilder into, byte[] data, int base, blockdemo.TableBlockInfo info) {
        into.append("projection ").append(info.name).append(" @0\n");
        if (!dumpRecord(into, data, base, info, "")) {
            return false;
        }
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (!f.outOfLine) {
                continue;
            }
            long offsetOf = blockdemo.TableBytes.i64(data, base + f.offsetOfOffset);
            long count = blockdemo.TableBytes.u32(data, base + f.countOffset);
            long stride = blockdemo.TableBytes.u32(data, base + f.strideOffset);
            blockdemo.TableBlockInfo rowInfo = f.element();
            if (rowInfo == null) {
                System.err.println("driver: " + f.name + " names no element");
                return false;
            }
            into.append("array ").append(f.name).append(' ').append(rowInfo.name)
                    .append(" @").append(offsetOf)
                    .append(" count=").append(count)
                    .append(" stride=").append(stride).append('\n');
            for (long r = 0; r < count; r++) {
                long at = offsetOf + r * stride;
                into.append("row ").append(r).append(" @").append(at).append('\n');
                if (!dumpRecord(into, data, base + (int) at, rowInfo, "")) {
                    return false;
                }
            }
        }
        return true;
    }

    private static boolean blockDump(String name, byte[] bytes, StringBuilder into) {
        Placed p = place(bytes, bytes.length, 0);
        if (name.startsWith("block_render")) {
            blockdemo.RenderFrameBlock block = blockdemo.RenderFrameBlock.open(p.data, p.base, p.length);
            return block != null && dumpBlock(into, block.data(), block.base(), blockdemo.RenderFrameBlock.type());
        }
        if (name.startsWith("block_padded")) {
            blockdemo.PaddedFrameBlock block = blockdemo.PaddedFrameBlock.open(p.data, p.base, p.length);
            return block != null && dumpBlock(into, block.data(), block.base(), blockdemo.PaddedFrameBlock.type());
        }
        System.err.println("driver: no block named " + name);
        return false;
    }

    private static int surfaceBlockDump(String out) throws IOException {
        for (String[] f : kind("block")) {
            StringBuilder text = new StringBuilder();
            if (!blockDump(f[1], Files.readAllBytes(Paths.get(f[3])), text)) {
                return 1;
            }
            spill(out, f[1], text.toString());
        }
        return 0;
    }

    // ---- the COOK surfaces
    //
    // The walk is generic over the cook descriptors, which is the whole point of
    // them: a pointer slot is eight bytes holding the SIGNED SELF-RELATIVE delta
    // of §6.3, so a deref is one add — through the generated <Root>Cook.at, which
    // is also what bounds it. A by-value nesting is not a node; it is storage
    // inside one, and the walk descends through it to reach the pointer slots.

    private interface CookOpen { CookHandle open(byte[] data, int offset, long length); }

    /** the generated deref: the slot's offset and the pointee's size (§6.3). */
    private interface Deref { int at(int slot, int size); }

    private static final class CookHandle {
        byte[] data;
        int region;
        long regionLength;
        Deref at;
    }

    private static final class Root {
        String name;
        CookOpen open;
        graphdemo.TableCookInfo type;
    }

    private static Root root(String name, CookOpen open, graphdemo.TableCookInfo type) {
        Root r = new Root();
        r.name = name;
        r.open = open;
        r.type = type;
        return r;
    }

    private static CookHandle handle(byte[] data, int region, long length, Deref at) {
        CookHandle h = new CookHandle();
        h.data = data;
        h.region = region;
        h.regionLength = length;
        h.at = at;
        return h;
    }

    private static final List<Root> roots = new ArrayList<>();

    static {
        roots.add(root("Scene", (d, o, n) -> {
            graphdemo.SceneCook c = graphdemo.SceneCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.SceneCook.type()));
        roots.add(root("Depot", (d, o, n) -> {
            graphdemo.DepotCook c = graphdemo.DepotCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.DepotCook.type()));
        roots.add(root("Album", (d, o, n) -> {
            graphdemo.AlbumCook c = graphdemo.AlbumCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.AlbumCook.type()));
        roots.add(root("TreeNode", (d, o, n) -> {
            graphdemo.TreeNodeCook c = graphdemo.TreeNodeCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.TreeNodeCook.type()));
        roots.add(root("ListNode", (d, o, n) -> {
            graphdemo.ListNodeCook c = graphdemo.ListNodeCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.ListNodeCook.type()));
        roots.add(root("Marker", (d, o, n) -> {
            graphdemo.MarkerCook c = graphdemo.MarkerCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.MarkerCook.type()));
    }

    private static Root rootNamed(String name) {
        for (Root r : roots) {
            if (r.name.equals(name)) {
                return r;
            }
        }
        System.err.println("driver: no cook root named " + name);
        System.exit(1);
        return null;
    }

    private static final List<Long> reachedOffsets = new ArrayList<>();
    private static final List<graphdemo.TableCookInfo> reachedTypes = new ArrayList<>();

    private static int findReached(long offset) {
        for (int i = 0; i < reachedOffsets.size(); i++) {
            if (reachedOffsets.get(i) == offset) {
                return i;
            }
        }
        return -1;
    }

    private static void fail(String what) {
        System.err.println("driver: " + what);
        System.exit(1);
    }

    private static void node(StringBuilder dump, CookHandle cook, long offset,
                             graphdemo.TableCookInfo type, int depth) {
        if (depth > 4096) {
            fail("the walk nested past any depth a region can hold — a cycle the deref did not close");
        }
        int at = findReached(offset);
        if (at >= 0) {
            if (reachedTypes.get(at) != type) {
                fail("two references name the node at offset " + offset + " as two different tables: " +
                        reachedTypes.get(at).name + " and " + type.name);
            }
            // one node, one visit: sharing and a back-reference are the same fact (§6.3)
            return;
        }
        if (offset > cook.regionLength || type.size > cook.regionLength - offset) {
            fail("the node at offset " + offset + " (" + type.name + ", sizeof " + type.size +
                    ") does not fit inside the region's " + cook.regionLength + " bytes");
        }
        int index = reachedOffsets.size();
        reachedOffsets.add(offset);
        reachedTypes.add(type);
        dump.append("node ").append(index).append(' ').append(type.name).append(" @").append(offset).append('\n');
        storage(dump, cook, cook.region + (int) offset, type, "", depth);
    }

    private static void storage(StringBuilder dump, CookHandle cook, int at,
                                graphdemo.TableCookInfo type, String path, int depth) {
        for (int i = 0; i < type.numFields; i++) {
            graphdemo.TableCookFieldInfo f = type.fields[i];
            String name = path.isEmpty() ? f.name : path + "." + f.name;

            // every COUNT COMPANION, against its declared bound, and a NEGATIVE one
            // refuses too — an extent is never negative, and a walker handed one
            // indexes backwards out of the region (§7.4's pass two)
            int used = -1;
            if (f.countOffset >= 0) {
                used = graphdemo.TableBytes.i32(cook.data, at + f.countOffset);
                if (used < 0 || used > f.arrayBound) {
                    fail(type.name + "." + f.name + " carries a count companion of " + used +
                            ", outside [ 0, " + f.arrayBound + " ]");
                }
            }

            if (f.isPointer) {
                int slot = at + f.offset;
                long delta = graphdemo.TableBytes.i64(cook.data, slot);
                if (delta == 0) {
                    // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
                    emit(dump, name, "null");
                    continue;
                }
                // THE GENERATED DEREF, and it is the reader under test: -1 is a
                // delta that leaves the region, which the walk calls a failure and
                // the reader calls a refusal
                if (f.record() == null) {
                    fail(type.name + "." + f.name + " is a pointer whose descriptor names no record");
                }
                // the size is the READER's bound now, not the walker's: at
                // refuses a delta whose whole record does not fit (§6.3)
                int target = cook.at.at(slot, f.record().size);
                if (target < 0) {
                    fail(type.name + "." + f.name + " resolves outside the region — a delta of " + delta +
                            " from a slot at " + (slot - cook.region));
                }
                if (f.record() == null) {
                    fail(type.name + "." + f.name + " is a pointer whose descriptor names no record");
                }
                long targetOffset = target - cook.region;
                emit(dump, name, "-> @" + targetOffset);
                node(dump, cook, targetOffset, f.record(), depth + 1);
                continue;
            }

            switch (f.storage) {
                case STRING:
                case BYTES:
                    emit(dump, name, text(cook.data, at + f.offset, used));
                    break;
                case RECORD: {
                    // a nested record — by value, or every slot of an array of them.
                    // A COUNTED array writes all N slots (§7.2), and a slot past the
                    // live count holds the value-initialized element, whose pointer
                    // slots are zero: walking all of them is what the check does too.
                    int slots = f.isArray ? f.arrayBound : 1;
                    for (int s = 0; s < slots; s++) {
                        String element = f.isArray ? name + "[" + s + "]" : name;
                        storage(dump, cook, at + f.offset + s * f.elemSize, f.record(), element, depth);
                    }
                    break;
                }
                default: {
                    int slots = f.isArray ? f.arrayBound : 1;
                    for (int s = 0; s < slots; s++) {
                        String element = f.isArray ? name + "[" + s + "]" : name;
                        emit(dump, element, scalar(cook.data, at + f.offset + s * f.elemSize, f.storage, f.elemSize));
                    }
                    break;
                }
            }

            if (f.countOffset >= 0 && f.storage != graphdemo.TableCookStorage.STRING &&
                    f.storage != graphdemo.TableCookStorage.BYTES) {
                emit(dump, name + "#count", Integer.toString(used));
            }
            if (f.presentOffset >= 0) {
                emit(dump, name + "#present", cook.data[at + f.presentOffset] != 0 ? "true" : "false");
            }
        }
    }

    private static void emit(StringBuilder dump, String path, String value) {
        dump.append("  ").append(path).append(" = ").append(value).append('\n');
    }

    // A string's or a `bytes`' USED bytes, without the zero tail past them (§7.2)
    // — printed the same way on both legs, non-printable bytes escaped so the
    // comparison stays a byte comparison of text.
    private static String text(byte[] data, int at, int used) {
        if (used < 0) {
            used = 0;
        }
        StringBuilder b = new StringBuilder();
        b.append('"');
        for (int i = 0; i < used; i++) {
            int c = data[at + i] & 0xff;
            if (c >= 0x20 && c < 0x7f && c != '"' && c != '\\') {
                b.append((char) c);
            } else {
                b.append("\\x").append(String.format("%02x", c));
            }
        }
        b.append('"');
        b.append(" len=").append(used);
        return b.toString();
    }

    private static String scalar(byte[] data, int at, graphdemo.TableCookStorage storage, int width) {
        switch (storage) {
            case BOOL:
                return data[at] != 0 ? "true" : "false";
            case FLOAT:
                // Nothing in the pointered corpus is a float, and a canonical
                // cross-language spelling of one is a decision this gate should not
                // make in passing. The day a float arrives, the gate says so rather
                // than drifting.
                fail("the dump met a float, whose canonical cross-language spelling this gate does not fix");
                return "";
            case SIGNED:
                switch (width) {
                    case 1: return Byte.toString(graphdemo.TableBytes.i8(data, at));
                    case 2: return Short.toString(graphdemo.TableBytes.i16(data, at));
                    case 4: return Integer.toString(graphdemo.TableBytes.i32(data, at));
                    default: return Long.toString(graphdemo.TableBytes.i64(data, at));
                }
            default:
                switch (width) {
                    case 1: return Integer.toString(graphdemo.TableBytes.u8(data, at));
                    case 2: return Integer.toString(graphdemo.TableBytes.u16(data, at));
                    case 4: return Long.toString(graphdemo.TableBytes.u32(data, at));
                    default: return Long.toUnsignedString(graphdemo.TableBytes.i64(data, at));
                }
        }
    }

    private static int surfaceCook(String out) throws IOException {
        for (String[] f : kind("cook")) {
            Root r = rootNamed(f[3]);
            byte[] file = Files.readAllBytes(Paths.get(f[4]));
            CookHandle cook = r.open.open(file, 0, file.length);
            if (cook == null) {
                System.err.println("driver: " + f[1] + " does not open");
                return 1;
            }
            reachedOffsets.clear();
            reachedTypes.clear();
            StringBuilder dump = new StringBuilder();
            node(dump, cook, 0, r.type, 0);
            spill(out, f[1], dump.toString());
        }
        return 0;
    }

    // the `cook-forgery` surface: the whole battery as DATA, one verdict per row.
    // <pointer> is the BUFFER the caller holds — 0 an aligned base, 1..63 that
    // many bytes past one, `null` no buffer at all. An unaligned base is a
    // pointer fact rather than a file fact, which is why it is a column; in Java
    // it is the base OFFSET's residue, which is the same arithmetic.
    private static int surfaceCookForgery(String out) throws IOException {
        for (String[] f : kind("forgery")) {
            if (!f[2].equals("cook")) {
                continue;
            }
            Root r = rootNamed(f[3]);
            byte[] source = Files.readAllBytes(Paths.get(f[4]));
            long claim = Long.parseLong(f[5]);
            if (claim < 0) {
                claim = source.length;
            }
            boolean nullBuffer = f[6].equals("null");
            int lead = nullBuffer ? 0 : Integer.parseInt(f[6]);
            boolean opened;
            if (nullBuffer) {
                opened = r.open.open(null, 0, claim) != null;
            } else {
                Placed p = place(source, claim, lead);
                opened = r.open.open(p.data, p.base, p.length) != null;
            }
            spill(out, f[1], opened ? "open\n" : "refuse\n");
        }
        return 0;
    }

    public static int run(String[] args) throws IOException {
        if (args.length < 2) {
            System.err.println("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>");
            return 2;
        }
        readManifest(args[0]);
        String surface = args[1];
        if (surface.equals("list")) {
            System.out.print("wire\nreport\njson-read\njson-write\njson-hostile\nblock\nblock-foreign\n"
                    + "block-dump\nforgery\ncook\ncook-foreign\ncook-forgery\n");
            return 0;
        }
        if (args.length < 3) {
            System.err.println("usage: driver <manifest> <surface> <outdir>");
            return 2;
        }
        String out = args[2];
        switch (surface) {
            case "wire": return surfaceWire(out);
            case "report": return surfaceReport(out);
            case "json-read": return surfaceJsonRead(out);
            case "json-write": return surfaceJsonWrite(out);
            case "json-hostile": return surfaceJsonHostile(out);
            case "block": return surfaceBlock(out);
            case "block-foreign": return surfaceBlockForeign(out);
            case "block-dump": return surfaceBlockDump(out);
            case "forgery": return surfaceForgery(out);
            case "cook": return surfaceCook(out);
            case "cook-foreign": return surfaceCookForeign(out);
            case "cook-forgery": return surfaceCookForgery(out);
            default: return 2;
        }
    }

    public static void main(String[] args) throws IOException {
        System.exit(run(args));
    }
}
