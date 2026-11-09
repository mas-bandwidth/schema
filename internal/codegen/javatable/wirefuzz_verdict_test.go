package javatable_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WIRE-FUZZ VERDICT PIPE FOR JAVA (docs/SPEC-TABLES.md §4.2, §6.5,
// §6.6; test/conformance/README.md, "The wire-fuzz driver"): every mutant sent
// over the verdict pipe must produce the same strict read verdict in the Java
// leg (test/conformance/java/src/TablesWireFuzz.java) and the harness's Go
// oracle (internal/tablewire), and every accepted read must write back the
// canonical bytes. The strict read is one read — the whole value or a refusal
// (docs/plans/strict-only.md): a fixed root's file holds one record, so a read
// of exactly that record, clean, is an accept, and a read of none (a truncated
// file) or a named refusal is a refusal of the whole wire, never damage.
//
// TestJavaWireFuzzVerdictPipe runs the real committed driver on its pipe
// through the real conformance wire-fuzz harness, the exact command
// `make tables-java-wire-fuzz` runs, over a staged subset of the corpus that
// carries both fixed and variable roots, and requires zero divergences.
// TestJavaWireFuzzVerdictPipeNegativeControl replants the pre-strict fault (a
// fixed root loaded unconditionally) in a copy of the real driver source,
// recompiles it against the same generated units, and requires the pipe to go
// red on the fixed root's truncation.

// javaWireFuzzManifest is the staged subset of the corpus the verdict-pipe
// tests drive: a fixed-class root (LoadoutConfig) beside variable-class roots
// (Scene, Feed, Catalog), all with small wires so the enumerated pass stays
// quick. The units are the same four `make tables-java-wire-fuzz` generates
// for and the conformance classpath carries.
const javaWireFuzzManifest = `unit tabledemo tables/examples
unit graphdemo tables/pointers
unit streamdemo tables/stream
unit blobdemo tables/blobs
instance loadout_full tabledemo LoadoutConfig testdata/conformance/tables/wire/loadout_full.bin
instance graph_tree graphdemo Scene testdata/wire/tables/graph_tree.bin
instance stream_chain streamdemo Feed testdata/wire/tables/stream_chain.bin
instance blob_small blobdemo Catalog testdata/wire/tables/blob_small.bin
`

// javaWireFuzzStrictRead is the real driver's strict fixed-root read, an
// anchor the negative control replaces. It must appear in the committed
// TablesWireFuzz.java exactly once.
const javaWireFuzzStrictRead = `                boolean ok = read > 0 && !r.damaged && !r.refused;
                r.loaded = ok;
                if (!ok) {
                    r.refused = true;
                    r.damaged = false;
                }
                r.measure = -1;
                if (ok) {
                    int size = (int) fixedMeasure.invoke(null, read);
                    byte[] buf = new byte[size];
                    int wrote = (int) fixedSave.invoke(null, values, read, buf);
                    r.saved = wrote == size ? buf : null;
                }`

// javaWireFuzzUnstrictRead is the pre-strict tolerant read the negative control
// replants: it accepts a fixed root whatever the read said, so a truncated
// zero-record file is accepted when the oracle refuses it.
const javaWireFuzzUnstrictRead = `                r.loaded = true; // SABOTAGED strict read
                r.measure = -1;
                if (read > 0) {
                    int size = (int) fixedMeasure.invoke(null, read);
                    byte[] buf = new byte[size];
                    int wrote = (int) fixedSave.invoke(null, values, read, buf);
                    r.saved = wrote == size ? buf : null;
                }`

// writeJavaWireFuzzManifest writes the staged manifest and returns its path.
func writeJavaWireFuzzManifest(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(p, []byte(javaWireFuzzManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// runJavaWireFuzzVerdictPipe runs the harness's wire-fuzz verdict pipe over the
// staged manifest with the given driver, in the plain arm or the retention arm
// (--retain) when retain is set. It returns the harness output.
func runJavaWireFuzzVerdictPipe(t *testing.T, root, driver string, retain bool) string {
	t.Helper()
	failed := filepath.Join(t.TempDir(), "failed.bin")
	args := []string{"wire-fuzz"}
	if retain {
		args = append(args, "--retain")
	}
	args = append(args,
		"--manifest", writeJavaWireFuzzManifest(t),
		"--vectors", "",
		"--driver", driver,
		"--seed", "1",
		"--n", "100",
		"--failed", failed,
	)
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "build", "conformance-harness"), args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the verdict pipe failed:\n%s", out)
	}
	return string(out)
}

// TestJavaWireFuzzVerdictPipe is the verdict-pipe differential: the real driver
// on its pipe, through the real harness, over staged fixed and variable roots,
// in the plain and the retention arms, with zero divergences
// (docs/SPEC-TABLES.md §4.2, §6.6).
func TestJavaWireFuzzVerdictPipe(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus (it compiles and runs the generated Java)")
	root := javaConformanceRoot(t)
	javaConformanceBuild(t, root)

	driver := fmt.Sprintf("java -cp %s TablesWireFuzz", filepath.Join(root, "build", "conformance-java"))
	for _, tc := range []struct {
		name   string
		retain bool
	}{
		{name: "plain"},
		{name: "retain", retain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := runJavaWireFuzzVerdictPipe(t, root, driver, tc.retain)
			if !strings.Contains(out, "0 divergences") {
				t.Fatalf("the %s verdict pipe output lacks 0 divergences:\n%s", tc.name, out)
			}
			if !regexp.MustCompile(`[1-9][0-9]* seeds over [1-9][0-9]* roots`).MatchString(out) {
				t.Fatalf("the %s verdict pipe fuzzed no live seed:\n%s", tc.name, out)
			}
			if !regexp.MustCompile(`[1-9][0-9]* mutants`).MatchString(out) {
				t.Fatalf("the %s verdict pipe fuzzed no mutants:\n%s", tc.name, out)
			}
		})
	}
}

// TestJavaWireFuzzVerdictPipeNegativeControl replants the pre-strict tolerant
// read (a fixed root loaded unconditionally) in a copy of the real driver
// source, recompiles it against the same generated units, and requires the
// verdict pipe to go red on the fixed root's truncation.
func TestJavaWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus (it compiles and runs the generated Java)")
	root := javaConformanceRoot(t)
	javaConformanceBuild(t, root)

	srcPath := filepath.Join(root, "test", "conformance", "java", "src", "TablesWireFuzz.java")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), javaWireFuzzStrictRead); n != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the strict-read anchor appears %d times in TablesWireFuzz.java, want 1", n)
	}
	sabotaged := strings.Replace(string(src), javaWireFuzzStrictRead, javaWireFuzzUnstrictRead, 1)

	dir := t.TempDir()
	sabSrc := filepath.Join(dir, "TablesWireFuzz.java")
	if err := os.WriteFile(sabSrc, []byte(sabotaged), 0o600); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	javac := exec.Command("javac", "--release", "17", "-nowarn",
		"-cp", filepath.Join(root, "build", "conformance-java"),
		"-d", classes, sabSrc)
	if out, err := javac.CombinedOutput(); err != nil {
		t.Fatalf("javac sabotaged driver: %v\n%s", err, out)
	}

	failed := filepath.Join(t.TempDir(), "failed.bin")
	driver := fmt.Sprintf("java -cp %s:%s TablesWireFuzz", classes, filepath.Join(root, "build", "conformance-java"))
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "build", "conformance-harness"), "wire-fuzz",
		"--manifest", writeJavaWireFuzzManifest(t),
		"--vectors", "",
		"--driver", driver,
		"--seed", "1",
		"--n", "100",
		"--failed", failed,
	)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the tolerant read left the verdict pipe green:\n%s", out)
	}
	if !strings.Contains(string(out), "read acceptance differs: the leg says true, the oracle says false") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe went red, but not on a read acceptance divergence:\n%s", out)
	}
}
