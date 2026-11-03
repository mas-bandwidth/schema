package lobstertable_test

import (
	"bytes"
	"fmt"
	"maps"
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
	for _, forbidden := range []string{"block_form.lobster", "cook.lobster", "json.lobster", "message.lobster", "fixed.lobster", "schema_save.lobster"} {
		if view[forbidden] != nil {
			t.Errorf("table-free reflection emits unrelated runtime %s", forbidden)
		}
	}
	combined := maps.Clone(packet)
	maps.Copy(combined, view)
	after, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	check := func(candidate map[string][]byte) error {
		for name, data := range packet {
			if !bytes.Equal(data, candidate[name]) {
				return fmt.Errorf("packet file %s changed", name)
			}
			for _, line := range strings.Split(string(candidate[name]), "\n") {
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
	if err := check(after); err != nil {
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
