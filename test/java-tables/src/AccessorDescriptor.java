/*
    J1 — ACCESSOR AND DESCRIPTOR AGREEMENT (docs/PORTING.md, schema#421).

    J1 was a gap on seven of the nine legs, and Java was one of them. The
    generated ACCESSOR and the generated DESCRIPTOR are two independent
    derivations of ONE layout, so the only honest read is two-way: call the
    accessor, read at the descriptor's own offset, require the same answer.

    This leg already ships TableBlockLayout.verify(), and it cannot see this
    row: it compares the offsets TABLE (<Name>Row.offsets) against the
    descriptor's offsets — two CONSTANTS out of the one derivation — and never
    calls an accessor body. A moved accessor body leaves it green.

    This gate compares two READS instead. Its two controls move the emitter,
    each through `go build -overlay`: the block projection scalar +4 bytes
    (block.go), and the pointer slot +8 bytes (rows.go) — the latter moving the
    SLOT, not the delta reader beside it. A shallow reflective poison proves
    nothing here: every generated field is `final` (rows.go:148-152,
    cook.go:340-342), so it cannot be written and reads back equal to fresh.
*/

import java.io.IOException;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.HashSet;
import java.util.Objects;
import java.util.Set;

public final class AccessorDescriptor {
    private AccessorDescriptor() {}

    // A gate that compares nothing prints OK and is worth less than nothing:
    // refuse a run whose scalar or slot count is zero, or whose total of
    // comparisons falls under this floor.
    private static final int FLOOR = 32;

    private static int scalars = 0;
    private static int slots = 0;
    private static int total = 0;

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

    // lowerCamel reproduces the emitter's javaName(f.Name): ir.GoExportName's
    // lower_snake_case -> UpperCamelCase, then lower the first rune. A field
    // the descriptor spells `dynamic_props` the accessor spells `dynamicProps`.
    private static String lowerCamel(String name) {
        StringBuilder sb = new StringBuilder(name.length());
        boolean upper = true;
        for (int i = 0; i < name.length(); i++) {
            char c = name.charAt(i);
            if (c == '_') {
                upper = true;
                continue;
            }
            if (upper && c >= 'a' && c <= 'z') {
                c = (char) (c - 'a' + 'A');
            }
            upper = false;
            sb.append(c);
        }
        String out = sb.toString();
        if (out.isEmpty()) {
            return out;
        }
        return Character.toLowerCase(out.charAt(0)) + out.substring(1);
    }

    private static Class<?> rowClass(String pkg, String name) {
        try {
            return Class.forName(pkg + "." + name + "Row");
        } catch (ClassNotFoundException e) {
            fail("no row class " + pkg + "." + name + "Row for a record the descriptor named");
            return null;
        }
    }

    // An accessor the walker classified but cannot resolve is a REFUSAL by
    // name, never a silent skip: skipping a field it could not name is the
    // exact failure this row exists to prevent.
    private static Method accessor(Class<?> c, String name, Class<?>... params) {
        try {
            return c.getMethod(name, params);
        } catch (NoSuchMethodException e) {
            StringBuilder sig = new StringBuilder();
            for (Class<?> p : params) {
                if (sig.length() > 0) {
                    sig.append(", ");
                }
                sig.append(p.getSimpleName());
            }
            fail("cannot resolve accessor " + c.getSimpleName() + "." + name + "(" + sig
                    + ") for a descriptor field it classified");
            return null;
        }
    }

    // Every accessor invocation is wrapped: a throw out of a moved accessor is
    // reported in the same words as a disagreement, exception appended, so a
    // control's grep holds whichever shape the run produced.
    private static Object invoke(Method m, Object... args) {
        try {
            return m.invoke(null, args);
        } catch (InvocationTargetException e) {
            fail("the accessor and the descriptor disagree: the accessor threw " + e.getCause());
        } catch (ReflectiveOperationException e) {
            fail("the accessor and the descriptor disagree: " + e);
        }
        return null;
    }

    private static Object readDescriptor(byte[] data, int at, Class<?> t, boolean cook) {
        if (t == boolean.class) {
            return cook ? graphdemo.TableBytes.bool(data, at) : blockdemo.TableBytes.bool(data, at);
        }
        if (t == byte.class) {
            return cook ? graphdemo.TableBytes.i8(data, at) : blockdemo.TableBytes.i8(data, at);
        }
        if (t == short.class) {
            return cook ? graphdemo.TableBytes.i16(data, at) : blockdemo.TableBytes.i16(data, at);
        }
        if (t == int.class) {
            return cook ? graphdemo.TableBytes.i32(data, at) : blockdemo.TableBytes.i32(data, at);
        }
        if (t == long.class) {
            return cook ? graphdemo.TableBytes.i64(data, at) : blockdemo.TableBytes.i64(data, at);
        }
        if (t == float.class) {
            return cook ? graphdemo.TableBytes.f32(data, at) : blockdemo.TableBytes.f32(data, at);
        }
        if (t == double.class) {
            return cook ? graphdemo.TableBytes.f64(data, at) : blockdemo.TableBytes.f64(data, at);
        }
        fail("a scalar accessor answers " + t + ", which is not a table scalar");
        return null;
    }

    private static void checkScalar(Class<?> row, String rec, String field, byte[] data, int at, int offset, boolean cook) {
        describe(rec + "." + field);
        Method m = accessor(row, lowerCamel(field), byte[].class, int.class);
        Object via = invoke(m, data, at);
        Object desc = readDescriptor(data, at + offset, m.getReturnType(), cook);
        scalars++;
        total++;
        if (!Objects.equals(via, desc)) {
            fail(rec + "." + field + ": the accessor and the descriptor disagree");
        }
    }

    private static int checkAt(Class<?> row, String rec, String field, int at, int offset) {
        describe(rec + "." + field);
        Method m = accessor(row, lowerCamel(field) + "At", int.class);
        Object via = invoke(m, at);
        total++;
        int answer = (Integer) via;
        if (answer != at + offset) {
            fail(rec + "." + field + ": the accessor and the descriptor disagree about the record offset");
        }
        return answer;
    }

    private static int checkArrayAt(Class<?> row, String rec, String field, int at, int offset, int elemSize, int i) {
        describe(rec + "." + field + "[" + i + "]");
        Method m = accessor(row, lowerCamel(field) + "At", int.class, int.class);
        Object via = invoke(m, at, i);
        total++;
        int answer = (Integer) via;
        if (answer != at + offset + i * elemSize) {
            fail(rec + "." + field + ": the accessor and the descriptor disagree about the element offset");
        }
        return answer;
    }

    private static void checkSlot(Class<?> row, String rec, String field, int at, int offset) {
        describe(rec + "." + field);
        Method m = accessor(row, lowerCamel(field) + "Slot", int.class);
        int via;
        try {
            via = (Integer) m.invoke(null, at);
        } catch (InvocationTargetException e) {
            fail(rec + "." + field + ": the slot accessor's offset is not the descriptor's: the accessor threw "
                    + e.getCause());
            return;
        } catch (ReflectiveOperationException e) {
            fail(rec + "." + field + ": the slot accessor's offset is not the descriptor's: " + e);
            return;
        }
        slots++;
        total++;
        if (via != at + offset) {
            fail(rec + "." + field + ": the slot accessor's offset is not the descriptor's");
        }
    }

    private static void checkStringAt(Class<?> row, String rec, String field, int at, int offset) {
        describe(rec + "." + field);
        Method m = accessor(row, lowerCamel(field) + "At", int.class);
        Object via = invoke(m, at);
        total++;
        if ((Integer) via != at + offset) {
            fail(rec + "." + field + ": the accessor and the descriptor disagree about the buffer offset");
        }
    }

    // ---- the block tier: the projection's inline scalars, and the rows every
    // out-of-line column reaches, each read twice.

    private static void walkBlockRecord(Class<?> row, String rec, blockdemo.TableBlockInfo info, byte[] data, int at) {
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.element() != null) {
                blockdemo.TableBlockInfo child = f.element();
                Class<?> childRow = rowClass("blockdemo", child.name);
                if (f.isArray) {
                    for (int i = 0; i < f.arrayBound; i++) {
                        int childAt = checkArrayAt(row, rec, f.name, at, f.offset, f.elemSize, i);
                        walkBlockRecord(childRow, child.name, child, data, childAt);
                    }
                } else {
                    int childAt = checkAt(row, rec, f.name, at, f.offset);
                    walkBlockRecord(childRow, child.name, child, data, childAt);
                }
            } else if (f.elemSize > 0) {
                checkScalar(row, rec, f.name, data, at, f.offset, false);
            } else {
                fail(rec + "." + f.name + ": the block walker could not place this field");
            }
        }
    }

    private static void walkBlock(blockdemo.RenderFrameBlock blk) {
        byte[] data = blk.data();
        int base = blk.base();
        blockdemo.TableBlockInfo info = blockdemo.RenderFrameBlock.type();
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.outOfLine) {
                long offsetOf = blockdemo.TableBytes.i64(data, base + f.offsetOfOffset);
                long count = blockdemo.TableBytes.u32(data, base + f.countOffset);
                long stride = blockdemo.TableBytes.u32(data, base + f.strideOffset);
                blockdemo.TableBlockInfo elem = f.element();
                Class<?> row = rowClass("blockdemo", elem.name);
                for (long r = 0; r < count; r++) {
                    int rowAt = base + (int) offsetOf + (int) (r * stride);
                    walkBlockRecord(row, elem.name, elem, data, rowAt);
                }
            } else if (f.element() != null) {
                blockdemo.TableBlockInfo child = f.element();
                Class<?> childRow = rowClass("blockdemo", child.name);
                if (f.isArray) {
                    for (int i = 0; i < f.arrayBound; i++) {
                        int childAt = checkArrayAt(blockdemo.RenderFrameBlock.class, "RenderFrame", f.name, base, f.offset, f.elemSize, i);
                        walkBlockRecord(childRow, child.name, child, data, childAt);
                    }
                } else {
                    int childAt = checkAt(blockdemo.RenderFrameBlock.class, "RenderFrame", f.name, base, f.offset);
                    walkBlockRecord(childRow, child.name, child, data, childAt);
                }
            } else {
                checkScalar(blockdemo.RenderFrameBlock.class, "RenderFrame", f.name, data, base, f.offset, false);
            }
        }
    }

    // ---- the cook tier: the root, then every record a pointer edge reaches.

    private static void walkCook(graphdemo.SceneCook cook, int at, graphdemo.TableCookInfo info, Set<Integer> seen, int depth) {
        if (depth > 512 || !seen.add(at)) {
            return;
        }
        Class<?> row = rowClass("graphdemo", info.name);
        byte[] data = cook.data();
        for (graphdemo.TableCookFieldInfo f : info.fields) {
            if (f.isPointer) {
                checkSlot(row, info.name, f.name, at, f.offset);
                graphdemo.TableCookInfo child = f.record();
                if (child != null) {
                    int target = cook.at(at + f.offset, child.size);
                    if (target >= 0) {
                        walkCook(cook, target, child, seen, depth + 1);
                    }
                }
            } else if (f.storage == graphdemo.TableCookStorage.RECORD) {
                graphdemo.TableCookInfo child = f.record();
                if (child == null) {
                    fail(info.name + "." + f.name + ": a RECORD field names no record");
                }
                Class<?> childRow = rowClass("graphdemo", child.name);
                if (f.isArray) {
                    for (int i = 0; i < f.arrayBound; i++) {
                        int childAt = checkArrayAt(row, info.name, f.name, at, f.offset, f.elemSize, i);
                        walkCook(cook, childAt, child, seen, depth + 1);
                    }
                } else {
                    int childAt = checkAt(row, info.name, f.name, at, f.offset);
                    walkCook(cook, childAt, child, seen, depth + 1);
                }
            } else if (f.storage == graphdemo.TableCookStorage.STRING || f.storage == graphdemo.TableCookStorage.BYTES) {
                checkStringAt(row, info.name, f.name, at, f.offset);
            } else {
                checkScalar(row, info.name, f.name, data, at, f.offset, true);
            }
        }
    }

    public static void main(String[] args) throws IOException {
        if (args.length < 2) {
            System.err.println("usage: AccessorDescriptor <block image> <cook file>");
            System.exit(1);
        }
        byte[] image = Files.readAllBytes(Paths.get(args[0]));
        describe("open the block image");
        blockdemo.RenderFrameBlock blk = blockdemo.RenderFrameBlock.open(image, 0, image.length);
        if (blk == null) {
            fail("the block image did not open");
        }
        walkBlock(blk);

        byte[] region = Files.readAllBytes(Paths.get(args[1]));
        describe("open the cook file");
        graphdemo.SceneCook cook = graphdemo.SceneCook.open(region, 0, region.length);
        if (cook == null) {
            fail("the cook file did not open");
        }
        walkCook(cook, cook.root(), graphdemo.SceneCook.type(), new HashSet<>(), 0);

        if (scalars == 0) {
            fail("the gate compared zero scalar accessors against their descriptors");
        }
        if (slots == 0) {
            fail("the gate compared zero pointer slots against their descriptors");
        }
        if (total < FLOOR) {
            fail("the gate made " + total + " comparisons, below the floor of " + FLOOR);
        }
        System.out.println("accessor/descriptor: " + scalars + " scalars, " + slots + " slots, " + total + " comparisons");
        System.out.println("OK");
    }
}
