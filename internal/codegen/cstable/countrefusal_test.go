package cstable

import (
	"encoding/binary"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
	"testing"
)

func TestCountCapRefusesWithoutDamageCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	wire := func(n uint64) string {
		p := binary.AppendUvarint([]byte{13}, n)
		if n != 0 {
			p = append(p, 1, 0)
		}
		b := binary.AppendUvarint([]byte{1, 1, 14}, uint64(len(p)))
		b = append(b, p...)
		b = append(b, 0)
		b = binary.LittleEndian.AppendUint64(b, ir.TableWireId("entries"))
		b = binary.LittleEndian.AppendUint64(b, 1)
		nums := make([]string, len(b))
		for i, v := range b {
			nums[i] = fmt.Sprint(v)
		}
		return "new byte[] {" + strings.Join(nums, ",") + "}"
	}
	body := "byte[][] caps = new byte[][] {" + wire(1<<31) + "," + wire(1<<32) + "," + wire(1<<32+1) + "};\n"
	body += "byte[] clean = " + wire(0) + ";\nbyte[] shortBody = " + wire(0x7fffffff) + ";\n"
	body += `
foreach (byte[] bytes in caps) {
 foreach (bool before in new[] { false, true }) {
  var report = new Probe.TableReport { Unknown = 3, Malformed = before };
  var value = new Probe.Root();
  var verdict = Probe.Schema.RootLoadVerdict(value, bytes, report);
  if (verdict != Probe.Schema.TableWire.Verdict.Refused || !report.Refused ||
      report.Reason != "count_over_extent_cap" || report.Malformed != before || report.Unknown != 3)
   bad += ProbeLog.Fail("count cap must preserve prior counters and malformed history");
  if (!Probe.Schema.RootLoad(value, clean, report) || report.Refused || report.Reason != null || report.Malformed != before)
   bad += ProbeLog.Fail("count refusal poisoned a clean read");
 }
}
var damage = new Probe.TableReport();
if (Probe.Schema.RootLoad(new Probe.Root(), shortBody, damage) || !damage.Malformed || damage.Refused)
 bad += ProbeLog.Fail("INT32_MAX short body must remain damage");
`
	t15RunCS(t, "package probe\ntable Root { entries map[int32]int32 }\n", body)
}
