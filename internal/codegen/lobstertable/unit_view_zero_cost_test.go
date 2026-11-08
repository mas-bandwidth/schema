package lobstertable_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

func TestLobsterTableFreePacketIdentity(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	view, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name := range view {
		if name != u.Package+"View.lobster" && name != "descriptors.lobster" && name != "schema_tables.lobster" {
			t.Errorf("table-free reflection emits table module or codec %s", name)
		}
	}
	combined := maps.Clone(packet)
	maps.Copy(combined, view)
	check := func(candidate map[string][]byte) error {
		for name, data := range packet {
			if !bytes.Equal(data, candidate[name]) {
				return fmt.Errorf("packet file %s changed", name)
			}
			for line := range strings.SplitSeq(string(candidate[name]), "\n") {
				dep, ok := strings.CutPrefix(strings.TrimSpace(line), "import .")
				if ok && view[dep+".lobster"] != nil {
					return fmt.Errorf("packet file %s imports reflection %s", name, dep)
				}
			}
		}
		return nil
	}
	if err := check(combined); err != nil {
		t.Fatal(err)
	}
	control := maps.Clone(combined)
	for name, data := range packet {
		if name == "schema_runtime.lobster" {
			continue
		}
		control[name] = append(append([]byte(nil), data...), []byte("\nimport .descriptors\n")...)
		break
	}
	if check(control) == nil {
		t.Fatal("packet import mutation escaped identity gate")
	}
}

// Load two different units: merely generating the same unit twice does not
// prove that introducing a table leaves the packet source unchanged.
func TestLobsterAddingTableMovesNoPacketByte(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	const packetSource = `package zeroprobe
enum Mode { One }
flags Options { Fast }
type Payload {
    mode Mode
    options Options
    n int32
}
`
	var packet map[string][]byte
	for _, addedTable := range []bool{false, true} {
		source := packetSource
		if addedTable {
			source += "table Root { payload Payload }\n"
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		files, err := lobster.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if !addedTable {
			packet = files
			continue
		}
		if len(files) != len(packet) {
			t.Fatalf("adding a table changed the packet file set: %d -> %d", len(packet), len(files))
		}
		for name, before := range packet {
			if !bytes.Equal(before, files[name]) {
				t.Errorf("adding a table changed packet file %s", name)
			}
		}
		view, err := lobstertable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if view["ProbeTable.lobster"] == nil || view["load.lobster"] == nil {
			t.Fatal("added table did not grow its module and read runtime")
		}
	}
}
