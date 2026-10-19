package ci

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// schemaPorts is the port roster the random-input differential runs. The
// roster is discovered from the committed generated shared-output tree under
// repo/generated: one port per first-level subdirectory whose name is a
// language leg. This keeps the gate faithful to what the generator actually
// produces rather than to a hand-typed list that can drift.
func schemaPorts(genRoot string) []string {
	entries, err := os.ReadDir(genRoot)
	if err != nil {
		return nil
	}
	var ports []string
	for _, e := range entries {
		if e.IsDir() && !strings.Contains(e.Name(), "ludicrous") && e.Name() != "bench" {
			ports = append(ports, e.Name())
		}
	}
	return ports
}

// schemaPortToolchain names the executable that probes each port's toolchain.
var schemaPortToolchain = map[string]string{
	"c":        "cc",
	"cpp":      "c++",
	"cs":       "dotnet",
	"dart":     "dart",
	"elixir":   "elixir",
	"go":       "go",
	"java":     "java",
	"js":       "node",
	"rust":     "cargo",
	"squirrel": "cc",
	"canary":   "cc",
	"python":   "python3",
}

// TestSchemaSharedTwelvePortRandomDifferential is the readiness half of the
// random-input differential gate. The verdict-comparison body (feeding the
// same deterministic inputs through every leg and comparing accept/refuse,
// refusal reason and decoded value) is the heavy nightly part and lives behind
// the slow gate; this test is the fast per-push half and proves the lens can
// stand: every port has a generated shared-output tree on this machine, a
// conformance driver registered for it, and every port's toolchain is present.
// A missing toolchain is named, per the card's Step 1, as
// "tool missing: <name>".
func TestSchemaSharedTwelvePortRandomDifferential(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	genRoot := filepath.Join(root, "generated")
	ports := schemaPorts(genRoot)
	if len(ports) == 0 {
		t.Fatalf("no generated port trees found under generated/")
	}
	missing := make([]string, 0)
	for _, name := range ports {
		probe, ok := schemaPortToolchain[name]
		if !ok {
			t.Errorf("%s: no toolchain probe registered", name)
			continue
		}
		if _, err := exec.LookPath(probe); err != nil {
			missing = append(missing, name)
		}
		if _, err := os.Stat(filepath.Join(root, "test", "conformance", name, "driver")); err != nil {
			t.Errorf("port %s: no conformance driver registered (test/conformance/%s/driver)", name, name)
		}
	}
	if len(missing) > 0 {
		for _, m := range missing {
			t.Logf("tool missing: %s", m)
		}
		t.Errorf("differential cannot stand: toolchain missing for %d port(s)", len(missing))
	}
}

// differentialPort is one leg the verdict-comparison body drives: the language
// name and the driver command (test/conformance/<lang>/driver), which is the
// repo's one driver contract.
type differentialPort struct {
	lang string
	cmd  string
}

// differentialPorts discovers the legs whose driver answers `list`. A driver
// that cannot start is named as "tool missing: <lang>" rather than skipped in
// silence; the caller decides whether too few legs remain for a differential.
func differentialPorts(t *testing.T, root string) []differentialPort {
	t.Helper()
	drivers, err := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "driver"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(drivers)
	var ports []differentialPort
	for _, driver := range drivers {
		lang := filepath.Base(filepath.Dir(driver))
		cmd := exec.Command(driver, emptyManifest(t, root), "list")
		cmd.Dir = root
		if out, err := cmd.Output(); err != nil {
			t.Logf("tool missing: %s (driver %s: %v)", lang, driver, err)
			continue
		} else if !strings.Contains(string(out), "block") {
			t.Logf("tool missing: %s (no block surface in its driver)", lang)
			continue
		}
		ports = append(ports, differentialPort{lang: lang, cmd: driver})
	}
	return ports
}

func emptyManifest(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "build", "schema-shared-12-port-random-differential", "empty.manifest")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# empty: the list surface reads no rows\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// corpusBlocks reads the block rows of the ONE corpus manifest the Makefile's
// conformance targets read. It is the corpus the card names; no second one is
// invented here.
func corpusBlocks(t *testing.T, root string) []struct{ name, unit, file string } {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []struct{ name, unit, file string }
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "block ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			t.Fatalf("malformed block row: %q", line)
		}
		out = append(out, struct{ name, unit, file string }{f[1], f[2], f[3]})
	}
	if len(out) == 0 {
		t.Fatal("the corpus manifest names no block rows")
	}
	return out
}

// randomBlockInputs derives the SAME deterministic mutants for every leg. The
// seed is fixed, so a red is reproduced exactly; the generator is the repo's
// own math/rand and no network, clock or host entropy enters it.
func randomBlockInputs(t *testing.T, root, scratch string, blocks []struct{ name, unit, file string }, perBase int) []struct {
	name, unit, file string
} {
	t.Helper()
	rng := rand.New(rand.NewSource(0x51ca5e))
	inputDir := filepath.Join(scratch, "inputs")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var cases []struct{ name, unit, file string }
	for _, b := range blocks {
		base, err := os.ReadFile(filepath.Join(root, b.file))
		if err != nil {
			t.Fatalf("%s: %v", b.name, err)
		}
		for i := 0; i < perBase; i++ {
			data := append([]byte(nil), base...)
			switch rng.Intn(10) {
			case 0: // a truncation: the file is shorter than the image it claims
				data = data[:rng.Intn(len(data))]
			case 1: // a tail of junk past the image
				extra := make([]byte, 1+rng.Intn(64))
				rng.Read(extra)
				data = append(data, extra...)
			default: // one to three byte flips inside the image
				for n := 1 + rng.Intn(3); n > 0; n-- {
					if len(data) == 0 {
						break
					}
					data[rng.Intn(len(data))] ^= byte(1 + rng.Intn(255))
				}
			}
			name := fmt.Sprintf("%s_r%04d", b.name, i)
			file := filepath.Join(inputDir, name+".bin")
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			cases = append(cases, struct{ name, unit, file string }{name, b.unit, file})
		}
	}
	return cases
}

func writeDifferentialManifest(t *testing.T, path string, cases []struct{ name, unit, file string }, withForgery bool) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# DERIVED by the 12-port random-input differential — do not edit, do not commit.\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "block %s %s %s\n", c.name, c.unit, c.file)
		if withForgery {
			subject := c.name
			if i := strings.LastIndex(subject, "_r"); i >= 0 {
				subject = subject[:i]
			}
			fmt.Fprintf(&b, "forgery %s block %s %s -1 0\n", c.name, subject, c.file)
			fmt.Fprintf(&b, "refusal %s block\n", c.name)
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runDriverSurface(t *testing.T, root string, p differentialPort, manifest, surface, outDir string) {
	t.Helper()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(p.cmd, manifest, surface, outDir)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", p.lang, surface, err, out)
	}
}

// driverSurface runs one surface and returns the answer of its FIRST case, or
// the driver's own error text. The one-case manifests above use it.
func driverSurface(root string, p differentialPort, manifest, surface, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command(p.cmd, manifest, surface, outDir)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		// The driver's OWN line is the answer: a per-leg prefix would make two
		// legs that refused for the same reason look different.
		if line := firstLine(string(out)); line != "" {
			line = strings.TrimPrefix(line, "driver: ")
			line = strings.TrimPrefix(line, surface+": ")
			return "", fmt.Errorf("%s", line)
		}
		return "", fmt.Errorf("%v", err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", fmt.Errorf("%s %s: driver wrote no answer", p.lang, surface)
}

// firstLine trims a multi-line driver error to the line that names the cause.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func readAnswers(t *testing.T, dir string, names []struct{ name, unit, file string }) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, c := range names {
		data, err := os.ReadFile(filepath.Join(dir, c.name))
		if err != nil {
			t.Fatalf("%s: driver wrote no answer: %v", c.name, err)
		}
		out[c.name] = strings.TrimSpace(string(data))
	}
	return out
}

// differentialFindings compares one surface's answers against the reference
// leg and names every divergence. It is the comparison the negative control
// plants a known-bad answer into.
func differentialFindings(refLang, portLang string, ref, got map[string]string) []string {
	var names []string
	for name := range ref {
		names = append(names, name)
	}
	sort.Strings(names)
	var findings []string
	for _, name := range names {
		want := ref[name]
		have, ok := got[name]
		if !ok {
			findings = append(findings, fmt.Sprintf("%s: %s wrote no answer (reference %q)", name, portLang, want))
			continue
		}
		if have != want {
			findings = append(findings, fmt.Sprintf("%s: %s=%q differs from %s=%q", name, portLang, have, refLang, want))
		}
	}
	return findings
}

// TestSchemaSharedTwelvePortRandomDifferentialVerdicts is the verdict-
// comparison body: the same deterministic random inputs through every
// registered leg, comparing accept/refuse, the refusal reason and the decoded
// value (the canonical block dump). It is heavy (every leg's process starts),
// so the card's workflow runs it nightly and on demand, never per push.
func TestSchemaSharedTwelvePortRandomDifferentialVerdicts(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_SHARED_12_PORT_DIFFERENTIAL") != "1" {
		t.Skip("heavy differential: set SCHEMA_SHARED_12_PORT_DIFFERENTIAL=1 (the card's nightly target)")
	}
	root := repoRoot(t)
	ports := differentialPorts(t, root)
	if len(ports) < 2 {
		t.Fatalf("differential needs at least two legs, found %d (the rest are named `tool missing` above)", len(ports))
	}
	ref := ports[0]
	for _, p := range ports {
		if p.lang == "cpp" {
			ref = p
		}
	}

	scratch := filepath.Join(root, "build", "schema-shared-12-port-random-differential")
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatal(err)
	}
	cases := randomBlockInputs(t, root, scratch, corpusBlocks(t, root), 64)
	allManifest := filepath.Join(scratch, "all.manifest")
	writeDifferentialManifest(t, allManifest, cases, true)

	verdicts := map[string]map[string]string{}
	reasons := map[string]map[string]string{}
	for _, p := range ports {
		runDriverSurface(t, root, p, allManifest, "block", filepath.Join(scratch, p.lang, "block"))
		verdicts[p.lang] = readAnswers(t, filepath.Join(scratch, p.lang, "block"), cases)
		runDriverSurface(t, root, p, allManifest, "block-reason", filepath.Join(scratch, p.lang, "reason"))
		reasons[p.lang] = readAnswers(t, filepath.Join(scratch, p.lang, "reason"), cases)
	}

	// The decoded value is compared only where the reference ACCEPTED: a
	// refused image has no value to dump, and demanding one would compare a
	// refusal's shape rather than the read.
	var open []struct{ name, unit, file string }
	for _, c := range cases {
		if verdicts[ref.lang][c.name] == "open" {
			open = append(open, c)
		}
	}
	// A dump is taken one case at a time so that a walk that refuses ONE image
	// (a forged extent it will not read) is that case's answer, not the whole
	// surface's lost answer and not a panic that hides the other legs.
	dumps := map[string]map[string]string{}
	oneManifest := filepath.Join(scratch, "open.manifest")
	for _, c := range open {
		writeDifferentialManifest(t, oneManifest, []struct{ name, unit, file string }{c}, false)
		for _, p := range ports {
			dir := filepath.Join(scratch, p.lang, "dump", c.name)
			answer, err := driverSurface(root, p, oneManifest, "block-dump", dir)
			if err != nil {
				answer = "ERROR: " + firstLine(err.Error())
			}
			if dumps[p.lang] == nil {
				dumps[p.lang] = map[string]string{}
			}
			dumps[p.lang][c.name] = answer
		}
	}

	findings := 0
	for _, p := range ports {
		if p.lang == ref.lang {
			continue
		}
		for _, f := range differentialFindings(ref.lang, p.lang, verdicts[ref.lang], verdicts[p.lang]) {
			t.Errorf("verdict: %s", f)
			findings++
		}
		for _, f := range differentialFindings(ref.lang, p.lang, reasons[ref.lang], reasons[p.lang]) {
			t.Errorf("refusal reason: %s", f)
			findings++
		}
		if dumps[p.lang] != nil {
			for _, f := range differentialFindings(ref.lang, p.lang, dumps[ref.lang], dumps[p.lang]) {
				t.Errorf("decoded value: %s", f)
				findings++
			}
		}
	}
	t.Logf("differential: %d leg(s) (%s), %d inputs, %d accepted, %d divergence(s)",
		len(ports), strings.Join(portNames(ports), ","), len(cases), len(open), findings)
}

func portNames(ports []differentialPort) []string {
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = p.lang
	}
	return out
}

// TestSchemaSharedTwelvePortRandomDifferentialNegativeControl is the planted-
// bad-pattern control. It feeds the comparator a known-bad answer — one leg
// says `refuse` where the reference says `open`, and one names a different
// refusal reason — and requires the differential to catch it and name the
// planted case. With the plant removed the same comparison is empty. The
// control is registered in make/negative-controls.json and runs nightly with
// the heavy body.
func TestSchemaSharedTwelvePortRandomDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	plant := "block_render_planted-known-bad-pattern"
	ref := map[string]string{plant: "open"}
	if got := differentialFindings("cpp", "cpp", ref, ref); len(got) != 0 {
		t.Fatalf("the comparator reports a divergence on identical answers: %v", got)
	}
	findings := differentialFindings("cpp", "go", ref, map[string]string{plant: "refuse"})
	if len(findings) != 1 || !strings.Contains(findings[0], plant) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the planted verdict was not caught and named: %v", findings)
	}
	reasonFindings := differentialFindings("cpp", "go", map[string]string{plant: "ok"}, map[string]string{plant: "truncated"})
	if len(reasonFindings) != 1 || !strings.Contains(reasonFindings[0], plant) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the planted refusal reason was not caught and named: %v", reasonFindings)
	}
}
