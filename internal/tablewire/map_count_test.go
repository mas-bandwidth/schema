package tablewire_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMapBuilderCountCap(t *testing.T) {
	m := listModel(t, "package probe\ntable Root { entries map[int32]int32 }\n")
	for _, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
		// From the file grammar: one map field, one physically present empty
		// entry, and a separately announced count. No generated writer is used.
		body := binary.AppendUvarint([]byte{13}, count)
		body = append(body, 1, 0)
		wire := binary.AppendUvarint([]byte{1, 1, 14}, uint64(len(body)))
		wire = append(wire, body...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("entries"))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		report := tabletext.Report{Unknown: 3}
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), wire, &report)
		var refusal *tablewire.CountRefusal
		if ok || !errors.As(err, &refusal) || report != (tabletext.Report{Unknown: 3}) {
			t.Fatalf("count %d must refuse before entries without adding damage: %v %v %+v", count, ok, err, report)
		}
	}
}
