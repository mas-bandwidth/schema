// THE JAVA UNIT-REGISTRY LISTING (docs/SPEC-TABLES.md §8.7): one printer, with
// no declaration names. TableRegistry.unitView() is its only registry, and the
// listing it prints is byte-compared against the compiler's own
// (internal/viewlisting) by `make tables-java-view`.
//
// The unit is supplied by the build, never by a list in this program: the one
// import line below names the package and make/java.mk rewrites it per unit.
import tabledemo.*; // UNIT

import java.io.PrintStream;
import java.nio.charset.StandardCharsets;

public final class View {
    private View() {}

    private static final PrintStream out = new PrintStream(System.out, false, StandardCharsets.UTF_8);

    private static String hex(long value) { return String.format("%016x", value); }

    // the doc LAST and FLATTENED to one line: each newline written \n and the
    // escape's own backslash \\ (§8.7)
    private static void annotation(String[] tags, String doc) {
        out.print(" tags=[" + String.join(",", tags) + "] doc=");
        out.print(doc.replace("\\", "\\\\").replace("\n", "\\n"));
        out.print("\n");
    }

    private static void vocabulary(String what, TableVocabularyInfo v, String row) {
        out.print(what + " " + v.name + " file=" + v.file + " max=" + v.max + " bits=" + v.storageBits + " variants=" + v.variants.length);
        annotation(v.tags, v.doc);
        for (TableVariantInfo r : v.variants) {
            out.print(what + " " + v.name + " " + row + " " + r.value + " " + r.name + " id=" + hex(r.id));
            annotation(r.tags, r.doc);
        }
    }

    // A union's arms are measured through a field that HOLDS it: the tables in
    // registry order, then the types (§8.7).
    private static boolean held(TableUnitView unit, String name) {
        for (TableViewEntry[] set : new TableViewEntry[][] {unit.tables, unit.types}) {
            for (TableViewEntry entry : set) {
                for (TableFieldInfo f : entry.type.fields) {
                    if (f.named.equals(name)) return true;
                }
            }
        }
        return false;
    }

    private static void union(TableUnitView unit, TableVocabularyInfo v) {
        out.print("union " + v.name + " file=" + v.file + " max=" + v.max + " bits=" + v.storageBits + " variants=" + v.variants.length);
        annotation(v.tags, v.doc);
        boolean holder = held(unit, v.name);
        for (TableVariantInfo r : v.variants) {
            out.print("union " + v.name + " arm " + r.value + " " + r.name + " id=" + hex(r.id)
                    + " payload=" + (r.payloadName == null ? "-" : r.payloadName)
                    + " record=" + (r.payload != null ? "yes" : "no")
                    + " field=" + (r.field != null ? "yes" : "no"));
            if (r.field == null) {
                out.print(" kind=- bound=- overlay=-");
            } else {
                out.print(" kind=" + r.field.kind + " bound=" + r.field.bound + " overlay=" + (holder ? Integer.toString(r.field.offset) : "-"));
            }
            annotation(r.tags, r.doc);
        }
    }

    private static void declaration(String what, TableViewEntry entry) {
        out.print(what + " " + entry.name + " file=" + entry.file + " fields=" + entry.type.fields.length);
        annotation(entry.tags, entry.doc);
        for (TableFieldInfo f : entry.type.fields) {
            out.print(what + " " + entry.name + " field " + f.name + " json=" + (f.json == null ? "-" : f.json)
                    + " type=" + f.typeName + " id=" + hex(f.id) + " optional=" + f.optional);
            annotation(f.tags, f.doc);
        }
    }

    public static void main(String[] args) {
        TableUnitView unit = TableRegistry.unitView();
        out.print("unit package=" + unit.packageName + " protocol=" + hex(unit.protocolId) + "\n");
        for (TableConstantInfo c : unit.constants) {
            out.print("constant " + c.name + " file=" + c.file + " type=" + c.typeName + " float=" + c.isFloat
                    + " int=" + c.intValue + " real=" + hex(c.isFloat ? Double.doubleToRawLongBits(c.floatValue) : 0L));
            annotation(c.tags, c.doc);
        }
        for (TableVocabularyInfo v : unit.enums) vocabulary("enum", v, "variant");
        for (TableVocabularyInfo v : unit.flags) vocabulary("flags", v, "bit");
        for (TableVocabularyInfo v : unit.unions) union(unit, v);
        for (TableViewEntry e : unit.types) declaration("type", e);
        for (TableViewEntry e : unit.tables) declaration("table", e);
        out.flush();
    }
}
