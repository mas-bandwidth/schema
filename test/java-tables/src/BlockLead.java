/*
    THE JAVA BASE-ALIGNMENT GATE (docs/PORTING.md I5, #387).

    §19.1 lays a block at a 64-byte aligned base and §19.2 checks it. A Java
    caller holds an array and an index, so the residue that decides the check is
    the OFFSET's, and the gate is the index a caller passes beside its array.
    This copies every committed block image to offset L of a fresh array for
    every L in 0..64 and opens at offset L: 0 and 64 must open (and a row must
    read), 1..63 must answer null. Any other answer fails by name with the
    image, the lead and the verdict, in the words of the Elixir gate.

    Prints the gate line and exits 0 — no test framework, the exit code is the
    verdict.
*/

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class BlockLead {
    private BlockLead() {}

    private static void fail(String what) {
        System.out.flush();
        System.err.println();
        System.err.println("FAILED: " + what);
        System.err.println();
        System.exit(1);
    }

    // every field of every row, through the DESCRIPTORS — the reflective read a
    // walker makes, and the guarantee that an image that opens also has rows
    // that read.
    private static void readColumn(byte[] data, int at, blockdemo.TableBlockInfo info) {
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.outOfLine) {
                continue;
            }
            int slots = f.isArray ? f.arrayBound : 1;
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                if (f.element() != null) {
                    readColumn(data, value, f.element());
                } else if (f.elemSize > 0) {
                    switch (f.elemSize) {
                        case 1: blockdemo.TableBytes.u8(data, value); break;
                        case 2: blockdemo.TableBytes.u16(data, value); break;
                        case 4: blockdemo.TableBytes.u32(data, value); break;
                        default: blockdemo.TableBytes.i64(data, value); break;
                    }
                }
            }
        }
    }

    private static void readBlock(byte[] data, int base, blockdemo.TableBlockInfo info) {
        readColumn(data, base, info);
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (!f.outOfLine) {
                continue;
            }
            long offsetOf = blockdemo.TableBytes.i64(data, base + f.offsetOfOffset);
            long count = blockdemo.TableBytes.u32(data, base + f.countOffset);
            long stride = blockdemo.TableBytes.u32(data, base + f.strideOffset);
            for (long r = 0; r < count; r++) {
                readColumn(data, base + (int) (offsetOf + r * stride), f.element());
            }
        }
    }

    // the image at offset `lead` of a fresh array, opened at that offset. 0 and
    // 64 return true (and read a row), 1..63 return false (null).
    private static boolean openAndRead(String name, byte[] image, int lead) {
        int claim = image.length;
        byte[] buffer = new byte[claim + lead];
        System.arraycopy(image, 0, buffer, lead, claim);
        if (name.startsWith("block_render")) {
            blockdemo.RenderFrameBlock b = blockdemo.RenderFrameBlock.open(buffer, lead, claim);
            if (b == null) {
                return false;
            }
            readBlock(b.data(), b.base(), blockdemo.RenderFrameBlock.type());
            return true;
        }
        blockdemo.PaddedFrameBlock p = blockdemo.PaddedFrameBlock.open(buffer, lead, claim);
        if (p == null) {
            return false;
        }
        readBlock(p.data(), p.base(), blockdemo.PaddedFrameBlock.type());
        return true;
    }

    public static void main(String[] args) throws IOException {
        String[] names = {"block_render", "block_padded"};
        byte[][] images = new byte[names.length][];
        for (int i = 0; i < names.length; i++) {
            String path = (args.length > i && !args[i].isEmpty())
                    ? args[i] : "testdata/wire/tables/" + names[i] + ".bin";
            images[i] = Files.readAllBytes(Paths.get(path));
        }

        for (int i = 0; i < names.length; i++) {
            for (int lead = 0; lead <= 64; lead++) {
                boolean want = (lead % 64 == 0);
                boolean got = openAndRead(names[i], images[i], lead);
                if (got != want) {
                    fail("BASE ALIGNMENT GATE FAILED: " + names[i] + " at lead " + lead +
                            " answered " + (got ? "open" : "null") +
                            ", wanted " + (want ? "open" : "null") +
                            " — §19.2 checks the base's alignment and lead is how this leg carries it");
                }
            }
        }

        System.out.println("java base-alignment gate: " + names.length +
                " block image(s) x 65 leads: 0 and 64 open, 1..63 refuse");
    }
}
