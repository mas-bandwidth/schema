package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadDirUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(matches) == 0 {
		t.Fatalf("no .schema files in %s", dir)
	}
	sort.Strings(matches)
	return loadUnit(t, matches...)
}

func TestUnitViewDescriptorsElixir(t *testing.T) {
	t.Parallel()

	elixirBin := elixirBinary(t)

	units := []struct {
		dir string
		pkg string
		cli bool
	}{
		{dir: "../../../tables/examples", pkg: "tabledemo", cli: true},
		{dir: "../../../tables/pointers", pkg: "graphdemo", cli: true},
		{dir: "../../../tables/block", pkg: "blockdemo", cli: true},
		{dir: "../../../tables/blockhome", pkg: "blockhome", cli: true},
		{dir: "../../../tables/blobs", pkg: "blobdemo", cli: true},
		{dir: "../../../tables/lists", pkg: "listdemo", cli: true},
		{dir: "../../../tables/maps", pkg: "mapdemo", cli: false},
		{dir: "../../../tables/arms", pkg: "armdemo", cli: false},
		{dir: "../../../tables/vocab", pkg: "vocabdemo", cli: true},
		{dir: "../../../tables/vocab9", pkg: "vocab9demo", cli: true},
	}

	driverPath, err := filepath.Abs("../../../test/elixir-view/view_main.exs")
	if err != nil {
		t.Fatalf("abs driver path: %v", err)
	}

	for _, tc := range units {
		u := loadDirUnit(t, tc.dir)
		if u.Package != tc.pkg {
			t.Fatalf("%s: package = %q, want %q", tc.dir, u.Package, tc.pkg)
		}
		var viewBytes []byte
		if tc.cli {
			out, err := Generate(u)
			if err != nil {
				t.Fatalf("%s: Generate failed: %v", tc.dir, err)
			}
			var ok bool
			viewBytes, ok = out["UnitView.ex"]
			if !ok || len(viewBytes) == 0 {
				t.Fatalf("%s: Generate did not emit UnitView.ex (emitted: %v)", tc.dir, keysOf(out))
			}
			if tableBytes, hasTable := out["Table.ex"]; hasTable {
				tableSrc := string(tableBytes)
				if !strings.Contains(tableSrc, "doc:") || !strings.Contains(tableSrc, "tags:") {
					t.Fatalf("%s: Table.ex descriptors do not carry doc: and tags:", tc.dir)
				}
			}
		} else {
			viewBytes = unitViewModule(u, ir.GoExportName(u.Package))
		}

		tmpDir := t.TempDir()
		viewFile := filepath.Join(tmpDir, "UnitView.ex")
		if err := os.WriteFile(viewFile, viewBytes, 0o644); err != nil {
			t.Fatalf("%s: write UnitView.ex: %v", tc.dir, err)
		}

		cmd := exec.Command(elixirBin, "-r", viewFile, driverPath, tc.pkg)
		cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
		printed, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: elixir view_main.exs failed: %v\n%s", tc.dir, err, string(printed))
		}

		pin := viewlisting.Listing(u)
		if string(printed) != pin {
			t.Fatalf("%s: UnitView listing does not match IR pin:\ngot:\n%s\nwant:\n%s", tc.dir, string(printed), pin)
		}

		if tc.pkg == "tabledemo" {
			unflatCmd := exec.Command(elixirBin, "-r", viewFile, driverPath, tc.pkg, "--unflattened")
			unflatCmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
			unflatOut, err := unflatCmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: elixir view_main.exs --unflattened failed: %v\n%s", tc.dir, err, string(unflatOut))
			}
			if string(unflatOut) == pin {
				t.Fatalf("%s: negative control failed: --unflattened output matched flattened IR pin", tc.dir)
			}
		}
	}
}
