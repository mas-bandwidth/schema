package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsonHostileReportClampCounterJava pins the Java text reader's `clamped`
// counter to the Go oracle (internal/tabletext's Report) on every json-hostile
// row of testdata/conformance/tables/MANIFEST.txt whose oracle report counts a
// clamp (docs/SPEC-TABLES.md §16.2, §16.3, §17.5). The Java report's clamped
// count and the verdict (read vs refused) must equal the oracle's, one subtest
// per row.
func TestJsonHostileReportClampCounterJava(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")

	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// The manifest declares each unit's key and the directory (or single
	// schema file) it is built from, above the rows that name the key.
	unitPath := map[string]string{}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" {
			unitPath[fields[1]] = fields[2]
		}
	}

	type row struct {
		name string
		key  string
		root string
		text []byte
		want tabletext.Report
		ok   bool
	}
	var rows []row
	loadedUnits := map[string]*ir.Unit{}

	// The oracle is the authority on which rows count a clamp: read every
	// row's text through tabletext and keep the ones whose report does.
	for line := range strings.SplitSeq(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != "json-hostile" {
			continue
		}
		key, root := fields[2], fields[3]
		p, ok := unitPath[key]
		if !ok {
			t.Fatalf("row %s names unit %s with no `unit` line", fields[1], key)
		}
		u, ok := loadedUnits[key]
		if !ok {
			u = unitFromDir(t, filepath.Join(repo, p))
			loadedUnits[key] = u
		}
		table := u.Tables[root]
		if table == nil {
			t.Fatalf("row %s: unit %s has no table %s", fields[1], u.Package, root)
		}
		text, err := os.ReadFile(filepath.Join(repo, fields[4], root+".json"))
		if err != nil {
			t.Fatalf("row %s: read %s: %v", fields[1], fields[4], err)
		}
		model := tabletext.NewModel(u)
		inst := model.New(table)
		rep := new(tabletext.Report)
		okRead := model.Read(inst, text, rep)
		if rep.Clamped == 0 {
			continue
		}
		rows = append(rows, row{fields[1], key, root, text, *rep, okRead})
	}
	if len(rows) == 0 {
		t.Fatal("manifest has no json-hostile row the oracle counts a clamp on")
	}

	// Generate each named unit once, in its own package directory so the
	// per-package runtimes cannot overwrite one another.
	tmp := t.TempDir()
	classes := filepath.Join(tmp, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	packages := map[string]string{}
	for _, r := range rows {
		if _, ok := packages[r.key]; ok {
			continue
		}
		u := loadedUnits[r.key]
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate %s: %v", r.key, err)
		}
		packet, err := javacodegen.Generate(u)
		if err != nil {
			t.Fatalf("generate packet %s: %v", r.key, err)
		}
		for _, name := range []string{"Int128.java", "UInt128.java"} {
			if data := packet[name]; data != nil && files[name] == nil {
				files[name] = data
			}
		}
		pkg := javaPackageOf(u)
		packages[r.key] = pkg
		dir := filepath.Join(tmp, "src", pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, p)
		}
	}

	driver := `
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        String pkg = args[0], root = args[1];
        byte[] text = Files.readAllBytes(Paths.get(args[2]));
        Class<?> codec = Class.forName(pkg + "." + root + "Variable");
        Class<?> reportT = Class.forName(pkg + ".TableVariable$Report");
        Object report = reportT.getConstructor().newInstance();
        Object v = codec.getMethod("fromJson", byte[].class, reportT).invoke(null, text, report);
        System.out.println("R "
            + reportT.getField("unknown").getInt(report) + ","
            + reportT.getField("kindMismatch").getInt(report) + ","
            + reportT.getField("widened").getInt(report) + ","
            + reportT.getField("clamped").getInt(report) + ","
            + reportT.getField("duplicate").getInt(report) + ","
            + reportT.getField("malformed").getBoolean(report) + ","
            + reportT.getField("refused").getBoolean(report));
        if (v == null) System.out.println("V null"); else System.out.println("V ok");
    }
}
`
	dpath := filepath.Join(tmp, "Driver.java")
	if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dpath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			tp := filepath.Join(t.TempDir(), "row.json")
			if err := os.WriteFile(tp, r.text, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", packages[r.key], r.root, tp).CombinedOutput()
			if err != nil {
				t.Fatalf("java: %v\n%s", err, out)
			}
			var javaFields []string
			javaValue := ""
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "R "); ok {
					javaFields = strings.Split(after, ",")
				}
				if after, ok := strings.CutPrefix(line, "V "); ok {
					javaValue = after
				}
			}
			if len(javaFields) != 7 {
				t.Fatalf("java report shape for %s: %q\n%s", r.name, strings.Join(javaFields, ","), out)
			}
			gotClamped := javaFields[3]
			wantClamped := fmt.Sprintf("%d", r.want.Clamped)
			if gotClamped != wantClamped {
				t.Errorf("row %s: java clamped=%s oracle clamped=%s (java %q, oracle %+v)", r.name, gotClamped, wantClamped, strings.Join(javaFields, ","), r.want)
			}
			// The oracle's verdict is Read's bool: on the text form a refusal
			// is a malformed read (§16.2), not a form-byte Refused. The Java
			// reader reports the same damage as malformed, so the two verdicts
			// are `malformed == !ok`, and a read must also hand back a value.
			gotMalformed := javaFields[5]
			wantMalformed := fmt.Sprintf("%t", !r.ok)
			if gotMalformed != wantMalformed {
				t.Errorf("row %s: java malformed=%s, oracle read ok=%t (java %q, oracle %+v)", r.name, gotMalformed, r.ok, strings.Join(javaFields, ","), r.want)
			}
			if r.ok && javaValue != "ok" {
				t.Errorf("row %s: oracle read a value but java returned %q (java %q, oracle %+v)", r.name, javaValue, strings.Join(javaFields, ","), r.want)
			}
		})
	}
}
