package goosetable

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4, §13.10, §22.6;
// docs/rulings.json fixed-dead-bytes-are-zero). The reference's dead-byte
// corpus stains one dead region at a time: an absent optional's payload, a
// counted array's slots past its count, a string(N)'s or bytes(N)'s bytes past
// its used length, and an inactive union arm. The generated load refuses the
// whole load for each, before a record lands, and the control — the same
// record with every dead byte zero — reads and writes back the reference's
// bytes. The verdict is the contract; the refusal's name is this leg's own.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose (it runs the generated reader over the fixed-form corpus)")
	corpus := fixedCorpus(t)
	u := fixedUnit(t, "test/tables/DeadBytes.schema")
	ns := goose.Namespace(u)
	for _, vec := range fixedvector.DeadBytes {
		vec := vec
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			data := gooseCorpusFile(t, corpus, vec.File)
			got := deadBytesProbe(t, u, ns, data)
			if !vec.Reads {
				if got.n != -1 {
					t.Fatalf("%s: the generated load answered n=%d; a dead byte that is not zero refuses the whole load", vec.File, got.n)
				}
				if got.after != got.before {
					t.Fatalf("%s: a refusal wrote the caller's destination\n was %s\n now %s", vec.File, got.before, got.after)
				}
				return
			}
			if got.n != 1 {
				t.Fatalf("%s: the control answered n=%d; it must read its one record", vec.File, got.n)
			}
			if want := hex.EncodeToString(data); got.written != len(data) || got.saved != want {
				t.Fatalf("%s: the control's write differs from the reference's image\n got %d %s\nwant %d %s",
					vec.File, got.written, got.saved, len(data), want)
			}
		})
	}
}

// deadBytesProbeResult is one generated program's answer for one corpus file.
type deadBytesProbeResult struct {
	n       int
	reason  string
	before  string
	after   string
	written int
	saved   string
}

// deadBytesProbe loads data through DeadRootFixedLoad into a destination whose
// record image is printed before and after, then writes a reading load back
// through DeadRootFixedSave.
func deadBytesProbe(t *testing.T, u *ir.Unit, ns string, data []byte) deadBytesProbeResult {
	t.Helper()
	main := fmt.Sprintf(`fn image(v: const %[1]s::DeadRoot&) {
    var img: u8[>..] = [];
    %[1]s::_DeadRootFixedWrite(img, v);
    hex(img);
}

fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { print("read-failed"); return; }
    var values: %[1]s::DeadRoot[1] = [%[1]s::DeadRoot { .. }; 1];
    values[0].ArrCount = 1;
    values[0].Arr[0] = 12345;
    values[0].TextLength = 1;
    values[0].Text[0] = 65;
    values[0].Pick.Type = 1;
    values[0].Pick.Narrow = 7;
    print("before");
    image(values[0]);
    var rep = schema_rt::FixedReport { .. };
    let n = %[1]s::DeadRootFixedLoad(values, data, rep);
    print("report ", n, " ", schema_rt::fixed_refusal_name(rep.reason));
    print("after");
    image(values[0]);
    if n >= 0 {
        var out: u8[>..] = [];
        let w = %[1]s::DeadRootFixedSave(values[0..n], out);
        print("saved ", w);
        hex(out);
    }
}
`, ns)
	out := runCustomFixedGoose(t, u, nil, main, map[string][]byte{"in.bin": data})
	return deadBytesParse(t, out)
}

func deadBytesParse(t *testing.T, out string) deadBytesProbeResult {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	next := func(i int) string {
		if i+1 >= len(lines) {
			t.Fatalf("the probe ended after %q:\n%s", lines[i], out)
		}
		return lines[i+1]
	}
	var r deadBytesProbeResult
	sawReport := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "report "):
			f := strings.Fields(l)
			if len(f) != 3 {
				t.Fatalf("an unreadable report line %q", l)
			}
			r.n, _ = strconv.Atoi(f[1])
			r.reason = f[2]
			sawReport = true
		case l == "before":
			r.before = next(i)
			i++
		case l == "after":
			r.after = next(i)
			i++
		case strings.HasPrefix(l, "saved "):
			f := strings.Fields(l)
			if len(f) != 2 {
				t.Fatalf("an unreadable saved line %q", l)
			}
			r.written, _ = strconv.Atoi(f[1])
			r.saved = next(i)
			i++
		}
	}
	if !sawReport {
		t.Fatalf("the probe printed no report:\n%s", out)
	}
	return r
}
