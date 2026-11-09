package gotable_test

// THE GO CONFORMANCE DRIVER'S REPORT AND RETAIN SURFACES (docs/SPEC-TABLES.md
// §3.4, §4, §6.5, §6.6):
//   - A root declared `fixed table` whose record exceeds §3.4's 65536-byte
//     fixed-form ceiling (tabledemo.WideBlob) has no file form: every file
//     read on the report surface refuses (`previous_form_wide_blob`), while
//     `variableLoad` keeps the form-1 read for the framing battery alone.
//   - A retaining read on `tblrt1.Node` that skips and excludes an unknown
//     field containing a nested kind-17 node pointer (`retain_node_index`)
//     marks the target node record reachable so the post-load graph check
//     accepts the value, answers `read\n` plus the known JSON value on the
//     `retain` surface, and writes back the canonical retained wire on the
//     `retain-save` surface.
//
// This test builds and drives the real Go conformance driver over the
// manifest's `report`, `retain`, and `retain-save` surfaces, so it is a
// functional test: it skips by name without SCHEMA_SLOW, and fails by name
// under SCHEMA_REQUIRE_CORPUS=1 when make or the Go toolchain is missing.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type goReportRow struct {
	name, unit, root, wire, verdict string
}

type goRetainRow struct {
	name, unit, root, wire, capacity, ids, saves string
}

func goConformanceRoot(t *testing.T) string {
	t.Helper()
	slowtest.Gate(t, "make and the Go toolchain (it builds and runs the Go conformance driver)")
	for _, tool := range []string{"make", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", tool, err)
			}
			t.Skipf("%s: %v", tool, err)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func goEnsureGeneratedTables(t *testing.T, root string) {
	t.Helper()
	stamp := filepath.Join(root, "build", "tables-generated-go", ".stamp")
	cards := filepath.Join(root, "build", "tables-generated-go", "cards", "cards.go")
	if _, err := os.Stat(stamp); err == nil {
		if _, err := os.Stat(cards); err == nil {
			return
		}
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "build/conformance-go")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make build/conformance-go: %v\n%s", err, out)
	}
}

func goBuildDriver(t *testing.T, root, overlayPath string) string {
	t.Helper()
	goEnsureGeneratedTables(t, root)
	bin := filepath.Join(t.TempDir(), "conformance-go")
	args := []string{"build"}
	if overlayPath != "" {
		args = append(args, "-overlay", overlayPath)
	}
	args = append(args, "-o", bin, ".")
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = filepath.Join(root, "test", "conformance", "go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return bin
}

func goReportAndRetainRows(t *testing.T, root string) ([]goReportRow, []goRetainRow) {
	t.Helper()
	reportsData, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	verdicts := map[string]string{}
	for line := range strings.SplitSeq(string(reportsData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 2 {
			verdicts[f[0]] = f[1]
		}
	}
	manifestData, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var reports []goReportRow
	var retains []goRetainRow
	for line := range strings.SplitSeq(string(manifestData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "report":
			if len(f) != 5 {
				t.Fatalf("malformed report row: %q", line)
			}
			v, ok := verdicts[f[1]]
			if !ok {
				t.Fatalf("reports.txt has no verdict for %q", f[1])
			}
			reports = append(reports, goReportRow{name: f[1], unit: f[2], root: f[3], wire: f[4], verdict: v})
		case "retain":
			if len(f) != 8 {
				t.Fatalf("malformed retain row: %q", line)
			}
			retains = append(retains, goRetainRow{name: f[1], unit: f[2], root: f[3], wire: f[4], capacity: f[5], ids: f[6], saves: f[7]})
		}
	}
	if len(reports) == 0 || len(retains) == 0 {
		t.Fatalf("manifest rows missing: reports=%d retains=%d", len(reports), len(retains))
	}
	return reports, retains
}

func goDriverRunSurface(t *testing.T, root, bin, manifest, surface string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), surface)
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, manifest, surface, out)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver %s: %v\n%s", surface, err, b)
	}
	return out
}

func goDriverCheckAll(t *testing.T, root, bin string, reports []goReportRow, retains []goRetainRow) []string {
	t.Helper()
	var derived strings.Builder
	for _, r := range reports {
		fmt.Fprintf(&derived, "report %s %s %s %s\n", r.name, r.unit, r.root, r.wire)
	}
	for _, r := range retains {
		fmt.Fprintf(&derived, "retain %s %s %s %s %s %s %s\n", r.name, r.unit, r.root, r.wire, r.capacity, r.ids, r.saves)
	}
	manifest := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte(derived.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var wrong []string

	reportOut := goDriverRunSurface(t, root, bin, manifest, "report")
	for _, r := range reports {
		ans, err := os.ReadFile(filepath.Join(reportOut, r.name))
		if err != nil {
			wrong = append(wrong, fmt.Sprintf("report/%s: missing answer", r.name))
			continue
		}
		head, body, ok := strings.Cut(string(ans), "\n")
		if !ok {
			wrong = append(wrong, fmt.Sprintf("report/%s: malformed answer %q", r.name, ans))
			continue
		}
		gotVerdict := head
		if idx := strings.LastIndexByte(head, ','); idx >= 0 {
			gotVerdict = head[idx+1:]
		}
		if gotVerdict != r.verdict {
			wrong = append(wrong, fmt.Sprintf("report/%s: got verdict %s, want %s", r.name, gotVerdict, r.verdict))
			continue
		}
		wireFile := filepath.Join(reportOut, r.name+".wire")
		if r.verdict == "refused" {
			if body != "" {
				wrong = append(wrong, fmt.Sprintf("report/%s: refused read wrote value bytes", r.name))
			}
			if _, err := os.Stat(wireFile); err == nil {
				wrong = append(wrong, fmt.Sprintf("report/%s: refused read wrote save wire", r.name))
			}
			continue
		}
		wantVal, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "report-values", r.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if body != string(wantVal) {
			wrong = append(wrong, fmt.Sprintf("report/%s: value text differs", r.name))
			continue
		}
		gotWire, err := os.ReadFile(wireFile)
		if err != nil {
			wrong = append(wrong, fmt.Sprintf("report/%s: missing write-back wire", r.name))
			continue
		}
		wantWire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "save-wire", r.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotWire, wantWire) {
			wrong = append(wrong, fmt.Sprintf("report/%s: write-back wire differs", r.name))
		}
	}

	retainOut := goDriverRunSurface(t, root, bin, manifest, "retain")
	for _, r := range retains {
		got, err := os.ReadFile(filepath.Join(retainOut, r.name))
		if err != nil {
			wrong = append(wrong, fmt.Sprintf("retain/%s: missing answer", r.name))
			continue
		}
		wantVal, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "retain-values", r.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		want := append([]byte("read\n"), wantVal...)
		if !bytes.Equal(got, want) {
			wrong = append(wrong, fmt.Sprintf("retain/%s: answer differs from read + retain-values/%s.json", r.name, r.name))
		}
	}

	retainSaveOut := goDriverRunSurface(t, root, bin, manifest, "retain-save")
	for _, r := range retains {
		if r.saves == "-" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(retainSaveOut, r.name))
		if err != nil {
			wrong = append(wrong, fmt.Sprintf("retain-save/%s: driver wrote nothing", r.name))
			continue
		}
		want, err := os.ReadFile(filepath.Join(root, r.saves))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			wrong = append(wrong, fmt.Sprintf("retain-save/%s: saved wire (%d bytes) differs from %s (%d bytes)", r.name, len(got), r.saves, len(want)))
		}
	}

	return wrong
}

func buildSabotagedGoDriver(t *testing.T, root, relFile, anchor, replacement string) string {
	t.Helper()
	origPath := filepath.Join(root, filepath.FromSlash(relFile))
	src, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(src, []byte(anchor)); n != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: anchor %q appears %d times in %s, want 1", anchor, n, relFile)
	}
	planted := bytes.Replace(src, []byte(anchor), []byte(replacement), 1)
	dir := t.TempDir()
	plantedPath := filepath.Join(dir, "planted.go")
	if err := os.WriteFile(plantedPath, planted, 0o600); err != nil {
		t.Fatal(err)
	}
	overlayData, err := json.Marshal(struct {
		Replace map[string]string
	}{
		Replace: map[string]string{origPath: plantedPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlayData, 0o600); err != nil {
		t.Fatal(err)
	}
	return goBuildDriver(t, root, overlayPath)
}

func TestGoDriverReportAndRetain(t *testing.T) {
	root := goConformanceRoot(t)
	reports, retains := goReportAndRetainRows(t, root)
	bin := goBuildDriver(t, root, "")
	for _, w := range goDriverCheckAll(t, root, bin, reports, retains) {
		t.Errorf("%s", w)
	}
}

func TestGoDriverReportAndRetainNegativeControl(t *testing.T) {
	root := goConformanceRoot(t)
	reports, retains := goReportAndRetainRows(t, root)

	t.Run("wide_blob_no_file_form", func(t *testing.T) {
		bin := buildSabotagedGoDriver(
			t,
			root,
			"test/conformance/go/fixed.go",
			`bind("tabledemo", "WideBlob", noFileForm)`,
			`// SABOTAGED: WideBlob left on its variable-form load`,
		)
		wrong := goDriverCheckAll(t, root, bin, reports, retains)
		want := "report/previous_form_wide_blob: got verdict read, want refused"
		if len(wrong) != 1 || wrong[0] != want {
			t.Fatalf("NEGATIVE CONTROL FAILED: removing WideBlob noFileForm binding produced %v, want [%q]", wrong, want)
		}
	})

	t.Run("retain_unknown_node_refs", func(t *testing.T) {
		bin := buildSabotagedGoDriver(
			t,
			root,
			"test/conformance/go/retain.go",
			`markUnknownNodeRefs(tblrt1.TableReader{Buffer: r.Buffer, Ids: r.Ids, Nodes: r.Nodes, Report: r.Report}, knownPtrID)`,
			`// SABOTAGED: unknown nested kind-17 pointers do not mark target nodes`,
		)
		wrong := goDriverCheckAll(t, root, bin, reports, retains)
		want := []string{
			"retain/retain_node_index: answer differs from read + retain-values/retain_node_index.json",
			"retain-save/retain_node_index: driver wrote nothing",
		}
		if len(wrong) != len(want) || wrong[0] != want[0] || wrong[1] != want[1] {
			t.Fatalf("NEGATIVE CONTROL FAILED: removing markUnknownNodeRefs produced %v, want %v", wrong, want)
		}
	})
}
