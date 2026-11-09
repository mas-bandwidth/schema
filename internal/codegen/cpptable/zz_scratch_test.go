package cpptable

import (
	"fmt"
	"strings"
	"testing"
)

func TestScratchDumpMessage(t *testing.T) {
	schema := `package msgstrict

fixed table Sig
{
    lead uint32
    rng int32 | min = 0, max = 100
    arr [2..4]uint16
}
`
	u := cppUnitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.Contains(name, "Table.h") {
			fmt.Printf("==== %s ====\n", name)
			fmt.Println(string(data))
		}
	}
}
