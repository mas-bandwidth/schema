package rusttable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const rustCountRefusalSchema = `package countcheck
table Leaf { entries map[int32]int32
    values []int32 }
union Choice { leaf Leaf }
union Wrapped { choice Choice }
table Holder { inner Leaf
    maybe ?Leaf
    leaves []Leaf
    choice Choice
    choices []Choice
    head *Leaf }
table MapHolder { entries map[int32]Leaf }
table Wrapper { value Wrapped }
`

// The small vectors follow the file grammar; counts never allocate elements.
// The engine independently verifies the distinction between storage refusal
// and body-short damage before the generated reader sees the same bytes.
func rustCountWire(t *testing.T, u *ir.Unit, shape string, count uint64, short bool) (string, []byte) {
	t.Helper()
	var ids []uint64
	ref := func(id uint64) []byte {
		for i, v := range ids {
			if v == id {
				return binary.AppendUvarint(nil, uint64(i+1))
			}
		}
		ids = append(ids, id)
		return binary.AppendUvarint(nil, uint64(len(ids)))
	}
	length := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	field := func(id uint64, kind byte, payload []byte) []byte {
		return append(append(ref(id), kind), payload...)
	}
	named := func(name string, kind byte, payload []byte) []byte {
		return field(ir.TableWireId(name), kind, payload)
	}
	collection := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
	if count != 0 && !short {
		collection = append(collection, 1, 0) // one physically present empty entry
	}
	leaf := append(named("entries", byte(ir.TableKindArray), length(collection)), 0)
	arm := func(union string, variant int, kind byte, body []byte) []byte {
		name := "leaf"
		if union == "Wrapped" {
			name = "choice"
		}
		return append(append(ref(ir.TableWireId(name)), kind), length(body)...)
	}
	root := "Holder"
	var body []byte
	switch shape {
	case "map":
		root, body = "Leaf", leaf[:len(leaf)-1]
	case "list":
		list := binary.AppendUvarint([]byte{byte(ir.TableKindI32)}, count)
		root, body = "Leaf", named("values", byte(ir.TableKindArray), length(list))
	case "widened", "framing-tail":
		list := binary.AppendUvarint([]byte{byte(ir.TableKindI16)}, count)
		list = append(list, 0, 0)
		leaf = append(named("values", byte(ir.TableKindArray), length(list)), 0)
		body = named("inner", byte(ir.TableKindTable), length(leaf))
		if shape == "framing-tail" {
			// The declared tail body exceeds the actual root, after the refusal.
			body = append(body, named("damaged_tail", byte(ir.TableKindTable), []byte{127})...)
		}
	case "nested", "optional":
		name := "inner"
		if shape == "optional" {
			name = "maybe"
		}
		body = named(name, byte(ir.TableKindTable), length(leaf))
	case "array":
		payload := append([]byte{byte(ir.TableKindTable), 1}, length(leaf)...)
		body = named("leaves", byte(ir.TableKindArray), length(payload))
	case "union":
		body = named("choice", byte(ir.TableKindUnion), arm("Choice", 0, byte(ir.TableKindTable), leaf))
	case "union-array":
		payload := append([]byte{byte(ir.TableKindUnion), 1}, arm("Choice", 0, byte(ir.TableKindTable), leaf)...)
		body = named("choices", byte(ir.TableKindArray), length(payload))
	case "nested-union":
		root = "Wrapper"
		body = named("value", byte(ir.TableKindUnion), arm("Wrapped", 0, byte(ir.TableKindUnion), arm("Choice", 0, byte(ir.TableKindTable), leaf)))
	case "map-value", "map-descending", "map-equal", "map-ascending":
		root = "MapHolder"
		entryType := u.Tables[root].Fields[0].MapEntry
		entry := func(key byte, value []byte, keyLast bool) []byte {
			k := field(ir.MapKeyWireId, byte(ir.TableKindI32), []byte{key, 0, 0, 0})
			v := field(ir.TableFieldWireId(entryType.Fields[1]), byte(ir.TableKindTable), length(value))
			if keyLast {
				return append(append(v, k...), 0)
			}
			return append(append(k, v...), 0)
		}
		payload := append([]byte{byte(ir.TableKindTable), 1}, length(entry(1, leaf, false))...)
		if shape != "map-value" {
			key := byte(3)
			if shape == "map-equal" {
				key = 5
			}
			if shape == "map-ascending" {
				key = 7
			}
			payload = append([]byte{byte(ir.TableKindTable), 2}, length(entry(5, []byte{0}, false))...)
			payload = append(payload, length(entry(key, leaf, true))...)
		}
		body = named("entries", byte(ir.TableKindArray), length(payload))
	case "node", "unreachable-node":
		if shape == "node" {
			body = named("head", byte(ir.TableKindPointer), []byte{2})
		}
		nodes := append([]byte{1}, ref(ir.TableWireId(u.Tables["Leaf"].WireName()))...)
		nodes = append(nodes, length(leaf)...)
		body = append(body, field(math.MaxUint64, byte(ir.TableKindString), length(nodes))...)
	default:
		t.Fatalf("unknown shape %s", shape)
	}
	// An earlier unknown field exercises preservation of report/retention work.
	unknown := field(ir.TableWireId("unrecognized"), byte(ir.TableKindI32), []byte{17, 0, 0, 0})
	body = append(unknown, body...)
	body = append(body, 0)
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return root, binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func TestCountRefusalDiscardsTheWholeLoadRust(t *testing.T) {
	slowtest.Gate(t, "rustc (the generated count-refusal and retention probe)")
	u := unitFrom(t, rustCountRefusalSchema)
	model := tabletext.NewModel(u)
	var body strings.Builder
	body.WriteString(`
    let mut failures = 0;
    macro_rules! check {
        ($ok:expr, $name:expr) => { if !$ok { eprintln!("FAIL: {}", $name); failures += 1; } };
    }
`)
	shapes := []string{"map", "list", "nested", "optional", "array", "union", "union-array", "nested-union", "map-value", "map-equal", "map-ascending", "node", "unreachable-node"}
	for _, shape := range shapes {
		for _, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
			root, wire := rustCountWire(t, u, shape, count, false)
			r := tabletext.Report{Unknown: 7}
			ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &r)
			var refusal *tablewire.CountRefusal
			if ok || !errors.As(err, &refusal) || r.Malformed || r.Unknown < 7 {
				t.Fatalf("%s/%d: oracle did not preserve storage refusal: %v %v %+v", shape, count, ok, err, r)
			}
			load := ir.RustSnake(root)
			fmt.Fprintf(&body, `
    {
        let wire: &[u8] = &[%s];
        for old_damage in [false, true] {
            for retaining in [false, true] {
                let mut report = TableReport { unknown: 7, kind_mismatch: 11, widened: 13, clamped: 17,
                    duplicate: 19, retained: 23, retain_lost: 29, malformed: old_damage,
                    cycle_on_save: true, ..Default::default() };
                let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: vec![123] };
                store.bytes.push(99);
                let result = if retaining { %s_load_retain(wire, &mut store, &mut report) } else { %s_load(wire, &mut report) };
                check!(result.is_none(), "%s/%d partial value");
                check!(report.refused && report.reason == TableReason::CountOverExtentCap, "%s/%d named count refusal");
                check!(report.malformed == old_damage && report.cycle_on_save, "%s/%d damage history");
                check!(report.unknown == %d && report.kind_mismatch == 11 && report.widened == 13 &&
                    report.clamped == 17 && report.duplicate == 19 && report.retained >= 23 &&
                    report.retain_lost >= 29, "%s/%d counters");
                check!(!retaining || (store.bytes.is_empty() && store.ids.is_empty()), "%s/%d retained partial state");
            }
        }
    }
`, rustBytes(wire), load, load, shape, count, shape, count, shape, count, r.Unknown, shape, count, shape, count)
		}
		root, wire := rustCountWire(t, u, shape, 0, false)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &rep); !ok || err != nil || rep.Malformed {
			t.Fatalf("%s: valid twin rejected by oracle: %v %v %+v", shape, ok, err, rep)
		}
		load := ir.RustSnake(root)
		fmt.Fprintf(&body, `
    {
        let wire: &[u8] = &[%s];
        for retaining in [false, true] {
            let mut report = TableReport { refused: true, reason: TableReason::CountOverExtentCap,
                malformed: true, cycle_on_save: true, ..Default::default() };
            let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::new() };
            let result = if retaining { %s_load_retain(wire, &mut store, &mut report) } else { %s_load(wire, &mut report) };
            check!(result.is_some(), "%s prior refusal is not current control state");
            check!(report.refused && report.reason == TableReason::CountOverExtentCap &&
                report.malformed && report.cycle_on_save, "%s history preserved after good read");
        }
    }
`, rustBytes(wire), load, load, shape, shape)
	}
	root, descending := rustCountWire(t, u, "map-descending", 1<<31, false)
	var descendingReport tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), descending, &descendingReport); ok || err != nil || !descendingReport.Malformed {
		t.Fatalf("descending-map control is not damage: %v %v %+v", ok, err, descendingReport)
	}
	fmt.Fprintf(&body, `
    for retaining in [false, true] {
        let mut report = TableReport::default();
        let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::new() };
        let wire: &[u8] = &[%s];
        let result = if retaining { map_holder_load_retain(wire, &mut store, &mut report) } else { map_holder_load(wire, &mut report) };
        check!(result.is_none() && report.reason != TableReason::CountOverExtentCap, "descending key precedes nested value refusal");
        check!(!retaining || (store.bytes.is_empty() && store.ids.is_empty()), "descending map retained state");
    }
`, rustBytes(descending))
	_, short := rustCountWire(t, u, "map", 1, true)
	var rep tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup("Leaf")), short, &rep); ok || err != nil || !rep.Malformed {
		t.Fatalf("body-short control is not damage: %v %v %+v", ok, err, rep)
	}
	fmt.Fprintf(&body, `
    for retaining in [false, true] {
        let mut report = TableReport::default();
        let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::new() };
        let wire: &[u8] = &[%s];
        let result = if retaining { leaf_load_retain(wire, &mut store, &mut report) } else { leaf_load(wire, &mut report) };
        check!(result.is_none() && report.reason != TableReason::CountOverExtentCap, "body-short remains damage");
        check!(!retaining || (store.bytes.is_empty() && store.ids.is_empty()), "body-short retained state");
    }
    for (form, reason) in [(2, TableReason::MessageFormAsFile), (255, TableReason::NewerForm)] {
        let mut report = TableReport { malformed: true, unknown: 7, ..Default::default() };
        check!(leaf_load(&[form], &mut report).is_none() && report.refused && report.reason == reason, "form refusal name");
        check!(report.malformed && report.unknown == 7, "form preserves history");
    }
    if failures != 0 { std::process::exit(1); }
`, rustBytes(short))
	out, err := runRustDiscardProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("count-refusal probe: %v\n%s", err, out)
	}
}

// A later measure failure cannot erase the diagnostic prefix before refusal.
func TestCountRefusalKeepsEarnedPrefixRust(t *testing.T) {
	slowtest.Gate(t, "rustc (the generated count-refusal diagnostic-prefix probe)")
	u := unitFrom(t, rustCountRefusalSchema)
	model := tabletext.NewModel(u)
	var body strings.Builder
	for _, shape := range []string{"widened", "framing-tail"} {
		root, wire := rustCountWire(t, u, shape, 1<<31, false)
		report := tabletext.Report{Unknown: 7, Widened: 13}
		ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &report)
		var refusal *tablewire.CountRefusal
		if ok || !errors.As(err, &refusal) || report.Malformed || report.Unknown != 8 || report.Widened != 14 {
			t.Fatalf("%s: unexpected oracle prefix: %v %v %+v", shape, ok, err, report)
		}
		retainingReport := tabletext.Report{Unknown: 7, Widened: 13, Retained: 23, RetainLost: 29}
		retained := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
		ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup(root)), wire, &retained, &retainingReport)
		if ok || !errors.As(err, &refusal) || retainingReport.Malformed || retainingReport.Unknown != 8 || retainingReport.Widened != 14 {
			t.Fatalf("%s: unexpected retaining oracle prefix: %v %v %+v", shape, ok, err, retainingReport)
		}
		fmt.Fprintf(&body, `
    {
        let wire: &[u8] = &[%s];
        for retaining in [false, true] {
            let mut report = TableReport { unknown: 7, widened: 13, retained: 23, retain_lost: 29, ..Default::default() };
            let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::new() };
            let result = if retaining { holder_load_retain(wire, &mut store, &mut report) } else { holder_load(wire, &mut report) };
            assert!(result.is_none() && report.refused && report.reason == TableReason::CountOverExtentCap, "%s reason");
            assert!(!report.malformed && report.unknown == %d && report.widened == %d,
                "%s prefix retaining={}: unknown={} widened={} malformed={}", retaining, report.unknown, report.widened, report.malformed);
            assert!(report.retained == if retaining { %d } else { 23 } &&
                report.retain_lost == if retaining { %d } else { 29 },
                "%s retention counters: retained={} lost={}", report.retained, report.retain_lost);
            assert!(store.bytes.is_empty() && store.ids.is_empty(), "%s partial retained state");
        }
`, rustBytes(wire), shape, report.Unknown, report.Widened, shape, retainingReport.Retained, retainingReport.RetainLost, shape, shape)
		if shape == "framing-tail" {
			body.WriteString("        assert!(holder_load_measure(wire) < 0, \"tail must take scratch refusal path\");\n")
		}
		body.WriteString("    }\n")
	}
	out, err := runRustDiscardProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("count diagnostic-prefix probe: %v\n%s", err, out)
	}
}
