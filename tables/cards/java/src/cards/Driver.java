// The Java leg of the card conformance set (tables/cards/conformance/FORMAT.md,
// test/conformance/README.md "The shape"). It answers the driver contract
// `<driver> <manifest> list` and `<driver> <manifest> <surface> <outdir>` over
// this card's five surfaces — load, save, render, parse and bounds — through the
// owning facade in cards.Cards. The second half's four surfaces (upgrade, edit,
// taskhash, view) belong to a-java-card-upgrades-edits-and-walks-the-shared-
// corpus and answer absent (exit 2).
//
// A test-only environment switch, CARDS_JAVA_NEGATIVE_RENDER, changes one
// rendered byte so the negative-control target can prove the render surface
// bites; nothing else reads it.
package cards;

import java.io.IOException;
import java.io.PrintWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;

public final class Driver {
    private Driver() {}

    static final String[] SURFACES = {
        "load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"
    };

    public static void main(String[] args) throws Exception {
        if (args.length < 2) {
            System.err.println("usage: cards-driver <manifest> (list | <surface> <outdir>)");
            System.exit(1);
        }
        String manifest = args[0];
        String verb = args[1];
        if (verb.equals("list")) {
            PrintWriter w = new PrintWriter(System.out);
            for (String s : SURFACES) {
                w.println(s);
            }
            w.flush();
            return;
        }
        if (args.length < 3) {
            System.err.println("usage: cards-driver <manifest> <surface> <outdir>");
            System.exit(1);
        }
        String surface = verb;
        Path outdir = Paths.get(args[2]);
        // The second half's surfaces answer absent (exit 2).
        if (surface.equals("upgrade") || surface.equals("edit")
                || surface.equals("taskhash") || surface.equals("view")) {
            System.exit(2);
        }
        List<Case> cases = readManifest(manifest);
        Files.createDirectories(outdir);
        for (Case c : cases) {
            if (!c.surface.equals(surface)) {
                continue;
            }
            one(surface, c, outdir);
        }
    }

    static final class Case {
        final String surface;
        final String name;
        final String input;
        Case(String surface, String name, String input) {
            this.surface = surface;
            this.name = name;
            this.input = input;
        }
    }

    static List<Case> readManifest(String path) throws IOException {
        List<Case> out = new ArrayList<>();
        for (String raw : Files.readAllLines(Paths.get(path), StandardCharsets.UTF_8)) {
            String line = raw.trim();
            if (line.isEmpty() || line.startsWith("#")) {
                continue;
            }
            String[] f = line.split("\\s+");
            if (f.length < 3) {
                throw new IOException("malformed manifest line " + line);
            }
            out.add(new Case(f[0], f[1], f[2]));
        }
        return out;
    }

    static void one(String surface, Case c, Path outdir) throws IOException {
        byte[] input = Files.readAllBytes(Paths.get(c.input));
        try {
            switch (surface) {
                case "load": {
                    Cards.Record rec = Cards.load(input);
                    byte[] json = Cards.toJSON(rec);
                    write(outdir, c.name, "json", json);
                    writeReport(outdir, c.name, rec.report);
                    break;
                }
                case "save":
                case "bounds": {
                    Cards.Record rec = Cards.fromJSON(input);
                    write(outdir, c.name, "wire", rec.wire());
                    break;
                }
                case "render": {
                    Cards.Record rec = Cards.load(input);
                    byte[] text = Cards.render(rec);
                    if (System.getenv("CARDS_JAVA_NEGATIVE_RENDER") != null && text.length > 0) {
                        text = text.clone();
                        text[0] = (byte) (text[0] ^ 0x01);
                    }
                    write(outdir, c.name, "txt", text);
                    break;
                }
                case "parse": {
                    Cards.Record rec = Cards.parse(input);
                    write(outdir, c.name, "wire", rec.wire());
                    break;
                }
                default:
                    throw new IOException("unknown surface " + surface);
            }
        } catch (Cards.Refusal r) {
            writeRefused(outdir, c.name, r.cls);
        }
    }

    static void write(Path outdir, String name, String ext, byte[] data) throws IOException {
        Files.write(outdir.resolve(name + "." + ext), data);
    }

    static void writeRefused(Path outdir, String name, String cls) throws IOException {
        write(outdir, name, "refused", (cls + "\n").getBytes(StandardCharsets.UTF_8));
    }

    // writeReport writes the read report, one Name=value line per counter, in
    // the order tables/cards/conformance/FORMAT.md fixes.
    static void writeReport(Path outdir, String name, TableVariable.Report r) throws IOException {
        StringBuilder b = new StringBuilder();
        b.append("Retained=").append(r.retained).append('\n');
        b.append("RetainLost=").append(r.retainLost).append('\n');
        b.append("Widened=").append(r.widened).append('\n');
        b.append("Unknown=").append(r.unknown).append('\n');
        b.append("KindMismatch=").append(r.kindMismatch).append('\n');
        b.append("Clamped=").append(r.clamped).append('\n');
        b.append("Duplicate=0\n");
        b.append("Malformed=").append(r.malformed).append('\n');
        write(outdir, name, "report", b.toString().getBytes(StandardCharsets.UTF_8));
    }
}
