package cpptable

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conformanceDriverHeaders are the C++ conformance driver's own headers: the
// declarations every unit shares (driver.h), the manifest reader (manifest.h)
// and the block walk (block_walk.h).
var conformanceDriverHeaders = []string{"driver.h", "manifest.h", "block_walk.h"}

// driverSource is one file of the C++ conformance driver, named from the root.
type driverSource struct {
	path string
	text []byte
}

// conformanceDriverTUs reads the driver's translation units from the
// Makefile's CONFORMANCE_DRIVER_TUS, the one list the build compiles: a unit
// the build compiles is a unit these source tests read.
func conformanceDriverTUs(makefile []byte) ([]string, error) {
	text := strings.ReplaceAll(string(makefile), "\\\n", " ")
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(line, "CONFORMANCE_DRIVER_TUS := "); ok {
			units := strings.Fields(value)
			if len(units) == 0 {
				return nil, errors.New("the Makefile's CONFORMANCE_DRIVER_TUS names no unit")
			}
			return units, nil
		}
	}
	return nil, errors.New("the Makefile assigns no CONFORMANCE_DRIVER_TUS")
}

// conformanceDriverSources reads every translation unit of the C++
// conformance driver and, with headers, its own headers too.
func conformanceDriverSources(t *testing.T, root string, headers bool) []driverSource {
	t.Helper()
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	units, err := conformanceDriverTUs(makefile)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range units {
		names = append(names, u+".cpp")
	}
	if headers {
		names = append(names, conformanceDriverHeaders...)
	}
	var out []driverSource
	for _, name := range names {
		path := filepath.Join("test", "conformance", "cpp", name)
		text, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%v: the Makefile's driver unit is not in the tree", err)
		}
		out = append(out, driverSource{path: filepath.ToSlash(path), text: text})
	}
	return out
}

// unregisteredFamilies names every family unit (driver_<family>.cpp defining
// driver_tables_<family>) that main.cpp's registry does not list: a family it
// does not list is a family whose codecs no row ever finds.
func unregisteredFamilies(sources []driverSource, mainSource []byte) []string {
	var missing []string
	for _, s := range sources {
		_, after, ok := bytes.Cut(s.text, []byte("const DriverTables driver_tables_"))
		if !ok {
			continue
		}
		name := after
		if i := bytes.IndexAny(name, " ={;\n"); i >= 0 {
			name = name[:i]
		}
		if !bytes.Contains(mainSource, []byte(fmt.Sprintf("&driver_tables_%s,", name))) {
			missing = append(missing, fmt.Sprintf("%s defines driver_tables_%s and main.cpp's families[] does not list it", s.path, name))
		}
	}
	return missing
}

// TestCppConformanceDriverRegistersEveryFamily holds main.cpp's family
// registry to the units the Makefile compiles, and goes red on a planted
// family it does not list.
func TestCppConformanceDriverRegistersEveryFamily(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	sources := conformanceDriverSources(t, root, false)
	var mainSource []byte
	families := 0
	for _, s := range sources {
		if s.path == "test/conformance/cpp/main.cpp" {
			mainSource = s.text
		}
		if bytes.Contains(s.text, []byte("const DriverTables driver_tables_")) {
			families++
		}
	}
	if mainSource == nil {
		t.Fatal("CONFORMANCE_DRIVER_TUS does not name main")
	}
	if families == 0 {
		t.Fatal("no driver unit defines a DriverTables: the scan read nothing")
	}
	for _, m := range unregisteredFamilies(sources, mainSource) {
		t.Error(m)
	}
	planted := append(append([]driverSource{}, sources...), driverSource{path: "planted.cpp", text: []byte("extern const DriverTables driver_tables_planted;\nconst DriverTables driver_tables_planted = {};\n")})
	if got := unregisteredFamilies(planted, mainSource); len(got) != 1 || !strings.Contains(got[0], "driver_tables_planted") {
		t.Errorf("a planted family main.cpp does not list was not named: %v", got)
	}
}

// TestConformanceDriverUnitsReadTheMakefileList: the list is read with its
// continuation lines, and a Makefile without it, or with it empty, is red.
func TestConformanceDriverUnitsReadTheMakefileList(t *testing.T) {
	t.Parallel()
	units, err := conformanceDriverTUs([]byte("X := 1\nCONFORMANCE_DRIVER_TUS := a b \\\n\tc main\nY := 2\n"))
	if err != nil || strings.Join(units, " ") != "a b c main" {
		t.Errorf("got %v, %v; want a b c main", units, err)
	}
	if _, err := conformanceDriverTUs([]byte("X := 1\n")); err == nil {
		t.Error("a Makefile without CONFORMANCE_DRIVER_TUS passed")
	}
	if _, err := conformanceDriverTUs([]byte("CONFORMANCE_DRIVER_TUS := \n")); err == nil {
		t.Error("an empty CONFORMANCE_DRIVER_TUS passed")
	}
}
