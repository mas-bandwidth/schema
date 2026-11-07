package cstable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The measure walk cannot decide a load verdict: it does not earn prefix
// counters and can inspect a map value whose key prevents admission.
func TestCountCapPreservesLogicalPrefixCSharp(t *testing.T) {
	t.Skip("layer 4 (cs): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "the .NET SDK")
	schema := "package probe\ntable Child { items []int32 }\nunion Pick { child Child }\ntable Root {\n inner Child\n objects map[int32]Child\n choices []Pick\n}\n"
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	frame := func(ref, kind byte, p []byte) []byte {
		b := binary.AppendUvarint([]byte{ref, kind}, uint64(len(p)))
		return append(b, p...)
	}
	scalar := func(ref byte, n uint32) []byte { return binary.LittleEndian.AppendUint32([]byte{ref, 4}, n) }
	file := func(body []byte) []byte {
		b := append([]byte{1}, body...)
		for _, name := range []string{"before", "inner", "items", "objects", "key", "value", "after", "choices", "child"} {
			b = binary.LittleEndian.AppendUint64(b, ir.TableWireId(name))
		}
		return binary.LittleEndian.AppendUint64(b, 9)
	}
	list := binary.AppendUvarint([]byte{3}, 1<<31)
	list = append(list, 0, 0)
	child := append(frame(3, 14, list), 0)
	prefix := append(scalar(1, 1), frame(2, 13, child)...)
	first := append(scalar(5, 2), 0)
	second := append(frame(6, 13, child), scalar(5, 1)...)
	second = append(second, 0)
	mapBody := []byte{13, 2}
	for _, entry := range [][]byte{first, second} {
		mapBody = binary.AppendUvarint(mapBody, uint64(len(entry)))
		mapBody = append(mapBody, entry...)
	}
	unionList := func(tail []byte) []byte {
		b := append([]byte{15, 2}, frame(9, 13, child)...)
		b = append(b, tail...)
		return file(append(frame(8, 14, b), 0))
	}
	rows := []struct {
		name             string
		wire             []byte
		cap              bool
		unknown, widened int
	}{
		{"prefix", file(append(append([]byte{}, prefix...), 0)), true, 8, 14},
		{"later-damage", file(append(append([]byte{}, prefix...), 7, 13, 127)), true, 8, 14},
		{"descending-map", file(append(frame(4, 14, mapBody), 0)), false, 7, 13},
		{"union-list-later-unknown", unionList([]byte{1, 4, 4, 0, 0, 0, 0}), true, 7, 14},
		{"union-list-later-damage", unionList([]byte{127}), true, 7, 14},
	}
	var body strings.Builder
	for _, row := range rows {
		report := tabletext.Report{Unknown: 7, Widened: 13}
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), row.wire, &report)
		var refusal *tablewire.CountRefusal
		if ok || errors.As(err, &refusal) != row.cap || report.Refused == row.cap || report.Unknown != row.unknown || report.Widened != row.widened {
			t.Fatalf("%s oracle: ok=%v err=%v report=%+v", row.name, ok, err, report)
		}
		nums := make([]string, len(row.wire))
		for i, v := range row.wire {
			nums[i] = fmt.Sprint(v)
		}
		fmt.Fprintf(&body, "{ var r = new Probe.TableReport { Unknown = 7, Widened = 13 }; var v = new Probe.Root();\n")
		fmt.Fprintf(&body, "var verdict = Probe.Schema.RootLoadVerdict(v, new byte[] {%s}, r);\n", strings.Join(nums, ","))
		if row.cap {
			fmt.Fprintf(&body, "if (verdict != Probe.Schema.TableWire.Verdict.Refused || !r.Refused || r.Reason != \"count_over_extent_cap\" || r.Malformed || r.Unknown != %d || r.Widened != %d) bad += ProbeLog.Fail(\"%s prefix events: unknown=\" + r.Unknown + \" widened=\" + r.Widened + \" malformed=\" + r.Malformed);\n", row.unknown, row.widened, row.name)
		} else {
			fmt.Fprintf(&body, "if (verdict != Probe.Schema.TableWire.Verdict.Damaged || r.Refused || !r.Malformed || r.Unknown != %d || r.Widened != %d) bad += ProbeLog.Fail(\"%s hidden cap must not outrank key admission\");\n", row.unknown, row.widened, row.name)
		}
		body.WriteString("}\n")
	}
	t15RunCS(t, schema, body.String())
}
