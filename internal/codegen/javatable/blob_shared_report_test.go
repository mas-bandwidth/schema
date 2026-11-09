package javatable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type blobSharedReportCase struct {
	Name, Wire, Saved    string
	Shared, KindMismatch int
}

func TestJavaBlobSharedReportCensus(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()

	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "testdata", "conformance", "feature-witnesses", "blob-shared-report")
	u := unitFromDir(t, dir)
	rawCases, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []blobSharedReportCase
	if err := json.Unmarshal(rawCases, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 9 {
		t.Fatalf("expected 9 shared blob-report cases, got %d", len(cases))
	}

	// The census counters are retired (docs/plans/strict-only.md): the oracle's
	// verdict on each case is what the Java leg owes. A blob owned by more than
	// one slot (Shared) or a blob slot under another kind (KindMismatch) must
	// be among the refusals; a case the oracle reads reads with no unknown.
	refused := map[string]bool{}
	m := tabletext.NewModel(u)
	for _, row := range cases {
		wire, err := hex.DecodeString(row.Wire)
		if err != nil {
			t.Fatal(err)
		}
		v := m.New(u.Tables["Root"])
		for _, retaining := range []bool{false, true} {
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			for repeat := 1; repeat <= 2; repeat++ {
				r := tabletext.Report{}
				var ok bool
				var err error
				if retaining {
					ok, err = tablewire.DecodeRetain(m, v, wire, store, &r)
				} else {
					ok, err = tablewire.Decode(m, v, wire, &r)
				}
				if err != nil && !tablewire.ReadRefusal(err) {
					t.Fatalf("%s retain=%v: oracle: %v", row.Name, retaining, err)
				}
				verdict := !ok || r.Refused
				if first, seen := refused[row.Name]; seen && first != verdict {
					t.Fatalf("%s retain=%v repeat=%d: the oracle's verdict moved: %+v %v", row.Name, retaining, repeat, r, err)
				}
				refused[row.Name] = verdict
				if (row.Shared != 0 || row.KindMismatch != 0) && !verdict {
					t.Fatalf("%s retain=%v: the oracle read a shared or mis-kinded blob: %+v", row.Name, retaining, r)
				}
				if !verdict && r.Unknown != 0 {
					t.Fatalf("%s retain=%v: the oracle read with unknown: %+v", row.Name, retaining, r)
				}
			}
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok && strings.HasSuffix(name, ".java") {
			files[name] = data
		}
	}

	build := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(build, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	var body strings.Builder
	for _, tc := range cases {
		wire, err := hex.DecodeString(tc.Wire)
		if err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		var elems []string
		for _, b := range wire {
			elems = append(elems, fmt.Sprintf("(byte) 0x%02x", b))
		}
		fmt.Fprintf(&body, "        check(%q, new byte[]{%s}, %q, %t);\n",
			tc.Name, strings.Join(elems, ", "), tc.Saved, refused[tc.Name])
	}

	driver := fmt.Sprintf(`import %s.*;
import java.util.HexFormat;

public final class BlobSharedReportDriver {
    private static int checks = 0;
    private static int failures = 0;

    private static void fail(String why) {
        System.out.println("FAIL " + why);
        failures++;
    }

    private static void check(String name, byte[] wire, String savedHex, boolean refuse) {
        for (boolean retaining : new boolean[]{false, true}) {
            for (int history : new int[]{0, 7}) {
                TableRetain retain = new TableRetain(1 << 16, 1024);
                for (int repeat = 1; repeat <= 2; repeat++) {
                    TableVariable.Report report = new TableVariable.Report();
                    report.unknown = 11 + history;
                    report.widened = 13;
                    RootVariable.Value v = retaining
                            ? RootRetain.loadRetain(wire, retain, report)
                            : RootVariable.load(wire, report);
                    String why = name + " retain=" + retaining + " history=" + history + " repeat=" + repeat;
                    if (refuse) {
                        // the oracle refuses this case: the whole load refuses and
                        // hands back nothing (docs/plans/strict-only.md)
                        if (v != null || !(report.refused || report.damaged)) {
                            fail("strict refusal " + why
                                    + " value=" + (v != null) + " refused=" + report.refused + " damaged=" + report.damaged);
                        }
                        if (retaining && (retain.used != 0 || retain.idUsed != 0)) {
                            fail("refused retaining load kept a tail " + why);
                        }
                        checks++;
                        continue;
                    }
                    if (v == null || report.damaged || report.refused) {
                        fail("read " + why
                                + " damaged=" + report.damaged + " refused=" + report.refused);
                        checks++;
                        continue;
                    }
                    if (report.unknown != 11 + history || report.widened != 13) {
                        fail("history preserved " + why);
                    }
                    if (name.startsWith("distinct_")) {
                        if (v.a == null || v.b == null || v.c == null || v.d == null || v.c == v.d) {
                            fail("distinct equal blob identities " + why);
                        }
                    }
                    if (name.equals("all_null")) {
                        if (v.a != null || v.b != null || v.c != null || v.d != null) {
                            fail("null slots " + why);
                        }
                    }
                    byte[] saved = RootVariable.save(v);
                    if (saved == null || !HexFormat.of().formatHex(saved).equals(savedHex)) {
                        fail("distinct/null save " + why);
                    }
                    if (retaining) {
                        byte[] rsaved = RootRetain.saveRetain(v, retain, report);
                        if (rsaved == null || !HexFormat.of().formatHex(rsaved).equals(savedHex)) {
                            fail("distinct/null retained save " + why);
                        }
                    }
                    report.reset();
                    if (report.unknown != 0 || report.widened != 0 || report.damaged
                            || report.refused || report.reason != null) {
                        fail(name + ": reset did not clear report");
                    }
                    checks++;
                }
            }
            RootVariable.Value noReport = retaining
                    ? RootRetain.loadRetain(wire, new TableRetain(1 << 16, 1024), null)
                    : RootVariable.load(wire, null);
            if ((noReport == null) != refuse) {
                fail("a null report changes the verdict " + name);
            }
        }
    }

    public static void main(String[] args) {
%s        if (failures != 0) {
            System.out.println("BLOB REPORT FAILED " + failures + " of " + checks);
            System.exit(1);
        }
        System.out.println("BLOB REPORT OK " + checks);
    }
}
`, javaPackageOf(u), body.String())

	driverPath := filepath.Join(build, "BlobSharedReportDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "BlobSharedReportDriver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "BLOB REPORT OK 72\n") {
		t.Fatalf("unexpected output: %s", out)
	}
}
