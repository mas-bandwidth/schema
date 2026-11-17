// The packet's end on the Lobster type wire (SPEC §5, "Read termination"),
// the Lobster row of packet-end (make/lobster.mk). test/packet-end/End.schema
// is generated and test/packet-end/lobster/main.lobster reads the shared
// vectors under testdata/wire/packet-end through the generated public
// ReadEnd: the exact packet is accepted, a whole trailing byte and set
// padding in the final byte are refused. A failed vector prints
// "FAILED: <vector>"; packet-end-lobster-negative-control removes the end
// check from the emitter and requires the trailing and padding rows red.
package lobster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

func TestLobsterPacketEnd(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-end", "End.schema"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "End.schema", string(schema))
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	driver, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-end", "lobster", "main.lobster"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, name := range []string{"exact", "trailing", "padding"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "packet-end", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name+".bin"] = data
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", string(driver))
	out := string(lobsterrun.Run(t, dir, "main.lobster", inputs))
	if strings.TrimSpace(out) != "packet end Lobster: exact accepted, trailing byte and set padding refused" {
		t.Fatalf("packet end:\n%s", out)
	}
}
