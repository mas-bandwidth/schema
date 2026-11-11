// TestSpecPacketReadRefusesNonFiniteFloat pins the packet-wire float rule
// (docs/rulings.json, packet-floats-finite; SPEC.md §4.3): a packet float
// carries finite values only, so a reader refuses a NaN or an infinity on its
// raw bits as non_finite_float, and -0 and subnormals stay bit exact. The
// pages must state that rule everywhere a port reads from them: the read
// table, the read-validates list, the coverage matrix, and SPEC §5's
// read-side refusal list.
package compiler

import (
	"testing"
)

func TestSpecPacketReadRefusesNonFiniteFloat(t *testing.T) {
	t.Parallel()

	const pwa = "../docs/PACKET-WIRE-ALGORITHM.md"
	const spec = "../docs/SPEC.md"

	readTable := specSection(t, pwa, "### 3.3 READ_FIELD / READ_SCALAR", "### 3.4 Compressed float (read)")
	readValidates := specSection(t, pwa, "## 3. The reader", "### 3.1 READ_ITEM")
	coverage := specSection(t, pwa, "## 7. Coverage matrix stub", "## 8. What a port takes")
	trust := specSection(t, spec, "## 5. Trust model — inherited", "### 5.1 The write contract")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§3.3 the float rows refuse a non-finite value",
			text: readTable,
			wants: []string{
				"refuse a NaN or an infinity as `non_finite_float` before the field is written",
				"`-0` and subnormals stay bit exact",
			},
			stales: []string{
				"no repair",
			},
		},
		{
			row:  "§3 the read-validates list names non-finite floats",
			text: readValidates,
			wants: []string{
				"non-finite floats",
			},
		},
		{
			row:  "§7 the NaN construct row is the non-finite refusal",
			text: coverage,
			wants: []string{
				"each infinity refused",
			},
			stales: []string{
				"bit-identity",
			},
		},
		{
			row:  "SPEC §5 the read-side refusal list names non-finite floats",
			text: trust,
			wants: []string{
				"non-finite floats",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			mustState(t, r.row, r.text, want)
		}
		for _, stale := range r.stales {
			mustNotState(t, r.row, r.text, stale)
		}
	}
}
