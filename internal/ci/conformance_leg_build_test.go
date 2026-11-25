package ci

// EVERY PORT NAMES ITS ONE-LEG CONFORMANCE BUILD (test registry L4;
// Makefile "ONE LEG'S CONFORMANCE BUILD"). The corpus adapter builds a
// language's leg with `make conformance-leg-<lang>`, which builds
// CONFORMANCE_LEG_<lang>. A port with no such variable is refused by name at
// make time; this holds it statically, so a new port's make file cannot land
// without the line its schema-test row needs.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

var conformanceLegBuild = regexp.MustCompile(`(?m)^CONFORMANCE_LEG_([a-z]+) :=`)

// schemaPorts is SCHEMA_PORTS: every make/<name>.mk whose name has no "-".
func schemaPorts(names []string) []string {
	var ports []string
	for _, n := range names {
		base := strings.TrimSuffix(filepath.Base(n), ".mk")
		if !strings.Contains(base, "-") {
			ports = append(ports, base)
		}
	}
	slices.Sort(ports)
	return ports
}

// conformanceLegBuildGaps names each port with no CONFORMANCE_LEG_<port>, and
// each such variable that names no port, across the given make texts.
func conformanceLegBuildGaps(ports []string, texts []string) []string {
	have := map[string]bool{}
	for _, t := range texts {
		for _, m := range conformanceLegBuild.FindAllStringSubmatch(t, -1) {
			have[m[1]] = true
		}
	}
	var gaps []string
	for _, p := range ports {
		if !have[p] {
			gaps = append(gaps, "port "+p+" has no CONFORMANCE_LEG_"+p+": make conformance-leg-"+p+" cannot build its leg")
		}
		delete(have, p)
	}
	for v := range have {
		gaps = append(gaps, "CONFORMANCE_LEG_"+v+" names no port (SCHEMA_PORTS)")
	}
	slices.Sort(gaps)
	return gaps
}

func TestEveryPortNamesItsConformanceLegBuild(t *testing.T) {
	t.Parallel()
	root := repoRootDir(t)
	files, err := provesran.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	ports := schemaPorts(files)
	var texts []string
	for _, f := range append(files, filepath.Join(root, "Makefile")) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, string(b))
	}
	for _, g := range conformanceLegBuildGaps(ports, texts) {
		t.Error(g)
	}
	// planted: a port that loses its line, and a line for no port, are each red
	if g := conformanceLegBuildGaps(append(ports, "cobol"), texts); len(g) != 1 || !strings.Contains(g[0], "port cobol has no CONFORMANCE_LEG_cobol") {
		t.Errorf("a port with no leg build: %q", g)
	}
	if g := conformanceLegBuildGaps(ports, append(texts, "CONFORMANCE_LEG_cobol := x\n")); len(g) != 1 || !strings.Contains(g[0], "CONFORMANCE_LEG_cobol names no port") {
		t.Errorf("a leg build for no port: %q", g)
	}
}
