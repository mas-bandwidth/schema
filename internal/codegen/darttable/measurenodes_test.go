package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestWireMeasureCountsNodeRecordsDart pins the Dart <root>WireMeasure against
// the reference tablewire.FileRegionMeasure (internal/tablewire/extent.go) on
// the region byte count a caller asks for before it loads (docs/SPEC-TABLES.md
// §6.2). Before this card the Dart measure counted the root body's fixed size
// and its out-of-line list extents, every string blob and one directory entry
// per node, but NOT the storage of the node records of pointer-reachable
// tables nor *bytes blobs: on graphdemo Scene it measured 288 where the Go
// oracle measures 504. Every variable-class root here reaches a pointer table
// (graphdemo Scene) or a *bytes blob (blobdemo Catalog) and must agree byte for
// byte with FileRegionMeasure on the same bytes.
func TestWireMeasureCountsNodeRecordsDart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		unit    string
		root    string
		wire    string
		oracles []string
	}{
		{
			name: "graphdemo scene",
			unit: "tables/pointers",
			root: "Scene",
			wire: "GraphdemoWire.dart",
			oracles: []string{
				"graph_tree.bin",
				"graph_shared.bin",
				"graph_deep.bin",
			},
		},
		{
			name: "blobdemo catalog",
			unit: "tables/blobs",
			root: "Catalog",
			wire: "BlobdemoWire.dart",
			oracles: []string{
				"blob_small.bin",
				"blob_shared.bin",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			schemas, err := filepath.Glob(filepath.Join("..", "..", "..", c.unit, "*.schema"))
			if err != nil || len(schemas) == 0 {
				t.Fatalf("no schema files under %s: %v", c.unit, err)
			}
			u := loadUnit(t, schemas...)
			root := u.Tables[c.root]
			if root == nil {
				t.Fatalf("unit has no root table %s", c.root)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate for %s: %v", c.name, err)
			}
			wire, ok := files[c.wire]
			if !ok || len(wire) == 0 {
				t.Fatalf("Generate emitted no %s", c.wire)
			}
			for _, o := range c.oracles {
				t.Run(o, func(t *testing.T) {
					t.Parallel()
					oracle := filepath.Join("..", "..", "..", "testdata", "wire", "tables", o)
					data, err := os.ReadFile(oracle)
					if err != nil {
						t.Fatalf("read oracle: %v", err)
					}
					got := measureProbe(t, c.wire, wire, c.root, oracle, packetFiles(t, u))
					want, _, _ := tablewire.FileRegionMeasure(u, root, data)
					if int64(got) != want {
						t.Errorf("Dart %sWireMeasure = %d, tablewire.FileRegionMeasure = %d on %s", c.root, got, want, o)
					}
				})
			}
		})
	}
}

// measureProbe runs the generated <root>WireMeasure over an oracle .bin and
// returns the byte count it reports.
func measureProbe(t *testing.T, wireName string, wire []byte, root, oracle string, packet map[string][]byte) int {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, packet)
	if err := os.WriteFile(filepath.Join(dir, wireName), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	fn := strings.ToLower(root[:1]) + root[1:]
	probe := `import 'dart:io';
import '` + wireName + `';

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  stdout.writeln(` + fn + `WireMeasure(wire));
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	oracleAbs, err := filepath.Abs(oracle)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "probe.dart", oracleAbs)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s measure over the generated library: %v\n%s", root, err, out)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("probe did not print a measure: %q", out)
	}
	return v
}
