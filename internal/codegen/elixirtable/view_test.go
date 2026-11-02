package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadDirUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(matches) == 0 {
		t.Fatalf("no .schema files in %s", dir)
	}
	sort.Strings(matches)
	return loadUnit(t, matches...)
}

// TestUnitViewDescriptorsElixir holds the Elixir leg's reflection
// (docs/SPEC-TABLES.md §8.1 descriptors with doc comments and tags, §8.2 type
// view, §8.3 unit registry) against EXPLICIT expectations written below: the
// whole listing <Ns>.UnitView prints for tables/examples, tables/arms and
// tables/maps (wantListing*), and whole descriptor lines of tables/examples'
// Table.ex that carry a doc comment and tags (wantExamplesDescriptorLines).
// Every VIEW_CORPUS unit is then also compared with internal/viewlisting's IR
// listing; the five `schema generate --lang elixir` still refuses by name
// (make/elixir.mk VIEW_ELIXIR_REGISTRY_ONLY) are listed from UnitView alone.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestUnitViewDescriptorsElixir(t *testing.T) {
	t.Parallel()

	elixirBin := elixirBinary(t)

	units := []struct {
		dir      string
		pkg      string
		generate bool   // false: the compiler refuses the unit for elixir by name; the registry alone is emitted
		want     string // the explicit listing, where the card pins one
	}{
		{dir: "tables/examples", pkg: "tabledemo", generate: true, want: wantListingTabledemo},
		{dir: "tables/pointers", pkg: "graphdemo", generate: true},
		{dir: "tables/block", pkg: "blockdemo", generate: true},
		{dir: "tables/blockhome", pkg: "blockhome", generate: true},
		{dir: "tables/messages", pkg: "messagedemo"},
		{dir: "tables/stream", pkg: "streamdemo"},
		{dir: "tables/blobs", pkg: "blobdemo", generate: true},
		{dir: "tables/scalars", pkg: "scalardemo", generate: true},
		{dir: "tables/maps", pkg: "mapdemo", generate: true, want: wantListingMapdemo},
		{dir: "tables/lists", pkg: "listdemo", generate: true},
		{dir: "tables/arms", pkg: "armdemo", want: wantListingArmdemo},
		{dir: "tables/backend", pkg: "backenddemo"},
		{dir: "tables/vocab", pkg: "vocabdemo", generate: true},
		{dir: "tables/vocab9", pkg: "vocab9demo", generate: true},
		{dir: "examples-wide", pkg: "wide"},
	}

	driverPath, err := filepath.Abs("../../../test/elixir-view/view_main.exs")
	if err != nil {
		t.Fatalf("abs driver path: %v", err)
	}
	run := func(viewFile string, args ...string) string {
		t.Helper()
		cmd := exec.Command(elixirBin, append([]string{"-r", viewFile, driverPath}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("elixir view_main.exs %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}

	for _, tc := range units {
		u := loadDirUnit(t, filepath.Join("../../..", tc.dir))
		if u.Package != tc.pkg {
			t.Fatalf("%s: package = %q, want %q", tc.dir, u.Package, tc.pkg)
		}
		viewBytes := UnitView(u)
		var out map[string][]byte
		if tc.generate {
			var err error
			if out, err = Generate(u); err != nil {
				t.Fatalf("%s: Generate failed: %v", tc.dir, err)
			}
			if got := string(out[UnitViewModule+".ex"]); got != string(viewBytes) {
				t.Fatalf("%s: Generate's UnitView.ex is not UnitView(u):\n%s", tc.dir, got)
			}
		}

		if tc.pkg == "tabledemo" {
			if want := strings.Join(wantExamplesDescriptorLines, "\n") + "\n"; !strings.Contains(string(out["Table.ex"]), want) {
				t.Fatalf("%s: Table.ex has no descriptor run\n%s", tc.dir, want)
			}
		}

		viewFile := filepath.Join(t.TempDir(), "UnitView.ex")
		if err := os.WriteFile(viewFile, viewBytes, 0o644); err != nil {
			t.Fatalf("%s: write UnitView.ex: %v", tc.dir, err)
		}
		printed := run(viewFile, tc.pkg)
		if tc.want != "" && printed != tc.want {
			t.Fatalf("%s: UnitView listing is not the explicit expectation:\ngot:\n%s\nwant:\n%s", tc.dir, printed, tc.want)
		}
		if pin := viewlisting.Listing(u); printed != pin {
			t.Fatalf("%s: UnitView listing does not match the IR listing:\ngot:\n%s\nwant:\n%s", tc.dir, printed, pin)
		}

		// the negative control: an unflattened doc comment is a damaged listing
		if tc.pkg == "tabledemo" {
			if unflat := run(viewFile, tc.pkg, "--unflattened"); unflat == tc.want {
				t.Fatalf("%s: negative control failed: --unflattened output matched the expected listing", tc.dir)
			}
		}
	}
}

// wantExamplesDescriptorLines are one consecutive run of whole lines of
// tables/examples' Table.ex, in mix format's shape (issue #547): the
// WeaponConfig descriptor with its doc comment and tag, and its damage field
// with a multi-line doc comment (backslash and quotes kept) and three tags
// (docs/SPEC-TABLES.md §8.1).
var wantExamplesDescriptorLines = []string{
	`    "WeaponConfig" => %{`,
	`      name: "WeaponConfig",`,
	`      size: 28,`,
	`      align: 4,`,
	`      type_id: 0x0577780D86C4B0C3,`,
	`      record: 28,`,
	`      node_align: 4,`,
	`      has_extent: false,`,
	`      string_blob: false,`,
	`      variable: false,`,
	`      doc: "One weapon's tuning. A designer edits this table and nothing else.",`,
	`      tags: ["designer_facing"],`,
	`      fields: [`,
	`        %{`,
	`          name: "damage",`,
	`          json: "damage",`,
	`          type_name: "float32",`,
	`          id: 0x7F6308BE8AB37FC0,`,
	`          kind: 10,`,
	`          ordinal: 0,`,
	`          t: :float,`,
	`          width: 4,`,
	`          non_finite: "WeaponConfig.damage",`,
	`          cook_off: 0,`,
	`          count_off: -1,`,
	`          present_off: -1,`,
	`          shape: :value,`,
	`          bound: nil,`,
	`          doc:`,
	`            " Damage per hit, before armor.\n\nThe armor curve is written \"hardness \\ hit\" in the design notes,\nand the backslash is part of the name.",`,
	`          tags: ["ui_slider", "hot", "tuning"],`,
	`          default: 21.0`,
	`        },`,
}

// wantListingTabledemo is the listing tables/examples' UnitView prints
// (docs/SPEC-TABLES.md §8.1-§8.3).
const wantListingTabledemo = `unit package=tabledemo protocol=ba7b344f28584d33
constant MaxBlobBytes file=Wide.schema type=int64 float=false int=70000 real=0000000000000000 tags=[] doc=
constant MaxLoadoutSlots file=Tables.schema type=int64 float=false int=8 real=0000000000000000 tags=[tuning] doc=The slots a loadout admits. Raising it is a free edit on the wire: a\nbound sizes storage, and it is not an id.
constant MaxProfileName file=Tables.schema type=int64 float=false int=32 real=0000000000000000 tags=[] doc=
enum Difficulty file=Pack.schema max=3 bits=8 variants=4 tags=[] doc=
enum Difficulty variant 0 None id=0000000000000000 tags=[] doc=
enum Difficulty variant 1 Easy id=483d9e6c1ccd1f9b tags=[] doc=
enum Difficulty variant 2 Normal id=9ff3121d30f88d52 tags=[] doc=
enum Difficulty variant 3 Hard id=58c7b5d87587287c tags=[] doc=
enum Grade file=Tables.schema max=3 bits=8 variants=4 tags=[progression] doc=A profile's standing, ranked low to high.
enum Grade variant 0 None id=0000000000000000 tags=[] doc=
enum Grade variant 1 Bronze id=c4927739aac4c591 tags=[] doc=
enum Grade variant 2 Silver id=c3e51adeeaf12580 tags=[] doc=
enum Grade variant 3 Gold id=c416c97e0d3218b3 tags=[celebrate,rare,announce] doc=The top rank. A profile that reaches it keeps it.
enum Hull file=Keyed.schema max=3 bits=8 variants=4 tags=[] doc=
enum Hull variant 0 None id=0000000000000000 tags=[] doc=
enum Hull variant 1 Interceptor id=ae61a6cbe88c2cf4 tags=[] doc=
enum Hull variant 2 Gunship id=334d24e1420dbc1f tags=[] doc=
enum Hull variant 3 Freighter id=6c2321a3d00e23db tags=[] doc=
enum ShipType file=Pack.schema max=3 bits=8 variants=4 tags=[] doc=
enum ShipType variant 0 None id=0000000000000000 tags=[] doc=
enum ShipType variant 1 Fighter id=d011f7c3c15285c2 tags=[] doc=
enum ShipType variant 2 Bomber id=a8216aa1c554cb8a tags=[] doc=
enum ShipType variant 3 Scout id=7d573c625719c1a5 tags=[] doc=
enum Team file=Keyed.schema max=3 bits=8 variants=4 tags=[] doc=
enum Team variant 0 None id=0000000000000000 tags=[] doc=
enum Team variant 1 Red id=9ff1de19feac1b7c tags=[] doc=
enum Team variant 2 Blue id=ecf3d3a7c1693e2d tags=[] doc=
enum Team variant 3 Green id=cf00d78fd5953f1c tags=[] doc=
enum Weapon file=Keyed.schema max=3 bits=8 variants=4 tags=[] doc=
enum Weapon variant 0 None id=0000000000000000 tags=[] doc=
enum Weapon variant 1 Cannon id=5854debe3b2e767c tags=[] doc=
enum Weapon variant 2 Missile id=b528592e5a4583e3 tags=[] doc=
enum Weapon variant 3 Mine id=04dc16aea8ff5276 tags=[] doc=
flags Perks file=Tables.schema max=2 bits=8 variants=3 tags=[] doc=
flags Perks bit 0 Shielded id=0000000000000000 tags=[] doc=
flags Perks bit 1 Cloaked id=0000000000000000 tags=[] doc=
flags Perks bit 2 Turbo id=0000000000000000 tags=[] doc=
union Effect file=Tables.schema max=2 bits=8 variants=3 tags=[gameplay] doc=What a weapon does to a target beyond its damage.
union Effect arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Effect arm 1 buff id=ffb5be9be2e469cc payload=Buff record=yes field=no kind=- bound=- overlay=- tags=[positive] doc=A multiplier that rides while the effect is live.
union Effect arm 2 debuff id=13cdc5ede73d2fd7 payload=Debuff record=yes field=no kind=- bound=- overlay=- tags=[] doc=
type Attachment file=Tables.schema fields=2 tags=[] doc=
type Attachment field slot json=slot type=int32 id=6a771618f6fe31d1 optional=false tags=[] doc=
type Attachment field power json=power type=float32 id=eef9d1358ae7b4e6 optional=false tags=[] doc=
type Buff file=Tables.schema fields=1 tags=[] doc=
type Buff field multiplier json=multiplier type=float32 id=9adc623a805c87c6 optional=false tags=[] doc=
type Debuff file=Tables.schema fields=1 tags=[] doc=
type Debuff field amount json=amount type=int32 id=8113fe7ea2b16969 optional=false tags=[] doc=
type ScoreBoard file=Keyed.schema fields=1 tags=[] doc=
type ScoreBoard field per_team json=per_team type=int32 id=f10fad739a0e1660 optional=false tags=[] doc=
table ArchiveConfig file=Nested.schema fields=2 tags=[] doc=
table ArchiveConfig field root json=root type=RootConfig id=a354fd1ff0c467c5 optional=false tags=[] doc=
table ArchiveConfig field count json=count type=int32 id=b1e5e28e4479a274 optional=false tags=[] doc=
table GlobalSettings file=Pack.schema fields=4 tags=[] doc=
table GlobalSettings field tick_rate json=tick_rate type=uint32 id=a65f859b617deaf3 optional=false tags=[] doc=
table GlobalSettings field difficulty json=difficulty type=Difficulty id=467f35a74c6e4a70 optional=false tags=[] doc=
table GlobalSettings field build_note json=build_note type=string id=14ec921cc2549d60 optional=false tags=[] doc=
table GlobalSettings field spawn_delays json=spawn_delays type=float32 id=09ae5613b0051271 optional=false tags=[] doc=
table GunnerConfig file=Keyed.schema fields=2 tags=[] doc=
table GunnerConfig field reaction json=reaction type=float32 id=b75aa3662201646a optional=false tags=[] doc=
table GunnerConfig field tracking json=tracking type=bool id=a6bf719a4602b0bc optional=false tags=[] doc=
table GunnerSettings file=Pack.schema fields=3 tags=[] doc=
table GunnerSettings field reaction json=reaction type=float32 id=b75aa3662201646a optional=false tags=[] doc=
table GunnerSettings field tracking json=tracking type=bool id=a6bf719a4602b0bc optional=false tags=[] doc=
table GunnerSettings field callsign json=callsign type=string id=5bc44627b9848818 optional=false tags=[] doc=
table HullConfig file=Keyed.schema fields=3 tags=[] doc=
table HullConfig field health json=health type=float32 id=7f69d4b5288ba9cf optional=false tags=[] doc=
table HullConfig field mass json=mass type=float32 id=1f3757a2ce7b0ab1 optional=false tags=[] doc=
table HullConfig field turrets json=turrets type=TurretConfig id=84f8260bc283608c optional=false tags=[] doc=
table KeyedConfig file=Keyed.schema fields=3 tags=[] doc=
table KeyedConfig field teams json=teams type=TeamConfig id=baaeb048a5a8fa6d optional=false tags=[] doc=
table KeyedConfig field hulls json=hulls type=HullConfig id=ce0ac3c25694d8ff optional=false tags=[] doc=
table KeyedConfig field scores json=scores type=ScoreBoard id=01986b0b27400fb2 optional=false tags=[] doc=
table LoadoutConfig file=Tables.schema fields=7 tags=[] doc=
table LoadoutConfig field grade json=grade type=Grade id=32a89b2977c48ad4 optional=false tags=[] doc=
table LoadoutConfig field grades json=grades type=Grade id=d90a4e7682f799c5 optional=false tags=[] doc=
table LoadoutConfig field podium json=podium type=Grade id=b46ff45a23d72549 optional=false tags=[] doc=
table LoadoutConfig field perks json=perks type=Perks id=4b06f5847096e54a optional=false tags=[] doc=
table LoadoutConfig field primary json=primary type=WeaponConfig id=bf31137ff783e939 optional=false tags=[] doc=
table LoadoutConfig field backups json=backups type=WeaponConfig id=de28f0f5118acc24 optional=false tags=[] doc=
table LoadoutConfig field attachments json=attachments type=Attachment id=f901aa0340249a41 optional=false tags=[] doc=
table PackConfig file=Pack.schema fields=5 tags=[] doc=
table PackConfig field version json=version type=uint32 id=bb62c62c9808ea37 optional=false tags=[] doc=
table PackConfig field global json=global type=GlobalSettings id=7fb43557b54149ce optional=false tags=[] doc=
table PackConfig field ships json=ships type=ShipEntry id=294a5c4913e1ad44 optional=false tags=[] doc=
table PackConfig field thresholds json=thresholds type=int32 id=9cda940a344e1571 optional=false tags=[] doc=
table PackConfig field reserves json=reserves type=ShipEntry id=77707fccd201c228 optional=false tags=[] doc=
table Patrol file=Guarded.schema fields=6 tags=[] doc=
table Patrol field active json=active type=bool id=6580790b036f0c6f optional=false tags=[] doc=
table Patrol field speed json=speed type=float32 id=2281498aa0200e40 optional=false tags=[] doc=
table Patrol field has_target json=has_target type=bool id=247f0e7f55aacfbd optional=false tags=[] doc=
table Patrol field target_id json=target_id type=int32 id=b7bc9ac015a25050 optional=false tags=[] doc=
table Patrol field wander json=wander type=float32 id=b8c758d8bd1845d4 optional=false tags=[] doc=
table Patrol field note json=note type=string id=3bf8fbbad1587cdd optional=false tags=[] doc=
table ProfileConfig file=Tables.schema fields=13 tags=[] doc=
table ProfileConfig field name json=name type=string id=c4bcadba8e631b86 optional=false tags=[] doc=
table ProfileConfig field icon json=icon type=bytes id=dbff4cc56c2092f0 optional=false tags=[] doc=
table ProfileConfig field experience json=experience type=uint32 id=ab8b6d521f80b969 optional=false tags=[] doc=
table ProfileConfig field tilt json=tilt type=int8 id=1e5088ef2e9bafc6 optional=false tags=[] doc=
table ProfileConfig field heading json=heading type=int16 id=b128829190ca0f75 optional=false tags=[] doc=
table ProfileConfig field timestamp json=timestamp type=int64 id=5ddc92338ef53403 optional=false tags=[] doc=
table ProfileConfig field badge json=badge type=uint8 id=10bda981fe095518 optional=false tags=[] doc=
table ProfileConfig field port json=port type=uint16 id=8c2cdb0da8933fa6 optional=false tags=[] doc=
table ProfileConfig field epoch json=epoch type=uint64 id=fc9697d1196e6ba6 optional=false tags=[] doc=
table ProfileConfig field precision json=precision type=float64 id=288427f5babd05f7 optional=false tags=[] doc=
table ProfileConfig field ratings json=ratings type=float32 id=b921eb8a3d0cb0f9 optional=false tags=[] doc=
table ProfileConfig field has_loadout json=has_loadout type=bool id=a06f5e0a148e253c optional=false tags=[] doc=
table ProfileConfig field loadout json=loadout type=LoadoutConfig id=5759ce7586bbb5a3 optional=false tags=[] doc=
table RangedSigned file=Ranges.schema fields=17 tags=[] doc=
table RangedSigned field i8_span json=i8_span type=int8 id=48121511bf702eb5 optional=false tags=[] doc=
table RangedSigned field i8_low json=i8_low type=int8 id=942bfe3168090f7d optional=false tags=[] doc=
table RangedSigned field i8_high json=i8_high type=int8 id=d182acd105b55dd1 optional=false tags=[] doc=
table RangedSigned field i8_inside json=i8_inside type=int8 id=ef9ded23c8912a81 optional=false tags=[] doc=
table RangedSigned field i16_span json=i16_span type=int16 id=b655574ea23760b4 optional=false tags=[] doc=
table RangedSigned field i16_low json=i16_low type=int16 id=d217b3af8d7a2bde optional=false tags=[] doc=
table RangedSigned field i16_high json=i16_high type=int16 id=90652f70c9127900 optional=false tags=[] doc=
table RangedSigned field i16_inside json=i16_inside type=int16 id=6a659d7c354db158 optional=false tags=[] doc=
table RangedSigned field i32_span json=i32_span type=int32 id=e693bd88532c91d6 optional=false tags=[] doc=
table RangedSigned field i32_low json=i32_low type=int32 id=065ee827cf99fbb4 optional=false tags=[] doc=
table RangedSigned field i32_high json=i32_high type=int32 id=c9b121c4c2a186ee optional=false tags=[] doc=
table RangedSigned field i32_inside json=i32_inside type=int32 id=d363dfa465acf27a optional=false tags=[] doc=
table RangedSigned field i64_span json=i64_span type=int64 id=7549c70700c49d6f optional=false tags=[] doc=
table RangedSigned field i64_low json=i64_low type=int64 id=1cce6617419771db optional=false tags=[] doc=
table RangedSigned field i64_high json=i64_high type=int64 id=3b54fa60620b5597 optional=false tags=[] doc=
table RangedSigned field i64_inside json=i64_inside type=int64 id=d308fcb98ece5d23 optional=false tags=[] doc=
table RangedSigned field edges json=edges type=int16 id=c70cde6b85b6197d optional=false tags=[] doc=
table RangedUnsigned file=Ranges.schema fields=17 tags=[] doc=
table RangedUnsigned field u8_span json=u8_span type=uint8 id=0f8897557f37c021 optional=false tags=[] doc=
table RangedUnsigned field u8_low json=u8_low type=uint8 id=2e17553f8200a2a1 optional=false tags=[] doc=
table RangedUnsigned field u8_high json=u8_high type=uint8 id=a0e5c60df9301b7d optional=false tags=[] doc=
table RangedUnsigned field u8_inside json=u8_inside type=uint8 id=1cb2c073a6f5844d optional=false tags=[] doc=
table RangedUnsigned field u16_span json=u16_span type=uint16 id=4ca0c150b1790960 optional=false tags=[] doc=
table RangedUnsigned field u16_low json=u16_low type=uint16 id=f252136836b04cb2 optional=false tags=[] doc=
table RangedUnsigned field u16_high json=u16_high type=uint16 id=4f37e1f216e7610c optional=false tags=[] doc=
table RangedUnsigned field u16_inside json=u16_inside type=uint16 id=d176b605978f3304 optional=false tags=[] doc=
table RangedUnsigned field u32_span json=u32_span type=uint32 id=200b924de7479cda optional=false tags=[] doc=
table RangedUnsigned field u32_low json=u32_low type=uint32 id=a53d2f2a31b5acb0 optional=false tags=[] doc=
table RangedUnsigned field u32_high json=u32_high type=uint32 id=9759be8ea1a4e3d2 optional=false tags=[] doc=
table RangedUnsigned field u32_inside json=u32_inside type=uint32 id=8dc515fc082e3c0e optional=false tags=[] doc=
table RangedUnsigned field u64_span json=u64_span type=uint64 id=beecef11dbdf8973 optional=false tags=[] doc=
table RangedUnsigned field u64_low json=u64_low type=uint64 id=0117ad66e0fff227 optional=false tags=[] doc=
table RangedUnsigned field u64_high json=u64_high type=uint64 id=f2b45af3b50689db optional=false tags=[] doc=
table RangedUnsigned field u64_inside json=u64_inside type=uint64 id=713e12017eebcaf7 optional=false tags=[] doc=
table RangedUnsigned field counts json=counts type=uint64 id=c341febe5aae51e5 optional=false tags=[] doc=
table RangedWidths file=Ranges.schema fields=6 tags=[] doc=
table RangedWidths field b8 json=b8 type=bits(8) id=08a60c07b54d8dc7 optional=false tags=[] doc=
table RangedWidths field b16 json=b16 type=bits(16) id=ff95701912ad97be optional=false tags=[] doc=
table RangedWidths field b32 json=b32 type=bits(32) id=ff9c5c1912b39470 optional=false tags=[] doc=
table RangedWidths field b64 json=b64 type=bits(64) id=ff92701912ab61e7 optional=false tags=[] doc=
table RangedWidths field b12 json=b12 type=bits(12) id=ff95741912ad9e8a optional=false tags=[] doc=
table RangedWidths field b48 json=b48 type=bits(48) id=ff8b681912a535a1 optional=false tags=[] doc=
table RootConfig file=Tables.schema fields=3 tags=[] doc=
table RootConfig field version_note json=version_note type=string id=8a67c9728746d394 optional=false tags=[] doc=
table RootConfig field weapons json=weapons type=WeaponConfig id=41cbd901b87fabb6 optional=false tags=[] doc=
table RootConfig field profiles json=profiles type=ProfileConfig id=8181e61fc0436767 optional=false tags=[] doc=
table ShipEntry file=Pack.schema fields=5 tags=[] doc=
table ShipEntry field display_name json=name type=string id=2d21d7cd66bd5a5d optional=false tags=[] doc=
table ShipEntry field health json=health type=float32 id=7f69d4b5288ba9cf optional=false tags=[] doc=
table ShipEntry field mass json=mass type=float32 id=1f3757a2ce7b0ab1 optional=false tags=[] doc=
table ShipEntry field hardpoints json=hardpoints type=int32 id=95d0cce09ca82b73 optional=false tags=[] doc=
table ShipEntry field gunner json=gunner type=GunnerSettings id=40dbb648c0cd44aa optional=true tags=[] doc=
table TeamConfig file=Keyed.schema fields=2 tags=[] doc=
table TeamConfig field spawn_count json=spawn_count type=int32 id=ceec99e2d65db674 optional=false tags=[] doc=
table TeamConfig field banner json=banner type=string id=bca0dab1c7a00ccf optional=false tags=[] doc=
table TurretConfig file=Keyed.schema fields=3 tags=[] doc=
table TurretConfig field damage json=damage type=float32 id=7f6308be8ab37fc0 optional=false tags=[] doc=
table TurretConfig field cooldown json=cooldown type=float32 id=dc2cbe6953343d48 optional=false tags=[] doc=
table TurretConfig field gunner json=gunner type=GunnerConfig id=40dbb648c0cd44aa optional=true tags=[] doc=
table WeaponConfig file=Tables.schema fields=6 tags=[designer_facing] doc=One weapon's tuning. A designer edits this table and nothing else.
table WeaponConfig field damage json=damage type=float32 id=7f6308be8ab37fc0 optional=false tags=[ui_slider,hot,tuning] doc= Damage per hit, before armor.\n\nThe armor curve is written "hardness \\ hit" in the design notes,\nand the backslash is part of the name.
table WeaponConfig field speed json=speed type=float32 id=1733055702acb1d2 optional=false tags=[] doc=
table WeaponConfig field penetration json=penetration type=int32 id=4f31c5309e13e932 optional=false tags=[] doc=
table WeaponConfig field channel json=channel type=bits(6) id=a5013e9ad5caeda4 optional=false tags=[] doc=
table WeaponConfig field homing json=homing type=bool id=ad6587bc602e3f59 optional=false tags=[] doc=
table WeaponConfig field effect json=effect type=Effect id=36c1c83b51f24394 optional=false tags=[] doc=
table WideBlob file=Wide.schema fields=3 tags=[] doc=
table WideBlob field label json=label type=string id=39f7fcec8fcb623d optional=false tags=[] doc=
table WideBlob field payload json=payload type=bytes id=cfb8a9d063b5e9e5 optional=false tags=[] doc=
table WideBlob field samples json=samples type=uint16 id=e3b1ca6a3b48dddc optional=false tags=[] doc=
`

// wantListingArmdemo is the listing tables/arms' UnitView prints: every union
// arm's payload, field line, doc comment and tags (docs/SPEC-TABLES.md §8.1).
const wantListingArmdemo = `unit package=armdemo protocol=727cea182a6959a7
union Carry file=Carry.schema max=2 bits=8 variants=3 tags=[] doc=
union Carry arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Carry arm 1 leaf id=24ad84ada20208d5 payload=Leaf record=yes field=no kind=- bound=- overlay=- tags=[] doc=
union Carry arm 2 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[overlay] doc=The arm that names no record: a scalar in the union's own overlay.
union Inner file=Nest.schema max=2 bits=8 variants=3 tags=[] doc=
union Inner arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Inner arm 1 leaf id=24ad84ada20208d5 payload=Leaf record=yes field=no kind=- bound=- overlay=- tags=[] doc=
union Inner arm 2 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[] doc=
union Outer file=Nest.schema max=2 bits=8 variants=3 tags=[] doc=
union Outer arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Outer arm 1 inner id=2d4c068f5f889287 payload=Inner record=no field=yes kind=15 bound=0 overlay=0 tags=[] doc=
union Outer arm 2 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[] doc=
union Reach file=Gate.schema max=3 bits=8 variants=4 tags=[] doc=
union Reach arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Reach arm 1 only id=072ddab46af04ab7 payload=*Only record=no field=yes kind=17 bound=0 overlay=0 tags=[] doc=
union Reach arm 2 text id=fa04f4ef1995407e payload=*string record=no field=yes kind=17 bound=0 overlay=0 tags=[] doc=
union Reach arm 3 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[] doc=
union Slot file=Ring.schema max=2 bits=8 variants=3 tags=[] doc=
union Slot arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Slot arm 1 vertex id=3c2f1bbad18642ad payload=*Node record=no field=yes kind=17 bound=0 overlay=0 tags=[] doc=
union Slot arm 2 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[] doc=
union Slots file=Ring.schema max=2 bits=8 variants=3 tags=[] doc=
union Slots arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Slots arm 1 many id=1f6459a2cea1fc02 payload=[..2]*Node record=no field=yes kind=17 bound=2 overlay=0 tags=[] doc=
union Slots arm 2 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[] doc=
table Chain file=Carry.schema fields=2 tags=[] doc=
table Chain field links json=links type=Carry id=5cc201a9f1b8274e optional=false tags=[] doc=
table Chain field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Gate file=Gate.schema fields=2 tags=[] doc=
table Gate field reach json=reach type=Reach id=891da1f305deb858 optional=false tags=[] doc=
table Gate field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Hand file=Carry.schema fields=2 tags=[] doc=
table Hand field entries json=entries type=Carry id=c5b2a72c0845a253 optional=false tags=[] doc=
table Hand field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Holder file=Carry.schema fields=2 tags=[] doc=
table Holder field carry json=carry type=Carry id=afcd9a3b099f8ef0 optional=false tags=[] doc=
table Holder field tail json=tail type=int32 id=d94c56ef0798d723 optional=false tags=[] doc=
table Leaf file=Carry.schema fields=1 tags=[] doc=
table Leaf field items json=items type=int32 id=3e7884bf4f412c6f optional=false tags=[] doc=
table Nest file=Nest.schema fields=2 tags=[] doc=
table Nest field outer json=outer type=Outer id=30e6ed678f5e1bd4 optional=false tags=[] doc=
table Nest field tail json=tail type=int32 id=d94c56ef0798d723 optional=false tags=[] doc=
table Node file=Ring.schema fields=1 tags=[] doc=
table Node field v json=v type=int32 id=af63eb4c86020609 optional=false tags=[] doc=
table Only file=Gate.schema fields=1 tags=[] doc=
table Only field w json=w type=int32 id=af63ea4c86020456 optional=false tags=[] doc=
table Rack file=Ring.schema fields=2 tags=[] doc=
table Rack field items json=items type=Slots id=3e7884bf4f412c6f optional=false tags=[] doc=
table Rack field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Ring file=Ring.schema fields=2 tags=[] doc=
table Ring field items json=items type=Slot id=3e7884bf4f412c6f optional=false tags=[] doc=
table Ring field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Tray file=Ring.schema fields=2 tags=[] doc=
table Tray field entries json=entries type=Slots id=c5b2a72c0845a253 optional=false tags=[] doc=
table Tray field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Twig file=Nest.schema fields=1 tags=[] doc=
table Twig field inner json=inner type=Inner id=2d4c068f5f889287 optional=false tags=[] doc=
`

// wantListingMapdemo is the listing tables/maps' UnitView prints
// (docs/SPEC-TABLES.md §8.1-§8.3).
const wantListingMapdemo = `unit package=mapdemo protocol=1ac124decde5b2aa
enum Slot file=Depth.schema max=2 bits=8 variants=3 tags=[] doc=
enum Slot variant 0 None id=0000000000000000 tags=[] doc=
enum Slot variant 1 Alpha id=4fda04cbc245e18b tags=[] doc=
enum Slot variant 2 Beta id=a0b562a796b69487 tags=[] doc=
union Force file=Depth.schema max=2 bits=8 variants=3 tags=[] doc=
union Force arm 0 None id=0000000000000000 payload=- record=no field=no kind=- bound=- overlay=- tags=[] doc=
union Force arm 1 squad id=d5c2bb95d63e6331 payload=Squad record=yes field=no kind=- bound=- overlay=- tags=[] doc=
union Force arm 2 plain id=fd4d194e1652b207 payload=int32 record=no field=yes kind=4 bound=0 overlay=0 tags=[] doc=
table Cells file=Cells.schema fields=2 tags=[] doc=
table Cells field rows json=rows type=map[string(8)]int32 id=a3a7061ff10a8138 optional=false tags=[] doc=
table Cells field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Chunks file=Chunks.schema fields=2 tags=[] doc=
table Chunks field blobs json=blobs type=map[int32]*bytes id=14e2eaab9cde925b optional=false tags=[] doc=
table Chunks field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Crews file=Crews.schema fields=2 tags=[] doc=
table Crews field members json=members type=map[uint32]*Item id=79d594675e391090 optional=false tags=[] doc=
table Crews field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Depth file=Depth.schema fields=5 tags=[] doc=
table Depth field one json=one type=Squad id=1a08aa1921ca5caf optional=false tags=[] doc=
table Depth field many json=many type=Squad id=1f6459a2cea1fc02 optional=false tags=[] doc=
table Depth field keyed json=keyed type=Squad id=70551ff29550f15d optional=false tags=[] doc=
table Depth field arm json=arm type=Force id=e756c0190570ccb5 optional=false tags=[] doc=
table Depth field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Docs file=Docs.schema fields=2 tags=[] doc=
table Docs field pages json=pages type=map[string(8)]*string id=ab01daa76a48769f optional=false tags=[] doc=
table Docs field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table EdgeRow file=Rows.schema fields=3 tags=[] doc=
table EdgeRow field names json=names type=map[string(300)]Item id=afb728fff268814f optional=false tags=[] doc=
table EdgeRow field ids json=ids type=map[uint64]Item id=2b7dea192bb7be29 optional=false tags=[] doc=
table EdgeRow field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Fleet file=Fleet.schema fields=5 tags=[] doc=
table Fleet field ships json=ships type=map[string(32)]ShipConfig id=294a5c4913e1ad44 optional=false tags=[] doc=
table Fleet field by_id json=by_id type=map[uint32]*ShipConfig id=7b024c46e98d3404 optional=false tags=[] doc=
table Fleet field flagship json=flagship type=ShipConfig id=63dfa0c4a4b3815d optional=false tags=[] doc=
table Fleet field loadouts json=loadouts type=map[string(16)]map[uint8]Item id=294fa1b3f0f5f070 optional=false tags=[] doc=
table Fleet field tiers json=tiers type=map[int16]Item id=6dd8dc6c5fdae3ce optional=false tags=[] doc=
table Item file=Fleet.schema fields=1 tags=[] doc=
table Item field count json=count type=int32 id=b1e5e28e4479a274 optional=false tags=[] doc=
table Pairs file=Pairs.schema fields=2 tags=[] doc=
table Pairs field slots json=slots type=map[uint32]*Item id=e68c2e6bb1ee5646 optional=false tags=[] doc=
table Pairs field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Row file=Rows.schema fields=2 tags=[] doc=
table Row field entries json=entries type=map[string(8)]Item id=c5b2a72c0845a253 optional=false tags=[] doc=
table Row field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Runs file=Runs.schema fields=2 tags=[] doc=
table Runs field spans json=spans type=map[uint16]Item id=437dfc8ab2566816 optional=false tags=[] doc=
table Runs field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table ShipConfig file=Fleet.schema fields=2 tags=[] doc=
table ShipConfig field name json=name type=string id=c4bcadba8e631b86 optional=false tags=[] doc=
table ShipConfig field health json=health type=int32 id=7f69d4b5288ba9cf optional=false tags=[] doc=
table Slots file=Slots.schema fields=2 tags=[] doc=
table Slots field seats json=seats type=map[int32]int32 id=7f6548303072b061 optional=false tags=[] doc=
table Slots field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Spans file=Spans.schema fields=2 tags=[] doc=
table Spans field tracks json=tracks type=map[uint8]Item id=755332609c470fbd optional=false tags=[] doc=
table Spans field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Squad file=Depth.schema fields=1 tags=[] doc=
table Squad field roster json=roster type=map[uint8]Item id=1c84390d304f4f42 optional=false tags=[] doc=
table Text file=Text.schema fields=4 tags=[] doc=
table Text field names json=names type=map[string(8)]string id=afb728fff268814f optional=false tags=[] doc=
table Text field wide json=wide type=map[uint16]wstring id=a633f1f655715cca optional=false tags=[] doc=
table Text field blobs json=blobs type=map[int32]bytes id=14e2eaab9cde925b optional=false tags=[] doc=
table Text field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table Trails file=Trails.schema fields=2 tags=[] doc=
table Trails field steps json=steps type=map[uint32]*Item id=124250ad5a5b6d14 optional=false tags=[] doc=
table Trails field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
table WideRow file=Rows.schema fields=2 tags=[] doc=
table WideRow field entries json=entries type=map[uint32]Item id=c5b2a72c0845a253 optional=false tags=[] doc=
table WideRow field past json=past type=int32 id=bf82010f6f71eae9 optional=false tags=[] doc=
`
