/*
    THE JAVA TABLES ACCESSOR/DESCRIPTOR AGREEMENT LEG (docs/PORTING.md J1).

    Every row of every committed block image and every node of the cook is read
    TWICE: once through the generated accessor named for the field, once through
    the descriptor's own offset, storage kind and element size. Two independent
    derivations of one layout, held against each other, so a moved accessor or
    a moved descriptor offset is seen without a pinned dump that happens to
    cover the field. A field the descriptor reaches that no accessor names fails
    by name.

    Prints OK and exits 0 — no test framework, the exit code is the verdict.

    Usage: AgreeAccessors <block_render.bin> <block_padded.bin> <scene.cook>
*/

import java.nio.file.Files;
import java.nio.file.Paths;

public final class AgreeAccessors {
    private AgreeAccessors() {}

    private static String site = "(none)";
    private static long rows = 0;
    private static long nodes = 0;
    private static long fields = 0;

    private static void fail(String what) {
        System.out.flush();
        System.err.println();
        System.err.println("FAILED: " + what);
        System.err.println("  site  " + site);
        System.err.println();
        System.exit(1);
    }

    private static String camel(String name) {
        StringBuilder out = new StringBuilder();
        boolean upper = false;
        for (int i = 0; i < name.length(); i++) {
            char c = name.charAt(i);
            if (c == '_') { upper = true; continue; }
            out.append(upper ? Character.toUpperCase(c) : c);
            upper = false;
        }
        return out.toString();
    }

    private static int usedLen(int raw, int bound) {
        return (raw < 0 || raw > bound) ? 0 : raw;
    }

    private static Object callStatic(Class<?> cls, String m, Class<?>[] sig, Object... args) {
        try {
            java.lang.reflect.Method method = cls.getMethod(m, sig);
            return method.invoke(null, args);
        } catch (java.lang.reflect.InvocationTargetException e) {
            throw new RuntimeException(e.getCause());
        } catch (Exception e) {
            fail(cls.getSimpleName() + " has a descriptor and no accessor: " + m);
            return null;
        }
    }

    private static Object callInstance(Object handle, String m, Class<?>[] sig, Object... args) {
        try {
            java.lang.reflect.Method method = handle.getClass().getMethod(m, sig);
            return method.invoke(handle, args);
        } catch (java.lang.reflect.InvocationTargetException e) {
            throw new RuntimeException(e.getCause());
        } catch (Exception e) {
            fail(handle.getClass().getSimpleName() + " has a descriptor and no accessor: " + m);
            return null;
        }
    }

    // The descriptor-side read is chosen from the DESCRIPTOR's kind/storage, not
    // from the type the accessor happened to return: the whole point of holding
    // two derivations against each other is that a moved or mistyped accessor is
    // seen, and reading the descriptor with the accessor's own type would let a
    // mistyped accessor agree with itself.
    // The width is the DESCRIPTOR's elemSize, and the kind only decides the
    // spelling (bool / float / integer): a block enum's kind is always u16
    // while its stored width is whatever the declaration says, and following the
    // kind for width would read two bytes where the layout has one.
    private static Object byBlockDescriptor(byte[] data, int value, int kind, int elemSize) {
        if (kind == 1) { return blockdemo.TableBytes.bool(data, value); }              // tkBool
        if (kind == 10) { return blockdemo.TableBytes.f32(data, value); }              // tkF32
        if (kind == 11) { return blockdemo.TableBytes.f64(data, value); }              // tkF64
        switch (elemSize) {
            case 1: return blockdemo.TableBytes.i8(data, value);
            case 2: return blockdemo.TableBytes.i16(data, value);
            case 4: return blockdemo.TableBytes.i32(data, value);
            default: return blockdemo.TableBytes.i64(data, value);
        }
    }

    private static Object byCookDescriptor(byte[] data, int value,
                                          graphdemo.TableCookStorage storage, int elemSize) {
        switch (storage) {
            case BOOL: return graphdemo.TableBytes.bool(data, value);
            case FLOAT: return elemSize == 4
                    ? graphdemo.TableBytes.f32(data, value)
                    : graphdemo.TableBytes.f64(data, value);
            case SIGNED: case UNSIGNED:
                switch (elemSize) {
                    case 1: return graphdemo.TableBytes.i8(data, value);
                    case 2: return graphdemo.TableBytes.i16(data, value);
                    case 4: return graphdemo.TableBytes.i32(data, value);
                    default: return graphdemo.TableBytes.i64(data, value);
                }
            default:
                fail("a cook storage " + storage + " is not a scalar the accessors spell");
                return null;
        }
    }

    // THE REVERSE CHECK: a row class's accessors must all be accounted for by the
    // descriptor, so an accessor that names a field the descriptor does not carry
    // fails by name — the mirror of the forward "the descriptor reaches a field no
    // accessor names" check in callStatic/callInstance.
    private static void checkNoAccessorOnly(String where, Class<?> rowClass, String[] allowedMethods) {
        java.util.Set<String> allowed = new java.util.HashSet<>();
        for (String m : allowedMethods) { allowed.add(m); }
        for (java.lang.reflect.Method m : rowClass.getMethods()) {
            if (!java.lang.reflect.Modifier.isStatic(m.getModifiers())) { continue; }
            String mn = m.getName();
            if (allowed.contains(mn)) { continue; }
            fail(where + ": a field has an accessor and no descriptor: " + mn);
        }
    }

    private static String[] blockFieldAccessorNames(blockdemo.TableBlockFieldInfo[] fields) {
        java.util.List<String> out = new java.util.ArrayList<>();
        out.add("blockInfo");
        out.add("cookInfo");
        for (blockdemo.TableBlockFieldInfo f : fields) {
            String stem = camel(f.name);
            out.add(stem);
            out.add(stem + "At");
            out.add(stem + "Length");
            out.add(stem + "Present");
            out.add(stem + "Count");
            out.add(stem + "Slot");
            out.add(stem + "Delta");
        }
        return out.toArray(new String[0]);
    }

    // THE KEY FORM'S ALIASES (docs/SPEC-TABLES.md §2.4, docs/PORTING.md M5;
    // internal/codegen/javatable/rows.go emitRowKeyForm). A keyed array's row
    // carries <name>ByKey (a scalar slot by key) or <name>AtKey (a class-ref
    // element's offset by key) beside the slot form's <name> and <name>At. They
    // are not new fields, so the descriptor names none of them; an alias is
    // accepted into the reverse check only when it VERIFIES against the
    // descriptor's own array and the slot accessor it spells: the exact
    // signature, the same return type, key k answering exactly what slot k-1
    // answers for every slot of the descriptor's bound, and key 0 (None) and key
    // bound+1 refused. Any other method stays an accessor with no descriptor.

    /** One descriptor array a key alias may stand beside: its accessor stem and bound. */
    private static final class Keyed {
        final String stem;
        final int bound;

        Keyed(String stem, int bound) {
            this.stem = stem;
            this.bound = bound;
        }
    }

    private static java.util.Map<String, Keyed> blockKeyedFields(blockdemo.TableBlockFieldInfo[] fields) {
        java.util.Map<String, Keyed> out = new java.util.HashMap<>();
        for (blockdemo.TableBlockFieldInfo f : fields) {
            if (f.isArray && !f.outOfLine && f.arrayBound > 0) {
                out.put(camel(f.name), new Keyed(camel(f.name), f.arrayBound));
            }
        }
        return out;
    }

    private static java.util.Map<String, Keyed> cookKeyedFields(graphdemo.TableCookFieldInfo[] fields) {
        java.util.Map<String, Keyed> out = new java.util.HashMap<>();
        for (graphdemo.TableCookFieldInfo f : fields) {
            if (f.isArray && !f.isPointer && f.arrayBound > 0) {
                out.put(camel(f.name), new Keyed(camel(f.name), f.arrayBound));
            }
        }
        return out;
    }

    private static java.lang.reflect.Method staticMethod(Class<?> cls, String name, Class<?>... sig) {
        try {
            java.lang.reflect.Method m = cls.getMethod(name, sig);
            return java.lang.reflect.Modifier.isStatic(m.getModifiers()) ? m : null;
        } catch (NoSuchMethodException e) {
            return null;
        }
    }

    private static final Object REFUSED = new Object();

    // a key alias's answer, or REFUSED when it refuses the key by name
    private static Object invokeKey(java.lang.reflect.Method m, Object... args) throws Exception {
        try {
            return m.invoke(null, args);
        } catch (java.lang.reflect.InvocationTargetException e) {
            if (e.getCause() instanceof IllegalArgumentException) {
                return REFUSED;
            }
            throw e;
        }
    }

    /**
     * The descriptor-derived names, plus every key alias on rowClass that
     * verifies against its descriptor array at this row. An alias that fails
     * to verify fails the gate by name; a ByKey/AtKey method with no keyed
     * array under it is left out, so the reverse check refuses it.
     */
    private static String[] withKeyAliases(Class<?> rowClass, byte[] data, int at, String[] names,
                                           java.util.Map<String, Keyed> keyed) throws Exception {
        java.util.List<String> out = new java.util.ArrayList<>(java.util.Arrays.asList(names));
        String row = rowClass.getSimpleName();
        for (java.lang.reflect.Method m : rowClass.getMethods()) {
            if (!java.lang.reflect.Modifier.isStatic(m.getModifiers())) { continue; }
            String mn = m.getName();
            boolean byKey = mn.endsWith("ByKey");
            boolean atKey = mn.endsWith("AtKey");
            if (!byKey && !atKey) { continue; }
            Keyed k = keyed.get(mn.substring(0, mn.length() - "ByKey".length()));
            if (k == null) { continue; }
            java.lang.reflect.Method alias;
            java.lang.reflect.Method slot;
            if (byKey) {
                alias = staticMethod(rowClass, mn, byte[].class, int.class, int.class);
                slot = staticMethod(rowClass, k.stem, byte[].class, int.class, int.class);
            } else {
                alias = staticMethod(rowClass, mn, int.class, int.class);
                slot = staticMethod(rowClass, k.stem + "At", int.class, int.class);
            }
            if (alias == null || !alias.equals(m)) {
                fail(row + "." + mn + ": a key alias does not have the key form's signature");
            }
            if (slot == null) {
                fail(row + "." + mn + ": a key alias has no slot accessor under it: " + k.stem);
            }
            if (alias.getReturnType() != slot.getReturnType()) {
                fail(row + "." + mn + ": the key alias and its slot accessor return different types");
            }
            for (int key = 1; key <= k.bound; key++) {
                Object a = byKey ? invokeKey(alias, data, at, key) : invokeKey(alias, at, key);
                Object b = byKey ? slot.invoke(null, data, at, key - 1) : slot.invoke(null, at, key - 1);
                if (a == REFUSED || !a.equals(b)) {
                    fail(row + "." + mn + ": key " + key + " does not answer what slot " + (key - 1) + " answers");
                }
            }
            for (int key : new int[] { 0, k.bound + 1 }) {
                Object a = byKey ? invokeKey(alias, data, at, key) : invokeKey(alias, at, key);
                if (a != REFUSED) {
                    fail(row + "." + mn + ": key " + key + " is outside the descriptor's " + k.bound
                            + " slots and was not refused");
                }
            }
            out.add(mn);
        }
        return out.toArray(new String[0]);
    }

    private static String[] cookFieldAccessorNames(graphdemo.TableCookFieldInfo[] fields) {
        java.util.List<String> out = new java.util.ArrayList<>();
        out.add("blockInfo");
        out.add("cookInfo");
        for (graphdemo.TableCookFieldInfo f : fields) {
            String stem = camel(f.name);
            out.add(stem);
            out.add(stem + "At");
            out.add(stem + "Length");
            out.add(stem + "Present");
            out.add(stem + "Count");
            out.add(stem + "Slot");
            out.add(stem + "Delta");
        }
        return out.toArray(new String[0]);
    }

    private static void expectEq(Object accessor, Object descriptor, String where) {
        if (accessor instanceof Float || accessor instanceof Double) {
            double a = ((Number) accessor).doubleValue();
            double b = ((Number) descriptor).doubleValue();
            if (Double.doubleToLongBits(a) != Double.doubleToLongBits(b)) {
                fail(where + ": the accessor and the descriptor disagree about the value");
            }
            return;
        }
        if (!accessor.equals(descriptor)) {
            fail(where + ": the accessor and the descriptor disagree about the value");
        }
    }

    // ---- the BLOCK ----
    //
    // A block handle is opened, then the projection record and every out-of-line
    // array's rows are walked. The projection's fields use the block class as
    // their accessor holder; each array's rows use the <Name>Row class.

    private static void agreeBlock(Object handle, Class<?> blockClass) throws Exception {
        byte[] data = (byte[]) blockClass.getMethod("data").invoke(handle);
        int base = (Integer) blockClass.getMethod("base").invoke(handle);
        blockdemo.TableBlockInfo info = (blockdemo.TableBlockInfo) blockClass.getMethod("type").invoke(null);

        agreeBlockRecord(data, base, info, blockClass);

        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (!f.outOfLine) { continue; }
            String cap = camel(f.name);
            long offsetOf = blockdemo.TableBytes.i64(data, base + f.offsetOfOffset);
            long count = blockdemo.TableBytes.u32(data, base + f.countOffset);
            long stride = blockdemo.TableBytes.u32(data, base + f.strideOffset);

            int accCount = (Integer) callInstance(handle, cap + "Count", new Class<?>[0]);
            if (accCount != count) {
                fail(f.name + ": the count accessor and the descriptor disagree");
            }
            blockdemo.TableBlockInfo rowInfo = f.element();
            if (rowInfo.size != stride) {
                fail(f.name + ": the row object's size is not the pitch the instance carries");
            }
            Class<?> rowClass = Class.forName("blockdemo." + rowInfo.name + "Row");
            for (long r = 0; r < count; r++) {
                int accAt = (Integer) callInstance(handle, cap + "At", new Class<?>[] { int.class }, (int) r);
                int descAt = (int) (base + offsetOf + r * stride);
                if (accAt != descAt) {
                    fail(f.name + "[" + r + "]: the row accessor's offset is not the descriptor's");
                }
                agreeBlockRecord(data, descAt, rowInfo, rowClass);
            }
        }
    }

    private static void agreeBlockRecord(byte[] data, int at, blockdemo.TableBlockInfo info, Class<?> rowClass)
            throws Exception {
        rows++;
        // the reverse check runs on ROW classes, not on the block handle class,
        // which carries the prologue accessors (magic, buildVersion, byteOrder)
        // that are not descriptor fields.
        if (rowClass.getSimpleName().endsWith("Row")) {
            checkNoAccessorOnly(rowClass.getSimpleName(), rowClass,
                    withKeyAliases(rowClass, data, at, blockFieldAccessorNames(info.fields), blockKeyedFields(info.fields)));
        }
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.outOfLine) { continue; }
            fields++;
            String where = rowClass.getSimpleName().replace("Row", "") + "." + f.name;
            site = where;

            if (f.counted && f.arrayBound > 0 && f.elemSize == 1 && f.elementRef == null) {
                // a string or a bytes: the buffer offset and the used length
                int accAt = (Integer) callStatic(rowClass, camel(f.name) + "At",
                        new Class<?>[] { int.class }, at);
                if (accAt != at + f.offset) {
                    fail(where + ": the buffer accessor's offset is not the descriptor's");
                }
                int accLen = (Integer) callStatic(rowClass, camel(f.name) + "Length",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                int descLen = usedLen(blockdemo.TableBytes.i32(data, at + f.countOffset), f.arrayBound);
                if (accLen != descLen) {
                    fail(where + ": the used length accessor and the descriptor disagree");
                }
                continue;
            }

            if (f.optional) {
                boolean accPresent = (Boolean) callStatic(rowClass, camel(f.name) + "Present",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                if (accPresent != blockdemo.TableBytes.bool(data, at + f.presentOffset)) {
                    fail(where + ": the present accessor and the descriptor disagree");
                }
            }

            int slots = f.isArray ? f.arrayBound : 1;
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                if (f.elementRef != null) {
                    int accPos;
                    if (f.isArray) {
                        accPos = (Integer) callStatic(rowClass, camel(f.name) + "At",
                                new Class<?>[] { int.class, int.class }, at, s);
                    } else {
                        accPos = (Integer) callStatic(rowClass, camel(f.name) + "At",
                                new Class<?>[] { int.class }, at);
                    }
                    if (accPos != value) {
                        fail(where + "[" + s + "]: the accessor's offset is not the descriptor's");
                    }
                    agreeBlockRecord(data, value, f.element(),
                            Class.forName("blockdemo." + f.element().name + "Row"));
                    continue;
                }
                Object accVal = f.isArray
                        ? callStatic(rowClass, camel(f.name),
                                new Class<?>[] { byte[].class, int.class, int.class }, data, at, s)
                        : callStatic(rowClass, camel(f.name),
                                new Class<?>[] { byte[].class, int.class }, data, at);
                Object descVal = byBlockDescriptor(data, value, f.kind, f.elemSize);
                expectEq(accVal, descVal, where + "[" + s + "]");
            }

            if (f.counted && f.isArray) {
                int accCount = (Integer) callStatic(rowClass, camel(f.name) + "Count",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                int descCount = usedLen(blockdemo.TableBytes.i32(data, at + f.countOffset), f.arrayBound);
                if (accCount != descCount) {
                    fail(where + ": the count accessor and the descriptor disagree");
                }
            }
        }
    }

    // ---- the COOK ----
    //
    // Every node of the region is walked: the record's fields through the
    // <Name>Row accessors against the descriptor, then every pointer edge through
    // the reader's own `at`.

    private static void agreeCook(graphdemo.SceneCook cook) throws Exception {
        walkCookNode(cook, cook.root(), graphdemo.SceneCook.type(), 0, new java.util.HashSet<>());
    }

    private static void walkCookNode(graphdemo.SceneCook cook, int at, graphdemo.TableCookInfo info,
                                     int depth, java.util.Set<Integer> seen) throws Exception {
        if (depth > 512 || !seen.add(at)) { return; }
        nodes++;
        agreeCookRecord(cook.data(), at, info, Class.forName("graphdemo." + info.name + "Row"));
        for (graphdemo.TableCookFieldInfo f : info.fields) {
            if (!f.isPointer || f.record() == null) { continue; }
            int target = cook.at(at + f.offset, f.record().size);
            if (target < 0) { continue; } // null, or a refused delta
            walkCookNode(cook, target, f.record(), depth + 1, seen);
        }
    }

    private static void agreeCookRecord(byte[] data, int at, graphdemo.TableCookInfo info, Class<?> rowClass)
            throws Exception {
        rows++;
        checkNoAccessorOnly(rowClass.getSimpleName(), rowClass,
                withKeyAliases(rowClass, data, at, cookFieldAccessorNames(info.fields), cookKeyedFields(info.fields)));
        for (graphdemo.TableCookFieldInfo f : info.fields) {
            fields++;
            String where = rowClass.getSimpleName().replace("Row", "") + "." + f.name;
            site = where;

            if (f.isPointer) {
                int accSlot = (Integer) callStatic(rowClass, camel(f.name) + "Slot",
                        new Class<?>[] { int.class }, at);
                if (accSlot != at + f.offset) {
                    fail(where + ": the slot accessor's offset is not the descriptor's");
                }
                long accDelta = (Long) callStatic(rowClass, camel(f.name) + "Delta",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                if (accDelta != graphdemo.TableBytes.i64(data, at + f.offset)) {
                    fail(where + ": the accessor and the descriptor disagree about the delta");
                }
                continue;
            }

            if (f.storage == graphdemo.TableCookStorage.STRING || f.storage == graphdemo.TableCookStorage.BYTES) {
                int accAt = (Integer) callStatic(rowClass, camel(f.name) + "At",
                        new Class<?>[] { int.class }, at);
                if (accAt != at + f.offset) {
                    fail(where + ": the buffer accessor's offset is not the descriptor's");
                }
                int accLen = (Integer) callStatic(rowClass, camel(f.name) + "Length",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                int descLen = usedLen(graphdemo.TableBytes.i32(data, at + f.countOffset), f.arrayBound);
                if (accLen != descLen) {
                    fail(where + ": the used length accessor and the descriptor disagree");
                }
                continue;
            }

            if (f.presentOffset >= 0) {
                boolean accPresent = (Boolean) callStatic(rowClass, camel(f.name) + "Present",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                if (accPresent != graphdemo.TableBytes.bool(data, at + f.presentOffset)) {
                    fail(where + ": the present accessor and the descriptor disagree");
                }
            }

            int slots = f.isArray ? f.arrayBound : 1;
            for (int s = 0; s < slots; s++) {
                int value = at + f.offset + s * f.elemSize;
                if (f.storage == graphdemo.TableCookStorage.RECORD) {
                    int accPos;
                    if (f.isArray) {
                        accPos = (Integer) callStatic(rowClass, camel(f.name) + "At",
                                new Class<?>[] { int.class, int.class }, at, s);
                    } else {
                        accPos = (Integer) callStatic(rowClass, camel(f.name) + "At",
                                new Class<?>[] { int.class }, at);
                    }
                    if (accPos != value) {
                        fail(where + "[" + s + "]: the accessor's offset is not the descriptor's");
                    }
                    agreeCookRecord(data, value, f.record(),
                            Class.forName("graphdemo." + f.record().name + "Row"));
                    continue;
                }
                Object accVal = f.isArray
                        ? callStatic(rowClass, camel(f.name),
                                new Class<?>[] { byte[].class, int.class, int.class }, data, at, s)
                        : callStatic(rowClass, camel(f.name),
                                new Class<?>[] { byte[].class, int.class }, data, at);
                Object descVal = byCookDescriptor(data, value, f.storage, f.elemSize);
                expectEq(accVal, descVal, where + "[" + s + "]");
            }

            if (f.isArray && f.countOffset >= 0) {
                int accCount = (Integer) callStatic(rowClass, camel(f.name) + "Count",
                        new Class<?>[] { byte[].class, int.class }, data, at);
                int descCount = usedLen(graphdemo.TableBytes.i32(data, at + f.countOffset), f.arrayBound);
                if (accCount != descCount) {
                    fail(where + ": the count accessor and the descriptor disagree");
                }
            }
        }
    }

    public static void main(String[] args) throws Exception {
        if (args.length < 3) {
            System.err.println("usage: AgreeAccessors <block_render.bin> <block_padded.bin> <scene.cook>");
            System.exit(1);
        }

        byte[] render = Files.readAllBytes(Paths.get(args[0]));
        blockdemo.RenderFrameBlock rHandle = blockdemo.RenderFrameBlock.open(render);
        if (rHandle == null) { fail("testdata/wire/tables/block_render.bin did not open"); }
        agreeBlock(rHandle, blockdemo.RenderFrameBlock.class);

        byte[] padded = Files.readAllBytes(Paths.get(args[1]));
        blockdemo.PaddedFrameBlock pHandle = blockdemo.PaddedFrameBlock.open(padded);
        if (pHandle == null) { fail("testdata/wire/tables/block_padded.bin did not open"); }
        agreeBlock(pHandle, blockdemo.PaddedFrameBlock.class);

        byte[] cookBytes = Files.readAllBytes(Paths.get(args[2]));
        graphdemo.SceneCook cook = graphdemo.SceneCook.open(cookBytes, 0, cookBytes.length);
        if (cook == null) { fail("build/cook-open/Scene.cook did not open"); }
        agreeCook(cook);

        System.out.println("agree: " + rows + " rows, " + nodes + " nodes, " + fields + " fields compared");
        System.out.println("OK");
    }
}
