// THE PAIRED BENCH CORPUS ON THE FIXED FORM (docs/SPEC-TABLES.md §3.4), the
// Java half.
//
// The C++ REFERENCE decodes the matched bench's sixty-four logical records from
// the canonical PACKET corpus and writes them with its form-3 writer
// (test/bench/fixedform_corpus.cpp), stating the VALUES beside the bytes. This
// leg is checked twice over against that:
//
//   THE BYTES   read the reference's file and save it back; the bytes must be
//               IDENTICAL. Every field is reached, because a byte this port
//               encodes differently is a byte that does not come back.
//   THE VALUES  check what was DECODED against the oracle, stated
//               independently. A reader and a writer that share one offset
//               mistake round trip perfectly and are both wrong, so the bytes
//               alone are not enough.
//
// It is also the one fixed-form corpus that carries the WIDE KINDS — a
// `fixed(24, 8)`, a `ufixed(8, 8)`, a bare `uint128` and a ranged `int128` —
// which §15 refuses to the two ACCELERATORS in this backend and which this
// form carries by its own constant-size table.

import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class BenchFixed {
    private BenchFixed() {}

    private static int failures = 0;

    static void check(boolean ok, String what) {
        if (!ok) {
            System.out.println("FAILED: " + what);
            failures++;
        }
    }

    static byte[] slurp(String path) {
        try {
            return Files.readAllBytes(Paths.get(path));
        } catch (java.io.IOException e) {
            System.out.println("FAILED: cannot read " + path + ": " + e);
            failures++;
            return new byte[0];
        }
    }

    // ---- the value oracle -------------------------------------------------
    //
    // The oracle is one JSON object per record. Only the record's own flat keys
    // are read here — each is unique inside a record, so a scan is enough and a
    // JSON library this generated code does not need is a dependency it does
    // not take.

    static List<String> records(String json) {
        final List<String> out = new ArrayList<>();
        int depth = 0;
        int start = -1;
        for (int i = 0; i < json.length(); i++) {
            final char c = json.charAt(i);
            if (c == '{') {
                if (depth == 0) { start = i; }
                depth++;
            } else if (c == '}') {
                depth--;
                if (depth == 0 && start >= 0) {
                    out.add(json.substring(start, i + 1));
                    start = -1;
                }
            }
        }
        return out;
    }

    static long num(String record, String key) {
        final Matcher m = Pattern.compile("\"" + key + "\":\"?(-?\\d+)\"?").matcher(record);
        if (!m.find()) {
            System.out.println("FAILED: the oracle has no key " + key);
            failures++;
            return Long.MIN_VALUE;
        }
        // AN UNSIGNED 64-BIT VALUE RIDES BIT-TRANSPARENT in Java's signed
        // long, which is the packet codec's own rule, so the oracle's decimal
        // is read the same way: signed where it fits and unsigned where it
        // does not, and either way the sixty-four bits are the sixty-four bits.
        final String raw = m.group(1);
        try {
            return Long.parseLong(raw);
        } catch (NumberFormatException e) {
            return Long.parseUnsignedLong(raw);
        }
    }

    static boolean flag(String record, String key) {
        return Pattern.compile("\"" + key + "\":true").matcher(record).find();
    }

    public static void main(String[] args) {
        if (args.length != 1) {
            System.out.println("usage: BenchFixed <corpus-dir>");
            System.exit(2);
        }
        final String dir = args[0];
        final byte[] golden = slurp(dir + "/bench_fixed.bin");
        final String oracle = new String(slurp(dir + "/bench_fixed.oracle.json"),
                java.nio.charset.StandardCharsets.UTF_8);
        final List<String> want = records(oracle);
        check(want.size() == 64, "the value oracle states sixty-four records, not " + want.size());

        final bench.FixedTableFixed.Value[] v = new bench.FixedTableFixed.Value[64];
        for (int i = 0; i < v.length; i++) { v[i] = new bench.FixedTableFixed.Value(); }
        final bench.TableFixed.Report r = new bench.TableFixed.Report();
        final int n = bench.FixedTableFixed.load(v, v.length, golden,
                bench.TableFixed.plan(8192), new short[8192], bench.FixedTableFixed.image(), r);
        check(n == 64, "the reference's corpus is sixty-four records, read " + n);
        check(!r.refused && !r.malformed, "the corpus reads clean: refused=" + r.refused + " malformed=" + r.malformed);
        check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0,
                "its own layout is the identity plan, and a clean read moves no counter");

        if (n == 64 && want.size() == 64) {
            for (int k = 0; k < 64; k++) {
                final bench.BenchMixedFixed.Value m = v[k].value;
                final String o = want.get(k);
                final String at = "record " + k + ": ";
                check((m.sequence & 0xffffL) == num(o, "sequence"), at + "sequence");
                check(m.ackSequence == num(o, "ack_sequence"), at + "ack_sequence");
                check((m.ackBits & 0xffffffffL) == num(o, "ack_bits"), at + "ack_bits");
                check(m.sessionId == num(o, "session_id"), at + "session_id");
                check((m.clientId & 0xffffffffL) == num(o, "client_id"), at + "client_id");
                check(m.nonce == num(o, "nonce"), at + "nonce");
                check(m.worldTime == num(o, "world_time"), at + "world_time");
                check(m.frameTick == num(o, "frame_tick"), at + "frame_tick");
                // A FIXED-POINT FIELD RIDES ITS RAW SCALED INTEGER at its
                // storage width, and not as a quantized index (§3.4).
                check(m.serverTime == num(o, "server_time"), at + "server_time — fixed(24, 8), the raw scaled integer");
                check(m.entitiesCount == num(o, "entities_count"), at + "entities_count");
                check(m.statsCount == num(o, "stats_count"), at + "stats_count");
                check(m.gameEvent.type == num(o, "game_event_type"), at + "the union's tag");
                check(m.playerNameLength == num(o, "player_name_length"), at + "player_name_length");
                check(m.payloadLength == num(o, "payload_length"), at + "payload_length");
                // FLOATS RIDE THEIR IEEE-754 BIT PATTERN with no
                // canonicalisation (§3.4), so the comparison is exact.
                check((Float.floatToRawIntBits(m.aimX) & 0xffffffffL) == num(o, "aim_x_bits"), at + "aim_x, by its bits");
                check((Float.floatToRawIntBits(m.aimY) & 0xffffffffL) == num(o, "aim_y_bits"), at + "aim_y, by its bits");
                check((Float.floatToRawIntBits(m.aimZ) & 0xffffffffL) == num(o, "aim_z_bits"), at + "aim_z, by its bits");
                check((Float.floatToRawIntBits(m.recoil) & 0xffffffffL) == num(o, "recoil_bits"), at + "recoil, by its bits");
                check(Double.doubleToRawLongBits(m.drift) == num(o, "drift_bits"), at + "drift, by its bits");
                // 128 BITS, SIXTEEN BYTES, THE LOW HALF THEN THE HIGH (§3).
                check(m.wideKey.lo == num(o, "wide_key_lo"), at + "wide_key, the low half");
                check(m.wideKey.hi == num(o, "wide_key_hi"), at + "wide_key, the high half");
                check(m.flux.lo == num(o, "flux_lo"), at + "flux, the low half");
                check(m.flux.hi == num(o, "flux_hi"), at + "flux, the high half");
                check((m.ping & 0xffff) == num(o, "ping"), at + "ping — ufixed(8, 8), the raw scaled integer");
                check((m.crcHint & 0xffffffffL) == num(o, "crc_hint"), at + "crc_hint");
                check(m.hasExtra == flag(o, "has_extra"), at + "has_extra");
                check(m.extra == num(o, "extra"), at + "extra");
                check(m.idleTicks == num(o, "idle_ticks"), at + "idle_ticks");
            }
        }

        final byte[] back = new byte[bench.FixedTableFixed.measure(64)];
        check(bench.FixedTableFixed.save(v, 64, back) == back.length, "save fills what measure says");
        if (!Arrays.equals(back, golden)) {
            int at = 0;
            while (at < Math.min(back.length, golden.length) && back[at] == golden[at]) { at++; }
            check(false, "byte " + at + " is the first byte differing from the C++ reference"
                    + " (ours " + (back[at] & 0xff) + ", theirs " + (golden[at] & 0xff) + ")");
        }

        if (failures != 0) {
            System.out.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("java fixed form, the paired bench corpus: 64/64 records, "
                + "the values are the reference's and so are the bytes");
    }
}
