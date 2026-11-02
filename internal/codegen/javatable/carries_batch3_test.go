package javatable

import (
	"testing"
)

func TestJavaIntegrityCarry(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Ledger | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}

table Plain
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}

fixed table FixedLedger | integrity = "crc32c"
{
    n int32
    count uint16
}

fixed table FixedPlain
{
    n int32
    count uint16
}
`
	const driver = `import java.util.Arrays;
import probe.*;

public final class Main {
    private static void check(boolean cond, String msg) {
        if (!cond) throw new AssertionError(msg);
    }

    private static String toHex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte x : b) {
            sb.append(String.format("%02x", x & 0xff));
        }
        return sb.toString();
    }

    public static void main(String[] args) {
        // 1. Variable table with integrity = "crc32c" matches Go oracle golden (51b5231d trailer)
        LedgerVariable.Value l = new LedgerVariable.Value();
        l.n = 1000L;
        l.label = "ok";
        l.count = 7;
        PlainVariable.Value p = new PlainVariable.Value();
        p.n = 1000L;
        p.label = "ok";
        p.count = 7;

        byte[] pWire = PlainVariable.save(p);
        byte[] lWire = LedgerVariable.save(l);
        check(pWire != null && lWire != null && lWire.length == pWire.length + 4, "Ledger save length");
        String plainGolden = "010105e803000000000000020c026f6b030707000071f801864ce363af3d62cb8fecfcf73974a279448ee2e5b10300000000000000";
        String ledgerGolden = plainGolden + "51b5231d";
        check(plainGolden.equals(toHex(pWire)), "Plain golden: " + toHex(pWire));
        check(ledgerGolden.equals(toHex(lWire)), "Ledger golden: " + toHex(lWire));

        // Clean load, loadMeasure, loadRetain, measureRetain, saveRetain
        TableVariable.Report rep = new TableVariable.Report();
        LedgerVariable.Value lLoaded = LedgerVariable.load(lWire, rep);
        check(lLoaded != null && !rep.malformed && !rep.refused && lLoaded.n == 1000L && "ok".equals(lLoaded.label) && lLoaded.count == 7, "Ledger load");
        check(LedgerVariable.loadMeasure(lWire) > 0, "Ledger loadMeasure");

        TableRetain ret = new TableRetain(1 << 16, 1 << 10);
        rep.reset();
        LedgerVariable.Value lrLoaded = LedgerRetain.loadRetain(lWire, ret, rep);
        check(lrLoaded != null && !rep.malformed && !rep.refused, "Ledger loadRetain");
        long mr = LedgerRetain.measureRetain(lrLoaded, ret);
        rep.reset();
        byte[] sr = LedgerRetain.saveRetain(lrLoaded, ret, rep);
        check(sr != null && sr.length == mr && Arrays.equals(lWire, sr), "Ledger saveRetain/measureRetain");

        // Every byte flip of lWire: byte 0 is direction refusal (message_form_as_file or newer_form), bytes 1..end are malformed integrity_mismatch
        for (int i = 0; i < lWire.length; i++) {
            for (int bit : new int[]{1, 2, 4, 8, 16, 32, 64, 128, 255}) {
                byte[] bad = Arrays.copyOf(lWire, lWire.length);
                bad[i] ^= (byte) bit;
                rep.reset();
                LedgerVariable.Value got = LedgerVariable.load(bad, rep);
                check(got == null, "Ledger load accepted flipped byte " + i);
                if (i == 0) {
                    String want = (bad[0] & 0xff) == 2 ? "message_form_as_file" : "newer_form";
                    check(rep.refused && !rep.malformed && want.equals(rep.reason), "byte 0 flip reason: " + rep.reason);
                } else {
                    check(rep.malformed && !rep.refused && "integrity_mismatch".equals(rep.reason), "byte " + i + " flip reason: " + rep.reason);
                }
                TableVariable.Measure m = new TableVariable.Measure();
                check(LedgerVariable.loadMeasure(bad, m) < 0, "Ledger loadMeasure accepted flipped byte " + i);
                rep.reset();
                check(LedgerRetain.loadRetain(bad, new TableRetain(1 << 16, 1 << 10), rep) == null, "Ledger loadRetain accepted flipped byte " + i);
            }
        }

        // 2. Fixed table with integrity = "crc32c":
        // (a) Variable form 1 (stella-b78d32f07f14): FixedLedgerVariable and FixedLedgerRetain also carry the 4-byte CRC-32C trailer!
        FixedLedgerVariable.Value flv = new FixedLedgerVariable.Value();
        flv.n = 1000;
        flv.count = 7;
        FixedPlainVariable.Value fpv = new FixedPlainVariable.Value();
        fpv.n = 1000;
        fpv.count = 7;
        byte[] flvWire = FixedLedgerVariable.save(flv);
        byte[] fpvWire = FixedPlainVariable.save(fpv);
        check(flvWire.length == fpvWire.length + 4, "FixedLedgerVariable save has 4-byte CRC trailer");
        rep.reset();
        FixedLedgerVariable.Value flvLoaded = FixedLedgerVariable.load(flvWire, rep);
        check(flvLoaded != null && !rep.malformed && !rep.refused && flvLoaded.n == 1000 && flvLoaded.count == 7, "FixedLedgerVariable load");
        byte[] flvCorrupt = Arrays.copyOf(flvWire, flvWire.length);
        flvCorrupt[flvCorrupt.length - 1] ^= 1;
        rep.reset();
        check(FixedLedgerVariable.load(flvCorrupt, rep) == null && rep.malformed && "integrity_mismatch".equals(rep.reason), "FixedLedgerVariable corrupt CRC");

        // (b) Fixed form 3: FixedLedgerFixed has +4 bytes and checks CRC after form byte
        FixedLedgerFixed.Value flf = new FixedLedgerFixed.Value();
        flf.n = 1000;
        flf.count = 7;
        FixedPlainFixed.Value fpf = new FixedPlainFixed.Value();
        fpf.n = 1000;
        fpf.count = 7;
        check(FixedLedgerFixed.measure(1) == FixedPlainFixed.measure(1) + 4, "FixedLedgerFixed.measure +4");
        byte[] fWire = new byte[FixedLedgerFixed.measure(1)];
        check(FixedLedgerFixed.save(new FixedLedgerFixed.Value[]{flf}, 1, fWire) == fWire.length, "FixedLedgerFixed.save");
        FixedLedgerFixed.Value[] fBack = {new FixedLedgerFixed.Value()};
        TableFixed.Report fRep = new TableFixed.Report();
        check(FixedLedgerFixed.load(fBack, 1, fWire, TableFixed.plan(1 << 14), new short[1 << 14], FixedLedgerFixed.image(), fRep) == 1, "FixedLedgerFixed.load");
        check(!fRep.malformed && !fRep.refused && fBack[0].n == 1000 && fBack[0].count == 7, "FixedLedgerFixed values");
        for (int i = 0; i < fWire.length; i++) {
            byte[] bad = Arrays.copyOf(fWire, fWire.length);
            bad[i] ^= 1;
            fRep.reset();
            check(FixedLedgerFixed.load(fBack, 1, bad, TableFixed.plan(1 << 14), new short[1 << 14], FixedLedgerFixed.image(), fRep) == -1, "FixedLedgerFixed flip " + i);
            if (i == 0) {
                check(fRep.refused && !fRep.malformed, "FixedLedgerFixed byte 0 flip is refused");
            } else {
                check(fRep.malformed && !fRep.refused, "FixedLedgerFixed byte " + i + " flip is malformed");
            }
        }

        // (c) Message form 2: FixedLedgerMessage matches Go oracle golden (0200813e00002007000078f2be34)
        byte[] ann = new byte[TableVocabulary.announceMeasure()];
        check(TableVocabulary.announce(ann) == ann.length, "announce");
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(4096);
        check(TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report()), "announceRead");

        TableVocabulary.Report mRep = new TableVocabulary.Report();
        int mLen = FixedLedgerMessage.measureMessages(new FixedLedgerFixed.Value[]{flf}, 1, mRep);
        int mpLen = FixedPlainMessage.measureMessages(new FixedPlainFixed.Value[]{fpf}, 1, mRep);
        check(mLen == mpLen + 4, "FixedLedgerMessage.measureMessages +4");
        byte[] mWire = new byte[mLen];
        check(FixedLedgerMessage.saveMessages(new FixedLedgerFixed.Value[]{flf}, 1, mWire, mRep) == mLen, "FixedLedgerMessage.saveMessages");
        check("0200813e00002007000078f2be34".equals(toHex(mWire)), "FixedLedgerMessage golden: " + toHex(mWire));
        FixedLedgerFixed.Value[] mBack = {new FixedLedgerFixed.Value()};
        mRep.zero();
        check(FixedLedgerMessage.loadMessages(mBack, 1, vocab, mWire, mRep) == 1 && !mRep.malformed && !mRep.refused && mBack[0].n == 1000 && mBack[0].count == 7, "FixedLedgerMessage.loadMessages");
        for (int i = 0; i < mWire.length; i++) {
            byte[] bad = Arrays.copyOf(mWire, mWire.length);
            bad[i] ^= 1;
            mRep.zero();
            check(FixedLedgerMessage.loadMessages(mBack, 1, vocab, bad, mRep) == 0, "FixedLedgerMessage flip " + i);
            if (i == 0) {
                check(mRep.refused && "newer_form".equals(mRep.reason), "FixedLedgerMessage byte 0 flip");
            } else {
                check(mRep.malformed && "integrity_mismatch".equals(mRep.reason), "FixedLedgerMessage byte " + i + " flip");
            }
        }

        System.out.println("INTEGRITY OK");
    }
}
`
	if got := runJavaCarryProbe(t, schema, "IntegrityDriver", driver); got != "INTEGRITY OK" {
		t.Fatalf("unexpected output: %s", got)
	}
}
