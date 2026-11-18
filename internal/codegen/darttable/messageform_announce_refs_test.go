package darttable

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceRootReferences uses every ID in a flat root body. Unlike the nested
// count-only witness, this directly exercises multibyte announcement references.
func announceRootReferences(t *testing.T, u *ir.Unit, count int) ([]byte, map[int]int, int) {
	t.Helper()
	base := tablewire.Announce(u)
	seedCount := int(binary.LittleEndian.Uint64(base[len(base)-8:]))
	end := len(base) - 8 - seedCount*8
	body := append([]byte(nil), base[:end-1]...)
	ids := append([]byte(nil), base[end:len(base)-8]...)
	positions := map[int]int{}
	for ref := seedCount + 1; ref <= count; ref++ {
		positions[ref] = len(body)
		body = append(body, lebBytes(uint64(ref))...)
		body = append(body, ir.TableKindU8, 1)
		ids = binary.LittleEndian.AppendUint64(ids, 0x5100000000000000+uint64(ref))
	}
	body = append(body, 0)
	wire := slices.Concat(body, ids)
	wire = binary.LittleEndian.AppendUint64(wire, uint64(count))
	return wire, positions, count - seedCount
}

func runDartAnnounceRootReferences(t *testing.T, runtime string) (string, error) {
	t.Helper()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	type wireCase struct {
		name    string
		wire    []byte
		unknown int
		valid   bool
	}
	var cases []wireCase
	for _, count := range []int{127, 128, 129, 4096} {
		wire, _, unknown := announceRootReferences(t, u, count)
		cases = append(cases, wireCase{fmt.Sprintf("valid_%d_refs", count), wire, unknown, true})
	}
	wire, positions, _ := announceRootReferences(t, u, 128)
	bodyEnd := len(wire) - 8 - 128*8
	replace := func(at, n int, bytes ...byte) []byte {
		out := append([]byte(nil), wire[:at]...)
		out = append(out, bytes...)
		return append(out, wire[at+n:]...)
	}
	truncate := func(bytes ...byte) []byte {
		out := append([]byte(nil), wire[:positions[128]]...)
		out = append(out, bytes...)
		return append(out, wire[bodyEnd:]...)
	}
	cases = append(cases,
		wireCase{name: "overlong_127", wire: replace(positions[127], 1, 0xff, 0)},
		wireCase{name: "overlong_128", wire: replace(positions[128], 2, 0x80, 0x81, 0)},
		wireCase{name: "overlong_terminator", wire: replace(bodyEnd-1, 1, 0x80, 0)},
		wireCase{name: "ref_past_table", wire: replace(positions[128], 2, 0x81, 1)},
		wireCase{name: "uint64_max_ref", wire: replace(positions[128], 2, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1)},
		wireCase{name: "overflow_ref", wire: replace(positions[128], 2, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 2)},
		wireCase{name: "eleven_byte_ref", wire: replace(positions[128], 2, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0)},
		wireCase{name: "truncated_ref", wire: truncate(0x80)},
		wireCase{name: "missing_kind_after_ref", wire: truncate(0x80, 1)},
	)
	var probe strings.Builder
	for _, tc := range cases {
		var v tablewire.Vocabulary
		var report tabletext.Report
		err := v.AnnounceRead(tc.wire, &report)
		if tc.valid {
			if err != nil || !v.Announced() || report.Refused || report.Unknown != tc.unknown {
				t.Fatalf("%s: Go did not accept the complete first-use body: %v %+v", tc.name, err, report)
			}
		} else if err == nil || !report.Refused || report.Unknown != 0 || !strings.HasPrefix(report.Reason, "wire_damaged:") {
			t.Fatalf("%s: Go did not refuse malformed reference: %v %+v", tc.name, err, report)
		}
		fmt.Fprintf(&probe, `
  {
    final v = t.TableVocabulary(entries: List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0)));
    final report = t.TableReport();
    final accepted = t.announceRead(v, Uint8List.fromList(%s), report);
    check(accepted == %t, '%s: accepted=$accepted reason=${report.reason}');
    check(v.announced == %t && report.refused == %t, '%s: announcement/report state');
    check(report.unknown == %d, '%s: unknown=${report.unknown}');
`, dartBytes(tc.wire), tc.valid, tc.name, tc.valid, !tc.valid, tc.name, tc.unknown, tc.name)
		if !tc.valid {
			fmt.Fprintf(&probe, "    check(report.reason == 'wire_damaged', '%s: reason=${report.reason}');\n", tc.name)
		}
		probe.WriteString("  }\n")
	}
	return runAnnounceProbeWithRuntime(t, u, probe.String(), runtime)
}

func TestDartAnnounceRootReferences(t *testing.T) {
	t.Parallel()
	if out, err := runDartAnnounceRootReferences(t, dartMessageAnnounceRuntime); err != nil {
		t.Fatalf("announcement root references diverged from Go: %v\n%s", err, out)
	}
}

func TestDartAnnounceRootReferencesNegativeControl(t *testing.T) {
	t.Parallel()
	const checked = `    final reference = tableMessageLeb(bytes, at, bodyEnd);
    if (!reference.$3 || reference.$1 < 0) {
      break;
    }
    final ref = reference.$1;
    at = reference.$2;`
	if strings.Count(dartMessageAnnounceRuntime, checked) != 1 {
		t.Fatal("missing checked root-reference read")
	}
	broken := strings.Replace(dartMessageAnnounceRuntime, checked, "    final ref = bytes[at];\n    at += 1;", 1)
	out, err := runDartAnnounceRootReferences(t, broken)
	if err == nil || !strings.Contains(out, "valid_128_refs: accepted=false") {
		t.Fatalf("one-byte reference plant did not fail at the 128 boundary: %v\n%s", err, out)
	}
}
