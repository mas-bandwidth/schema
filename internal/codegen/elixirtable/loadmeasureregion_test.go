package elixirtable

// TestLoadMeasureCoversFramingElixir pins docs/SPEC-TABLES.md §4.2, §6.3 and
// §6.5 on the Elixir variable-form emitter: load_measure/2 answers the region
// the framing commands, not merely the bytes a pristine value happens to use.
// Every mutant below is a wire-fuzz red the oracle internal/tablewire reads
// exactly. The file form is held to FileRegionMeasure, the message form to the
// oracle's batch read and MessageNodeRecords region; the report and the saved
// bytes are held to the oracle too.

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type loadMeasureMutant struct {
	name, unit, root, hex string
	message               bool
}

var loadMeasureMutants = []loadMeasureMutant{
	{"graphdemo.Scene", "graphdemo", "Scene", "010f0c0474726565020402000000031102041104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000", false},
	{"blobdemo.Catalog", "blobdemo", "Catalog", "010c0c03617274021102031106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000", false},
	{"tblw1.Fleet", "tblw1", "Fleet", "010b0c0661726d616461021102030e0411020302040c3202052c060c064175726f7261070e0506037a7a7108090200000000000000090d070a0c03766970000b04fa000000000501000029601b0c6c2931da5d81b3a4c4a0df6392e316b22ebbe524ffffffffffffffff7b7b5dc28e0c0d11861b638ebaadbcc4f3a4484419abd756f85775e8906b5ab5185509fe81a9bd103d62cb8fecfcf739f6adda11cc8cda800b00000000000000", false},
	{"tblw2.Fleet", "tblw2", "Fleet", "010b0c0661726d616461021102030e0411020302040c3202052c060c064175726f7261070e0506037a7a7108090200000000000000090d070a0c03766970000b04fa000000000501000029601b0c6c2931da5d81b3a4c4a0df6392e316b22ebbe524ffffffffffffffff7b7b5dc28e0c0d11861b638ebaadbcc4f3a4484419abd756f85775e8906b5ab5185509fe81a9bd103d62cb8fecfcf739f6adda11cc8cda800b00000000000000", false},
	{"listdemo.Save", "listdemo", "Save", "01090e380d030d020a0000803f0308030000000013020a00000040040a000000400308040000000013020a00004040040a0000804003080500000000050e051103020200060e0e04030a000000140000001e000000070c0a0108070908070000000000094b4d57aa3347d2071702864cf563af3a706e3e9343e59d541502864cf463af3154af1d19735012b20f40270b6b9801ffffffffffffffff5f8258ac3615785e8476bc2eef83761e0900000000000000", false},
	{"listdemo.Album", "listdemo", "Album", "01050e051103020200020c1001030d0408800200000508e0010000000030b13aff4ad9b140ffffffffffffffffc3648950d28da7f1bfe9d12f93cddadb2272347df60b72170500000000000000", false},
	{"listdemo.Sheet", "listdemo", "Sheet", "01070e530d030e020e040d01010003040a000000001c020e120d020704046400000000070404650000000003040b0000000024020e1a0d03070404c800000000070404c900000000070404ca0000000003040c00000000051102060c1f01071c020e120d0207040401000000000704040200000000030463000000000038810af11f06a7a36f2c414fbf84783e3d62cb8fecfcf739090602864ceb63af0f62ad077747825ffffffffffffffffffb06c9fe19e113a00700000000000000", false},
}

func loadMeasureUnit(t *testing.T, root, unit string) *ir.Unit {
	t.Helper()
	switch unit {
	case "graphdemo":
		return loadUnit(t, filepath.Join(root, "tables", "pointers", "Graph.schema"), filepath.Join(root, "tables", "pointers", "Marks.schema"), filepath.Join(root, "tables", "pointers", "Parts.schema"))
	case "blobdemo":
		return loadUnit(t, filepath.Join(root, "tables", "blobs", "Assets.schema"))
	case "tblw1":
		return loadUnit(t, filepath.Join(root, "test", "tables", "W1.schema"))
	case "tblw2":
		return loadUnit(t, filepath.Join(root, "test", "tables", "W2.schema"))
	case "listdemo":
		return loadUnit(t,
			filepath.Join(root, "tables", "lists", "Save.schema"),
			filepath.Join(root, "tables", "lists", "Shared.schema"),
			filepath.Join(root, "tables", "lists", "Report.schema"),
			filepath.Join(root, "tables", "lists", "Holders.schema"),
			filepath.Join(root, "tables", "lists", "Migrate.schema"))
	}
	t.Fatalf("no paths for unit %s", unit)
	return nil
}

func messageRegion(t *testing.T, u *ir.Unit, st *ir.Struct, data []byte) int64 {
	t.Helper()
	var v tablewire.Vocabulary
	if err := v.AnnounceRead(ir.TableAnnouncement(u), &tabletext.Report{}); err != nil {
		t.Fatalf("announcement: %v", err)
	}
	align := ir.TableRegionAlign(u)
	round := func(n int64) int64 { return (n + align - 1) & -align }
	size := map[uint64]int64{}
	for _, r := range ir.PointerReachable(st) {
		size[ir.TableWireId(r.WireName())] = round(ir.RecordLayout(u, r).Size)
	}
	bodies, whole := tablewire.MessageNodeRecords(data, &v)
	if !whole {
		t.Fatalf("the oracle's message scan is not whole")
	}
	total := int64(0)
	for _, records := range bodies {
		total += round(ir.RecordLayout(u, st).Size) + int64(len(records)+1)*16
		for _, rec := range records {
			if rec.Blob {
				term := int64(0)
				if rec.TypeId == ir.StringWireTypeId {
					term = 1
				}
				total += round(8 + rec.Length + term)
				continue
			}
			total += size[rec.TypeId]
		}
	}
	return total
}

func TestLoadMeasureCoversFramingElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]*ir.Unit{}
	files := map[string]map[string][]byte{}
	dirs := map[string]string{}
	names := map[string][]string{}
	for _, m := range loadMeasureMutants {
		if units[m.unit] != nil {
			continue
		}
		u := loadMeasureUnit(t, root, m.unit)
		units[m.unit] = u
		files[m.unit] = generateElixir(t, u)
		dirs[m.unit] = t.TempDir()
		names[m.unit] = writeModuleDir(t, dirs[m.unit], files[m.unit])
	}
	for _, m := range loadMeasureMutants {
		m := m
		t.Run(m.name, func(t *testing.T) {
			data, err := hex.DecodeString(m.hex)
			if err != nil {
				t.Fatal(err)
			}
			u := units[m.unit]
			st := u.Tables[m.root]
			if st == nil {
				st = u.Structs[m.root]
			}
			var want int64
			if m.message {
				want = messageRegion(t, u, st, data)
			} else {
				n, _, refused := tablewire.FileRegionMeasure(u, st, data)
				if refused {
					t.Fatalf("the oracle refused the framing")
				}
				want = n
			}
			probe := messageProbe(m, data)
			out := runProbe(t, elixirBin, dirs[m.unit], names[m.unit], probe)
			lines := strings.Split(strings.TrimSpace(out), "\n")
			got, err := strconv.ParseInt(strings.TrimSpace(lines[len(lines)-1]), 10, 64)
			if err != nil {
				t.Fatalf("the leg's measure is not an integer: %q", out)
			}
			if got != want {
				t.Fatalf("LoadMeasure asks for %d region bytes; the framing commands exactly %d", got, want)
			}
		})
	}
}

func messageProbe(m loadMeasureMutant, data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	if m.message {
		return fmt.Sprintf(`
b = Base.decode64!(%q)
{:ok, v} = Graphdemo.TableMessage.announce_read(Graphdemo.TableMessage.announce())
{:ok, bodies} = Graphdemo.TableMessage.load_messages(v, b)
{:ok, n} = Graphdemo.TableMessage.measure_messages(v, bodies)
IO.puts(n)
`, enc)
	}
	return fmt.Sprintf(`
b = Base.decode64!(%q)
n = %s.Table.load_measure(%q, b)
IO.puts(n)
`, enc, strings.Title(m.unit), m.root)
}
