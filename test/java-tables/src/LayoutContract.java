/*
    THE JAVA LAYOUT CONTRACT GATE (docs/PORTING.md M11, docs/SPEC-TABLES.md
    §19.3).

    Java has no record layout of its own: the offsets are constants the
    generator wrote, twice, once for the accessors (each Row's `offsets`) and
    once for the descriptors. TableBlockLayout and TableCookLayout hold the two
    against each other, and each array's pitch against its row size, ONCE,
    before the first block or cook opens.

        LayoutContract clean   <block> <cook>   both open
        LayoutContract planted <block> <cook>   the units were compiled from a
                                                copy with one accessor offset
                                                moved, so each open must THROW
                                                naming the layout and the record

    Prints the gate line and exits 0; any other answer fails by name.
*/

import java.nio.file.Files;
import java.nio.file.Paths;

public final class LayoutContract {
    private LayoutContract() {}

    private static void fail(String what) {
        System.out.flush();
        System.err.println();
        System.err.println("FAILED: " + what);
        System.err.println();
        System.exit(1);
    }

    // open answers "open", "null", or the refusal's message.
    private static String open(String which, byte[] bytes) {
        try {
            Object handle =
                    which.equals("block")
                            ? blockdemo.RenderFrameBlock.open(bytes)
                            : graphdemo.SceneCook.open(bytes, 0, bytes.length);
            return handle != null ? "open" : "null";
        } catch (IllegalStateException e) {
            return "threw " + e.getMessage();
        }
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 3 || !(args[0].equals("clean") || args[0].equals("planted"))) {
            System.err.println("usage: LayoutContract clean|planted <block> <cook>");
            System.exit(2);
        }
        boolean planted = args[0].equals("planted");
        byte[] block = Files.readAllBytes(Paths.get(args[1]));
        byte[] cook = Files.readAllBytes(Paths.get(args[2]));
        String[][] cases = {
            {"block", "layout: RenderCameraRow"},
            {"cook", "layout: SceneRow"},
        };
        for (String[] c : cases) {
            String got = open(c[0], c[0].equals("block") ? block : cook);
            if (!planted && !got.equals("open")) {
                fail("the " + c[0] + " of this build answered " + got + "; it must open");
            }
            if (planted && !(got.startsWith("threw schema ") && got.contains(c[1]))) {
                fail(
                        "the "
                                + c[0]
                                + " compiled with a moved accessor offset answered "
                                + got
                                + "; the layout contract must refuse it naming "
                                + c[1]);
            }
        }
        System.out.println(
                planted
                        ? "layout contract: a moved accessor offset is refused before any row is read, block and cook"
                        : "layout contract: this build's block and cook open");
    }
}
