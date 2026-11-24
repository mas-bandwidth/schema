package ci

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// nightlyGateLangs is the languages each per-port gate's own program covers:
// test/conformance/bom-entry's check_lang case and nightly-readiness's
// LANGUAGES tuple. They are read from the programs, never listed here.
func nightlyGateLangs(t *testing.T, root string) map[string][]string {
	t.Helper()
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	bom := regexp.MustCompile(`(?m)^\s+(cpp\|[a-z|]+)\) ;;$`).FindStringSubmatch(read("test/conformance/bom-entry"))
	if bom == nil {
		t.Fatal("test/conformance/bom-entry: check_lang's language case not found")
	}
	ready := regexp.MustCompile(`(?m)^LANGUAGES = \(([^)]*)\)`).FindStringSubmatch(read("test/conformance/nightly-readiness"))
	if ready == nil {
		t.Fatal("test/conformance/nightly-readiness: LANGUAGES not found")
	}
	var readiness []string
	for f := range strings.SplitSeq(ready[1], ",") {
		if f = strings.Trim(strings.TrimSpace(f), `"'`); f != "" {
			readiness = append(readiness, f)
		}
	}
	return map[string][]string{"bom-entry": strings.Split(bom[1], "|"), "readiness": readiness}
}

// THE NIGHTLY PORT GATES RUN FOR EVERY PORT OR NAME AN EXEMPTION. The BOM
// entry and readiness steps of nightly-conformance.yml's port job used to
// carry an `if:` over a hand list of the original nine, so for Goose, MOO and
// Python, which both gates cover, and for every other port, the steps showed
// `skipped` under a green job. Now each step runs for every port and asks
// test/conformance/nightly-exemption first; this test holds the ledger to the
// registry (test/conformance/<lang>/ci.json) and to the gates' own lists:
// every port is covered by the gate or exempt, never both and never neither.
func TestNightlyPortGatesRunOrNameAnExemption(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	covered := nightlyGateLangs(t, root)

	jsons, err := provesran.Glob(filepath.Join(root, "test/conformance/*/ci.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ports []string
	for _, j := range jsons {
		if lang := filepath.Base(filepath.Dir(j)); lang != "cpp" { // cpp is the reference job
			ports = append(ports, lang)
		}
	}
	if len(ports) < 17 {
		t.Fatalf("the registry names %d ports; want at least 17", len(ports))
	}

	f, err := os.Open(filepath.Join(root, "test/conformance/nightly-exemptions.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // errdiscard: read-only
	exempt := map[string]bool{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Errorf("nightly-exemptions.txt:%d: want <gate> <lang> <reason>: %q", n, line)
			continue
		}
		gate, lang := fields[0], fields[1]
		if _, ok := covered[gate]; !ok {
			t.Errorf("nightly-exemptions.txt:%d: no gate %q (bom-entry, readiness)", n, gate)
		}
		if !slices.Contains(ports, lang) {
			t.Errorf("nightly-exemptions.txt:%d: %q is not a port in the registry", n, lang)
		}
		if slices.Contains(covered[gate], lang) {
			t.Errorf("nightly-exemptions.txt:%d: %s covers %s: the exemption is stale, delete it", n, gate, lang)
		}
		if exempt[gate+" "+lang] {
			t.Errorf("nightly-exemptions.txt:%d: %s %s is listed twice", n, gate, lang)
		}
		exempt[gate+" "+lang] = true
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for gate, langs := range covered {
		for _, port := range ports {
			if !slices.Contains(langs, port) && !exempt[gate+" "+port] {
				t.Errorf("port %s: %s neither covers it nor names an exemption in test/conformance/nightly-exemptions.txt", port, gate)
			}
		}
	}

	// and the steps carry no language filter: they run, and ask the ledger
	wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/nightly-conformance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ name, gate string }{
		{"every port checks the four BOM entry cases", "bom-entry"},
		{"the runtime readiness gate", "readiness"},
	} {
		_, body, ok := strings.Cut(string(wf), "      - name: "+step.name+"\n")
		if !ok {
			t.Fatalf("nightly-conformance.yml: step %q not found", step.name)
		}
		body, _, _ = strings.Cut(body, "\n      - ")
		if strings.Contains(body, "matrix.lang)") && strings.Contains(body, "if: ") && strings.Contains(body, "fromJSON") {
			t.Errorf("step %q filters ports by a list again", step.name)
		}
		if !strings.Contains(body, "sh test/conformance/nightly-exemption "+step.gate+" ${{ matrix.lang }}") {
			t.Errorf("step %q does not ask test/conformance/nightly-exemption for its exemption", step.name)
		}
	}
}
