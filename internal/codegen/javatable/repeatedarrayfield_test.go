package javatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJavaFixRepeatedArrayFieldKeepsDecoded verifies that when a fixed-length
// array field ([N]T) appears twice in a variable-form body and the second
// occurrence is damaged before reaching an element, the elements the earlier
// occurrence already decoded stay where the Go oracle keeps them
// (docs/SPEC-TABLES.md §3.3, §4; internal/tablewire/decode.go arrayBody).
func TestJavaFixRepeatedArrayFieldKeepsDecoded(t *testing.T) {
	t.Parallel()
	javac, java := javaTools(t)
	repo := filepath.Join("..", "..", "..")

	cases := []struct {
		name string
		dir  string
		root string
		hex  string
	}{
		{
			name: "JD1_MatchResult_random_14791",
			dir:  "backend",
			root: "MatchResult",
			hex:  "010109010101010101016b020ed1010d0a110309e8030000000000000404d204000000140309e92b00000000000004041f05000005060200140309ea0300000000000004046c05000005060300140309eb030000000000000404b905000005060400140309ec0300000000000004040606000005060500140309ed0300000000000004045306000005060600140309ee030000000000000404a006000005060700140309ef030000000000000404ed06000005060800140353f00300000000000004043a07000005060900140309f10300000000000004048707000005060a00020ed1010d0a110309e8030000000000000404d204000000140309e90300000000000004041f05000005060200140309ea0300000000000004046c05000005060300140309eb030000000000000404b905000005060400140309ec0300000000000004040606000005060500140309ed0300000000000004045306000005060600140309ee030000000000000404a006000005060700140309ef030000000000000404ed06000005060800ae0309f00300000000000004043a07000005060900140309f10300000000000004048707000005060a0000266814ba79e336f3298b551875ace39838c48d74ffe34ac1f524f6e7fb630ec1204a75304802a3980500000000000000",
		},
		{
			name: "JD2_LoadoutConfig_duplicate_damaged_1",
			dir:  "examples",
			root: "LoadoutConfig",
			hex:  "01011e02030e051e03040502060e051e0300020007090100000000000000080d16090a000048410a01010b0f0c0d070d0a0000003f00000e0e090d020100040f063f000e0e090d027f00040f063f00100e100d010d110406000000120a0000803e0000d48ac477299ba83291c5c4aa397792c4c599f782764e0ad9b318320d7ec916c48025f1eade1ae5c34925d7235af46fb44ae5967084f5064b39e983f77f1331bfc07fb38abe08637f593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9a24cc8a11f5f028dea4edcad59a3e01a5419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee1200000000000000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			u := unitFromDir(t, filepath.Join(repo, "tables", tc.dir))
			model := tabletext.NewModel(u)
			inst := model.New(u.Tables[tc.root])
			var orep tabletext.Report
			if _, err := tablewire.Decode(model, inst, wire, &orep); err != nil {
				t.Fatalf("oracle Decode: %v", err)
			}
			wantSaved, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("oracle Encode: %v", err)
			}
			wantVerdict := fmt.Sprintf("%d,%d,%d,%d,%d,%v,%v",
				orep.Unknown, orep.KindMismatch, orep.Widened, orep.Clamped, orep.Duplicate, orep.Malformed, orep.Refused)

			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
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
			inPath := filepath.Join(td, "in.bin")
			outPath := filepath.Join(td, "out.bin")
			if err := os.WriteFile(inPath, wire, 0o644); err != nil {
				t.Fatal(err)
			}
			driver := filepath.Join(td, "Driver.java")
			dsrc := fmt.Sprintf(`import java.nio.file.*;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%sVariable.Value v = %s.%sVariable.load(data, report);
        System.out.printf("%%d,%%d,%%d,%%d,%%d,%%b,%%b%%n",
            report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, report.malformed, report.refused);
        if (v != null && !report.refused) {
            byte[] saved = %s.%sVariable.save(v);
            Files.write(Paths.get(args[1]), saved);
        }
    }
}`, pkg, pkg, pkg, tc.root, pkg, tc.root, pkg, tc.root)
			if err := os.WriteFile(driver, []byte(dsrc), 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, driver)
			args := append([]string{"--release", "17", "-d", classes}, sources...)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("javac: %v\n%s", err, out)
			}
			out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", inPath, outPath).CombinedOutput()
			if err != nil {
				t.Fatalf("java: %v\n%s", err, out)
			}
			// legLine drops the JVM's stderr "Picked up JAVA_TOOL_OPTIONS" banner
			// before the verdict line (loginrequestkind_test.go).
			gotVerdict := legLine(string(out))
			if gotVerdict != wantVerdict {
				t.Errorf("%s: verdict got %q, want %q", tc.name, gotVerdict, wantVerdict)
			}
			gotSaved, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotSaved, wantSaved) {
				t.Errorf("%s: saved bytes (%d vs %d) differ:\ngot:  %x\nwant: %x", tc.name, len(gotSaved), len(wantSaved), gotSaved, wantSaved)
			}
		})
	}
}
