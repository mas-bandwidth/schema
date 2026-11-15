package goosetable_test

// A DECLARED FIXED TABLE PAST THE RECORD CEILING HAS NO FILE FORM
// (docs/SPEC-TABLES.md §1, STATUS; §3.4, SELECTION): its file save refuses,
// and its read refuses form 1. The build emits no <T>FixedLoad for it, so the
// reads it does have — the plain load (schema_rt::load_table), the strict
// read's one reason (load_error) and the retain load (load_table_retain) —
// refuse a form-1 file as previous_form, decoding nothing and moving no
// counter, the verdict the Go engine (internal/tablewire) gives the same
// bytes. The controls: an admitted fixed root still reads its form-3 file
// through <T>FixedLoad and its form-1 file through load_table as before, and a
// form-1 save of the oversized root refuses.

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// ceilingSchema is tables/examples' WideBlob as declared there, past the
// 65536-byte record ceiling, beside an admitted fixed root.
const ceilingSchema = `package probe

const MaxBlobBytes = 70000

fixed table WideBlob
{
    label   string(70000)
    payload bytes(MaxBlobBytes)
    samples [..70000]uint16
}

fixed table Small
{
    a    int32
    flag bool
}
`

const ceilingProbeMain = `
fn counters(r: const schema_rt::LoadReport&) -> i64 {
    var n = r.unknown + r.mismatch + r.widened + r.oor + r.repeated + r.blob_shared + r.retained + r.retain_lost;
    if r.body_damaged || r.damaged || r.non_finite || r.stopped || r.count_refused { n += 1; }
    return n;
}

fn yes(b: bool) -> i64 { if b { return 1; } return 0; }

fn loads(tag: const u8[:], root: const u8[:]) {
    let data = input(str(tag, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(conformance_unit(), root, data, vals, nodes, report);
    let reason = schema_rt::load_refusal_name(report.reason);
    print(tag, "_load ", yes(at >= 0), " ", yes(report.refused), " ", vals.len + nodes.len, " ", counters(report), " ", if reason.len == 0 { "-" } else { reason });
    let strict = schema_rt::load_refusal_name(schema_rt::load_error(report, at));
    print(tag, "_strict ", if strict.len == 0 { "ok" } else { strict });
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: bytes, recs: recs, ids: ids };
    var rvals: schema_rt::LoadVal[>..] = [];
    var rnodes: schema_rt::LoadNode[>..] = [];
    var rreport = schema_rt::LoadReport { .. };
    let rat = schema_rt::load_table_retain(conformance_unit(), root, data, rvals, rnodes, rreport, store);
    let rreason = schema_rt::load_refusal_name(rreport.reason);
    print(tag, "_retain ", yes(rat >= 0), " ", yes(rreport.refused), " ", rvals.len + rnodes.len + store.used, " ", counters(rreport), " ", if rreason.len == 0 { "-" } else { rreason });
}

fn main() {
    loads("wide1", "WideBlob");
    loads("small1", "Small");
    // THE ADMITTED ROOT'S FORM-3 FILE, through the read it does have
    var small: probe::Small[1] = [probe::NewSmall()];
    var frep = schema_rt::FixedReport { .. };
    let small3 = input("small3.bin");
    let fn_n = probe::SmallFixedLoad(small, small3, frep);
    print("small3_fixed ", fn_n, " ", frep.reason, " ", small[0].A, " ", yes(small[0].Flag));
    // THE FILE SAVE OF THE OVERSIZED ROOT refuses, and leaves no file
    var v = probe::NewWideBlob();
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    let rec = probe::WideBlobToLoaded(v, conformance_unit(), data, vals);
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..<] = [];
    let refusal = if rec < 0 { -1 } else { schema_rt::save_table(conformance_unit(), "WideBlob", data, vals, nodes, rec, out) };
    print("wide_save ", yes(rec >= 0), " ", yes(refusal != 0), " ", out.len);
}
`

func TestGooseFixedRootOverCeilingRefusesFormOne(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it builds and runs a probe over the table runtime)")

	u := probeUnit(t, ceilingSchema)
	m := tabletext.NewModel(u)
	value := func(root, text string) *tabletext.Instance {
		t.Helper()
		inst := m.New(m.Lookup(root))
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) || r.Refused {
			t.Fatalf("%s: the engine does not read %s: %+v", root, text, r)
		}
		return inst
	}
	wide := value("WideBlob", `{"label":"wide","samples":[1,2,3]}`)
	small := value("Small", `{"a":-7,"flag":true}`)

	wide1, err := tablewire.EncodeVariable(m, wide)
	if err != nil || len(wide1) == 0 || wide1[0] != 1 {
		t.Fatalf("the engine's variable encoder: %x, %v", wide1, err)
	}
	small1, err := tablewire.EncodeVariable(m, small)
	if err != nil || len(small1) == 0 || small1[0] != 1 {
		t.Fatalf("the engine's variable encoder at Small: %x, %v", small1, err)
	}
	small3, err := tablewire.EncodeFixed(m, small)
	if err != nil || len(small3) == 0 || small3[0] != 3 {
		t.Fatalf("the engine's fixed encoder at Small: %x, %v", small3, err)
	}

	// THE ENGINE'S VERDICT on the same bytes: the plain read and the retain
	// read refuse the oversized root's form-1 file, nothing decoded, and its
	// file save refuses.
	for name, read := range map[string]func(*tabletext.Instance, *tabletext.Report) (bool, error){
		"decode": func(inst *tabletext.Instance, r *tabletext.Report) (bool, error) {
			return tablewire.Decode(m, inst, wide1, r)
		},
		"retain": func(inst *tabletext.Instance, r *tabletext.Report) (bool, error) {
			return tablewire.DecodeRetain(m, inst, wide1, &tablewire.Retain{Capacity: 1 << 20}, r)
		},
	} {
		inst := m.New(m.Lookup("WideBlob"))
		var r tabletext.Report
		ok, err := read(inst, &r)
		var fixed *tablewire.FixedRefusal
		if ok || !errors.As(err, &fixed) || fixed.Reason != tablewire.FixedReasonPreviousForm {
			t.Fatalf("the engine's %s of WideBlob's form-1 file: ok=%v err=%v, want previous_form", name, ok, err)
		}
	}
	if data, err := tablewire.Encode(m, wide); err == nil || len(data) != 0 {
		t.Fatalf("the engine's file save of WideBlob: %x, %v; want a refusal", data, err)
	}
	// and the admitted root's two forms read
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Small")), small3, &tabletext.Report{}); !ok || err != nil {
		t.Fatalf("the engine's read of Small's form-3 file: %v, %v", ok, err)
	}
	if ok, err := tablewire.DecodeVariable(m, m.New(m.Lookup("Small")), small1, &tabletext.Report{}); !ok || err != nil {
		t.Fatalf("the engine's variable read of Small's form-1 file: %v, %v", ok, err)
	}

	files := probeFiles(t, u, nil)
	var main strings.Builder
	main.WriteString(probePrelude)
	mods := make([]string, 0, len(files))
	for name := range files {
		if mod, ok := strings.CutSuffix(name, ".goose"); ok && mod != "conformance_unit" {
			mods = append(mods, mod)
		}
	}
	sort.Strings(mods)
	for _, mod := range mods {
		if !slices.Contains([]string{"wire_load", "wire_retain", "wire_save"}, mod) {
			fmt.Fprintf(&main, "import .%s;\n", mod)
		}
	}
	main.WriteString(ceilingProbeMain)
	files["main.goose"] = []byte(main.String())
	files["wide1.bin"] = wide1
	files["small1.bin"] = small1
	files["small3.bin"] = small3
	got := probeLines(gooserun.Run(t, t.TempDir(), "main.goose", files))

	// each read entry at the oversized root: refused, no root, nothing
	// decoded, no counter moved, previous_form by Goose's own name
	want := map[string]string{
		"wide1_load":   "0 1 0 0 previous_form",
		"wide1_strict": "previous_form",
		"wide1_retain": "0 1 0 0 previous_form",
		// the admitted root's form-1 file reads through load_table as before
		"small1_load":   "1 0 3 0 -",
		"small1_strict": "ok",
		// the form-3 file through the read the admitted root has
		"small3_fixed": "1 0 -7 1",
		// the oversized root's file save refuses and leaves no file
		"wide_save": "1 1 0",
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if g := strings.Join(got[name], " "); g != want[name] {
			t.Errorf("%s: Goose answered %q, want %q", name, g, want[name])
		}
	}
	// the retain load at the admitted value-only root is refused as it was
	// (a fixed-class root carries no retention, §6.6), and is shown only
	t.Logf("small1_retain: %s", strings.Join(got["small1_retain"], " "))
}
