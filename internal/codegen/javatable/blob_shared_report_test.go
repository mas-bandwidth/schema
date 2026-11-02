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

	m := tabletext.NewModel(u)
	for _, row := range cases {
		wire, err := hex.DecodeString(row.Wire)
		if err != nil {
			t.Fatal(err)
		}
		v := m.New(u.Tables["Root"])
		for _, retaining := range []bool{false, true} {
			r := tabletext.Report{}
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			for repeat := 1; repeat <= 2; repeat++ {
				var ok bool
				var err error
				if retaining {
					ok, err = tablewire.DecodeRetain(m, v, wire, store, &r)
				} else {
					ok, err = tablewire.Decode(m, v, wire, &r)
				}
				if !ok || err != nil || r.Malformed || r.BlobShared != row.Shared*repeat || r.KindMismatch != row.KindMismatch*repeat {
					t.Fatalf("%s neutral retain=%v: %+v %v", row.Name, retaining, r, err)
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
		body.WriteString(fmt.Sprintf("        check(%q, new byte[]{%s}, %q, %d, %d);\n",
			tc.Name, strings.Join(elems, ", "), tc.Saved, tc.Shared, tc.KindMismatch))
	}

	driver := fmt.Sprintf(`import %s.*;
import java.util.HexFormat;

public final class BlobSharedReportDriver {
    private static int checks = 0;

    private static void check(String name, byte[] wire, String savedHex, int shared, int kindMismatch) {
        for (boolean retaining : new boolean[]{false, true}) {
            for (int history : new int[]{0, 7}) {
                TableVariable.Report report = new TableVariable.Report();
                if (report.blobShared != 0) {
                    throw new AssertionError("new report counter " + name);
                }
                report.blobShared = history;
                report.unknown = 11;
                report.widened = 13;
                report.clamped = 17;
                TableRetain retain = new TableRetain(1 << 16, 1024);
                for (int repeat = 1; repeat <= 2; repeat++) {
                    RootVariable.Value v = retaining
                            ? RootRetain.loadRetain(wire, retain, report)
                            : RootVariable.load(wire, report);
                    String why = name + " retain=" + retaining + " history=" + history + " repeat=" + repeat;
                    if (v == null || report.malformed || report.refused
                            || report.blobShared != history + shared * repeat
                            || report.kindMismatch != kindMismatch * repeat) {
                        throw new AssertionError("census " + why
                                + " blobShared=" + report.blobShared
                                + " kindMismatch=" + report.kindMismatch
                                + " malformed=" + report.malformed
                                + " refused=" + report.refused);
                    }
                    if (report.unknown != 11 || report.widened != 13 || report.clamped != 17) {
                        throw new AssertionError("history preserved " + why);
                    }
                    if (name.equals("shared_empty") || name.equals("shared_equal") || name.equals("repeated_field_naming")) {
                        if (v.a == null || v.a != v.b || v.c == null || v.c != v.d) {
                            throw new AssertionError("one shared object per blob " + why);
                        }
                    }
                    if (name.startsWith("distinct_")) {
                        if (v.a == null || v.b == null || v.c == null || v.d == null || v.c == v.d) {
                            throw new AssertionError("distinct equal blob identities " + why);
                        }
                    }
                    if (name.equals("all_null")) {
                        if (v.a != null || v.b != null || v.c != null || v.d != null) {
                            throw new AssertionError("null slots " + why);
                        }
                    }
                    if (shared == 0) {
                        byte[] saved = RootVariable.save(v);
                        if (saved == null || !HexFormat.of().formatHex(saved).equals(savedHex)) {
                            throw new AssertionError("distinct/null save " + why);
                        }
                        if (retaining) {
                            byte[] rsaved = RootRetain.saveRetain(v, retain, report);
                            if (rsaved == null || !HexFormat.of().formatHex(rsaved).equals(savedHex)) {
                                throw new AssertionError("distinct/null retained save " + why);
                            }
                        }
                    } else {
                        if (RootVariable.save(v) != null || RootVariable.toJson(v) != null) {
                            throw new AssertionError("shared writer refusal " + why);
                        }
                        if (retaining && (RootRetain.saveRetain(v, retain, report) != null || RootRetain.measureRetain(v, retain) >= 0)) {
                            throw new AssertionError("shared retained writer refusal " + why);
                        }
                    }
                    checks++;
                }
                report.reset();
                if (report.blobShared != 0 || report.unknown != 0 || report.kindMismatch != 0
                        || report.widened != 0 || report.clamped != 0 || report.duplicate != 0
                        || report.malformed || report.refused || report.reason != null) {
                    throw new AssertionError(name + ": reset did not clear report");
                }
            }
            RootVariable.Value noReport = retaining
                    ? RootRetain.loadRetain(wire, new TableRetain(1 << 16, 1024), null)
                    : RootVariable.load(wire, null);
            if (noReport == null) {
                throw new AssertionError("nil report remains tolerant " + name);
            }
        }
    }

    public static void main(String[] args) {
%s        System.out.println("BLOB REPORT OK " + checks);
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
