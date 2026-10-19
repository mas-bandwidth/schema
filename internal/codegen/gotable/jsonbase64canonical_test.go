package gotable

import (
	"bytes"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestGoJSONReadRefusesNonCanonicalBase64 pins docs/SPEC-TABLES.md §16.2 and
// §16.8: a bytes(N) body in the JSON form is CANONICAL base64. The strict read
// refuses a body that is base64 but not the canonical encoding of its bytes by
// name base64_not_canonical; the tolerant read accepts it and reports it; a
// body outside the alphabet keeps kind_mismatch; and the writer emits canonical
// base64 only, so a canonical input reads clean and round-trips. Red at base:
// readBase64 skipped `=` anywhere and ignored pad bits, so each non-canonical
// body read silently. The canonical rule runs over the string's DECODED text
// (§16.2: JSON escapes first), so an escaped canonical body reads clean and an
// escaped non-canonical one is base64_not_canonical, in both Go readers.
func TestGoJSONReadRefusesNonCanonicalBase64(t *testing.T) {
	t.Parallel()
	engine := tabletext.NewModel(goUnit(t, "package tabledemo\ntable E\n{\n    data bytes(8)\n    blob *bytes\n}\n"))
	for _, c := range []struct {
		name, text string
		want       []byte
		canonical  bool
	}{
		{"escaped_solidus_canonical", `{"data":"\/w==","blob":"\/w=="}`, []byte{0xff}, true},
		{"unicode_escape_canonical", `{"data":"\u0041Q==","blob":"\u0041Q=="}`, []byte{0x01}, true},
		{"escaped_nonzero_pad_bits", `{"data":"\/x==","blob":"\/x=="}`, []byte{0xff}, false},
		{"escaped_newline_inside", `{"data":"AQ\n==","blob":"AQ\n=="}`, []byte{0x01}, false},
	} {
		inst := engine.New(engine.Lookup("E"))
		var rep tabletext.Report
		if !engine.Read(inst, []byte(c.text), &rep) || rep.Malformed || rep.KindMismatch != 0 {
			t.Fatalf("%s: engine refused or mismatched: %+v", c.name, rep)
		}
		if got := rep.Reason == "base64_not_canonical: data"; got == c.canonical {
			t.Fatalf("%s: engine Reason %q, canonical=%v", c.name, rep.Reason, c.canonical)
		}
		if fv, _ := inst.FieldByKey("data"); !bytes.Equal(fv.Cell.Str, c.want) {
			t.Fatalf("%s: engine data = %x, want %x", c.name, fv.Cell.Str, c.want)
		}
		if fv, _ := inst.FieldByKey("blob"); fv.Cell.Blob == nil || !bytes.Equal(fv.Cell.Blob.Data, c.want) {
			t.Fatalf("%s: engine blob = %+v, want %x", c.name, fv.Cell.Blob, c.want)
		}
	}
	fixedSchema, err := os.ReadFile("../../../tables/examples/Tables.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(fixedSchema), `package tabledemo
import("encoding/base64";"errors";"runtime";"strings";"testing")

type b64case struct{ name, body string; notBase64 bool }

func cases(n int) []b64case {
	good := base64.StdEncoding.EncodeToString(make([]byte, n))
	pad := len(good)
	for i := 0; i < len(good); i++ { if good[i] == '=' { pad = i; break } }
	last := pad - 1
	return []b64case{
		{"nonzero_pad_bits", good[:last] + "B" + good[last+1:], false},
		{"equals_in_middle", good[:4] + "=" + good[4:], false},
		{"embedded_whitespace", good[:4] + " " + good[4:], false},
		{"missing_equals", good[:len(good)-1], false},
		{"extra_equals", good + "=", false},
		{"overbound_extra_equals", base64.StdEncoding.EncodeToString(make([]byte, n+1))[:len(good)] + "=", false},
		// The shared table-base64 harness vectors that this rule moves from
		// kind_mismatch to base64_not_canonical (test/table-base64/harness).
		{"harness_Q=Q==", "Q=Q==", false},
		{"harness_Q=Q=A=A=", "Q=Q=A=A=", false},
		{"harness_QQ==Q", "QQ==Q", false},
		{"harness_invalid-20", "QQ AA", false},
		{"not_base64", "!!!!", true},
	}
}

func TestFixedJSONBase64Escapes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, body string; want byte; canonical bool }{
		{"escaped_solidus_canonical", "\\/w==", 0xff, true},
		{"unicode_escape_canonical", "\\u0041Q==", 0x01, true},
		{"escaped_nonzero_pad_bits", "\\/x==", 0xff, false},
		{"escaped_newline_inside", "AQ\\n==", 0x01, false},
	} {
		text := []byte("{\"icon\":\"" + c.body + "\"}")
		var v ProfileConfig
		var strict TableReport
		ok := ProfileConfigFromJson(&v, text, &strict)
		if c.canonical {
			if !ok || strict.Err() != nil || v.IconLength != 1 || v.Icon[0] != c.want {
				t.Fatalf("%s: strict ok=%v report=%+v icon=%x", c.name, ok, strict, v.Icon[:v.IconLength])
			}
			continue
		}
		if ok || !errors.Is(strict.Err(), TableRefuseBase64NotCanonical) {
			t.Fatalf("%s: strict ok=%v err=%v", c.name, ok, strict.Err())
		}
		var tv ProfileConfig
		var tolerant TableReport
		if !ProfileConfigFromJsonTolerant(&tv, text, &tolerant) || !errors.Is(tolerant.Err(), TableRefuseBase64NotCanonical) || tv.IconLength != 1 || tv.Icon[0] != c.want {
			t.Fatalf("%s: tolerant report=%+v icon=%x", c.name, tolerant, tv.Icon[:tv.IconLength])
		}
	}
}

// Not parallel: the heap counters are process-wide, so the measurement runs
// in the sequential phase, before the parallel tests resume.
func TestFixedJSONBase64LargeBound(t *testing.T) {
	body := strings.Repeat("A", 4<<20)
	text := []byte("{\"icon\":\"" + body + "\"}")
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var v ProfileConfig
	var report TableReport
	if !ProfileConfigFromJsonTolerant(&v, text, &report) { t.Fatalf("large bounded input refused: %+v", report) }
	runtime.ReadMemStats(&after)
	if report.Clamped != 1 { t.Fatalf("large bounded input clamped %d times, want 1", report.Clamped) }
	if allocated := after.TotalAlloc-before.TotalAlloc; allocated > 512<<10 {
		t.Fatalf("bytes(16) read allocated %d bytes for a prebuilt 4 MiB body; want <= 512 KiB", allocated)
	}
}

func TestFixedJSONBase64(t *testing.T) {
	t.Parallel()
	good := base64.StdEncoding.EncodeToString(make([]byte, 16))
	var v ProfileConfig
	var r TableReport
	if !ProfileConfigFromJson(&v, []byte("{\"icon\":\""+good+"\"}"), &r) || r.Err() != nil {
		t.Fatalf("canonical: %+v", r)
	}
	buf := make([]byte, ProfileConfigToJsonMeasure(&v))
	n := ProfileConfigToJson(&v, buf)
	if n < 0 { t.Fatal("tojson canonical") }
	first := string(buf[:n])
	var v2 ProfileConfig
	var r2 TableReport
	if !ProfileConfigFromJson(&v2, []byte(first), &r2) || r2.Err() != nil { t.Fatalf("re-read canonical: %+v", r2) }
	buf2 := make([]byte, ProfileConfigToJsonMeasure(&v2))
	n2 := ProfileConfigToJson(&v2, buf2)
	if n2 < 0 || string(buf2[:n2]) != first { t.Fatalf("round trip: %q != %q", string(buf2[:n2]), first) }
	for _, c := range cases(16) {
		text := []byte("{\"icon\":\"" + c.body + "\"}")
		var v ProfileConfig
		var strict TableReport
		if ProfileConfigFromJson(&v, text, &strict) {
			t.Fatalf("%s: strict accepted %q", c.name, c.body)
		}
		err := strict.Err()
		if c.notBase64 {
			if !errors.Is(err, TableRefuseKindMismatch) { t.Fatalf("%s: strict err=%v", c.name, err) }
		} else if !errors.Is(err, TableRefuseBase64NotCanonical) || err.Error() != "base64_not_canonical" {
			t.Fatalf("%s: strict err=%v", c.name, err)
		}
		var tv ProfileConfig
		var tolerant TableReport
		if !ProfileConfigFromJsonTolerant(&tv, text, &tolerant) {
			t.Fatalf("%s: tolerant refused", c.name)
		}
		if c.notBase64 {
			if tolerant.KindMismatch != 1 { t.Fatalf("%s: tolerant report %+v", c.name, tolerant) }
		} else if !errors.Is(tolerant.Err(), TableRefuseBase64NotCanonical) || tolerant.Err().Error() != "base64_not_canonical" {
			t.Fatalf("%s: tolerant report %+v err=%v", c.name, tolerant, tolerant.Err())
		}
	}
}
`)
	runGenerated(t, `package tabledemo
table V
{
    data bytes(8)
    blob *bytes
}
`, `package tabledemo
import("encoding/base64";"errors";"testing")

type b64case struct{ name, body string; notBase64 bool }

func cases(n int) []b64case {
	good := base64.StdEncoding.EncodeToString(make([]byte, n))
	pad := len(good)
	for i := 0; i < len(good); i++ { if good[i] == '=' { pad = i; break } }
	last := pad - 1
	return []b64case{
		{"nonzero_pad_bits", good[:last] + "B" + good[last+1:], false},
		{"equals_in_middle", good[:4] + "=" + good[4:], false},
		{"embedded_whitespace", good[:4] + " " + good[4:], false},
		{"missing_equals", good[:len(good)-1], false},
		{"extra_equals", good + "=", false},
		{"overbound_extra_equals", base64.StdEncoding.EncodeToString(make([]byte, n+1))[:len(good)] + "=", false},
		// The shared table-base64 harness vectors that this rule moves from
		// kind_mismatch to base64_not_canonical (test/table-base64/harness).
		{"harness_Q=Q==", "Q=Q==", false},
		{"harness_Q=Q=A=A=", "Q=Q=A=A=", false},
		{"harness_QQ==Q", "QQ==Q", false},
		{"harness_invalid-20", "QQ AA", false},
		{"not_base64", "!!!!", true},
	}
}

// A *bytes body takes the bytes(N) canonical rule, and its Reason names the
// field the way tabletext's does (docs/SPEC-TABLES.md §16.2), so a strict
// refusal and a tolerant report both say which field was not canonical.
func TestVariableJSONBlobReasonNamesTheField(t *testing.T) {
	t.Parallel()
	text := []byte("{\"blob\":\"\\/x==\"}")
	for _, tolerant := range []bool{false, true} {
		var b VBuilder
		if !b.Init() { t.Fatal("init") }
		var r TableReport
		var ok bool
		if tolerant { ok = VFromJsonTolerant(&b, text, &r) } else { ok = VFromJson(&b, text, &r) }
		b.Shutdown()
		if ok != tolerant || !errors.Is(r.Err(), TableRefuseBase64NotCanonical) || r.Reason != "base64_not_canonical: blob" {
			t.Fatalf("tolerant=%v: ok=%v err=%v Reason=%q, want Reason %q", tolerant, ok, r.Err(), r.Reason, "base64_not_canonical: blob")
		}
	}
}

func TestVariableJSONBase64(t *testing.T) {
	t.Parallel()
	good := base64.StdEncoding.EncodeToString(make([]byte, 8))
	var b VBuilder
	if !b.Init() { t.Fatal("init") }
	defer b.Shutdown()
	var r TableReport
	if !VFromJson(&b, []byte("{\"data\":\""+good+"\"}"), &r) || r.Err() != nil {
		t.Fatalf("canonical: %+v", r)
	}
	buf := make([]byte, VToJsonMeasure(b.GetRoot()))
	n := VToJson(b.GetRoot(), buf)
	if n < 0 { t.Fatal("tojson canonical") }
	first := string(buf[:n])
	var b2 VBuilder
	if !b2.Init() { t.Fatal("init2") }
	defer b2.Shutdown()
	var r2 TableReport
	if !VFromJson(&b2, []byte(first), &r2) || r2.Err() != nil { t.Fatalf("re-read canonical: %+v", r2) }
	buf2 := make([]byte, VToJsonMeasure(b2.GetRoot()))
	n2 := VToJson(b2.GetRoot(), buf2)
	if n2 < 0 || string(buf2[:n2]) != first { t.Fatalf("round trip: %q != %q", string(buf2[:n2]), first) }
	for _, c := range cases(8) {
		text := []byte("{\"data\":\"" + c.body + "\"}")
		var strict VBuilder
		if !strict.Init() { t.Fatal("init") }
		var sr TableReport
		if VFromJson(&strict, text, &sr) {
			strict.Shutdown()
			t.Fatalf("%s: strict accepted %q", c.name, c.body)
		}
		err := sr.Err()
		if c.notBase64 {
			if !errors.Is(err, TableRefuseKindMismatch) { strict.Shutdown(); t.Fatalf("%s: strict err=%v", c.name, err) }
		} else if !errors.Is(err, TableRefuseBase64NotCanonical) || err.Error() != "base64_not_canonical" {
			strict.Shutdown()
			t.Fatalf("%s: strict err=%v", c.name, err)
		}
		strict.Shutdown()
		var tolerant VBuilder
		if !tolerant.Init() { t.Fatal("init") }
		var tr TableReport
		if !VFromJsonTolerant(&tolerant, text, &tr) {
			tolerant.Shutdown()
			t.Fatalf("%s: tolerant refused", c.name)
		}
		if c.notBase64 {
			if tr.KindMismatch != 1 { tolerant.Shutdown(); t.Fatalf("%s: tolerant report %+v", c.name, tr) }
		} else if !errors.Is(tr.Err(), TableRefuseBase64NotCanonical) || tr.Err().Error() != "base64_not_canonical" {
			tolerant.Shutdown()
			t.Fatalf("%s: tolerant report %+v err=%v", c.name, tr, tr.Err())
		}
		tolerant.Shutdown()
	}
}
`)
}
