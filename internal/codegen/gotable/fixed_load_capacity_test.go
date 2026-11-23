package gotable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestGoFixedLoadCapacityWitness(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go compiler")
	root := filepath.Join("..", "..", "..")
	read := func(relative string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	schema := read("test/fixed-empty-batch/Batch.schema")
	one := read("testdata/wire/fixed-empty-batch/one-record.bin")
	cases := read("testdata/conformance/feature-witnesses/fixed-load-capacity/cases.txt")
	source := fmt.Sprintf(goFixedLoadCapacityProbe, hex.EncodeToString(one), string(cases))
	out, err := runGeneratedResult(t, string(schema), source, "-v")
	if err != nil {
		t.Fatalf("fixed load capacities: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

const goFixedLoadCapacityProbe = `package fixedemptybatch

import (
    "bytes"
    "encoding/hex"
    "strconv"
    "strings"
    "testing"
)

func TestFixedLoadCapacities(t *testing.T) {
    wire, err := hex.DecodeString(%q)
    if err != nil { t.Fatal(err) }
    plan := make([]TableFixedEntry, BatchFixedPlanMax)
    for _, line := range strings.Split(%q, "\n") {
        row := strings.Fields(line)
        if len(row) == 0 || strings.HasPrefix(row[0], "#") { continue }
        if len(row) != 4 { t.Fatalf("invalid case row %%q", line) }
        t.Run(row[0], func(t *testing.T) {
            var observed string
            switch row[1] {
            case "load":
                if row[2] == "omitted" {
                    t.Skip("BatchFixedLoad requires a destination slice whose length is the capacity")
                }
                capacity, err := strconv.Atoi(row[2])
                if err != nil { t.Fatal(err) }
                values := make([]Batch, capacity)
                var report TableReport
                count := BatchFixedLoad(values, wire, plan, &report)
                if count < 0 {
                    observed = report.Reason
                } else {
                    observed = "read:" + strconv.FormatInt(count, 10)
                    if count != 1 || values[0].A != 7 || string(values[0].Name[:values[0].NameLength]) != "hello" || report.Err() != nil {
                        t.Fatalf("decoded %%d records, report %%+v, values %%+v", count, report, values)
                    }
                }
            case "save-empty":
                buffer := bytes.Repeat([]byte{0x5a}, len(wire))
                count, err := BatchFixedSaveReason(nil, buffer)
                if count != -1 || err == nil || !bytes.Equal(buffer, bytes.Repeat([]byte{0x5a}, len(wire))) {
                    t.Fatalf("empty save: count %%d, reason %%v, buffer %%x", count, err, buffer)
                }
                observed = err.Error()
            default:
                t.Fatalf("unknown operation %%q", row[1])
            }
            if observed != row[3] { t.Fatalf("got %%s, want %%s", observed, row[3]) }
        })
    }
}
`
