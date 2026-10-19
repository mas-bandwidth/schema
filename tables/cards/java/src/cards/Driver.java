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
        final String edit;
        Case(String surface, String name, String input) {
            this(surface, name, input, null);
        }
        Case(String surface, String name, String input, String edit) {
            this.surface = surface;
            this.name = name;
            this.input = input;
            this.edit = edit;
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
            if (f.length > 3) {
                out.add(new Case(f[0], f[1], f[2], f[3]));
            } else {
                out.add(new Case(f[0], f[1], f[2]));
            }
        }
        return out;
    }

    static void one(String surface, Case c, Path outdir) throws IOException {
        byte[] input;
        if (surface.equals("view")) {
            input = new byte[0];
        } else {
            input = Files.readAllBytes(Paths.get(c.input));
        }
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
                case "upgrade": {
                    Cards.UpgradeResult r = Cards.upgrade(input);
                    if (System.getenv("CARDS_JAVA_NEGATIVE_UPGRADE") != null
                            && r.report.retained > 0) {
                        writeRefused(outdir, c.name, "malformed");
                        break;
                    }
                    write(outdir, c.name, "wire", r.wire);
                    writeReport(outdir, c.name, r.report);
                    break;
                }
                case "edit": {
                    Cards.Record rec = applyEdit(input, c.edit);
                    write(outdir, c.name, "wire", rec.wire());
                    break;
                }
                case "taskhash": {
                    if (System.getenv("CARDS_JAVA_NEGATIVE_HASH") != null) {
                        write(outdir, c.name, "hex",
                            "0000000000000000000000000000000000000000000000000000000000000000"
                                .getBytes(StandardCharsets.UTF_8));
                        break;
                    }
                    write(outdir, c.name, "hex",
                        Cards.taskHash(input).getBytes(StandardCharsets.UTF_8));
                    break;
                }
                case "view": {
                    write(outdir, c.name, "txt", view());
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

    // applyEdit applies one edit line — "needs=a,b", "tier=heavy",
    // "priority=blocker", "paths=x,y", "column=review", "holder=h" — through
    // the Set* edit of the same name (tables/cards/edit.go).
    static Cards.Record applyEdit(byte[] wire, String edit) throws Cards.Refusal {
        if (edit == null || edit.indexOf('=') < 0) {
            throw new Cards.Refusal("malformed");
        }
        String key = edit.substring(0, edit.indexOf('='));
        String value = edit.substring(edit.indexOf('=') + 1);
        switch (key) {
            case "needs":
                return Cards.setNeeds(wire, splitList(value));
            case "paths":
                return Cards.setPaths(wire, splitList(value));
            case "tier":
                return Cards.setTier(wire, parseTier(value));
            case "priority":
                return Cards.setPriority(wire, parsePriority(value));
            case "column":
                return Cards.setColumn(wire, parseColumn(value));
            case "holder":
                return Cards.setHolder(wire, value);
            default:
                throw new Cards.Refusal("malformed");
        }
    }

    static List<String> splitList(String v) {
        List<String> out = new ArrayList<>();
        if (v.isEmpty()) {
            return out;
        }
        for (String part : v.split(",", -1)) {
            out.add(part);
        }
        return out;
    }

    static int parseTier(String s) throws Cards.Refusal {
        switch (s) {
            case "flash": return 1;
            case "pro": return 2;
            case "heavy": return 3;
            case "frontier": return 4;
            default: throw new Cards.Refusal("malformed");
        }
    }

    static int parsePriority(String s) throws Cards.Refusal {
        switch (s) {
            case "normal": return 1;
            case "high": return 2;
            case "critical": return 3;
            case "blocker": return 4;
            default: throw new Cards.Refusal("malformed");
        }
    }

    static int parseColumn(String s) throws Cards.Refusal {
        switch (s) {
            case "waiting": return 1;
            case "ready": return 2;
            case "working": return 3;
            case "review": return 4;
            case "merging": return 5;
            case "landed": return 6;
            case "plan": return 7;
            case "check": return 8;
            default: throw new Cards.Refusal("malformed");
        }
    }

    // view renders the generated view: every field of every table in the unit,
    // in declaration order, one line each (docs/SPEC-TABLES.md §8, FORMAT.md
    // "The view"). The type-name vocabulary is reconstructed from the generated
    // descriptors; lists and enums map by the field the schema declares.
    public static byte[] view() {
        TableTypeInfo[] tables = {
            CardDescriptor.tableType(),
            CardIDDescriptor.tableType(),
            ConsumerDescriptor.tableType(),
            KeyValueDescriptor.tableType(),
            PathTextDescriptor.tableType()
        };
        StringBuilder b = new StringBuilder();
        for (TableTypeInfo t : tables) {
            for (TableFieldInfo f : t.fields) {
                b.append(t.name).append(' ')
                 .append(f.name).append(' ')
                 .append(typeName(t.name, f)).append(' ')
                 .append(f.shape).append(' ')
                 .append(f.bound > 0 ? Long.toString(f.bound) : "-")
                 .append('\n');
            }
        }
        return b.toString().getBytes(StandardCharsets.UTF_8);
    }

    static String typeName(String table, TableFieldInfo f) {
        switch (f.kind) {
            case 12: case 17: return "string";
            case 4: return "int32";
            case 5: return "int64";
            case 30:
                if (table.equals("Card")) {
                    if (f.name.equals("tier")) { return "Tier"; }
                    if (f.name.equals("priority")) { return "Priority"; }
                    if (f.name.equals("column")) { return "Column"; }
                }
                return "?";
            case 14:
                if (table.equals("Card")) {
                    if (f.name.equals("paths") || f.name.equals("_new") || f.name.equals("shared")) { return "PathText"; }
                    if (f.name.equals("needs")) { return "CardID"; }
                    if (f.name.equals("extra")) { return "KeyValue"; }
                    if (f.name.equals("consumers")) { return "Consumer"; }
                }
                return "?";
            default:
                return "?";
        }
    }
}
