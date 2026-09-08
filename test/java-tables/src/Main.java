/*
    THE JAVA TABLES LEG, under test (docs/SPEC-TABLES.md).

    Three gates the conformance harness does not hold, because none of them is a
    case — each is a SEARCH or a FORGERY. (The tolerant wire's allocation gate and
    soak once stood beside them; they went with the wire's previous form, which
    this port no longer emits — schema#517 brings the id-table form.)

      fuzz  <block> <cook>     the READERS' oracle. Mutants of a block image and
                               a cooked file are handed to the generated Open,
                               and the answer must be a REFUSAL or a read that
                               stays inside the array it was given. An index out
                               of bounds is a refusal; an exception escaping into
                               a caller that asked a question is not, and this is
                               what says so.
      order <le> <be>          the byte-order leg: a cook of THIS reader's order
                               opens and one of the other order refuses.
      extent <cook>            the REFERENCE EXTENT gate (§6.3, §7.4): a root
                               reference whose target STARTS inside the region
                               and whose RECORD does not fit is refused by `at`,
                               not one call later in the caller's field read.

    Prints OK and exits 0 — no test framework, the exit code is the verdict.
*/

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

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

    public static void main(String[] args) throws IOException {
        if (args.length < 1) {
            System.err.println("usage: Main fuzz <block> <cook> | order <le> <be> | extent <cook>");
            System.exit(1);
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
