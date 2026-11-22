package jswriterefusalpacket

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// TestWriteRefusalPacketJS holds docs/SPEC.md §5.1's packet writer on the
// JavaScript leg: every caller-holdable value the declaration does not admit
// is refused, and the lawful value at the same boundary is written. The leg
// refuses in every build, so both generated modules (the runtime tier,
// PacketWriteRefusal.js, and the flat tier, PacketWriteRefusalFlat.js) run
// every row of rows.txt with NODE_ENV unset and with NODE_ENV=production.
//
// Three negative controls plant a defect in the EMITTER and require the
// surface red: the flat tier's upper bound comparison relaxed, the runtime
// tier's likewise, and the runtime tier's bits(N) width check removed. Each is
// held to the tier it plants in.
func TestWriteRefusalPacketJS(t *testing.T) {
	runWriteRefusalPacketJS(t)

	const surface = "TestWriteRefusalPacketJSSurface"
	for _, c := range []struct {
		name, tier, other, row string
		sabotage               slowtest.Sabotage
	}{
		{
			// A ranged int32 in the flat writer: `> max` becomes `>= max`.
			name: "flat-upper-bound", tier: "flat", other: "runtime", row: "scalar_at_max",
			sabotage: slowtest.Sabotage{
				File: "internal/codegen/js/flat.go",
				From: "|| %s > %s\", name, name, f.IntMin.String(), name, f.IntMax.String()),\n\t\t\t\t\"\", ind)\n\t\t\tg.emitWriteRangedNum(name, f.IntMin.Int64(), f.IntMax.Int64(), ind)\n\t\tcase \"int64\":",
				To:   "|| %s >= %s\", name, name, f.IntMin.String(), name, f.IntMax.String()),\n\t\t\t\t\"\", ind)\n\t\t\tg.emitWriteRangedNum(name, f.IntMin.Int64(), f.IntMax.Int64(), ind)\n\t\tcase \"int64\":",
			},
		},
		{
			// The runtime tier's folded range guard: `> hi` becomes `>= hi`.
			name: "runtime-upper-bound", tier: "runtime", other: "flat", row: "scalar_at_max",
			sabotage: slowtest.Sabotage{
				File: "internal/codegen/js/functions.go",
				From: `guard := fmt.Sprintf("%s < %s || %s > %s", expr, lo, expr, hi)`,
				To:   `guard := fmt.Sprintf("%s < %s || %s >= %s", expr, lo, expr, hi)`,
			},
		},
		{
			// The runtime tier's bits(N) width check (SPEC §5.1 row 2) removed:
			// serialize.js's checked stream throws and its production stream
			// masks, so neither is the generated writer's refusal.
			name: "runtime-bits-width", tier: "runtime", other: "flat", row: "bits_wide",
			sabotage: slowtest.Sabotage{
				File: "internal/codegen/js/functions.go",
				From: "\t\tg.emitBitsCheck(f, name, ind)\n\t\tif f.Type.Width <= 32 {",
				To:   "\t\tif f.Type.Width <= 32 {",
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, failed := slowtest.RunSabotaged(t, ".", "./test/conformance/js", "^"+surface+"$", c.sabotage)
			if !failed {
				t.Fatalf("negative control %s: the emitter was sabotaged and the packet surface stayed green\n%s", c.name, out)
			}
			if !strings.Contains(out, "FAILED: "+c.row) {
				t.Fatalf("negative control %s: the surface went red, but not at %s\n%s", c.name, c.row, out)
			}
			if !strings.Contains(out, "--- FAIL: "+surface+"/"+c.tier) {
				t.Fatalf("negative control %s: the %s tier did not go red\n%s", c.name, c.tier, out)
			}
			if strings.Contains(out, "--- FAIL: "+surface+"/"+c.other) {
				t.Fatalf("negative control %s: the %s tier went red for a defect planted in the %s tier\n%s", c.name, c.other, c.tier, out)
			}
		})
	}
}

// TestWriteRefusalPacketJSSurface is the surface alone, so the negative
// controls can run it under an overlay without re-entering themselves.
func TestWriteRefusalPacketJSSurface(t *testing.T) {
	runWriteRefusalPacketJS(t)
}

// serializeJS finds the serialize.js runtime the probe's WriteStream comes
// from: $SERIALIZE_JS, then the checkout beside the repository.
func serializeJS(t *testing.T) string {
	t.Helper()
	candidates := []string{"../../../../serialize.js"}
	if env := os.Getenv("SERIALIZE_JS"); env != "" {
		candidates = append([]string{env}, candidates...)
	}
	for _, c := range candidates {
		dir, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		index := filepath.Join(dir, "src", "index.js")
		if fi, err := os.Stat(index); err == nil && !fi.IsDir() {
			return index
		}
	}
	slowtest.Require(t, "SCHEMA_REQUIRE_NODE", "no serialize.js runtime: set SERIALIZE_JS or stage the sibling checkout")
	return ""
}

// nodeBinary is the leg's toolchain: $NODE, then node on PATH. A missing node
// is a skip with the reason, never a pass.
func nodeBinary(t *testing.T) string {
	t.Helper()
	if n := os.Getenv("NODE"); n != "" {
		if _, err := os.Stat(n); err == nil {
			return n
		}
	}
	return slowtest.Tool(t, "node")
}

// runWriteRefusalPacketJS generates PacketWriteRefusal.schema for JavaScript,
// writes the probe beside the two generated modules and runs it over rows.txt
// in each tier under each build mode. Behind slowtest.Gate: it shells out to
// node.
func runWriteRefusalPacketJS(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "node (it runs the generated packet writer in both tiers)")
	node := nodeBinary(t)
	serialize := serializeJS(t)
	probe, err := os.ReadFile("writerefusalpacket.mjs")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir("../../..")

	const root = "."
	rows := wrpacket.Rows(t, root)
	dir := wrpacket.Generate(t, root, "js", compiler.Options{})
	if err := os.WriteFile(filepath.Join(dir, "writerefusalpacket.mjs"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"runtime", "flat"} {
		t.Run(tier, func(t *testing.T) {
			for _, build := range []string{"checked", "production"} {
				t.Run(build, func(t *testing.T) {
					ctx, cancel := slowtest.ProbeContext(t)
					defer cancel()
					cmd := exec.CommandContext(ctx, node, filepath.Join(dir, "writerefusalpacket.mjs"),
						wrpacket.RowsPath(root), dir, serialize, tier)
					cmd.Dir = dir
					cmd.Env = withNodeEnv(os.Environ(), build)
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("the probe did not complete (an exception is not a refusal): %v\n%s", err, out)
					}
					wrpacket.Check(t, string(out), rows, nil)
				})
			}
		})
	}
}

// withNodeEnv returns env with NODE_ENV unset for the checked build and
// "production" for the production one.
func withNodeEnv(env []string, build string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "NODE_ENV=") {
			out = append(out, kv)
		}
	}
	if build == "production" {
		out = append(out, "NODE_ENV=production")
	}
	return out
}
