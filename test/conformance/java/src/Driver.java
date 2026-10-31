// THE JAVA CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp and test/conformance/cs/src/Program
// .java's C# sibling, and it is deliberately the same shape: one process per
// surface, every expectation in the data, nothing literal here.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// It answers every surface this port carries from ONE binary — the block
// surfaces, the cook surfaces (cook-forgery included) and the WIRE-CARRYING
// surfaces (wire, report, json-read, json-write) the variable form (form byte
// 1, docs/SPEC-TABLES.md §3) carries for the roots this leg's variable wire
// answers: the public root codecs compiled from the manifest units. Every case a
// surface meets is answered `<case>.absent` (test/conformance/README.md).
// The C# leg splits the cook surfaces off into a second project because its
// cook side lives in another assembly; Java's generated units are packages of
// one classpath, so the corpus units, the block unit and the pointered unit
// compile together and one JVM start-up answers every surface.

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

    private static void spill(String out, String name, byte[] body) throws IOException {
        Files.write(Path.of(out, name), body);
    }

    private static void spill(String out, String name, String body) throws IOException {
        spill(out, name, body.getBytes(StandardCharsets.UTF_8));
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

    private static String openBlock(String name, byte[] bytes, long extent, int lead) {
        // A CLAIM SHORTER THAN THE IMAGE IS A TRUNCATION: place copies what fits
        // and leaves the rest zero, so the array is the claim in that direction.
        long claim = extent < 0 ? bytes.length : extent;
        Placed p = place(bytes, claim, lead);
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
            spill(out, f[1], openBlock(f[1], Files.readAllBytes(Paths.get(f[3])), -1, 0));
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
            spill(out, f[1], openBlock(f[1], swapMagic(Files.readAllBytes(Paths.get(f[3]))), -1, 0));
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
            spill(out, f[1], openBlock(f[3], Files.readAllBytes(Paths.get(f[4])), Long.parseLong(f[5]),
                    f[6].equals("null") ? 0 : Integer.parseInt(f[6])));
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

    /** the generated openReason: the TableRefuseReason value name, or ok. */
    private interface CookReason { String open(byte[] data, int offset, long length); }

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
        CookReason reason;
        graphdemo.TableCookInfo type;
    }

    private static Root root(String name, CookOpen open, CookReason reason, graphdemo.TableCookInfo type) {
        Root r = new Root();
        r.name = name;
        r.open = open;
        r.reason = reason;
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
        }, graphdemo.SceneCook::openReason, graphdemo.SceneCook.type()));
        roots.add(root("Depot", (d, o, n) -> {
            graphdemo.DepotCook c = graphdemo.DepotCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.DepotCook::openReason, graphdemo.DepotCook.type()));
        roots.add(root("Album", (d, o, n) -> {
            graphdemo.AlbumCook c = graphdemo.AlbumCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.AlbumCook::openReason, graphdemo.AlbumCook.type()));
        roots.add(root("TreeNode", (d, o, n) -> {
            graphdemo.TreeNodeCook c = graphdemo.TreeNodeCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.TreeNodeCook::openReason, graphdemo.TreeNodeCook.type()));
        roots.add(root("ListNode", (d, o, n) -> {
            graphdemo.ListNodeCook c = graphdemo.ListNodeCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.ListNodeCook::openReason, graphdemo.ListNodeCook.type()));
        roots.add(root("Marker", (d, o, n) -> {
            graphdemo.MarkerCook c = graphdemo.MarkerCook.open(d, o, n);
            return c == null ? null : handle(c.data(), c.region(), c.regionLength(), c::at);
        }, graphdemo.MarkerCook::openReason, graphdemo.MarkerCook.type()));
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
            case UNION:
            case WIDE:
                // no cook fixture holds a union or a 128-bit slot; their dump
                // spelling is the reference walk's to fix first, not this one's
                fail("the dump met a " + storage + " slot, whose canonical spelling this gate does not fix");
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

    // ---- the WIRE-CARRYING surfaces (docs/SPEC-TABLES.md §3, §3.1, §4, §16, §16.7)
    //
    // The variable form resolves codecs from manifest data. A missing codec
    // remains an explicit absence; an exception from an emitted codec fails.

    private static int failInt(String msg) {
        System.err.println(msg);
        return 1;
    }

    private static boolean noText(String[] f) {
        return f.length > 5 && "no-text".equals(f[5]);
    }

    // sharesABlob answers whether two slots of the value graph hold the SAME
    // String or byte[] instance: the one node a tolerant load keeps for a blob
    // two slots name (docs/SPEC-TABLES.md §2.5), which every writer refuses.
    private static boolean sharesABlob(Object root) {
        java.util.IdentityHashMap<Object, Boolean> seen = new java.util.IdentityHashMap<>();
        java.util.IdentityHashMap<Object, Boolean> blobs = new java.util.IdentityHashMap<>();
        java.util.ArrayDeque<Object> work = new java.util.ArrayDeque<>();
        work.push(root);
        while (!work.isEmpty()) {
            Object o = work.pop();
            if (seen.put(o, Boolean.TRUE) != null) {
                continue;
            }
            if (o instanceof java.util.List) {
                for (Object e : (java.util.List<?>) o) {
                    if (e instanceof String || e instanceof byte[]) {
                        if (!"".equals(e) && blobs.put(e, Boolean.TRUE) != null) return true;
                    } else if (e != null) {
                        work.push(e);
                    }
                }
                continue;
            }
            if (o instanceof java.util.Map || o.getClass().isArray() || o.getClass().getName().startsWith("java.")) {
                continue;
            }
            for (java.lang.reflect.Field fd : o.getClass().getFields()) {
                if (java.lang.reflect.Modifier.isStatic(fd.getModifiers())) continue;
                Object v;
                try { v = fd.get(o); } catch (IllegalAccessException e) { continue; }
                if (v == null) continue;
                if (v instanceof String || v instanceof byte[]) {
                    // an empty String is the shared "" default literal, never a
                    // node the load handed out, so it is not counted
                    if (!"".equals(v) && blobs.put(v, Boolean.TRUE) != null) return true;
                } else if (!fd.getType().isPrimitive()) {
                    work.push(v);
                }
            }
        }
        return false;
    }

    private static void spillAbsent(String out, String name) throws IOException {
        Files.write(Path.of(out, name + ".absent"), new byte[0]);
    }

    // spillAbsentReason is spillAbsent with the reason the spec gives, written
    // into the marker so the matrix's absence names its rule.
    private static void spillAbsentReason(String out, String name, String reason) throws IOException {
        Files.write(Path.of(out, name + ".absent"), reason.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    // Resolve only public root codecs actually emitted for this unit. Entry
    // body helpers are package-private and cannot appear as conformance roots.
    private static final class VariableCodec {
        final Class<?> codec;
        final Class<?> reportType;
        final Class<?> valueType;
        VariableCodec(String pkg, String root) throws ReflectiveOperationException {
            codec = Class.forName(pkg + "." + root + "Variable");
            reportType = Class.forName(pkg + ".TableVariable$Report");
            valueType = Class.forName(pkg + "." + root + "Variable$Value");
            codec.getMethod("load", byte[].class, reportType);
        }
        Object report() throws ReflectiveOperationException { return reportType.getConstructor().newInstance(); }
        Object load(byte[] data, Object report) throws ReflectiveOperationException { return codec.getMethod("load", byte[].class, reportType).invoke(null, data, report); }
        Object fromJson(byte[] data, Object report) throws ReflectiveOperationException { return codec.getMethod("fromJson", byte[].class, reportType).invoke(null, data, report); }
        byte[] save(Object value) throws ReflectiveOperationException { return (byte[]) codec.getMethod("save", valueType).invoke(null, value); }
        byte[] toJson(Object value) throws ReflectiveOperationException { return (byte[]) codec.getMethod("toJson", valueType).invoke(null, value); }
        int count(Object report, String field) throws ReflectiveOperationException { return reportType.getField(field).getInt(report); }
        boolean flag(Object report, String field) throws ReflectiveOperationException { return reportType.getField(field).getBoolean(report); }
    }

    // The variable form is emitted for every table the unit holds, so the
    // codec is looked up for every unit. A unit on another classpath, or a
    // root the unit does not declare, is still an explicit absence below.
    private static boolean variablePkgCarried(String pkg) {
        return true;
    }

    // The manifest names a unit by its key; the emitted classes live in the
    // unit's schema package. The two differ for tables/scalars (package
    // scalardemo) and test/tables/Scalars2.schema (package scalardemo2), so
    // the codec is looked up
    // under the package, never under the unit key.
    private static String variableUnitPackage(String unit) {
        switch (unit) {
            case "scalars": return "scalardemo";
            case "tblscalars2": return "scalardemo2";
            default: return unit;
        }
    }

    private static VariableCodec variableCodec(String unit, String root) throws ReflectiveOperationException {
        if (!variablePkgCarried(unit)) return null;
        try { return new VariableCodec(variableUnitPackage(unit), root); }
        catch (ClassNotFoundException | NoSuchMethodException absent) { return null; }
    }

    // json-hostile: one tree per rule the text form states (§16.2, §16.3,
    // §17.5). The answer is the REPORT the read produces, or `refused`.
    private static int surfaceJsonHostile(String out) throws IOException {
        for (String[] f : kind("json-hostile")) {
            try {
                VariableCodec codec = variableCodec(f[2], f[3]);
                if (codec == null) { spillAbsent(out, f[1]); continue; }
                Object report = codec.report();
                byte[] text = Files.readAllBytes(Paths.get(f[4], f[3] + ".json"));
                Object value = codec.fromJson(text, report);
                boolean bad = value == null || codec.flag(report, "malformed") || codec.flag(report, "refused");
                if (bad) {
                    spill(out, f[1], "refused\n");
                } else {
                    spill(out, f[1], String.format("%d,%d,%d,%d,%d,false,read\n",
                            codec.count(report, "unknown"), codec.count(report, "kindMismatch"),
                            codec.count(report, "widened"), codec.count(report, "clamped"),
                            codec.count(report, "duplicate")));
                }
            } catch (java.lang.reflect.InvocationTargetException thrown) {
                // A hostile tree is allowed to be refused by breaking the read;
                // the oracle says `refused` for every rule whose text a reader
                // cannot take. A reflection failure that is NOT the codec (a
                // missing class or method) still fails the surface.
                Throwable cause = thrown.getCause();
                if (cause instanceof RuntimeException || cause instanceof Error) {
                    spill(out, f[1], "refused\n");
                } else {
                    return failInt(f[1] + ": JSON hostile reader failed: " + thrown);
                }
            } catch (ReflectiveOperationException failure) { return failInt(f[1] + ": JSON hostile reader failed: " + failure); }
        }
        return 0;
    }

    private static int surfaceWire(String out) throws IOException {
        for (String[] f : kind("instance")) {
            try {
                VariableCodec codec = variableCodec(f[2], f[3]);
                if (codec == null) { spillAbsent(out, f[1]); continue; }
                Object report = codec.report();
                Object value = codec.load(Files.readAllBytes(Paths.get(f[4])), report);
                if (value == null || codec.flag(report, "refused")) return failInt(f[1] + ": load refused " + f[4]);
                byte[] saved = codec.save(value);
                if (saved == null && sharesABlob(value)) {
                    // §2.5: a blob two slots name is refused by every writer; the
                    // tolerant load kept the one node, so the save refuses it by
                    // name rather than writing the reference's bytes
                    Files.write(Path.of(out, f[1] + ".absent"), "blob_shared".getBytes(java.nio.charset.StandardCharsets.UTF_8));
                    continue;
                }
                if (saved == null) return failInt(f[1] + ": save refused the value");
                spill(out, f[1], saved);
            } catch (ReflectiveOperationException failure) { return failInt(f[1] + ": wire codec failed: " + failure); }
        }
        return 0;
    }

    private static int surfaceReport(String out) throws IOException {
        for (String[] f : kind("report")) {
            try {
                VariableCodec codec = variableCodec(f[2], f[3]);
                if (codec == null) { spillAbsent(out, f[1]); continue; }
                Object report = codec.report();
                boolean ok = codec.load(Files.readAllBytes(Paths.get(f[4])), report) != null;
                boolean refused = codec.flag(report, "refused");
                boolean malformed = codec.flag(report, "malformed") || (!ok && !refused);
                spill(out, f[1], String.format("%d,%d,%d,%d,%d,%s,%s\n",
                        codec.count(report, "unknown"), codec.count(report, "kindMismatch"),
                        codec.count(report, "widened"), codec.count(report, "clamped"),
                        codec.count(report, "duplicate"), malformed, refused ? "refused" : "read"));
            } catch (ReflectiveOperationException failure) { return failInt(f[1] + ": report codec failed: " + failure); }
        }
        return 0;
    }

    // THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the read report as the
    // `report` surface prints it, then the §16 text of the value the read left,
    // unless the row says `no-value` or the read was refused. A derived line is
    // `framing <case> <unit> <root> <wire> [no-value]`. A damaged read owes the
    // refusal alone (§4, DAMAGE DISCARDS THE WHOLE LOAD), so its value is not
    // written.
    private static int surfaceFraming(String out) throws IOException {
        for (String[] f : kind("framing")) {
            try {
                VariableCodec codec = variableCodec(f[2], f[3]);
                if (codec == null) { spillAbsent(out, f[1]); continue; }
                Object report = codec.report();
                Object value = codec.load(Files.readAllBytes(Paths.get(f[4])), report);
                boolean refused = codec.flag(report, "refused");
                boolean malformed = codec.flag(report, "malformed") || (value == null && !refused);
                StringBuilder answer = new StringBuilder(String.format("%d,%d,%d,%d,%d,%s,%s\n",
                        codec.count(report, "unknown"), codec.count(report, "kindMismatch"),
                        codec.count(report, "widened"), codec.count(report, "clamped"),
                        codec.count(report, "duplicate"), malformed, refused ? "refused" : "read"));
                boolean noValue = f.length > 5 && f[5].equals("no-value");
                byte[] head = answer.toString().getBytes(java.nio.charset.StandardCharsets.UTF_8);
                if (refused || malformed || noValue) { spill(out, f[1], head); continue; }
                byte[] text = codec.toJson(value);
                if (text == null) return failInt(f[1] + ": toJson refused the value");
                byte[] both = new byte[head.length + text.length];
                System.arraycopy(head, 0, both, 0, head.length);
                System.arraycopy(text, 0, both, head.length, text.length);
                spill(out, f[1], both);
            } catch (ReflectiveOperationException failure) { return failInt(f[1] + ": framing codec failed: " + failure); }
        }
        return 0;
    }

    // ---- THE REASON BESIDE THE NULL (docs/SPEC-TABLES.md §6.5, §7, §19.2)
    //
    // cook-reason and block-reason (test/conformance/README.md): each forgery of
    // the kind, placed exactly as its line asks (the claim, the lead, or no
    // buffer at all) and the generated openReason's answer: the
    // TableRefuseReason value name of the FIRST failing clause, or `ok`. The
    // harness compares the rows the manifest's `refusal` lines name. The reason
    // is the generated reader's own: this driver places bytes and prints what it
    // is told, and judges nothing.
    private static String blockReason(String name, byte[] data, int base, long length) {
        if (name.startsWith("block_render")) {
            return blockdemo.RenderFrameBlock.openReason(data, base, length);
        }
        if (name.startsWith("block_padded")) {
            return blockdemo.PaddedFrameBlock.openReason(data, base, length);
        }
        System.err.println("driver: no block named " + name);
        System.exit(1);
        return "";
    }

    private static int surfaceReason(String out, String form) throws IOException {
        for (String[] f : kind("forgery")) {
            if (!f[2].equals(form)) {
                continue;
            }
            byte[] source = Files.readAllBytes(Paths.get(f[4]));
            long claim = Long.parseLong(f[5]);
            if (claim < 0) {
                claim = source.length;
            }
            String pointer = f.length > 6 ? f[6] : "0";
            String reason;
            if (pointer.equals("null")) {
                reason = form.equals("cook") ? rootNamed(f[3]).reason.open(null, 0, claim)
                                             : blockReason(f[3], null, 0, claim);
            } else {
                Placed p = place(source, claim, Integer.parseInt(pointer));
                reason = form.equals("cook") ? rootNamed(f[3]).reason.open(p.data, p.base, p.length)
                                             : blockReason(f[3], p.data, p.base, p.length);
            }
            spill(out, f[1], reason + "\n");
        }
        return 0;
    }

    private static int surfaceJsonRead(String out) throws IOException {
        for (String[] f : kind("instance")) {
            if (noText(f)) continue;
            try {
                VariableCodec codec = variableCodec(f[2], f[3]);
                if (codec == null) { spillAbsent(out, f[1]); continue; }
                Object report = codec.report();
                Object value = codec.fromJson(Files.readAllBytes(Paths.get("testdata/conformance/tables/json", f[1] + ".json")), report);
                if (value == null || codec.flag(report, "malformed") || codec.flag(report, "refused")) return failInt(f[1] + ": fromJson refused the text");
                byte[] saved = codec.save(value);
                if (saved == null) return failInt(f[1] + ": save refused the value");
                spill(out, f[1], saved);
            } catch (ReflectiveOperationException failure) { return failInt(f[1] + ": JSON reader failed: " + failure); }
        }
        return 0;
    }

    private static int surfaceJsonWrite(String out) throws IOException {
        for (String[] f : kind("instance")) {
            if (noText(f)) continue;
            try {
                VariableCodec codec = variableCodec(f[2], f[3]);
                if (codec == null) { spillAbsent(out, f[1] + ".json"); continue; }
                Object report = codec.report();
                Object value = codec.load(Files.readAllBytes(Paths.get(f[4])), report);
                if (value == null || codec.flag(report, "refused")) return failInt(f[1] + ": load refused " + f[4]);
                byte[] text = codec.toJson(value);
                if (text == null) return failInt(f[1] + ": toJson refused the value");
                spill(out, f[1] + ".json", text);
            } catch (ReflectiveOperationException failure) { return failInt(f[1] + ": JSON writer failed: " + failure); }
        }
        return 0;
    }

    private static final class CookCodec {
        final VariableCodec variable;
        final Class<?> cook;
        final java.lang.reflect.Method measure;
        final java.lang.reflect.Method measureReason;
        final java.lang.reflect.Method cookFrom;
        CookCodec(String pkg, String root, Class<?> cook) throws ReflectiveOperationException {
            variable = new VariableCodec(pkg, root);
            this.cook = cook;
            measure = cook.getMethod("cookMeasure", Object.class);
            measureReason = cook.getMethod("cookMeasureReason", Object.class);
            cookFrom = cook.getMethod("cookFrom", Object.class, byte[].class, int.class);
        }
        long measure(Object value) throws ReflectiveOperationException {
            return (long) measure.invoke(null, value);
        }
        Object measureReason(Object value) throws ReflectiveOperationException {
            return measureReason.invoke(null, value);
        }
        boolean cookFrom(Object value, byte[] buffer, int order) throws ReflectiveOperationException {
            return (boolean) cookFrom.invoke(null, value, buffer, order);
        }
    }

    // A cook-write row is ABSENT only for a reason the generator wrote down
    // (docs/SPEC-TABLES.md §7.6), never because a lookup failed:
    //   - the root's <Root>Cook is the generated STUB (cook.go emits a class
    //     with a private constructor and no method when the root's closure
    //     holds a construct the Java cook form does not carry, an unbounded
    //     array), or
    //   - the unit has no cook form in this leg at all: neither <Root>Cook nor
    //     the unit's TableCookInfo exists (the wide-kind unit gets no
    //     accelerators, and a unit off this classpath has neither).
    // Every other failure (a missing <Root>Cook in a unit that has a cook
    // form, a real writer whose cookMeasure, cookMeasureReason or cookFrom
    // lookup breaks, a Value class that will not load) is thrown, and the
    // surface FAILS, so a renamed or re-typed writer can never turn its rows
    // absent and leave the cell green.
    private static CookCodec cookCodec(String unit, String root) throws ReflectiveOperationException {
        String pkg = variableUnitPackage(unit);
        Class<?> cook;
        try {
            cook = Class.forName(pkg + "." + root + "Cook");
        } catch (ClassNotFoundException missing) {
            try { Class.forName(pkg + ".TableCookInfo"); }
            catch (ClassNotFoundException noCookForm) { return null; }
            throw missing;
        }
        if (cook.getDeclaredMethods().length == 0) { return null; }
        return new CookCodec(pkg, root, cook);
    }

    private static String[] instanceNamed(String name) {
        for (String[] f : kind("instance")) {
            if (f[1].equals(name)) { return f; }
        }
        return null;
    }

    private static int surfaceCookWrite(String out) throws IOException {
        for (String[] f : kind("cook-write")) {
            String[] inst = instanceNamed(f[1]);
            if (inst == null) { return failInt("missing instance " + f[1]); }
            CookCodec codec;
            try { codec = cookCodec(inst[2], inst[3]); }
            catch (ReflectiveOperationException broken) {
                return failInt(f[1] + ": cook writer lookup failed: " + broken);
            }
            if (codec == null) {
                spillAbsent(out, f[1]);
                spillAbsent(out, f[1] + "-be");
                continue;
            }
            try {
                Object report = codec.variable.report();
                Object value = codec.variable.load(Files.readAllBytes(Paths.get(inst[4])), report);
                if (value == null || codec.variable.flag(report, "refused") || codec.variable.flag(report, "malformed")) {
                    return failInt(f[1] + ": cook input did not load");
                }
                long size = codec.measure(value);
                if (size < 1) { return failInt(f[1] + ": cookMeasure refused"); }
                Object reason = codec.measureReason(value);
                long rBytes = (long) reason.getClass().getMethod("bytes").invoke(reason);
                String rText = (String) reason.getClass().getMethod("reason").invoke(reason);
                if (rBytes != size || !"ok".equals(rText)) {
                    return failInt(f[1] + ": cookMeasureReason disagreed with cookMeasure");
                }
                for (int order : new int[] { 1, 2 }) {
                    String name = order == 2 ? f[1] + "-be" : f[1];
                    byte[] shortBuf = new byte[(int) size - 1];
                    java.util.Arrays.fill(shortBuf, (byte) 0xa5);
                    if (codec.cookFrom(value, shortBuf, order)) {
                        return failInt(name + ": short buffer succeeded");
                    }
                    for (byte b : shortBuf) {
                        if (b != (byte) 0xa5) { return failInt(name + ": short buffer wrote bytes"); }
                    }
                    byte[] buffer = new byte[(int) size];
                    if (!codec.cookFrom(value, buffer, order)) {
                        return failInt(name + ": cookFrom refused");
                    }
                    spill(out, name, buffer);
                }
            } catch (ReflectiveOperationException failure) {
                return failInt(f[1] + ": cook-write codec failed: " + failure);
            }
        }
        return 0;
    }

    private static final class MessageCodec {
        final Class<?> vocabType;
        final Class<?> reportType;
        final Class<?> valueType;
        final Class<?> valueArrayType;
        final java.lang.reflect.Method reset;
        final java.lang.reflect.Method announceRead;
        final java.lang.reflect.Method loadMessages;
        final java.lang.reflect.Method measureMessages;
        final java.lang.reflect.Method saveMessages;

        MessageCodec(String pkg, String root) throws ReflectiveOperationException {
            vocabType = Class.forName(pkg + ".TableVocabulary");
            reportType = Class.forName(pkg + ".TableVocabulary$Report");
            valueType = Class.forName(pkg + "." + root + "Fixed$Value");
            valueArrayType = java.lang.reflect.Array.newInstance(valueType, 0).getClass();
            Class<?> msgType = Class.forName(pkg + "." + root + "Message");
            reset = vocabType.getMethod("reset", int.class);
            announceRead = vocabType.getMethod("announceRead", vocabType, byte[].class, reportType);
            loadMessages = msgType.getMethod("loadMessages", valueArrayType, int.class, vocabType, byte[].class, reportType);
            measureMessages = msgType.getMethod("measureMessages", valueArrayType, int.class, reportType);
            saveMessages = msgType.getMethod("saveMessages", valueArrayType, int.class, byte[].class, reportType);
        }

        Object vocab() throws ReflectiveOperationException {
            Object v = vocabType.getConstructor().newInstance();
            reset.invoke(v, 4096);
            return v;
        }

        Object report() throws ReflectiveOperationException {
            return reportType.getConstructor().newInstance();
        }

        boolean announce(Object vocab, byte[] wire, Object report) throws ReflectiveOperationException {
            return (boolean) announceRead.invoke(null, vocab, wire, report);
        }

        Object newValues(int cap) {
            return java.lang.reflect.Array.newInstance(valueType, cap);
        }

        int load(Object values, int cap, Object vocab, byte[] wire, Object report) throws ReflectiveOperationException {
            return (int) loadMessages.invoke(null, values, cap, vocab, wire, report);
        }

        int measure(Object values, int count, Object report) throws ReflectiveOperationException {
            return (int) measureMessages.invoke(null, values, count, report);
        }

        int save(Object values, int count, byte[] out, Object report) throws ReflectiveOperationException {
            return (int) saveMessages.invoke(null, values, count, out, report);
        }

        boolean flag(Object report, String field) throws ReflectiveOperationException {
            return reportType.getField(field).getBoolean(report);
        }
    }

    private static MessageCodec messageCodec(String unit, String root) throws ReflectiveOperationException {
        String pkg = variableUnitPackage(unit);
        try {
            return new MessageCodec(pkg, root);
        } catch (ClassNotFoundException | NoSuchMethodException absent) {
            return null;
        }
    }

    private static String[] connectionNamed(String key) {
        for (String[] f : kind("connection")) {
            if (f[1].equals(key)) { return f; }
        }
        return null;
    }

    private static int surfaceMessage(String out) throws IOException {
        for (String[] f : kind("message")) {
            String[] conn = connectionNamed(f[2]);
            if (conn == null) { return failInt("missing connection " + f[2]); }
            try {
                MessageCodec codec = messageCodec(conn[2], f[3]);
                if (f[5].equals("-")) {
                    // a named refusal row: the root is a variable table, which
                    // carries no message verbs (docs/SPEC-TABLES.md §3.3: the
                    // message form is fixed tables only)
                    if (codec != null) { return failInt(f[1] + ": a variable table carries a message codec"); }
                    spillAbsentReason(out, f[1], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)");
                    continue;
                }
                if (codec == null) { spillAbsentReason(out, f[1], ""); continue; }
                Object vocab = codec.vocab();
                Object report = codec.report();
                if (!codec.announce(vocab, Files.readAllBytes(Paths.get(conn[4])), report)
                        || codec.flag(report, "malformed") || codec.flag(report, "refused")) {
                    return failInt(f[1] + ": announceRead refused " + conn[4]);
                }
                Object values = codec.newValues(256);
                int count = codec.load(values, 256, vocab, Files.readAllBytes(Paths.get(f[5])), report);
                if (count < 1 || codec.flag(report, "malformed") || codec.flag(report, "refused")) {
                    return failInt(f[1] + ": loadMessages refused " + f[5]);
                }
                int size = codec.measure(values, count, report);
                if (size < 1 || codec.flag(report, "refused")) {
                    return failInt(f[1] + ": measureMessages refused");
                }
                byte[] buf = new byte[size];
                if (codec.save(values, count, buf, report) != size || codec.flag(report, "refused")) {
                    return failInt(f[1] + ": saveMessages refused");
                }
                spill(out, f[1], buf);
            } catch (ReflectiveOperationException failure) {
                return failInt(f[1] + ": message codec failed: " + failure);
            }
        }
        return 0;
    }

    // ---- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6; FORMAT.md, `retain`)
    //
    // RT1 loads what RT2 wrote, with retention on, into caller-owned storage:
    // a byte buffer and an id list, never grown. `short` is ONE BYTE SHORT OF
    // THE LAST RECORD, found the way the page says: load once at a roomy
    // capacity, read what it used, and load again one byte under that. The
    // `retain` surface answers the load's three counters and the retain_lost
    // the save adds; `retain-save` answers the saved file itself.
    private static final int RETAIN_BYTES = 1 << 20;
    private static final int RETAIN_IDS = 1 << 17;

    private static tblrt1.TableRetain retainStore(String ids) {
        return new tblrt1.TableRetain(RETAIN_BYTES, ids.equals("full") ? RETAIN_IDS : Integer.parseInt(ids));
    }

    private static int surfaceRetain(String out, boolean saved) throws IOException {
        for (String[] f : lines) {
            if (!f[0].equals("retain")) {
                continue;
            }
            if (!f[2].equals("tblrt1") || !f[3].equals("Node")) {
                return failInt(f[1] + ": no retaining codec for " + f[2] + " " + f[3]);
            }
            byte[] wire = Files.readAllBytes(Paths.get(f[4]));
            tblrt1.TableVariable.Report report = new tblrt1.TableVariable.Report();
            List<tblrt1.NodeVariable.Value> roots = new ArrayList<>();
            List<tblrt1.TableRetain> stores = new ArrayList<>();
            tblrt1.TableRetain store = retainStore(f[6]);
            if (f[5].equals("short")) {
                tblrt1.NodeRetain.loadRetain(wire, store, new tblrt1.TableVariable.Report());
                store.capacity = store.used - 1;
            }
            roots.add(tblrt1.NodeRetain.loadRetain(wire, store, report));
            stores.add(store);
            if (report.malformed || report.refused || roots.contains(null)) {
                return failInt(f[1] + ": the retaining load refused " + f[4]);
            }
            java.io.ByteArrayOutputStream bytes = new java.io.ByteArrayOutputStream();
            tblrt1.TableVariable.Report save = new tblrt1.TableVariable.Report();
            for (int i = 0; i < roots.size(); i++) {
                long size = tblrt1.NodeRetain.measureRetain(roots.get(i), stores.get(i));
                byte[] file = tblrt1.NodeRetain.saveRetain(roots.get(i), stores.get(i), save);
                if (size < 0 || file == null || file.length != size) {
                    return failInt(f[1] + ": the retaining save refused or disagreed with its measure");
                }
                bytes.write(file);
            }
            if (saved) {
                spill(out, f[1], bytes.toByteArray());
            } else {
                spill(out, f[1], report.retained + "," + report.retainLost + "," + report.unknown + " " + save.retainLost + "\n");
            }
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
            // the WIRE-CARRYING surfaces ride the id-table form (docs/SPEC-TABLES.md
            // §3, §3.1, §16.7) for the roots this leg's variable wire carries —
            // graphdemo's Scene and blobdemo's Catalog — and answer every other
            // case ABSENT, one by one.
            System.out.print("wire\nreport\nframing\nblock-reason\ncook-reason\njson-read\njson-write\njson-hostile\nmessage\n"
                    + "retain\nretain-save\n"
                    + "block\nblock-foreign\n"
                    + "block-dump\nforgery\ncook\ncook-write\ncook-foreign\ncook-forgery\n");
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
            case "framing": return surfaceFraming(out);
            case "block-reason": return surfaceReason(out, "block");
            case "cook-reason": return surfaceReason(out, "cook");
            case "json-read": return surfaceJsonRead(out);
            case "json-write": return surfaceJsonWrite(out);
            case "json-hostile": return surfaceJsonHostile(out);
            case "message": return surfaceMessage(out);
            case "retain": return surfaceRetain(out, false);
            case "retain-save": return surfaceRetain(out, true);
            case "block": return surfaceBlock(out);
            case "block-foreign": return surfaceBlockForeign(out);
            case "block-dump": return surfaceBlockDump(out);
            case "forgery": return surfaceForgery(out);
            case "cook": return surfaceCook(out);
            case "cook-write": return surfaceCookWrite(out);
            case "cook-foreign": return surfaceCookForeign(out);
            case "cook-forgery": return surfaceCookForgery(out);
            default: return 2;
        }
    }

    public static void main(String[] args) throws IOException {
        System.exit(run(args));
    }
}
