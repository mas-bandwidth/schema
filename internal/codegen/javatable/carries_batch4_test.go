package javatable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJavaCarriesTextList(t *testing.T) {
	t.Skip("layer 4 (java): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "compiles and runs generated Java text-list codecs")
	t.Parallel()

	const schema = `package probe

table Lists
{
    rules [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
    bases []?string(8)
}
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)

	inst := m.New(m.Lookup("Lists"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"rules":["a","b"],"first":["x",null],"repos":["r1",""],"bases":["yo",null,""]}`), &rep) || !rep.Silent() {
		t.Fatalf("oracle read: %+v", rep)
	}
	wireValid, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle encode: %v", err)
	}

	rawElemClamp := func() []byte {
		x := m.New(m.Lookup("Lists"))
		fv := &x.Fields[0]
		fv.Count = 1
		fv.Elems[0].Tab.Fields[0].Cell.Str = []byte("123456789")
		fv.Elems[0].Tab.Fields[0].Count = 9
		w, err := tablewire.Encode(m, x)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}()
	backClamp := m.New(m.Lookup("Lists"))
	var clampRep tabletext.Report
	if ok, err := tablewire.Decode(m, backClamp, rawElemClamp, &clampRep); !ok || err != nil || clampRep.Unknown != 1 {
		t.Fatalf("oracle decode clamped: ok=%v err=%v rep=%+v", ok, err, clampRep)
	}
	wantElemClampWire, err := tablewire.Encode(m, backClamp)
	if err != nil {
		t.Fatal(err)
	}

	rawBadUtf8 := func() []byte {
		x := m.New(m.Lookup("Lists"))
		fv := &x.Fields[0]
		fv.Count = 1
		fv.Elems[0].Tab.Fields[0].Cell.Str = []byte("\xff\xfe")
		fv.Elems[0].Tab.Fields[0].Count = 2
		w, err := tablewire.Encode(m, x)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}()

	driver := `import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import probe.*;

public final class Main {
    private static byte[] unhex(String s) {
        int n = s.length() / 2;
        byte[] out = new byte[n];
        for (int i = 0; i < n; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    public static void main(String[] args) throws Exception {
        byte[] wireValid = unhex("` + hex.EncodeToString(wireValid) + `");
        byte[] rawElemClamp = unhex("` + hex.EncodeToString(rawElemClamp) + `");
        byte[] wantElemClampWire = unhex("` + hex.EncodeToString(wantElemClampWire) + `");
        byte[] rawBadUtf8 = unhex("` + hex.EncodeToString(rawBadUtf8) + `");

        // 1. Round-trip variable wire
        TableVariable.Report rep = new TableVariable.Report();
        ListsVariable.Value v = ListsVariable.load(wireValid, rep);
        if (v == null || rep.malformed || rep.refused || rep.clamped != 0) {
            throw new AssertionError("valid load failed: " + rep.reason);
        }
        if (ListsVariable.loadMeasure(wireValid, new TableVariable.Measure()) < 0) {
            throw new AssertionError("loadMeasure failed");
        }
        byte[] saved = ListsVariable.save(v);
        if (!Arrays.equals(saved, wireValid)) {
            throw new AssertionError("saved wire mismatch");
        }

        // 2. Retaining wire
        TableRetain retained = new TableRetain(1 << 20, 1 << 17);
        ListsVariable.Value vr = ListsRetain.loadRetain(wireValid, retained, new TableVariable.Report());
        if (vr == null) {
            throw new AssertionError("loadRetain failed");
        }
        long mr = ListsRetain.measureRetain(vr, retained);
        byte[] sr = ListsRetain.saveRetain(vr, retained, new TableVariable.Report());
        if (sr == null || sr.length != mr || !Arrays.equals(sr, wireValid)) {
            throw new AssertionError("saveRetain mismatch");
        }

        // 3. JSON round-trip
        byte[] js = ListsVariable.toJson(v);
        if (js == null) {
            throw new AssertionError("toJson failed");
        }
        TableVariable.Report jrep = new TableVariable.Report();
        ListsVariable.Value vj = ListsVariable.fromJson(js, jrep);
        if (vj == null || jrep.malformed || jrep.clamped != 0) {
            throw new AssertionError("fromJson failed");
        }
        if (!Arrays.equals(ListsVariable.save(vj), wireValid)) {
            throw new AssertionError("fromJson -> save mismatch");
        }

        // 4. Cook round-trip & row accessors
        long cookLen = ListsCook.cookMeasure(v);
        if (cookLen <= 0) {
            throw new AssertionError("cookMeasure failed: " + cookLen);
        }
        byte[] cooked = new byte[(int) cookLen];
        if (!ListsCook.cookFrom(v, cooked, TableCookWrite.LITTLE)) {
            throw new AssertionError("cookFrom failed");
        }
        ListsCook c = ListsCook.open(cooked);
        if (c == null) {
            throw new AssertionError("ListsCook.open failed");
        }
        int root = c.root();
        if (ListsRow.rulesCount(cooked, root) != 2) {
            throw new AssertionError("rulesCount mismatch");
        }
        int r0 = ListsRow.rulesAt(root, 0);
        String r0Val = new String(cooked, ListsRulesTextRow.valueAt(r0), ListsRulesTextRow.valueLength(cooked, r0), StandardCharsets.UTF_8);
        if (!"a".equals(r0Val)) {
            throw new AssertionError("rules[0] mismatch: " + r0Val);
        }
        if (ListsRow.firstCount(cooked, root) != 2) {
            throw new AssertionError("firstCount mismatch");
        }
        int f0 = ListsRow.firstAt(root, 0);
        int f1 = ListsRow.firstAt(root, 1);
        String f0Val = new String(cooked, ListsFirstTextRow.valueAt(f0), ListsFirstTextRow.valueLength(cooked, f0), StandardCharsets.UTF_8);
        if (!ListsFirstTextRow.valuePresent(cooked, f0) || !"x".equals(f0Val)) {
            throw new AssertionError("first[0] mismatch");
        }
        if (ListsFirstTextRow.valuePresent(cooked, f1)) {
            throw new AssertionError("first[1] should be absent");
        }

        // 5. Element clamp on wire
        TableVariable.Report crep = new TableVariable.Report();
        ListsVariable.Value vc = ListsVariable.load(rawElemClamp, crep);
        if (vc == null || crep.malformed || crep.clamped != 1 || !"clamped: Lists.rules".equals(crep.reason)) {
            throw new AssertionError("elem clamp failed: clamped=" + crep.clamped + " reason=" + crep.reason);
        }
        if (!Arrays.equals(ListsVariable.save(vc), wantElemClampWire)) {
            throw new AssertionError("clamped save mismatch");
        }

        // 6. Malformed UTF-8 on wire
        TableVariable.Report mrep = new TableVariable.Report();
        ListsVariable.Value vm = ListsVariable.load(rawBadUtf8, mrep);
        if (vm != null || !mrep.malformed) {
            throw new AssertionError("expected malformed UTF-8 refusal");
        }

        // 7. Writer preflight refusal on element past M and count past N
        ListsVariable.Value badElem = new ListsVariable.Value();
        ListsRulesTextVariable.Value eTooLong = new ListsRulesTextVariable.Value();
        eTooLong.value = "123456789";
        badElem.rules.add(eTooLong);
        if (ListsVariable.save(badElem) != null || ListsVariable.toJson(badElem) != null) {
            throw new AssertionError("writer accepted element past M");
        }
        ListsVariable.Value badCount = new ListsVariable.Value();
        for (int i = 0; i < 5; i++) {
            ListsRulesTextVariable.Value item = new ListsRulesTextVariable.Value();
            item.value = "a";
            badCount.rules.add(item);
        }
        if (ListsVariable.save(badCount) != null || ListsVariable.toJson(badCount) != null) {
            throw new AssertionError("writer accepted count past N");
        }
        System.out.println("TEXT_LIST_OK");
    }
}
`
	_, out := compileAndRunJavaProbe(t, schema, "probe", driver)
	if !strings.Contains(out, "TEXT_LIST_OK") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestJavaCarriesTextListTotal(t *testing.T) {
	slowtest.Gate(t, "compiles and runs generated Java text-list-total codecs")
	t.Parallel()

	const totalsSchema = `package probe
table Totals {
 a [..3]string(5) | total=5
 b [..3]?string(5) | total=5
 c []string(5) | total=5
 d []?string(5) | total=5
}
`
	const inputJSON = `{"a":["ééé","a","z","tail"],"b":[null,"ééé","abc","tail"],"c":["ééé","a","z"],"d":[null,"ééé","abc","x"]}`
	const expectedJSON = `{"a":["éé","a"],"b":[null,"éé"],"c":["éé","a"],"d":[null,"éé"]}`

	const oldSchema = `package probe
table Totals {
    a [..8]string(16)
    b [..8]?string(16)
    c []string(16)
    d []?string(16)
}
`
	oldU := unitFrom(t, oldSchema)
	oldM := tabletext.NewModel(oldU)
	oldInst := oldM.New(oldM.Lookup("Totals"))
	var orep tabletext.Report
	if !oldM.Read(oldInst, []byte(inputJSON), &orep) || !orep.Silent() {
		t.Fatalf("old model read: %+v", orep)
	}
	wireTotals, err := tablewire.Encode(oldM, oldInst)
	if err != nil {
		t.Fatal(err)
	}

	buildNamedTailWire := func(tableName, opt string) ([]byte, []byte) {
		oldTailU := unitFrom(t, "package probe\ntable "+tableName+" {\n    names [..4]"+opt+"string(32)\n}\n")
		oldTailM := tabletext.NewModel(oldTailU)
		inst := oldTailM.New(oldTailM.Lookup(tableName))
		var r tabletext.Report
		if !oldTailM.Read(inst, []byte(`{"names":["abcde","xyz","toolong"]}`), &r) || !r.Silent() {
			t.Fatalf("tail read: %+v", r)
		}
		w, err := tablewire.Encode(oldTailM, inst)
		if err != nil {
			t.Fatal(err)
		}
		damaged := append([]byte(nil), w...)
		at := bytes.Index(damaged, []byte("toolong"))
		if at < 0 {
			t.Fatal("missing toolong in wire")
		}
		damaged[at] = 0
		return w, damaged
	}
	plainTailWire, plainTailDamaged := buildNamedTailWire("TailPlain", "")
	optTailWire, optTailDamaged := buildNamedTailWire("TailOpt", "?")

	combinedSchema := totalsSchema + "\ntable TailPlain {\n    names [..4]string(5) | total=5\n}\ntable TailOpt {\n    names [..4]?string(5) | total=5\n}\n"

	driver := `import java.nio.charset.StandardCharsets;
import probe.*;

public final class Main {
    private static byte[] unhex(String s) {
        int n = s.length() / 2;
        byte[] out = new byte[n];
        for (int i = 0; i < n; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    public static void main(String[] args) throws Exception {
        byte[] inputJson = unhex("` + hex.EncodeToString([]byte(inputJSON)) + `");
        byte[] wireTotals = unhex("` + hex.EncodeToString(wireTotals) + `");
        byte[] plainTailWire = unhex("` + hex.EncodeToString(plainTailWire) + `");
        byte[] plainTailDamaged = unhex("` + hex.EncodeToString(plainTailDamaged) + `");
        byte[] optTailWire = unhex("` + hex.EncodeToString(optTailWire) + `");
        byte[] optTailDamaged = unhex("` + hex.EncodeToString(optTailDamaged) + `");

        // 1. JSON witness
        TableVariable.Report jrep = new TableVariable.Report();
        TotalsVariable.Value vj = TotalsVariable.fromJson(inputJson, jrep);
        if (vj == null || jrep.malformed || jrep.clamped != 10 || !"clamped: Totals.a".equals(jrep.reason)) {
            throw new AssertionError("JSON witness mismatch: clamped=" + jrep.clamped + " reason=" + jrep.reason + " malformed=" + jrep.malformed);
        }
        byte[] outJson = TotalsVariable.toJson(vj);
        if (outJson == null) {
            throw new AssertionError("toJson on clamped Totals failed");
        }

        // 2. Wire witness
        TableVariable.Report wrep = new TableVariable.Report();
        TotalsVariable.Value vw = TotalsVariable.load(wireTotals, wrep);
        if (vw == null || wrep.malformed || wrep.clamped != 10 || !"clamped: Totals.a".equals(wrep.reason)) {
            throw new AssertionError("Wire witness mismatch: clamped=" + wrep.clamped + " reason=" + wrep.reason + " malformed=" + wrep.malformed);
        }
        byte[] outWireJson = TotalsVariable.toJson(vw);
        if (outWireJson == null || !new String(outJson, StandardCharsets.UTF_8).equals(new String(outWireJson, StandardCharsets.UTF_8))) {
            throw new AssertionError("Wire vs JSON output mismatch");
        }

        // 3. Skipped tail is not decoded (JSON and wire, plain and optional)
        for (String tail : new String[]{"\"toolong\"", "\"\\u0000\""}) {
            byte[] js = ("{\"names\":[\"abcde\",\"xyz\"," + tail + "]}").getBytes(StandardCharsets.UTF_8);
            TableVariable.Report r1 = new TableVariable.Report();
            TailPlainVariable.Value tp = TailPlainVariable.fromJson(js, r1);
            if (tp == null || r1.malformed || r1.clamped != 1 || tp.names.size() != 1) {
                throw new AssertionError("TailPlain JSON tail " + tail + ": clamped=" + r1.clamped + " malformed=" + r1.malformed);
            }
            TableVariable.Report r2 = new TableVariable.Report();
            TailOptVariable.Value to = TailOptVariable.fromJson(js, r2);
            if (to == null || r2.malformed || r2.clamped != 1 || to.names.size() != 1) {
                throw new AssertionError("TailOpt JSON tail " + tail + ": clamped=" + r2.clamped + " malformed=" + r2.malformed);
            }
        }
        for (byte[] w : new byte[][]{plainTailWire, plainTailDamaged}) {
            TableVariable.Report r = new TableVariable.Report();
            TailPlainVariable.Value tp = TailPlainVariable.load(w, r);
            if (tp == null || r.malformed || r.clamped != 1 || tp.names.size() != 1) {
                throw new AssertionError("TailPlain wire: clamped=" + r.clamped + " malformed=" + r.malformed);
            }
        }
        for (byte[] w : new byte[][]{optTailWire, optTailDamaged}) {
            TableVariable.Report r = new TableVariable.Report();
            TailOptVariable.Value to = TailOptVariable.load(w, r);
            if (to == null || r.malformed || r.clamped != 1 || to.names.size() != 1) {
                throw new AssertionError("TailOpt wire: clamped=" + r.clamped + " malformed=" + r.malformed);
            }
        }

        // 4. Writer preflight refusal on total overflow
        TotalsVariable.Value badA = new TotalsVariable.Value();
        for (int i = 0; i < 2; i++) {
            TotalsATextVariable.Value e = new TotalsATextVariable.Value();
            e.value = "abc";
            badA.a.add(e);
        }
        if (TotalsVariable.save(badA) != null || TotalsVariable.toJson(badA) != null) {
            throw new AssertionError("writer accepted total overflow on a");
        }
        TotalsVariable.Value badB = new TotalsVariable.Value();
        for (int i = 0; i < 2; i++) {
            TotalsBTextVariable.Value e = new TotalsBTextVariable.Value();
            e.value = "abc";
            e.valuePresent = true;
            badB.b.add(e);
        }
        if (TotalsVariable.save(badB) != null || TotalsVariable.toJson(badB) != null) {
            throw new AssertionError("writer accepted total overflow on b");
        }

        System.out.print(new String(outJson, StandardCharsets.UTF_8));
    }
}
`
	_, out := compileAndRunJavaProbe(t, combinedSchema, "probe", driver)
	var gotCompact bytes.Buffer
	if err := json.Compact(&gotCompact, []byte(out)); err != nil {
		t.Fatalf("json.Compact: %v\n%s", err, out)
	}
	if gotCompact.String() != expectedJSON {
		t.Fatalf("got JSON %s, want %s", gotCompact.String(), expectedJSON)
	}
}

func TestJavaCarriesUnboundedText(t *testing.T) {
	slowtest.Gate(t, "compiles and runs generated Java unbounded-text codecs")
	t.Parallel()

	const schema = `package probe

table Where
{
    rules []string
    first [..4]?string
    capped [..2]string
    optList []?string
    byname map[string]int32
}
`
	driver := `import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import probe.*;

public final class Main {
    private static String readUnbounded(byte[] cooked, int at, int len) {
        return new String(cooked, at, len, StandardCharsets.UTF_8);
    }

    public static void main(String[] args) throws Exception {
        String longBase = "x".repeat(70000);
        String longRule = longBase + "_rule";
        String longFirst = longBase + "_first";
        String longCapped = longBase + "_capped";
        String longOpt = longBase + "_opt";
        String keyA = longBase + "_A";
        String keyB = longBase + "_B";

        WhereVariable.Value v = new WhereVariable.Value();
        WhereRulesTextVariable.Value r0 = new WhereRulesTextVariable.Value();
        r0.value = longRule;
        v.rules.add(r0);

        WhereFirstTextVariable.Value f0 = new WhereFirstTextVariable.Value();
        f0.valuePresent = false;
        v.first.add(f0);
        WhereFirstTextVariable.Value f1 = new WhereFirstTextVariable.Value();
        f1.value = longFirst;
        f1.valuePresent = true;
        v.first.add(f1);

        WhereCappedTextVariable.Value c0 = new WhereCappedTextVariable.Value();
        c0.value = longCapped;
        v.capped.add(c0);

        WhereOptListTextVariable.Value o0 = new WhereOptListTextVariable.Value();
        o0.value = longOpt;
        o0.valuePresent = true;
        v.optList.add(o0);

        WhereBynameEntryVariable.Value m1 = new WhereBynameEntryVariable.Value();
        m1.key = keyB;
        m1.value = 20;
        v.byname.add(m1);
        WhereBynameEntryVariable.Value m0 = new WhereBynameEntryVariable.Value();
        m0.key = keyA;
        m0.value = 10;
        v.byname.add(m0);

        // 1. Variable wire round-trip
        byte[] wire = WhereVariable.save(v);
        if (wire == null) {
            throw new AssertionError("save failed");
        }
        TableVariable.Measure mrep = new TableVariable.Measure();
        long measured = WhereVariable.loadMeasure(wire, mrep);
        if (measured <= 0) {
            throw new AssertionError("loadMeasure failed: " + measured);
        }
        TableVariable.Report lrep = new TableVariable.Report();
        WhereVariable.Value loaded = WhereVariable.load(wire, lrep);
        if (loaded == null || lrep.malformed || lrep.refused || lrep.clamped != 0) {
            throw new AssertionError("load failed: clamped=" + lrep.clamped);
        }
        if (!longRule.equals(loaded.rules.get(0).value)) {
            throw new AssertionError("loaded rules[0] mismatch");
        }
        if (loaded.first.get(0).valuePresent || !loaded.first.get(1).valuePresent || !longFirst.equals(loaded.first.get(1).value)) {
            throw new AssertionError("loaded first mismatch");
        }
        if (!longCapped.equals(loaded.capped.get(0).value)) {
            throw new AssertionError("loaded capped[0] mismatch");
        }
        if (!loaded.optList.get(0).valuePresent || !longOpt.equals(loaded.optList.get(0).value)) {
            throw new AssertionError("loaded optList[0] mismatch");
        }
        if (loaded.byname.size() != 2 || !keyA.equals(loaded.byname.get(0).key) || loaded.byname.get(0).value != 10 || !keyB.equals(loaded.byname.get(1).key) || loaded.byname.get(1).value != 20) {
            throw new AssertionError("loaded byname mismatch");
        }

        // 2. JSON round-trip
        byte[] js = WhereVariable.toJson(loaded);
        if (js == null) {
            throw new AssertionError("toJson failed");
        }
        TableVariable.Report jrep = new TableVariable.Report();
        WhereVariable.Value fromJs = WhereVariable.fromJson(js, jrep);
        if (fromJs == null || jrep.malformed || jrep.clamped != 0) {
            throw new AssertionError("fromJson failed: clamped=" + jrep.clamped);
        }
        if (!Arrays.equals(WhereVariable.save(fromJs), wire)) {
            throw new AssertionError("fromJson -> save mismatch");
        }

        // 3. Cook round-trip and Row accessors + map Find
        long cookLen = WhereCook.cookMeasure(loaded);
        if (cookLen <= 0) {
            throw new AssertionError("cookMeasure failed: " + cookLen);
        }
        byte[] cooked = new byte[(int) cookLen];
        if (!WhereCook.cookFrom(loaded, cooked, TableCookWrite.LITTLE)) {
            throw new AssertionError("cookFrom failed");
        }
        WhereCook c = WhereCook.open(cooked);
        if (c == null) {
            throw new AssertionError("WhereCook.open failed");
        }
        int root = c.root();
        int rulesCount = WhereRow.rulesCount(cooked, root);
        int rulesArr = c.at(WhereRow.rulesSlot(root), rulesCount * WhereRow.rulesStride);
        if (rulesCount != 1 || rulesArr < 0) {
            throw new AssertionError("cooked rules count/arr mismatch");
        }
        int r0Off = rulesArr;
        String r0Cooked = readUnbounded(cooked, WhereRulesTextRow.valueAt(cooked, r0Off), WhereRulesTextRow.valueLength(cooked, r0Off));
        if (!longRule.equals(r0Cooked)) {
            throw new AssertionError("cooked rules[0] mismatch");
        }
        if (WhereFirstTextRow.valuePresent(cooked, WhereRow.firstAt(root, 0))) {
            throw new AssertionError("cooked first[0] should be absent");
        }
        int f1Off = WhereRow.firstAt(root, 1);
        String f1Cooked = readUnbounded(cooked, WhereFirstTextRow.valueAt(cooked, f1Off), WhereFirstTextRow.valueLength(cooked, f1Off));
        if (!WhereFirstTextRow.valuePresent(cooked, f1Off) || !longFirst.equals(f1Cooked)) {
            throw new AssertionError("cooked first[1] mismatch");
        }
        int cap0Off = WhereRow.cappedAt(root, 0);
        String cap0Cooked = readUnbounded(cooked, WhereCappedTextRow.valueAt(cooked, cap0Off), WhereCappedTextRow.valueLength(cooked, cap0Off));
        if (!longCapped.equals(cap0Cooked)) {
            throw new AssertionError("cooked capped[0] mismatch");
        }
        int bynameCount = WhereRow.bynameCount(cooked, root);
        int bynameEntries = c.at(WhereRow.bynameSlot(root), bynameCount * WhereRow.bynameStride);
        int foundA = WhereRow.bynameFind(cooked, bynameEntries, bynameCount, keyA.getBytes(StandardCharsets.UTF_8));
        int foundB = WhereRow.bynameFind(cooked, bynameEntries, bynameCount, keyB.getBytes(StandardCharsets.UTF_8));
        int foundMissing = WhereRow.bynameFind(cooked, bynameEntries, bynameCount, (longBase + "_C").getBytes(StandardCharsets.UTF_8));
        if (foundA < 0 || WhereBynameEntryRow.value(cooked, foundA) != 10) {
            throw new AssertionError("cooked bynameFind(keyA) failed: " + foundA);
        }
        if (foundB < 0 || WhereBynameEntryRow.value(cooked, foundB) != 20) {
            throw new AssertionError("cooked bynameFind(keyB) failed: " + foundB);
        }
        if (foundMissing != -1) {
            throw new AssertionError("cooked bynameFind(missing) should be -1");
        }
        System.out.println("UNBOUNDED_TEXT_OK");
    }
}
`
	_, out := compileAndRunJavaProbe(t, schema, "probe", driver)
	if !strings.Contains(out, "UNBOUNDED_TEXT_OK") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestJavaCarriesRequiredNestedGuards(t *testing.T) {
	slowtest.Gate(t, "compiles and runs generated Java nested guard required checks")
	t.Parallel()

	const schema = `package probe

table Guarded
{
    a bool
    if a
    {
        b bool
        if b
        {
            req int32 | required
        }
    }
}
`
	driver := `import java.nio.charset.StandardCharsets;
import probe.*;

public final class Main {
    public static void main(String[] args) throws Exception {
        // 1. a=true, b=false: req is inactive -> allowed on JSON and wire!
        TableVariable.Report r1 = new TableVariable.Report();
        GuardedVariable.Value v1 = GuardedVariable.fromJson("{\"a\":true,\"b\":false}".getBytes(StandardCharsets.UTF_8), r1);
        if (v1 == null || r1.refused || r1.malformed || r1.missingRequired != 0) {
            throw new AssertionError("a=true, b=false should not require req on JSON: " + r1.reason);
        }
        byte[] w1 = GuardedVariable.save(v1);
        if (w1 == null) {
            throw new AssertionError("save(a=true, b=false) failed");
        }
        TableVariable.Report rw1 = new TableVariable.Report();
        GuardedVariable.Value vw1 = GuardedVariable.load(w1, rw1);
        if (vw1 == null || rw1.refused || rw1.malformed || rw1.missingRequired != 0) {
            throw new AssertionError("a=true, b=false should not require req on wire: " + rw1.reason);
        }

        // 2. a=true, b=true, req missing: refused on JSON!
        TableVariable.Report r2 = new TableVariable.Report();
        GuardedVariable.Value v2 = GuardedVariable.fromJson("{\"a\":true,\"b\":true}".getBytes(StandardCharsets.UTF_8), r2);
        if (v2 != null || !r2.refused || r2.missingRequired != 1 || !"missing_required: req".equals(r2.reason)) {
            throw new AssertionError("a=true, b=true without req should be refused on JSON: " + r2.reason);
        }

        // 3. a=true, b=true, req=0 present: allowed on JSON and wire!
        TableVariable.Report r3 = new TableVariable.Report();
        GuardedVariable.Value v3 = GuardedVariable.fromJson("{\"a\":true,\"b\":true,\"req\":0}".getBytes(StandardCharsets.UTF_8), r3);
        if (v3 == null || r3.refused || r3.malformed || r3.missingRequired != 0) {
            throw new AssertionError("a=true, b=true, req=0 should succeed on JSON: " + r3.reason);
        }
        byte[] w3 = GuardedVariable.save(v3);
        if (w3 == null) {
            throw new AssertionError("save(a=true, b=true, req=0) failed");
        }
        TableVariable.Report rw3 = new TableVariable.Report();
        GuardedVariable.Value vw3 = GuardedVariable.load(w3, rw3);
        if (vw3 == null || rw3.refused || rw3.malformed || rw3.missingRequired != 0) {
            throw new AssertionError("a=true, b=true, req=0 should succeed on wire: " + rw3.reason);
        }
        System.out.println("NESTED_GUARD_REQ_OK");
    }
}
`
	_, out := compileAndRunJavaProbe(t, schema, "probe", driver)
	if !strings.Contains(out, "NESTED_GUARD_REQ_OK") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestJavaPopulatedFeatureWitnesses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")

	const schema = `package featurewitnesses

table Leaf { value int32 }
table Part { child *Leaf }

table Root | forms="wire,json,cook", integrity="crc32c" {
    revision int32 | required
    at timestamp
    lower int32 = 5 | min=5
    tags ?map[string(8)]int32
    optional ?Part
    labels [..4]string(8) | total=8
    optional_labels []?string(8)
    words []string
    by_name map[string]int32
}

table ArrayRoot | json="array" {
    values []int32
}
`
	const rootJSON = "{\n  \"revision\": 7,\n  \"at\": \"2026-11-02T12:34:56.123456789Z\",\n  \"lower\": 9,\n  \"tags\": {\n    \"a\": 3,\n    \"z\": 11\n  },\n  \"optional\": {\n    \"child\": {\n      \"value\": 17\n    }\n  },\n  \"labels\": [\n    \"é\",\n    \"schema\"\n  ],\n  \"optional_labels\": [\n    null,\n    \"\",\n    \"active\"\n  ],\n  \"words\": [\n    \"unbounded text is populated\"\n  ],\n  \"by_name\": {\n    \"a key longer than eight bytes\": 23\n  }\n}\n"
	const rootHex = "010104070000000224152d7260853ae418030409000000040e1a0d020b050c0161060403000000000b050c017a06040b00000000070d0408110200090e0c0c0202c3a906736368656d610a0e140d03010004060c00000a060c06616374697665000b0e1e0c011b756e626f756e646564207465787420697320706f70756c617465640c0e2a0d0127050c1d61206b6579206c6f6e676572207468616e206569676874206279746573060417000000000d0c0a010e07060411000000000058868e6cc4dba4d668a945b5074e9c08da3b912eb7099e8c80856f07ef3b1cd9ec105b36194ac93dea0ce83094fde47c39d505b3c67695629d480025763424628a0a9856f813c54be17728730a6983348a9123735e24754f2a1eaf98c4318ee8ffffffffffffffffb5d173bbb485c0a50e00000000000000ab108e4d"
	const arrayRootJSON = "[\n  3,\n  17,\n  -5\n]\n"
	const arrayRootHex = "01010e0e04030300000011000000fbffffff00fb674d1acf7b27210100000000000000"

	driver := fmt.Sprintf(`import java.nio.charset.StandardCharsets;
import featurewitnesses.*;

public final class Main {
    private static void check(boolean ok, String msg) {
        if (!ok) throw new RuntimeException("FAIL: " + msg);
    }
    private static String toHex(byte[] b) {
        if (b == null) return "null";
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte x : b) sb.append(String.format("%%02x", x & 0xff));
        return sb.toString();
    }
    private static byte[] fromHex(String s) {
        byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(i * 2, i * 2 + 2), 16);
        }
        return out;
    }

    public static void main(String[] args) {
        // Root
        String rootInput = %q;
        byte[] rootExpected = fromHex(%q);
        TableVariable.Report r = new TableVariable.Report();
        RootVariable.Value v = RootVariable.fromJson(rootInput.getBytes(StandardCharsets.UTF_8), r);
        check(v != null && !r.malformed && !r.refused, "Root fromJson");
        byte[] saved = RootVariable.save(v);
        check(toHex(saved).equals(toHex(rootExpected)), "Root save: got=" + toHex(saved) + " want=" + toHex(rootExpected));
        String js = new String(RootVariable.toJson(v), StandardCharsets.UTF_8);
        check(js.equals(rootInput), "Root toJson: got=" + js);

        TableRetain store = new TableRetain(65536, 8192);
        TableVariable.Report rr = new TableVariable.Report();
        RootVariable.Value back = RootRetain.loadRetain(rootExpected, store, rr);
        check(back != null && !rr.malformed && !rr.refused, "Root loadRetain");
        TableVariable.Report sr = new TableVariable.Report();
        byte[] savedRetain = RootRetain.saveRetain(back, store, sr);
        check(toHex(savedRetain).equals(toHex(rootExpected)), "Root saveRetain");

        long cookSize = RootCook.cookMeasure(v);
        check(cookSize > 24, "Root cookMeasure: got=" + cookSize);
        TableVariable.Measure mr = new TableVariable.Measure();
        long measured = RootVariable.loadMeasure(rootExpected, mr);
        check(measured == cookSize - 64, "Root loadMeasure: got=" + measured + " want=" + (cookSize - 64));

        byte[] cookLE = new byte[(int) cookSize];
        check(RootCook.cookFrom(v, cookLE, TableCookWrite.LITTLE), "Root cookFrom LE");
        RootCook rc = RootCook.open(cookLE);
        check(rc != null, "RootCook.open: " + RootCook.openReason(cookLE, 0, cookLE.length));
        int rootAt = rc.root();
        check(RootRow.revision(cookLE, rootAt) == 7, "RootCook revision");
        check(RootRow.wordsCount(cookLE, rootAt) == 1, "RootCook wordsCount");
        int wordsArr = rc.at(RootRow.wordsSlot(rootAt), RootRow.wordsCount(cookLE, rootAt) * RootRow.wordsStride);
        String w0 = new String(cookLE, RootWordsTextRow.valueAt(cookLE, wordsArr), RootWordsTextRow.valueLength(cookLE, wordsArr), StandardCharsets.UTF_8);
        check("unbounded text is populated".equals(w0), "RootCook words[0]: " + w0);
        int byNameCount = RootRow.byNameCount(cookLE, rootAt); int byNameArr = rc.at(RootRow.byNameSlot(rootAt), byNameCount * RootRow.byNameStride); check(RootRow.byNameFind(cookLE, byNameArr, byNameCount, "a key longer than eight bytes".getBytes(StandardCharsets.UTF_8)) >= 0, "RootCook byNameFind");

        byte[] cookBE = new byte[(int) cookSize];
        check(RootCook.cookFrom(v, cookBE, TableCookWrite.BIG), "Root cookFrom BE");

        // ArrayRoot
        String arrayInput = %q;
        byte[] arrayExpected = fromHex(%q);
        TableVariable.Report ar = new TableVariable.Report();
        ArrayRootVariable.Value av = ArrayRootVariable.fromJson(arrayInput.getBytes(StandardCharsets.UTF_8), ar);
        check(av != null && !ar.malformed && !ar.refused, "ArrayRoot fromJson");
        byte[] asaved = ArrayRootVariable.save(av);
        check(toHex(asaved).equals(toHex(arrayExpected)), "ArrayRoot save");
        String ajs = new String(ArrayRootVariable.toJson(av), StandardCharsets.UTF_8);
        check(ajs.equals(arrayInput), "ArrayRoot toJson");

        TableRetain astore = new TableRetain(65536, 8192);
        TableVariable.Report arr = new TableVariable.Report();
        ArrayRootVariable.Value aback = ArrayRootRetain.loadRetain(arrayExpected, astore, arr);
        check(aback != null && !arr.malformed && !arr.refused, "ArrayRoot loadRetain");
        TableVariable.Report asr = new TableVariable.Report();
        byte[] asavedRetain = ArrayRootRetain.saveRetain(aback, astore, asr);
        check(toHex(asavedRetain).equals(toHex(arrayExpected)), "ArrayRoot saveRetain");

        long acookSize = ArrayRootCook.cookMeasure(av);
        TableVariable.Measure amr = new TableVariable.Measure();
        long ameasured = ArrayRootVariable.loadMeasure(arrayExpected, amr);
        check(ameasured == acookSize - 64, "ArrayRoot loadMeasure: got=" + ameasured + " want=" + (acookSize - 64));

        byte[] acookLE = new byte[(int) acookSize];
        check(ArrayRootCook.cookFrom(av, acookLE, TableCookWrite.LITTLE), "ArrayRoot cookFrom LE");
        ArrayRootCook arc = ArrayRootCook.open(acookLE);
        check(arc != null, "ArrayRootCook.open");
    }
}
`, rootJSON, rootHex, arrayRootJSON, arrayRootHex)

	compileAndRunJavaProbe(t, schema, "featurewitnesses", driver)
}
