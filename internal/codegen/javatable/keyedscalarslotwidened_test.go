package javatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A keyed array whose element kind is narrower than the reader's declaration
// counts ONE widened for the field and decodes each scalar or enum slot at the
// wire kind's width while advancing past the slot by its length L; only a
// table slot whose body stops short of L is malformed (docs/SPEC-TABLES.md
// §3.2, §4).
func TestJavaFixKeyedScalarSlotWidened(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		dir   string
		table string
		wire  string
	}{
		{
			name:  "JK1a_mapdemo_Slots",
			dir:   filepath.Join("tables", "maps"),
			table: "Slots",
			wire:  "01010e340d02180204feffffff03100e0202040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000",
		},
		{
			name:  "JK1b_tabledemo_KeyedConfig",
			dir:   filepath.Join("tables", "examples"),
			table: "KeyedConfig",
			wire:  "010110220d02020d030408000000040c0372656400050f030402000000040c05677265656e000610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a000020420d0d0100000e1a0c0a0000a0400f0a000010410d0d0a100a0000403f1101010000001207090a0000404100130d1214100e020202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0ca0c7b1daa0bc1c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bcb002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000",
		},
	}
	javac, java := javaTools(t)
	repo := filepath.Join("..", "..", "..")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromDir(t, filepath.Join(repo, tc.dir))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			varClass := tc.table + "Variable"
			if files[varClass+".java"] == nil {
				t.Fatalf("%s.java is not emitted", varClass)
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
			classes := filepath.Join(t.TempDir(), "classes")
			if err := os.MkdirAll(classes, 0o755); err != nil {
				t.Fatal(err)
			}
			srcdir := t.TempDir()
			var sources []string
			for name, data := range files {
				path := filepath.Join(srcdir, name)
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, path)
			}
			mutant, err := hex.DecodeString(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			wp := filepath.Join(t.TempDir(), "mutant.bin")
			if err := os.WriteFile(wp, mutant, 0o644); err != nil {
				t.Fatal(err)
			}
			driver := fmt.Sprintf(`
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%s.Value v = %s.%s.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        if (v == null) System.out.println("J null"); else { System.out.print("J "); System.out.write(%s.%s.toJson(v)); }
    }
}
`, pkg, pkg, pkg, varClass, pkg, varClass, pkg, varClass)
			dpath := filepath.Join(srcdir, "Driver.java")
			if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, dpath)
			sort.Strings(sources)
			args := append([]string{"--release", "17", "-d", classes}, sources...)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("javac: %v\n%s", err, out)
			}
			out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wp).CombinedOutput()
			if err != nil {
				t.Fatalf("java: %v\n%s", err, out)
			}
			firstNewline := bytes.IndexByte(out, '\n')
			if firstNewline < 0 || !bytes.HasPrefix(out[:firstNewline], []byte("R ")) {
				t.Fatalf("no java report in:\n%s", out)
			}
			javaReport := strings.TrimSpace(string(out[2:firstNewline]))
			rest := out[firstNewline+1:]
			if !bytes.HasPrefix(rest, []byte("J ")) {
				t.Fatalf("no java json in:\n%s", out)
			}
			javaJSON := rest[2:]

			model := tabletext.NewModel(u)
			want := new(tabletext.Report)
			value := model.New(u.Tables[tc.table])
			ok, err := tablewire.Decode(model, value, mutant, want)
			if !ok || err != nil {
				t.Fatalf("oracle decode: ok=%v err=%v report=%+v", ok, err, want)
			}
			if want.Widened != 1 || want.Malformed {
				t.Fatalf("test setup: oracle report %+v, want widened=1 and malformed=false", want)
			}
			wantJSON, err := model.Write(value)
			if err != nil {
				t.Fatalf("oracle write: %v", err)
			}
			gotFields := strings.Split(javaReport, ",")
			if len(gotFields) != 7 {
				t.Fatalf("java report shape: %q", javaReport)
			}
			wantFields := []string{
				fmt.Sprintf("%d", want.Unknown),
				fmt.Sprintf("%d", want.KindMismatch),
				fmt.Sprintf("%d", want.Widened),
				fmt.Sprintf("%d", want.Clamped),
				fmt.Sprintf("%d", want.Duplicate),
				fmt.Sprintf("%t", want.Malformed),
				fmt.Sprintf("%t", want.Refused),
			}
			for i := range wantFields {
				if gotFields[i] != wantFields[i] {
					t.Errorf("report field %d: java=%s oracle=%s (java report %q, oracle %+v)", i, gotFields[i], wantFields[i], javaReport, want)
				}
			}
			if string(javaJSON) != string(wantJSON)+"\n" {
				t.Errorf("json mismatch:\njava:\n%s\noracle:\n%s", javaJSON, wantJSON)
			}
		})
	}
}
