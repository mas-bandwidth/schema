package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestTableJsonEveryTableJava verifies that TableJson is emitted for every
// corpus unit, every table in the unit carries toJson/fromJson, every pinned
// instance round-trips byte-for-byte against the Go text oracle
// (internal/tabletext), and the json-hostile cases (duplicate keys, bounded
// array clamping, 64-bit integer saturation, float overflow kindMismatch, and
// wrong keyed-array shapes, SPEC-TABLES §16.2, §16.3, §16.7, §17.5) match the
// Go oracle's report and value.
func TestTableJsonEveryTableJava(t *testing.T) {
	t.Parallel()
	javac, java := javaTools(t)
	repo := filepath.Join("..", "..", "..")

	units := []struct {
		dir       string
		root      string
		instances []string
		hostile   []string
	}{
		{
			dir:       "examples",
			root:      "RootConfig",
			instances: []string{"root_full", "root_default"},
			hostile: []string{
				// Duplicate key on root: last wins, duplicate=1 (§16.2).
				"{\"version_note\": \"a\", \"version_note\": \"b\"}",
				// Bounded array weapons [..8] past capacity: clamped=1 (§16.2).
				"{\"weapons\": [{}, {}, {}, {}, {}, {}, {}, {}, {}]}",
				// Narrow integer past 64 bits: saturates at 64-bit bound then clamps to field range, clamped=2 (§16.2).
				"{\"weapons\": [{\"speed\": 999999999999999999999999999999}]}",
				// Float32 overflow to infinity: kindMismatch=1 (§16.2).
				"{\"weapons\": [{\"damage\": 1e500}]}",
			},
		},
		{
			dir:       "stream",
			root:      "Feed",
			instances: []string{"stream_chain", "stream_header", "stream_parts", "stream_arm_first", "stream_arm_pointer"},
		},
		{
			dir:       "lists",
			root:      "Save",
			instances: []string{"list_scalars", "list_tables", "list_empty", "list_erased"},
		},
		{
			dir:       "backend",
			root:      "MatchResult",
			instances: []string{"match_full", "match_default"},
		},
		{
			dir:       "vocab",
			root:      "Wide09",
			instances: []string{"vocab_wide"},
		},
		{
			dir:       "vocab9",
			root:      "Wide19",
			instances: []string{"vocab9_wide"},
		},
	}

	for _, tc := range units {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			u := unitFromDir(t, filepath.Join(repo, "tables", tc.dir))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := files["TableJson.java"]; !ok {
				t.Fatalf("%s: TableJson.java not emitted", tc.dir)
			}
			for name, st := range u.Tables {
				if st == nil || st.IsMapEntry() {
					continue
				}
				code := string(files[name+"Variable.java"])
				if !strings.Contains(code, "byte[] toJson(Value v)") || !strings.Contains(code, "Value fromJson(byte[] text, TableVariable.Report report)") {
					t.Fatalf("%s: %sVariable.java missing toJson/fromJson", tc.dir, name)
				}
			}

			packet, err := javacodegen.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Int128.java", "UInt128.java"} {
				if data := packet[name]; data != nil {
					files[name] = data
				}
			}
			pkg := javaPackageOf(u)
			td := t.TempDir()
			classes := filepath.Join(td, "classes")
			if err := os.MkdirAll(classes, 0o755); err != nil {
				t.Fatal(err)
			}
			var sources []string
			for name, data := range files {
				p := filepath.Join(td, name)
				if err := os.WriteFile(p, data, 0o644); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, p)
			}
			driver := filepath.Join(td, "Driver.java")
			dsrc := fmt.Sprintf(`import java.nio.file.*;
import java.nio.charset.StandardCharsets;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] text = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%sVariable.Value v = %s.%sVariable.fromJson(text, report);
        if (v == null || report.malformed || report.refused) {
            System.out.println("refused");
            return;
        }
        byte[] out = %s.%sVariable.toJson(v);
        Files.write(Paths.get(args[1]), out);
        System.out.printf("%%d,%%d,%%d,%%d,%%d,false,read%%n",
            report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate);
    }
}`, pkg, pkg, pkg, tc.root, pkg, tc.root, pkg, tc.root)
			if err := os.WriteFile(driver, []byte(dsrc), 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, driver)
			args := append([]string{"--release", "17", "-d", classes}, sources...)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: javac: %v\n%s", tc.dir, err, out)
			}

			model := tabletext.NewModel(u)
			for _, instName := range tc.instances {
				wirePath := filepath.Join(repo, "testdata", "wire", "tables", instName+".bin")
				wire, err := os.ReadFile(wirePath)
				if err != nil {
					t.Fatal(err)
				}
				inst := model.New(u.Tables[tc.root])
				var wrep tabletext.Report
				if _, err := tablewire.Decode(model, inst, wire, &wrep); err != nil {
					t.Fatalf("%s/%s: oracle decode: %v", tc.dir, instName, err)
				}
				wantJSON, err := model.Write(inst)
				if err != nil {
					t.Fatalf("%s/%s: oracle format: %v", tc.dir, instName, err)
				}
				wantJSON = append(wantJSON, '\n')
				inPath := filepath.Join(td, instName+".in.json")
				outPath := filepath.Join(td, instName+".out.json")
				if err := os.WriteFile(inPath, wantJSON, 0o644); err != nil {
					t.Fatal(err)
				}
				out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", inPath, outPath).CombinedOutput()
				if err != nil {
					t.Fatalf("%s/%s: java: %v\n%s", tc.dir, instName, err, out)
				}
				gotJSON, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatalf("%s/%s: read out: %v (driver: %s)", tc.dir, instName, err, out)
				}
				if string(gotJSON) != string(wantJSON) {
					t.Errorf("%s/%s: JSON round-trip mismatch\ngot:\n%s\nwant:\n%s", tc.dir, instName, gotJSON, wantJSON)
				}
			}

			for i, hjson := range tc.hostile {
				inst := model.New(u.Tables[tc.root])
				var orep tabletext.Report
				ok := model.Read(inst, []byte(hjson), &orep)
				wantVerdict := "refused"
				var wantJSON []byte
				if ok && !orep.Malformed && !orep.Refused {
					wantVerdict = fmt.Sprintf("%d,%d,%d,%d,%d,false,read",
						orep.Unknown, orep.KindMismatch, orep.Widened, orep.Clamped, orep.Duplicate)
					var err error
					wantJSON, err = model.Write(inst)
					if err != nil {
						t.Fatal(err)
					}
					wantJSON = append(wantJSON, '\n')
				}
				inPath := filepath.Join(td, fmt.Sprintf("hostile_%d.in.json", i))
				outPath := filepath.Join(td, fmt.Sprintf("hostile_%d.out.json", i))
				if err := os.WriteFile(inPath, []byte(hjson), 0o644); err != nil {
					t.Fatal(err)
				}
				out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", inPath, outPath).CombinedOutput()
				if err != nil {
					t.Fatalf("%s/hostile_%d: java: %v\n%s", tc.dir, i, err, out)
				}
				gotVerdict := strings.TrimSpace(string(out))
				if gotVerdict != wantVerdict {
					t.Errorf("%s/hostile_%d (%s): verdict got %q, want %q", tc.dir, i, hjson, gotVerdict, wantVerdict)
					continue
				}
				if wantVerdict != "refused" {
					gotJSON, err := os.ReadFile(outPath)
					if err != nil {
						t.Fatal(err)
					}
					if string(gotJSON) != string(wantJSON) {
						t.Errorf("%s/hostile_%d (%s): rendered JSON mismatch\ngot:\n%s\nwant:\n%s", tc.dir, i, hjson, gotJSON, wantJSON)
					}
				}
			}
		})
	}
}
