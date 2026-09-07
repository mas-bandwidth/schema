import packetdefaults.Defaults;
import packetdefaults.Defaults.*;
import packetplain.Plain;
import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class Main {
    static void check(boolean ok, String what) { if (!ok) throw new AssertionError(what); }
    static boolean eq(Object a, Object b) throws Exception {
        if (a == null || b == null) return a == b;
        if (a.getClass() != b.getClass()) return false;
        if (a.getClass().isArray()) {
            int n = Array.getLength(a); if (n != Array.getLength(b)) return false;
            for (int i = 0; i < n; i++) if (!eq(Array.get(a, i), Array.get(b, i))) return false;
            return true;
        }
        if (a instanceof Number || a instanceof Boolean) return a.equals(b);
        for (Field f : a.getClass().getFields())
            if (!Modifier.isStatic(f.getModifiers()) && !eq(f.get(a), f.get(b))) return false;
        return true;
    }
    static byte[] bytes(int... values) {
        byte[] out = new byte[values.length]; for (int i = 0; i < values.length; i++) out[i] = (byte) values[i]; return out;
    }
    static void put(byte[] target, byte[] value) { System.arraycopy(value, 0, target, 0, value.length); }
    static Sample zero() {
        Sample s = new Sample(); Arrays.fill(s.name, (byte) 0); s.nameLength = 0;
        Arrays.fill(s.token, (byte) 0); s.tokenLength = 0; s.caps = 0;
        Arrays.fill(s.emptyName, (byte) 0); s.emptyNameLength = 0;
        Arrays.fill(s.emptyToken, (byte) 0); s.emptyTokenLength = 0; s.emptyCaps = 0; return s;
    }
    static Sample sample() {
        Sample s = zero(); put(s.name, bytes(195,169,240,144,128,128)); s.nameLength = 6;
        put(s.token, bytes(92,110,92,116)); s.tokenLength = 4; s.caps = 5; return s;
    }
    static Sample shortSample() {
        Sample s = zero(); s.name[0] = 65; s.nameLength = 1; s.token[1] = (byte)255; s.tokenLength = 2; s.caps = 2; return s;
    }
    static Sample dirty() {
        Sample s = zero(); Arrays.fill(s.name, (byte)100); s.nameLength = 6;
        Arrays.fill(s.token, (byte)161); s.tokenLength = 4; s.caps = 7;
        Arrays.fill(s.emptyName, (byte)111); s.emptyNameLength = 3;
        Arrays.fill(s.emptyToken, (byte)177); s.emptyTokenLength = 2; s.emptyCaps = 7; return s;
    }
    static void copy(Sample to, Sample from) {
        put(to.name, from.name); to.nameLength = from.nameLength; put(to.token, from.token); to.tokenLength = from.tokenLength; to.caps = from.caps;
        put(to.emptyName, from.emptyName); to.emptyNameLength = from.emptyNameLength;
        put(to.emptyToken, from.emptyToken); to.emptyTokenLength = from.emptyTokenLength; to.emptyCaps = from.emptyCaps;
    }
    static Sample overlay(Sample initial, Sample sent) {
        System.arraycopy(sent.name, 0, initial.name, 0, sent.nameLength); initial.nameLength = sent.nameLength;
        System.arraycopy(sent.token, 0, initial.token, 0, sent.tokenLength); initial.tokenLength = sent.tokenLength;
        initial.caps = sent.caps; initial.emptyNameLength = 0; initial.emptyTokenLength = 0; initial.emptyCaps = 0; return initial;
    }
    interface Writer<T> { int run(T value, byte[] data); }
    interface Reader<T> { boolean run(T value, byte[] data, int bits); }
    interface Measure<T> { int run(T value); }
    static <T> byte[] write(T value, Writer<T> encode) {
        byte[] buffer = new byte[4096]; int n = encode.run(value, buffer); check(n > 0, "write"); return Arrays.copyOf(buffer, n);
    }
    @SuppressWarnings("unchecked")
    static <T> void read(byte[] bytes, int bits, T initial, T want, Reader<T> decode) throws Exception {
        check(decode.run(initial, bytes, bits), "exact-bit read"); check(eq(initial, want), "read values and backing storage");
        // The API returns a verdict, not a cursor. Exact bits must suffice;
        // one bit fewer must fail on separate storage, establishing the bound.
        T truncated = (T) initial.getClass().getConstructor().newInstance();
        check(!decode.run(truncated, bytes, bits - 1), "one-bit-short read refuses");
    }
    static <T> void golden(String dir, String name, T value, T initial, T want, Writer<T> encode, Reader<T> decode, Measure<T> measure) throws Exception {
        byte[] bytes = Files.readAllBytes(Path.of(dir, name + ".bin"));
        int bits = Integer.parseInt(Files.readString(Path.of(dir, name + ".bits")).trim());
        check(Arrays.equals(write(value, encode), bytes) && measure.run(value) == bits, "C++ byte/bit pin " + name);
        read(bytes, bits, initial, want, decode);
    }
    public static void main(String[] args) throws Exception {
        String dir = args[0];
        check(eq(new Sample(), sample()), "packet-default constructor bytes");
        Sample reused = dirty(); byte[] buffer = reused.name;
        Defaults.initSample(reused); check(eq(reused, sample()) && reused.name == buffer, "Init defaults and retained buffer");
        Defaults.zeroSample(reused); check(eq(reused, zero()), "Zero differs from Init");
        EmptyOnly empty = new EmptyOnly(); check(empty.nameLength == 0 && empty.tokenLength == 0 && empty.caps == 0 &&
            Arrays.equals(empty.name, new byte[2]) && Arrays.equals(empty.token, new byte[1]), "empty defaults");
        Prefix prefix = new Prefix();
        check(Arrays.equals(prefix.name, bytes(195,169,0,0,0)) && prefix.nameLength == 2 &&
            Arrays.equals(prefix.token, bytes(92,110,0,0,0)) && prefix.tokenLength == 2, "short defaults and zero tails");
        Arrays.fill(prefix.name, (byte)255); Arrays.fill(prefix.token, (byte)255); Defaults.initPrefix(prefix);
        check(eq(prefix, new Prefix()), "Init restores short defaults");
        WideMask wide = new WideMask(); check(wide.high == Long.MIN_VALUE && wide.all == -1L, "bit63 and all64 defaults");
        byte[] wideBytes = new byte[16]; wideBytes[7] = (byte)128; Arrays.fill(wideBytes, 8, 16, (byte)255);
        check(Arrays.equals(write(wide, Defaults::writeWideMask), wideBytes) && Defaults.measureWideMask(wide) == 128, "independent 64-bit wire");
        WideMask wideOut = new WideMask(); wideOut.high = 0; wideOut.all = 0; read(wideBytes, 128, wideOut, wide, Defaults::readWideMask);
        SplitMask split = new SplitMask(); split.lead = 5; split.mask = 1L << 32; split.tail = 2;
        check(Arrays.equals(write(split, Defaults::writeSplitMask), bytes(5,0,0,0,40)) && Defaults.measureSplitMask(split) == 38, "independent 33-bit wire");
        read(bytes(5,0,0,0,40), 38, new SplitMask(), split, Defaults::readSplitMask);
        Plain.Sample plain = new Plain.Sample(); put(plain.name, sample().name); plain.nameLength = 6;
        put(plain.token, sample().token); plain.tokenLength = 4; plain.caps = 5;
        check(Arrays.equals(write(plain, Plain::writeSample), write(new Sample(), Defaults::writeSample)) &&
            Plain.measureSample(plain) == Defaults.measureSample(new Sample()), "defaultless twin");
        golden(dir, "sample-defaults", new Sample(), zero(), sample(), Defaults::writeSample, Defaults::readSample, Defaults::measureSample);
        Batch batch = new Batch(); check(eq(batch.head, sample()) && batch.countedCount == 1, "nested defaults and born count");
        for (Sample s : batch.items) check(eq(s, sample()), "fixed backing defaults");
        for (Sample s : batch.counted) check(eq(s, sample()), "counted backing defaults");
        Batch initial = new Batch(), want = new Batch(); copy(initial.counted[1], dirty()); copy(want.counted[1], dirty());
        copy(initial.counted[2], shortSample()); copy(want.counted[2], shortSample());
        golden(dir, "batch-defaults", batch, initial, want, Defaults::writeBatch, Defaults::readBatch, Defaults::measureBatch);
        ZeroCount z = new ZeroCount(); check(z.itemsCount == 0 && eq(z.items, new Sample[]{sample(),sample()}), "zero-count backing defaults");
        ZeroCount zi = new ZeroCount(), zw = new ZeroCount(); zi.itemsCount = 2;
        copy(zi.items[0], dirty()); copy(zw.items[0], dirty()); copy(zi.items[1], shortSample()); copy(zw.items[1], shortSample());
        golden(dir, "zero-count", z, zi, zw, Defaults::writeZeroCount, Defaults::readZeroCount, Defaults::measureZeroCount);
        check(new Conditional().enabled && eq(new Conditional().value, sample()), "conditional defaults");
        golden(dir, "conditional-on", new Conditional(), new Conditional(), new Conditional(), Defaults::writeConditional, Defaults::readConditional, Defaults::measureConditional);
        Conditional off = new Conditional(), oi = new Conditional(), ow = new Conditional(); off.enabled = false;
        copy(oi.value, dirty()); ow.enabled = false; copy(ow.value, zero());
        golden(dir, "conditional-off", off, oi, ow, Defaults::writeConditional, Defaults::readConditional, Defaults::measureConditional);
        Choice choice = new Choice(), ci = new Choice(), cw = new Choice(); choice.type = ChoiceType.sample;
        ci.type = ChoiceType.conditional; cw.type = ChoiceType.sample;
        golden(dir, "choice-sample", choice, ci, cw, Defaults::writeChoice, Defaults::readChoice, Defaults::measureChoice);
        golden(dir, "sample-short", shortSample(), dirty(), overlay(dirty(), shortSample()), Defaults::writeSample, Defaults::readSample, Defaults::measureSample);
        golden(dir, "sample-empty", zero(), dirty(), overlay(dirty(), zero()), Defaults::writeSample, Defaults::readSample, Defaults::measureSample);
        for (Sample sent : new Sample[]{shortSample(), zero()}) {
            copy(choice.sample, sent); byte[] wire = write(choice, Defaults::writeChoice); int bits = Defaults.measureChoice(choice);
            Choice output = new Choice(); output.type = ChoiceType.conditional; Sample arm = output.sample; byte[] name = arm.name;
            Choice expected = new Choice(); expected.type = ChoiceType.sample; copy(expected.sample, overlay(sample(), sent));
            for (int attempt = 0; attempt < 2; attempt++) {
                copy(arm, dirty()); read(wire, bits, output, expected, Defaults::readChoice);
                check(output.sample == arm && arm.name == name, "union read retains storage");
            }
        }
        Choice falseChoice = new Choice(); falseChoice.type = ChoiceType.conditional; falseChoice.conditional.enabled = false;
        byte[] falseWire = write(falseChoice, Defaults::writeChoice); Choice falseWant = new Choice(); falseWant.type = ChoiceType.conditional;
        falseWant.conditional.enabled = false; copy(falseWant.conditional.value, zero());
        Choice falseOut = new Choice(); falseOut.type = ChoiceType.sample;
        for (int attempt = 0; attempt < 2; attempt++) {
            copy(falseOut.conditional.value, dirty()); falseOut.conditional.enabled = true;
            read(falseWire, Defaults.measureChoice(falseChoice), falseOut, falseWant, Defaults::readChoice);
        }
        System.out.println("packet defaults Java: constructors, eight C++ goldens and reused storage OK");
    }
}
