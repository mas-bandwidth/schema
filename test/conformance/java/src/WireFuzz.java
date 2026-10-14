// THE JAVA WIRE-FUZZ DRIVER (test/conformance/README.md, "The wire-fuzz
// driver"). One process on a pipe; the harness owns the mutants and the oracle
// (test/conformance/harness/wirefuzz.go). This process binds a roster entry to
// its generated codec and answers, per mutant, the load verdict, the report and
// the re-saved bytes. It decides nothing about what a mutant means.
//
// The Card root is the one codec this stream carries: form 1 (the variable
// form, docs/SPEC-TABLES.md §3), read by cards.CardVariable.load, re-saved by
// cards.CardVariable.save, sized by cards.CardVariable.loadMeasure (§6.5).
// The roster's retain byte turns the same mutant through the RETAINING pair
// (§6.6): cards.CardRetain.loadRetain and saveRetain over a caller-owned
// TableRetain, whose two counters ride the reply beside the six.
//
// The stream is little-endian throughout (the harness's own convention).
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

public final class WireFuzz {
    private WireFuzz() {}

    public static void main(String[] args) throws IOException {
        InputStream stdin = System.in;
        OutputStream stdout = System.out;

        long count = readU32(stdin);
        if (count > 4096) { System.exit(1); }
        boolean[] available = new boolean[(int) count];
        boolean[] retain = new boolean[(int) count];
        for (int i = 0; i < count; i++) {
            String unit = readName(stdin);
            String root = readName(stdin);
            int form = stdin.read();
            int retainByte = stdin.read();
            // the Card root's variable form is the one codec this stream carries;
            // the roster's retain byte (docs/SPEC-TABLES.md §6.6) selects the arm
            retain[i] = retainByte != 0;
            available[i] = form == 1 && "cards".equals(unit) && "Card".equals(root);
            stdout.write(available[i] ? 1 : 0);
        }
        stdout.flush();

        byte[] out = new byte[1024];
        while (true) {
            long index;
            try {
                index = readU32(stdin);
            } catch (EOFException e) {
                break;
            }
            long size = readU32(stdin);
            byte[] wire = new byte[(int) size];
            int got = 0;
            while (got < size) {
                int n = stdin.read(wire, got, (int) size - got);
                if (n < 0) { System.exit(1); }
                got += n;
            }

            cards.TableVariable.Report report = new cards.TableVariable.Report();
            int loaded;
            long measure;
            long saved = -1;
            byte[] savedBytes = null;
            if (!available[(int) index]) {
                loaded = 0;
                measure = -1;
            } else if ((measure = cards.CardVariable.loadMeasure(wire)) < 0) {
                // A region framing refusal precedes Load and moves no report
                // counter (SPEC-TABLES §6.5, count_over_length / extent_cap).
                loaded = 0;
            } else if (retain[(int) index]) {
                // THE RETAINING PAIR (docs/SPEC-TABLES.md §6.6): the same mutant
                // read and re-saved through the caller-owned storage, with the two
                // counters read after the save so they accumulate across the pair.
                cards.TableRetain store = new cards.TableRetain(1 << 20, (1 << 20) / 8);
                cards.CardVariable.Value v = cards.CardRetain.loadRetain(wire, store, report);
                loaded = v != null ? 1 : 0;
                if (v != null) {
                    savedBytes = cards.CardRetain.saveRetain(v, store, report);
                    saved = savedBytes == null ? -1 : savedBytes.length;
                }
            } else {
                cards.CardVariable.Value v = cards.CardVariable.load(wire, report);
                loaded = v != null ? 1 : 0;
                if (v != null) {
                    savedBytes = cards.CardVariable.save(v);
                    saved = savedBytes.length;
                }
            }

            int n = 0;
            out[n++] = (byte) loaded;
            n = putI32(out, n, report.unknown);
            n = putI32(out, n, report.kindMismatch);
            n = putI32(out, n, report.widened);
            n = putI32(out, n, report.clamped);
            n = putI32(out, n, 0); // duplicate: the wire form raises none
            out[n++] = (byte) (report.malformed ? 1 : 0);
            out[n++] = (byte) (report.refused ? 1 : 0);
            n = putI64(out, n, measure);
            n = putI32(out, n, report.retained);
            n = putI32(out, n, report.retainLost);
            n = putI64(out, n, saved);
            stdout.write(out, 0, n);
            if (saved > 0) {
                stdout.write(savedBytes);
            }
            stdout.flush();
        }
    }

    private static long readU32(InputStream in) throws IOException {
        int b0 = in.read();
        int b1 = in.read();
        int b2 = in.read();
        int b3 = in.read();
        if (b3 < 0) { throw new EOFException(); }
        return (b0 & 0xff) | (b1 & 0xff) << 8 | (b2 & 0xff) << 16 | (long) (b3 & 0xff) << 24;
    }

    private static String readName(InputStream in) throws IOException {
        int n0 = in.read();
        int n1 = in.read();
        if (n1 < 0) { throw new EOFException(); }
        int n = (n0 & 0xff) | (n1 & 0xff) << 8;
        byte[] b = new byte[n];
        int got = 0;
        while (got < n) {
            int r = in.read(b, got, n - got);
            if (r < 0) { throw new EOFException(); }
            got += r;
        }
        return new String(b, java.nio.charset.StandardCharsets.UTF_8);
    }

    private static int putI32(byte[] out, int at, int v) {
        out[at] = (byte) v;
        out[at + 1] = (byte) (v >>> 8);
        out[at + 2] = (byte) (v >>> 16);
        out[at + 3] = (byte) (v >>> 24);
        return at + 4;
    }

    private static int putI64(byte[] out, int at, long v) {
        for (int i = 0; i < 8; i++) {
            out[at + i] = (byte) (v >>> (8 * i));
        }
        return at + 8;
    }
}
