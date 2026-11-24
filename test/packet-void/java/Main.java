// The Java leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. A thrown Throwable (an -ea contract assert in the generated
// writer, an index out of range) is caught per case, so every case still
// gets its line.

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.List;
import packetvoid.Void.Batch;
import packetvoid.Void.DefaultArm;
import packetvoid.Void.Empty;
import packetvoid.Void.EmptyType;
import packetvoid.Void.Frame;
import packetvoid.Void.Mixed;
import packetvoid.Void.MixedType;
import packetvoid.Void.Single;
import packetvoid.Void.SingleType;
import packetvoid.Void.Tags;
import packetvoid.Void.TagsType;

public final class Main {
    // A case's value, one field set by its type.
    static final class Value {
        final String type;
        final Mixed mixed = new Mixed();
        final Tags tags = new Tags();
        final Single single = new Single();
        final Empty empty = new Empty();
        final Frame frame = new Frame();
        final Batch batch = new Batch();

        Value(String type) {
            this.type = type;
        }
    }

    static final class Failed extends Exception {
        private static final long serialVersionUID = 1L;

        Failed(String why) {
            super(why);
        }
    }

    static final List<String> TYPES = List.of("Mixed", "Tags", "Single", "Empty", "Frame", "Batch");

    // The payload arm holds its declared defaults (entries empty, marker 5).
    static void selectPayload(Mixed m) {
        m.type = MixedType.PAYLOAD;
        packetvoid.Void.initDefaultArm(m.payload);
    }

    // The value of each accept case, by name; null for a name this driver
    // does not know, which is FAILED and never written with another case's value.
    static Value build(String name) {
        Value v;
        switch (name) {
            case "mixed-none":
                v = new Value("Mixed");
                v.mixed.type = MixedType.NONE;
                return v;
            case "mixed-ping":
                v = new Value("Mixed");
                v.mixed.type = MixedType.PING;
                return v;
            case "mixed-payload":
                v = new Value("Mixed");
                selectPayload(v.mixed);
                return v;
            case "tags-none":
                v = new Value("Tags");
                v.tags.type = TagsType.NONE;
                return v;
            case "tags-ping":
                v = new Value("Tags");
                v.tags.type = TagsType.PING;
                return v;
            case "tags-pong":
                v = new Value("Tags");
                v.tags.type = TagsType.PONG;
                return v;
            case "single-none":
                v = new Value("Single");
                v.single.type = SingleType.NONE;
                return v;
            case "single-ack":
                v = new Value("Single");
                v.single.type = SingleType.ACK;
                return v;
            case "empty-none":
                v = new Value("Empty");
                v.empty.type = EmptyType.NONE;
                return v;
            case "frame-ping":
                v = new Value("Frame");
                v.frame.lead = 5;
                v.frame.choice.type = MixedType.PING;
                v.frame.tail = 6;
                return v;
            case "batch-empty":
                v = new Value("Batch");
                v.batch.valuesCount = 0;
                return v;
            case "batch-ping":
                v = new Value("Batch");
                v.batch.valuesCount = 1;
                v.batch.values[0].type = MixedType.PING;
                return v;
            case "batch-ping-payload":
                v = new Value("Batch");
                v.batch.valuesCount = 2;
                v.batch.values[0].type = MixedType.PING;
                selectPayload(v.batch.values[1]);
                return v;
            default:
                return null;
        }
    }

    // Every read lands on storage dirtied first: a selected payload must be
    // constructed with its declared defaults, whatever the storage held.
    static void dirtyMixed(Mixed m) {
        m.type = MixedType.PAYLOAD;
        DefaultArm arm = m.payload;
        arm.entriesCount = 2;
        arm.entries[0].retries = 99;
        arm.entries[1].retries = 99;
        arm.marker = 1;
    }

    static Value dirty(String type) {
        Value v = new Value(type);
        dirtyMixed(v.mixed);
        v.tags.type = TagsType.PONG;
        v.single.type = SingleType.ACK;
        v.frame.lead = 1;
        v.frame.tail = 1;
        dirtyMixed(v.frame.choice);
        v.batch.valuesCount = 2;
        dirtyMixed(v.batch.values[0]);
        dirtyMixed(v.batch.values[1]);
        return v;
    }

    // writeBits returns the bit position after the value.
    static int writeBits(Value v, byte[] out) throws Failed {
        switch (v.type) {
            case "Mixed": return packetvoid.Void.writeMixedBits(v.mixed, out, 0);
            case "Tags": return packetvoid.Void.writeTagsBits(v.tags, out, 0);
            case "Single": return packetvoid.Void.writeSingleBits(v.single, out, 0);
            case "Empty": return packetvoid.Void.writeEmptyBits(v.empty, out, 0);
            case "Frame": return packetvoid.Void.writeFrameBits(v.frame, out, 0);
            case "Batch": return packetvoid.Void.writeBatchBits(v.batch, out, 0);
            default: throw new Failed("unknown type " + v.type);
        }
    }

    // writeBytes is the public whole-value writer: the bytes written.
    static int writeBytes(Value v, byte[] out) throws Failed {
        switch (v.type) {
            case "Mixed": return packetvoid.Void.writeMixed(v.mixed, out);
            case "Tags": return packetvoid.Void.writeTags(v.tags, out);
            case "Single": return packetvoid.Void.writeSingle(v.single, out);
            case "Empty": return packetvoid.Void.writeEmpty(v.empty, out);
            case "Frame": return packetvoid.Void.writeFrame(v.frame, out);
            case "Batch": return packetvoid.Void.writeBatch(v.batch, out);
            default: throw new Failed("unknown type " + v.type);
        }
    }

    // read is the public one-packet read: false refuses the wire.
    static boolean read(Value v, byte[] pin) throws Failed {
        final int numBits = pin.length * 8;
        switch (v.type) {
            case "Mixed": return packetvoid.Void.readMixed(v.mixed, pin, numBits);
            case "Tags": return packetvoid.Void.readTags(v.tags, pin, numBits);
            case "Single": return packetvoid.Void.readSingle(v.single, pin, numBits);
            case "Empty": return packetvoid.Void.readEmpty(v.empty, pin, numBits);
            case "Frame": return packetvoid.Void.readFrame(v.frame, pin, numBits);
            case "Batch": return packetvoid.Void.readBatch(v.batch, pin, numBits);
            default: throw new Failed("unknown type " + v.type);
        }
    }

    // readBits reports where the object ends: its bit count.
    static int readBits(Value v, byte[] pin) throws Failed {
        final int numBits = pin.length * 8;
        switch (v.type) {
            case "Mixed": return packetvoid.Void.readMixedBits(v.mixed, pin, 0, numBits);
            case "Tags": return packetvoid.Void.readTagsBits(v.tags, pin, 0, numBits);
            case "Single": return packetvoid.Void.readSingleBits(v.single, pin, 0, numBits);
            case "Empty": return packetvoid.Void.readEmptyBits(v.empty, pin, 0, numBits);
            case "Frame": return packetvoid.Void.readFrameBits(v.frame, pin, 0, numBits);
            case "Batch": return packetvoid.Void.readBatchBits(v.batch, pin, 0, numBits);
            default: throw new Failed("unknown type " + v.type);
        }
    }

    static boolean mixedEqual(Mixed got, Mixed want) {
        if (got.type != want.type) {
            return false;
        }
        if (want.type != MixedType.PAYLOAD) {
            return true; // an unselected arm's storage is unspecified
        }
        // every backing entry, used or not, holds what the arm's construction
        // gave it: a read constructs the arm before decoding
        return got.payload.entriesCount == want.payload.entriesCount
                && got.payload.marker == want.payload.marker
                && got.payload.entries[0].retries == want.payload.entries[0].retries
                && got.payload.entries[1].retries == want.payload.entries[1].retries;
    }

    static boolean valueEqual(Value got, Value want) {
        switch (want.type) {
            case "Mixed": return mixedEqual(got.mixed, want.mixed);
            case "Tags": return got.tags.type == want.tags.type;
            case "Single": return got.single.type == want.single.type;
            case "Empty": return got.empty.type == want.empty.type;
            case "Frame":
                return got.frame.lead == want.frame.lead && got.frame.tail == want.frame.tail
                        && mixedEqual(got.frame.choice, want.frame.choice);
            case "Batch":
                if (got.batch.valuesCount != want.batch.valuesCount) {
                    return false;
                }
                for (int i = 0; i < want.batch.valuesCount; i++) {
                    if (!mixedEqual(got.batch.values[i], want.batch.values[i])) {
                        return false;
                    }
                }
                return true;
            default:
                return false;
        }
    }

    static byte[] load(Path dir, String name) throws Failed {
        try {
            return Files.readAllBytes(dir.resolve(name + ".bin"));
        } catch (java.io.IOException e) {
            throw new Failed("cannot open " + dir.resolve(name + ".bin") + ": " + e.getMessage());
        }
    }

    // The pin's bytes from a value: the bit writer's count and bytes, and the
    // whole-value writer's byte count beside them.
    static byte[] written(Value v, int bits) throws Failed {
        byte[] out = new byte[64];
        int wrote = writeBits(v, out);
        if (wrote != bits) {
            throw new Failed("write has " + wrote + " bits, want " + bits);
        }
        byte[] whole = new byte[64];
        int bytes = writeBytes(v, whole);
        if (bytes != (bits + 7) / 8) {
            throw new Failed("whole write has " + bytes + " bytes, want " + (bits + 7) / 8);
        }
        if (!Arrays.equals(out, whole)) {
            throw new Failed("the bit writer and the whole writer differ");
        }
        return Arrays.copyOf(out, bytes);
    }

    static void acceptCase(Path dir, String name, String type, String bitsColumn) throws Failed {
        Value want = build(name);
        if (want == null) {
            throw new Failed("unknown accept case");
        }
        if (!want.type.equals(type)) {
            throw new Failed("case type " + type + ", the driver builds another");
        }
        final int bits;
        try {
            bits = Integer.parseInt(bitsColumn);
        } catch (NumberFormatException e) {
            throw new Failed("bits column " + bitsColumn);
        }
        byte[] pin = load(dir, name);
        if (!Arrays.equals(written(want, bits), pin)) {
            throw new Failed("write bytes differ from the pin");
        }
        Value got = dirty(type);
        if (!read(got, pin)) {
            throw new Failed("read refused");
        }
        if (!valueEqual(got, want)) {
            throw new Failed("read value differs");
        }
        if (!Arrays.equals(written(got, bits), pin)) {
            throw new Failed("the read value writes other bytes than the pin");
        }
        Value counted = dirty(type);
        int end = readBits(counted, pin);
        if (end != bits) {
            throw new Failed("read has " + end + " bits, want " + bits);
        }
    }

    static void refuseCase(Path dir, String name, String type) throws Failed {
        if (!TYPES.contains(type)) {
            throw new Failed("unknown type " + type);
        }
        byte[] pin = load(dir, name);
        if (read(dirty(type), pin)) {
            throw new Failed("read accepted");
        }
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 1) {
            System.err.println("usage: Main <testdata/wire/packet-void>");
            System.exit(2);
        }
        // The generated surface beside the wire: tag extents, debug names and
        // maximum sizes. A miss exits non-zero, which tools/casegate holds red.
        if (MixedType.MAX != 2 || TagsType.MAX != 2 || SingleType.MAX != 1 || EmptyType.MAX != 0
                || !packetvoid.Void.enumNameMixedType(MixedType.PING).equals("Ping")
                || !packetvoid.Void.enumNameTagsType(TagsType.PONG).equals("Pong")
                || packetvoid.Void.MIXED_MAX_BITS != 23 || packetvoid.Void.TAGS_MAX_BITS != 2
                || packetvoid.Void.SINGLE_MAX_BITS != 1 || packetvoid.Void.EMPTY_MAX_BITS != 0) {
            System.out.println("FAILED: packet-void tag extents, debug names or maximum bits");
            System.exit(1);
        }
        Path dir = Path.of(args[0]);
        final List<String> lines;
        try {
            lines = Files.readAllLines(dir.resolve("cases.txt"));
        } catch (java.io.IOException e) {
            System.err.println("cannot open " + dir.resolve("cases.txt") + ": " + e.getMessage());
            System.exit(2);
            return;
        }
        for (String line : lines) {
            String[] fields = line.trim().split("\\s+");
            String name = fields[0];
            if (name.isEmpty() || name.startsWith("#")) {
                continue;
            }
            String why = null;
            try {
                if (fields.length < 4) {
                    throw new Failed("a case line needs <name> <type> <verdict> <bits>");
                }
                switch (fields[2]) {
                    case "accept":
                        acceptCase(dir, name, fields[1], fields[3]);
                        break;
                    case "refuse":
                        refuseCase(dir, name, fields[1]);
                        break;
                    default:
                        throw new Failed("verdict " + fields[2]);
                }
            } catch (Failed e) {
                why = e.getMessage();
            } catch (Throwable e) {
                why = "threw " + e;
            }
            if (why == null) {
                System.out.println("case " + name + " ok");
            } else {
                System.out.println("case " + name + " FAILED: " + why);
            }
        }
    }
}
