package javatable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func runJavaCarryProbe(t *testing.T, schema, _ string, driver string) string {
	t.Helper()
	slowtest.Gate(t, "javac")
	_, out := compileAndRunJavaProbe(t, schema, "probe", driver)
	return out
}

func TestJavaTimestampCarry(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table FixedStamp {
    at timestamp
    tag int64
}

table VarStamp {
    at timestamp
    later timestamp
    tag int64
}
`
	const driver = `import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import probe.*;

public final class Main {
    private static void check(boolean cond, String msg) {
        if (!cond) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        // 1. Variable wire + JSON + Cook + Retain
        VarStampVariable.Value v = new VarStampVariable.Value();
        v.at = -1L;
        v.later = 1_500_000_000L;
        v.tag = 42L;

        byte[] wire = VarStampVariable.save(v);
        check(wire != null, "save null");
        TableVariable.Report rep = new TableVariable.Report();
        VarStampVariable.Value loaded = VarStampVariable.load(wire, rep);
        check(loaded != null && !rep.damaged && !rep.refused && rep.unknown == 0 && rep.widened == 0, "load report");
        check(loaded.at == -1L && loaded.later == 1_500_000_000L && loaded.tag == 42L, "loaded scalars");

        TableRetain ret = new TableRetain(1 << 16, 1 << 10);
        rep.reset();
        VarStampVariable.Value rLoaded = VarStampRetain.loadRetain(wire, ret, rep);
        check(rLoaded != null && !rep.damaged && !rep.refused, "loadRetain");
        byte[] rWire = VarStampRetain.saveRetain(rLoaded, ret, rep);
        check(Arrays.equals(wire, rWire), "saveRetain roundtrip");

        long[] boundaries = new long[]{
            0L,
            -1L,
            1_500_000_000L,
            1_709_208_896_789_000_000L,
            Long.MIN_VALUE,
            Long.MAX_VALUE
        };
        String[] boundaryTexts = new String[]{
            "\"1970-01-01T00:00:00Z\"",
            "\"1969-12-31T23:59:59.999999999Z\"",
            "\"1970-01-01T00:00:01.5Z\"",
            "\"2024-02-29T12:14:56.789Z\"",
            "\"1677-09-21T00:12:43.145224192Z\"",
            "\"2262-04-11T23:47:16.854775807Z\""
        };
        for (int i = 0; i < boundaries.length; i++) {
            VarStampVariable.Value bv = new VarStampVariable.Value();
            bv.at = boundaries[i];
            byte[] jb = VarStampVariable.toJson(bv);
            String jt = new String(jb, StandardCharsets.UTF_8);
            check(jt.contains(boundaryTexts[i]), "json boundary " + i + ": " + jt);
            rep.reset();
            VarStampVariable.Value fromJ = VarStampVariable.fromJson(jb, rep);
            check(fromJ != null && !rep.damaged && !rep.refused && rep.unknown == 0 && fromJ.at == boundaries[i], "fromJson boundary " + i);
        }

        // Cook LE & BE
        for (int order : new int[]{TableCookWrite.LITTLE, TableCookWrite.BIG}) {
            long cSize = VarStampCook.cookMeasure(v);
            check(cSize > 0, "cookMeasure");
            byte[] cooked = new byte[(int) cSize];
            check(VarStampCook.cookFrom(v, cooked, order), "cookFrom");
            if (order == TableCookWrite.LITTLE) {
                VarStampCook cRoot = VarStampCook.open(cooked);
                check(cRoot != null, "cook open");
                int root = cRoot.root();
                check(VarStampRow.at(cooked, root) == -1L && VarStampRow.later(cooked, root) == 1_500_000_000L && VarStampRow.tag(cooked, root) == 42L, "cook row scalars");
            } else {
                check("foreign_order".equals(VarStampCook.openReason(cooked, 0, cooked.length)), "cook big foreign_order");
            }
        }

        // 2. Fixed & Message forms on FixedStamp (stella-e6c648b12aad: fixed layout entry is kind 5, variable/message is kind 36)
        FixedStampFixed.Value fv = new FixedStampFixed.Value();
        fv.at = 1_000_000_000L;
        fv.tag = -2_000_000_000L;
        byte[] fWire = new byte[FixedStampFixed.measure(1)];
        check(FixedStampFixed.save(new FixedStampFixed.Value[]{fv}, 1, fWire) == fWire.length, "FixedStampFixed.save");
        FixedStampFixed.Value[] fBack = {new FixedStampFixed.Value()};
        TableFixed.Report fRep = new TableFixed.Report();
        check(FixedStampFixed.load(fBack, 1, fWire, TableFixed.plan(1 << 14), new short[1 << 14], FixedStampFixed.image(), fRep) == 1, "FixedStampFixed.load");
        check(!fRep.damaged && !fRep.refused && fBack[0].at == 1_000_000_000L && fBack[0].tag == -2_000_000_000L, "FixedStampFixed values");

        byte[] ann = new byte[TableVocabulary.announceMeasure()];
        check(TableVocabulary.announce(ann) == ann.length, "Announce.write");
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(4096);
        check(TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report()), "TableVocabulary.announceRead");
        byte[] msg = new byte[4096];
        int msgLen = FixedStampMessage.saveMessages(new FixedStampFixed.Value[]{fv}, 1, msg, new TableVocabulary.Report());
        check(msgLen > 0, "FixedStampMessage.saveMessages");
        FixedStampFixed.Value[] mBack = {new FixedStampFixed.Value()};
        TableVocabulary.Report mRep = new TableVocabulary.Report();
        check(FixedStampMessage.loadMessages(mBack, 1, vocab, Arrays.copyOf(msg, msgLen), mRep) == 1, "FixedStampMessage.loadMessages");
        check(!mRep.damaged && !mRep.refused && mBack[0].at == 1_000_000_000L && mBack[0].tag == -2_000_000_000L, "FixedStampMessage values");

        // 3. Strict RFC 3339 negative cases
        String[] badStamps = new String[]{
            "2023-02-29T00:00:00Z",
            "2024-06-30T23:59:60Z",
            "2024-01-01 00:00:00Z",
            "1970-01-01T00:00:00.1234567891Z",
            "1970-01-01T00:00:00.Z",
            "1970-01-01T00:00:00+24:00",
            "1970-01-01T00:00:00+00:60",
            "1677-09-21T00:12:43.145224191Z",
            "2262-04-11T23:47:16.854775808Z",
            "1970-01-01T00:00:00Z\\u0000"
        };
        for (String bad : badStamps) {
            rep.reset();
            String input = "{\"at\":\"" + bad + "\"}";
            VarStampVariable.Value badV = VarStampVariable.fromJson(input.getBytes(StandardCharsets.UTF_8), rep);
            check(badV == null && rep.damaged && "timestamp_not_rfc3339: at".equals(rep.reason), "expected timestamp_not_rfc3339: at for " + bad + ", got reason=" + rep.reason);
        }

        // Wrong JSON shape refuses the whole load: no value (§4, §16.2)
        rep.reset();
        VarStampVariable.Value shapeV = VarStampVariable.fromJson("{\"at\":123}".getBytes(StandardCharsets.UTF_8), rep);
        check(shapeV == null && (rep.refused || rep.damaged), "wrong shape timestamp not refused: refused=" + rep.refused + " damaged=" + rep.damaged);

        // Non-widening on wire: kind 5 (int64) into timestamp (kind 36) is a kind
        // mismatch, which refuses the whole load: no value, refused, not damaged
        byte[] intWire = Arrays.copyOf(wire, wire.length);
        check(intWire[2] == 36, "wire kind is 36");
        intWire[2] = 5; // int64 kind
        rep.reset();
        VarStampVariable.Value wLoaded = VarStampVariable.load(intWire, rep);
        check(wLoaded == null && rep.refused && !rep.damaged, "kind 5 does not widen to kind 36: refused=" + rep.refused + " damaged=" + rep.damaged);

        System.out.println("TIMESTAMP OK");
    }
}
`
	if got := runJavaCarryProbe(t, schema, "TimestampDriver", driver); got != "TIMESTAMP OK" {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestJavaOptionalMapCarry(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table OptMaps {
    maybeMap ?map[string(8)]int32
    optVals map[string(8)]?int32
    both ?map[int32]?int64
}
`
	const driver = `import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import probe.*;

public final class Main {
    private static void check(boolean cond, String msg) {
        if (!cond) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        // 1. Absent vs present empty vs present populated ?map
        OptMapsVariable.Value v = new OptMapsVariable.Value();
        check(!v.maybeMapPresent && !v.bothPresent, "fresh ?map absent");

        // Present with entries, and map[K]?V with absent and present values
        v.maybeMapPresent = true;
        OptMapsMaybeMapEntryVariable.Value m1 = new OptMapsMaybeMapEntryVariable.Value();
        m1.key = "x";
        m1.value = 7;
        v.maybeMap.add(m1);

        OptMapsOptValsEntryVariable.Value ov1 = new OptMapsOptValsEntryVariable.Value();
        ov1.key = "abs";
        ov1.valuePresent = false;
        ov1.value = 99; // stale value on absent entry must not ride
        v.optVals.add(ov1);

        OptMapsOptValsEntryVariable.Value ov2 = new OptMapsOptValsEntryVariable.Value();
        ov2.key = "pres";
        ov2.valuePresent = true;
        ov2.value = 0; // present default must ride
        v.optVals.add(ov2);

        byte[] wire = OptMapsVariable.save(v);
        TableVariable.Report rep = new TableVariable.Report();
        OptMapsVariable.Value loaded = OptMapsVariable.load(wire, rep);
        check(loaded != null && loaded.maybeMapPresent && loaded.maybeMap.size() == 1 && loaded.maybeMap.get(0).value == 7, "loaded maybeMap");
        check(!loaded.bothPresent, "loaded both absent");
        check(loaded.optVals.size() == 2, "loaded optVals size");
        check("abs".equals(loaded.optVals.get(0).key) && !loaded.optVals.get(0).valuePresent && loaded.optVals.get(0).value == 0, "loaded abs entry");
        check("pres".equals(loaded.optVals.get(1).key) && loaded.optVals.get(1).valuePresent && loaded.optVals.get(1).value == 0, "loaded pres entry");

        // JSON roundtrip
        byte[] jsonBytes = OptMapsVariable.toJson(loaded);
        String jsonText = new String(jsonBytes, StandardCharsets.UTF_8);
        check(jsonText.contains("\"abs\": null") && jsonText.contains("\"pres\": 0"), "json optVals: " + jsonText);
        rep.reset();
        OptMapsVariable.Value fromJ = OptMapsVariable.fromJson(jsonBytes, rep);
        check(fromJ != null && Arrays.equals(OptMapsVariable.save(fromJ), wire), "fromJson roundtrip");

        // Cook LE & BE
        for (int order : new int[]{TableCookWrite.LITTLE, TableCookWrite.BIG}) {
            long cSize = OptMapsCook.cookMeasure(loaded);
            byte[] cooked = new byte[(int) cSize];
            check(OptMapsCook.cookFrom(loaded, cooked, order), "cookFrom");
            if (order == TableCookWrite.LITTLE) {
                OptMapsCook cook = OptMapsCook.open(cooked);
                check(cook != null, "cook open");
                int root = cook.root();
                check(OptMapsRow.maybeMapPresent(cooked, root) && OptMapsRow.maybeMapCount(cooked, root) == 1, "row maybeMapPresent");
                check(!OptMapsRow.bothPresent(cooked, root) && OptMapsRow.bothCount(cooked, root) == 0, "row both absent");
                int ovCount = OptMapsRow.optValsCount(cooked, root);
                int ovBase = cook.at(OptMapsRow.optValsSlot(root), ovCount * OptMapsRow.optValsStride);
                int absAt = OptMapsRow.optValsFind(cooked, ovBase, ovCount, "abs".getBytes(StandardCharsets.UTF_8));
                int presAt = OptMapsRow.optValsFind(cooked, ovBase, ovCount, "pres".getBytes(StandardCharsets.UTF_8));
                check(absAt >= 0 && !OptMapsOptValsEntryRow.valuePresent(cooked, absAt), "row abs valuePresent");
                check(presAt >= 0 && OptMapsOptValsEntryRow.valuePresent(cooked, presAt) && OptMapsOptValsEntryRow.value(cooked, presAt) == 0, "row pres valuePresent");
            } else {
                check("foreign_order".equals(OptMapsCook.openReason(cooked, 0, cooked.length)), "cook big foreign_order");
            }
        }

        // Clearing maybeMapPresent with stale entries in v.maybeMap must act absent in save, toJson, and cookFrom
        loaded.maybeMapPresent = false;
        OptMapsVariable.Value freshAbsent = new OptMapsVariable.Value();
        freshAbsent.optVals.addAll(loaded.optVals);
        check(Arrays.equals(OptMapsVariable.save(loaded), OptMapsVariable.save(freshAbsent)), "stale ?map save");
        check(Arrays.equals(OptMapsVariable.toJson(loaded), OptMapsVariable.toJson(freshAbsent)), "stale ?map json");
        long cSize1 = OptMapsCook.cookMeasure(loaded);
        long cSize2 = OptMapsCook.cookMeasure(freshAbsent);
        check(cSize1 == cSize2, "stale ?map cookMeasure");
        byte[] c1 = new byte[(int) cSize1];
        byte[] c2 = new byte[(int) cSize2];
        check(OptMapsCook.cookFrom(loaded, c1, TableCookWrite.LITTLE) && OptMapsCook.cookFrom(freshAbsent, c2, TableCookWrite.LITTLE), "stale ?map cookFrom");
        check(Arrays.equals(c1, c2), "stale ?map cooked bytes");

        // stella-78465217791c (SPEC-TABLES.md §4, §16.2): a null in map[K]?V is ABSENT and reads clean;
        // a non-null wrong-shape value, or a repeated key, refuses the whole load: no value
        rep.reset();
        OptMapsVariable.Value nullOk = OptMapsVariable.fromJson(
            "{\"optVals\":{\"b\":null,\"c\":9}}".getBytes(StandardCharsets.UTF_8), rep);
        check(nullOk != null && !rep.damaged && !rep.refused && nullOk.optVals.size() == 2, "null entry read: refused=" + rep.refused + " damaged=" + rep.damaged);
        check("b".equals(nullOk.optVals.get(0).key) && !nullOk.optVals.get(0).valuePresent, "b absent");
        check("c".equals(nullOk.optVals.get(1).key) && nullOk.optVals.get(1).valuePresent && nullOk.optVals.get(1).value == 9, "c present");
        rep.reset();
        OptMapsVariable.Value wrongShape = OptMapsVariable.fromJson(
            "{\"optVals\":{\"a\":\"bad\",\"b\":null}}".getBytes(StandardCharsets.UTF_8), rep);
        check(wrongShape == null && (rep.refused || rep.damaged), "wrong-shape ?V not refused: refused=" + rep.refused + " damaged=" + rep.damaged);
        rep.reset();
        OptMapsVariable.Value dupKey = OptMapsVariable.fromJson(
            "{\"optVals\":{\"c\":9,\"c\":9}}".getBytes(StandardCharsets.UTF_8), rep);
        check(dupKey == null && (rep.refused || rep.damaged), "repeated key not refused: refused=" + rep.refused + " damaged=" + rep.damaged);

        System.out.println("OPTIONAL_MAP OK");
    }
}
`
	if got := runJavaCarryProbe(t, schema, "OptionalMapDriver", driver); got != "OPTIONAL_MAP OK" {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestJavaRequiredCarry(t *testing.T) {
	t.Parallel()
	const schema = `package probe

enum Key { A, B }

table Other {
    ver uint8
}

table RequiredEmpty {
    value Other | required
}

table KeyedReq {
    values [Key]int32 | required
}

table GuardedReq {
    active bool
    if active {
        token int32 | required
    }
    count int32 = 10 | min = 10, max = 20, required
}
`
	const driver = `import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import probe.*;

public final class Main {
    private static void check(boolean cond, String msg) {
        if (!cond) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        // 1. stella-48d4ab331f58: enum-keyed array with | required and all default slots still writes header on wire
        KeyedReqVariable.Value kv = new KeyedReqVariable.Value();
        byte[] kWire = KeyedReqVariable.save(kv);
        check(kWire != null && kWire.length > 3, "KeyedReq.save writes required field at default");
        TableVariable.Report rep = new TableVariable.Report();
        KeyedReqVariable.Value kLoaded = KeyedReqVariable.load(kWire, rep);
        check(kLoaded != null && !rep.refused && rep.missingRequired == 0, "KeyedReq.load accepts saved default required keyed array");

        TableRetain ret = new TableRetain(1 << 16, 1 << 10);
        rep.reset();
        KeyedReqVariable.Value krLoaded = KeyedReqRetain.loadRetain(kWire, ret, rep);
        check(krLoaded != null && !rep.refused && rep.missingRequired == 0, "KeyedReqRetain.loadRetain");
        byte[] krWire = KeyedReqRetain.saveRetain(krLoaded, ret, rep);
        check(Arrays.equals(kWire, krWire), "KeyedReqRetain.saveRetain writes required keyed array at default");

        // Empty wire body (01 00 00) refuses KeyedReq with missing_required: values
        rep.reset();
        KeyedReqVariable.Value kEmpty = KeyedReqVariable.load(new byte[]{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}, rep);
        check(kEmpty == null && rep.refused && rep.missingRequired == 1 && "missing_required: values".equals(rep.reason), "empty wire refuses KeyedReq: " + rep.reason);

        // 2. Guarded required field and range-constrained required field with no default
        GuardedReqVariable.Value gv = new GuardedReqVariable.Value();
        gv.count = 0;
        // count is 0 (outside 10..20), so save must refuse!
        check(GuardedReqVariable.save(gv) == null, "GuardedReq.save refuses out-of-range required count=0");
        gv.count = 15;
        byte[] gWireInactive = GuardedReqVariable.save(gv);
        check(gWireInactive != null, "GuardedReq.save succeeds when count=15 and active=false");
        rep.reset();
        GuardedReqVariable.Value gLoadedInactive = GuardedReqVariable.load(gWireInactive, rep);
        check(gLoadedInactive != null && !rep.refused && rep.missingRequired == 0 && gLoadedInactive.count == 15, "GuardedReq.load inactive");

        // JSON when active=false and token omitted succeeds; when active=true and token omitted refuses
        rep.reset();
        GuardedReqVariable.Value gj1 = GuardedReqVariable.fromJson("{\"active\":false,\"count\":15}".getBytes(StandardCharsets.UTF_8), rep);
        check(gj1 != null && !rep.refused && rep.missingRequired == 0, "JSON inactive without token succeeds");

        rep.reset();
        GuardedReqVariable.Value gj2 = GuardedReqVariable.fromJson("{\"active\":true,\"count\":15}".getBytes(StandardCharsets.UTF_8), rep);
        check(gj2 == null && rep.refused && rep.missingRequired == 1 && "missing_required: token".equals(rep.reason), "JSON active without token refuses: " + rep.reason);

        rep.reset();
        GuardedReqVariable.Value gj3 = GuardedReqVariable.fromJson("{\"active\":true}".getBytes(StandardCharsets.UTF_8), rep);
        check(gj3 == null && rep.refused && rep.missingRequired == 1 && "missing_required: token, count".equals(rep.reason), "JSON active without token or count refuses: " + rep.reason);

        // 3. stella-507f9ad0de25: required by-value nested table at default emits 0-field nested table header
        RequiredEmptyVariable.Value rev = new RequiredEmptyVariable.Value();
        byte[] reWire = RequiredEmptyVariable.save(rev);
        check(reWire != null && reWire.length == 22 && reWire[0] == 1 && reWire[1] == 1 && reWire[2] == 13 && reWire[3] == 1 && reWire[4] == 0 && reWire[5] == 0, "stella-507f9ad0de25 save");
        rep.reset();
        RequiredEmptyVariable.Value reLoaded = RequiredEmptyVariable.load(reWire, rep);
        check(reLoaded != null && !rep.refused && rep.missingRequired == 0, "stella-507f9ad0de25 load");
        rep.reset();
        byte[] reRetWire = RequiredEmptyRetain.saveRetain(rev, new TableRetain(1 << 16, 1 << 10), rep);
        check(Arrays.equals(reWire, reRetWire), "stella-507f9ad0de25 saveRetain");

        System.out.println("REQUIRED OK");
    }
}
`
	if got := runJavaCarryProbe(t, schema, "RequiredDriver", driver); got != "REQUIRED OK" {
		t.Fatalf("unexpected output: %s", got)
	}
}
