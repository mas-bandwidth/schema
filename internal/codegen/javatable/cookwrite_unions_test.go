package javatable

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

var cookWriteUnionsAndWideKinds = []cookWriteUnit{
	{"../../../tables/examples", "tabledemo", "tabledemo", []cookWriteRoot{
		{"RootConfig", []string{"root_full", "root_default"}},
		{"ProfileConfig", []string{"profile_elide"}},
		{"LoadoutConfig", []string{"loadout_full"}},
		{"ArchiveConfig", []string{"archive"}},
	}},
	{"../../../test/tables/V1.schema", "tblv1", "tblv1", []cookWriteRoot{
		{"Cfg", []string{"v1_cfg", "v1_seams"}},
	}},
	{"../../../test/tables/V2.schema", "tblv2", "tblv2", []cookWriteRoot{
		{"Cfg", []string{"v2_cfg", "v2_seams"}},
	}},
	{"../../../tables/messages", "messagedemo", "messagedemo", []cookWriteRoot{
		{"ToolMessage", []string{"message_open", "message_transaction", "message_ping", "message_save",
			"message_empty", "message_batch", "message_arms", "message_arm_edits", "message_ack", "message_trace"}},
	}},
	{"../../../tables/stream", "streamdemo", "streamdemo", []cookWriteRoot{
		{"Feed", []string{"stream_chain", "stream_header", "stream_parts", "stream_arm_first", "stream_arm_pointer"}},
	}},
	{"../../../test/tables/M1.schema", "tblm1", "tblm1", []cookWriteRoot{
		{"Msg", []string{"m1_open"}},
	}},
	{"../../../test/tables/M2.schema", "tblm2", "tblm2", []cookWriteRoot{
		{"Msg", []string{"m2_open"}},
	}},
	{"../../../tables/scalars", "scalars", "scalardemo", []cookWriteRoot{
		{"SimState", []string{"scalars_full", "scalars_default", "scalars_edges"}},
	}},
}

func cookUnitFilesWithPacketSupport(t *testing.T, schema string) map[string][]byte {
	t.Helper()
	dir := schema
	if strings.HasSuffix(schema, ".schema") {
		dir = t.TempDir()
		b, err := os.ReadFile(schema)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(schema)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u := unitFromDir(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", schema, err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("packet generate %s: %v", schema, err)
	}
	for _, support := range []string{"Int128.java", "UInt128.java"} {
		if body, ok := pkt[support]; ok {
			files[support] = body
		}
	}
	return files
}

func TestCookWriteUnionsAndWideKindsJava(t *testing.T) {
	t.Parallel()
	m := readCookManifest(t)
	javac, javaBin := javaTools(t)
	repo := filepath.Join("..", "..", "..")
	for _, u := range cookWriteUnionsAndWideKinds {
		u := u
		t.Run(u.unit, func(t *testing.T) {
			files := cookUnitFilesWithPacketSupport(t, u.schema)
			drivers := map[string]string{}
			for _, r := range u.roots {
				drivers["CookGolden"+r.root] = cookDriverSource("CookGolden"+r.root, u.pkg, r.root)
			}
			dir := compileCookUnit(t, javac, files, u.pkg, drivers)

			schemaDir := u.schema
			if strings.HasSuffix(schemaDir, ".schema") {
				schemaDir = t.TempDir()
				b, err := os.ReadFile(u.schema)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(schemaDir, filepath.Base(u.schema)), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			irUnit := unitFromDir(t, schemaDir)
			model := tabletext.NewModel(irUnit)

			for _, r := range u.roots {
				r := r
				t.Run(r.root, func(t *testing.T) {
					st := irUnit.Tables[r.root]
					if st == nil {
						t.Fatalf("missing root table %s in %s", r.root, u.unit)
					}
					for _, row := range r.rows {
						row := row
						t.Run(row, func(t *testing.T) {
							instMeta, cookMeta := m.instance[row], m.cook[row]
							wirePath := filepath.Join(repo, instMeta[2])
							wireBytes, err := os.ReadFile(wirePath)
							if err != nil {
								t.Fatal(err)
							}
							inst := model.New(st)
							var rep tabletext.Report
							if ok, err := tablewire.Decode(model, inst, wireBytes, &rep); !ok || err != nil || rep.Refused || rep.Malformed {
								t.Fatalf("decode %s: ok=%v err=%v refused=%v malformed=%v", row, ok, err, rep.Refused, rep.Malformed)
							}
							wantLittle, err := tablecook.Cook(model, inst, tablecook.Options{Big: false})
							if err != nil {
								t.Fatalf("tablecook little %s: %v", row, err)
							}
							wantBig, err := tablecook.Cook(model, inst, tablecook.Options{Big: true})
							if err != nil {
								t.Fatalf("tablecook big %s: %v", row, err)
							}
							littleFile, err := os.ReadFile(filepath.Join(repo, cookMeta[0]))
							if err != nil {
								t.Fatal(err)
							}
							bigFile, err := os.ReadFile(filepath.Join(repo, cookMeta[1]))
							if err != nil {
								t.Fatal(err)
							}
							if !bytes.Equal(wantLittle, littleFile) || !bytes.Equal(wantBig, bigFile) {
								t.Fatalf("%s: tablecook.Cook disagrees with committed cook golden", row)
							}
							cmd := exec.Command(javaBin, "-ea", "-cp", dir, "CookGolden"+r.root,
								wirePath, filepath.Join(repo, cookMeta[0]), filepath.Join(repo, cookMeta[1]))
							if out, err := cmd.CombinedOutput(); err != nil {
								t.Fatalf("java cook %s: %v\n%s", row, err, out)
							}
						})
					}
				})
			}
			// The Frame arm whose own wire name is "tag" (SPEC-TABLES §2.6):
			// its Value member is tagArm, so the cook descriptor must name the
			// member the generated class declares. No manifest row selects this
			// arm, so the regression is pinned here against tablecook.
			if u.unit == "streamdemo" {
				t.Run("stream_arm_tag", func(t *testing.T) {
					inst := model.New(irUnit.Tables["Feed"])
					var rep tabletext.Report
					if !model.Read(inst, []byte(`{"id":9,"frame":{"tag":7}}`), &rep) || rep.Malformed || rep.Refused {
						t.Fatalf("read stream_arm_tag: malformed=%v refused=%v", rep.Malformed, rep.Refused)
					}
					wireBytes, err := tablewire.Encode(model, inst)
					if err != nil {
						t.Fatal(err)
					}
					wantLittle, err := tablecook.Cook(model, inst, tablecook.Options{Big: false})
					if err != nil {
						t.Fatal(err)
					}
					wantBig, err := tablecook.Cook(model, inst, tablecook.Options{Big: true})
					if err != nil {
						t.Fatal(err)
					}
					td := t.TempDir()
					wirePath := filepath.Join(td, "stream_arm_tag.bin")
					littlePath := filepath.Join(td, "stream_arm_tag.cook")
					bigPath := filepath.Join(td, "stream_arm_tag-be.cook")
					for path, data := range map[string][]byte{
						wirePath: wireBytes, littlePath: wantLittle, bigPath: wantBig,
					} {
						if err := os.WriteFile(path, data, 0o644); err != nil {
							t.Fatal(err)
						}
					}
					cmd := exec.Command(javaBin, "-ea", "-cp", dir, "CookGoldenFeed",
						wirePath, littlePath, bigPath)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("java cook stream_arm_tag: %v\n%s", err, out)
					}
				})
			}
		})
	}
}
