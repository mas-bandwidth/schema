package elixirtable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestBlockBuildAndCookWriteElixir pins docs/SPEC-TABLES.md §19.1 and §7.6:
// the generated block builder's bytes equal the golden pin_block_golden writes
// (test/tables/block_main.cpp) and the Go builder's layout for the same
// counts, and cook_from equals schema cook in both byte orders. A buffer
// shorter than the measure returns false and writes nothing. An invalid value
// answers invalid_value.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestBlockBuildAndCookWriteElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("block", func(t *testing.T) {
		t.Parallel()
		pinBlockGolden(t, elixirBin, root)
	})
	t.Run("cook", func(t *testing.T) {
		t.Parallel()
		pinCookWrite(t, elixirBin, root)
	})
}

func pinOneCook(t *testing.T, elixirBin, root string, unit *ir.Unit, rootName, wireName, cookName string) {
	t.Helper()
	files := generateElixir(t, unit)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	mod := ir.GoExportName(unit.Package) + ".Table"
	wire := filepath.Join(root, "testdata", "wire", "tables", wireName)
	little := filepath.Join(root, "testdata", "conformance", "tables", "cook-write", cookName+".cook")
	big := filepath.Join(root, "testdata", "conformance", "tables", "cook-write", cookName+"-be.cook")
	littleOut := filepath.Join(dir, "little.cook")
	bigOut := filepath.Join(dir, "big.cook")
	probe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wire) + `)
{:ok, value, report} = ` + mod + `.load(` + elixirString(rootName) + `, wire)
Probe.check(report.malformed == false, "load")
{size, reason} = ` + mod + `.cook_measure_reason(` + elixirString(rootName) + `, value)
Probe.check(reason == :ok and size > 0, "measure " <> inspect({size, reason}))
Probe.check(` + mod + `.cook_measure(` + elixirString(rootName) + `, value) == size, "measure agrees")
stamp = :binary.copy(<<0xA5>>, size)
short = binary_part(stamp, 0, size - 1)
Probe.check(` + mod + `.cook_from(` + elixirString(rootName) + `, value, short, :little) == false, "short buffer returns false")
Probe.check(short == :binary.copy(<<0xA5>>, size - 1), "short buffer wrote nothing")
{true, little} = ` + mod + `.cook_from(` + elixirString(rootName) + `, value, stamp, :little)
{true, big} = ` + mod + `.cook_from(` + elixirString(rootName) + `, value, stamp, :big)
File.write!(` + elixirString(littleOut) + `, binary_part(little, 0, size))
File.write!(` + elixirString(bigOut) + `, binary_part(big, 0, size))
`
	if cookName == "graph_empty" {
		probe += `{bad, why} = ` + mod + `.cook_measure_reason("Scene", %{"name" => :not_a_string})
Probe.check(bad == -1 and why == :invalid_value, "invalid_value " <> inspect({bad, why}))
`
	}
	probe += `IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
	gotL, err := os.ReadFile(littleOut)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := os.ReadFile(bigOut)
	if err != nil {
		t.Fatal(err)
	}
	wantL, err := os.ReadFile(little)
	if err != nil {
		t.Fatal(err)
	}
	wantB, err := os.ReadFile(big)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotL, wantL) {
		t.Fatalf("little cook %d bytes, schema cook %d", len(gotL), len(wantL))
	}
	if !bytes.Equal(gotB, wantB) {
		t.Fatalf("big cook %d bytes, schema cook %d", len(gotB), len(wantB))
	}
}

func pinBlockGolden(t *testing.T, elixirBin, root string) {
	t.Helper()
	unit := loadUnit(t,
		filepath.Join(root, "tables", "block", "Render.schema"),
		filepath.Join(root, "tables", "block", "Padded.schema"),
	)
	files := generateElixir(t, unit)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	ns := ir.GoExportName(unit.Package)
	golden := filepath.Join(root, "testdata", "wire", "tables", "block_render.bin")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "built.bin")
	probe := blockFillProbe(t, unit, ns, outPath)
	runProbe(t, elixirBin, dir, names, probe)
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("built block %d bytes, golden %d bytes", len(got), len(want))
	}
}

func pinCookWrite(t *testing.T, elixirBin, root string) {
	t.Helper()
	cases := []struct {
		unit   string
		schema []string
		root   string
		wire   string
		name   string
	}{
		{"graphdemo", []string{"tables/pointers/Graph.schema", "tables/pointers/Marks.schema", "tables/pointers/Parts.schema"}, "Scene", "graph_empty.bin", "graph_empty"},
		{"graphdemo", []string{"tables/pointers/Graph.schema", "tables/pointers/Marks.schema", "tables/pointers/Parts.schema"}, "Scene", "graph_shared.bin", "graph_shared"},
		{"graphdemo", []string{"tables/pointers/Graph.schema", "tables/pointers/Marks.schema", "tables/pointers/Parts.schema"}, "Scene", "graph_tree.bin", "graph_tree"},
		{"blobdemo", []string{"tables/blobs/Assets.schema"}, "Catalog", "blob_empty.bin", "blob_empty"},
		{"blobdemo", []string{"tables/blobs/Assets.schema"}, "Catalog", "blob_small.bin", "blob_small"},
		{"blobdemo", []string{"tables/blobs/Assets.schema"}, "Catalog", "blob_shared.bin", "blob_shared"},
		{"tblp1", []string{"test/tables/P1.schema"}, "Chain", "chain_value.bin", "chain_value"},
		{"tblp3", []string{"test/tables/P3.schema"}, "Chain", "chain_optional.bin", "chain_optional"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			paths := make([]string, len(c.schema))
			for i, p := range c.schema {
				paths[i] = filepath.Join(root, p)
			}
			pinOneCook(t, elixirBin, root, loadUnit(t, paths...), c.root, c.wire, c.name)
		})
	}
}

func blockFillProbe(t *testing.T, u *ir.Unit, ns, outPath string) string {
	t.Helper()
	blocks := ir.Blocks(u)
	var bl *ir.BlockLayout
	for _, b := range blocks.Tables {
		if b.Table.Name == "RenderFrame" {
			bl = b
			break
		}
	}
	if bl == nil {
		t.Fatal("no RenderFrame block")
	}
	home := u.DeclFile["RenderFrame"]
	mod := ns + "." + moduleBase(home) + "Block"
	var b bytes.Buffer
	b.WriteString("defmodule Fill do\n")
	b.WriteString("  def put(bin, at, <<bytes::binary>>) do\n")
	b.WriteString("    pre = binary_part(bin, 0, at)\n")
	b.WriteString("    post = binary_part(bin, at + byte_size(bytes), byte_size(bin) - at - byte_size(bytes))\n")
	b.WriteString("    pre <> bytes <> post\n")
	b.WriteString("  end\n")
	b.WriteString("  def u8(bin, at, v), do: put(bin, at, <<v::unsigned-8>>)\n")
	b.WriteString("  def u32(bin, at, v), do: put(bin, at, <<v::little-unsigned-32>>)\n")
	b.WriteString("  def u64(bin, at, v), do: put(bin, at, <<v::little-unsigned-64>>)\n")
	b.WriteString("  def f32(bin, at, v), do: put(bin, at, <<v::little-float-32>>)\n")
	b.WriteString("  def f64(bin, at, v), do: put(bin, at, <<v::little-float-64>>)\n")
	b.WriteString("end\n")
	b.WriteString("{:ok, bin} = " + mod + ".block_begin_render_frame(%{\"cameras\" => 1, \"ships\" => 5, \"turrets\" => 7, \"missiles\" => 3, \"dynamic_props\" => 2, \"static_props\" => 4, \"cosmetic_props\" => 3, \"lasers\" => 6, \"explosions\" => 2})\n")
	ver := fieldOff(t, &bl.Projection, "version")
	b.WriteString("bin = Fill.u64(bin, " + itoa(ver) + ", 1)\n")
	counts := []struct {
		name  string
		count int
		fill  func(base int64) string
	}{
		{"cameras", 1, func(base int64) string { return fillCamera(t, u, base) }},
		{"ships", 5, func(base int64) string { return fillShip(t, u, base) }},
		{"turrets", 7, func(base int64) string { return fillTurret(t, u, base) }},
		{"missiles", 3, func(base int64) string { return fillMissile(t, u, base) }},
		{"dynamic_props", 2, func(base int64) string { return fillDynamic(t, u, base) }},
		{"static_props", 4, func(base int64) string { return fillStatic(t, u, base) }},
		{"cosmetic_props", 3, func(base int64) string { return fillCosmetic(t, u, base) }},
		{"lasers", 6, func(base int64) string { return fillLaser(t, u, base) }},
		{"explosions", 2, func(base int64) string { return fillExplosion(t, u, base) }},
	}
	offset := bl.Projection.Size
	for _, a := range bl.Arrays {
		align := int64(64)
		if a.ElemAlign() > align {
			align = a.ElemAlign()
		}
		offset = (offset + align - 1) & ^(align - 1)
		var spec *struct {
			name  string
			count int
			fill  func(base int64) string
		}
		for i := range counts {
			if counts[i].name == a.Field.Name {
				spec = &counts[i]
				break
			}
		}
		if spec == nil {
			t.Fatalf("no fill for %s", a.Field.Name)
		}
		b.WriteString("bin = (\n")
		b.WriteString("  Enum.reduce(0.." + itoa(int64(spec.count-1)) + ", bin, fn i, bin ->\n")
		b.WriteString("    at = " + itoa(offset) + " + i * " + itoa(a.Stride) + "\n")
		b.WriteString(spec.fill(0))
		b.WriteString("    bin\n")
		b.WriteString("  end)\n)\n")
		offset += int64(spec.count) * a.Stride
	}
	b.WriteString("File.write!(" + elixirString(outPath) + ", bin)\n")
	b.WriteString("IO.puts(\"ok\")\n")
	return b.String()
}

func fieldOff(t *testing.T, ml *ir.MemberLayout, name string) int64 {
	t.Helper()
	fl := ml.FieldByName(name)
	if fl == nil {
		t.Fatalf("no field %s", name)
	}
	return fl.Offset
}

func recOff(t *testing.T, u *ir.Unit, rec, name string) int64 {
	t.Helper()
	st := u.Tables[rec]
	if st == nil {
		st = u.Structs[rec]
	}
	return fieldOff(t, ir.RecordLayout(u, st), name)
}

func nest(t *testing.T, u *ir.Unit, rec, field, sub string) int64 {
	t.Helper()
	return recOff(t, u, rec, field) + recOff(t, u, fieldType(t, u, rec, field), sub)
}

func fieldType(t *testing.T, u *ir.Unit, rec, field string) string {
	t.Helper()
	st := u.Tables[rec]
	if st == nil {
		st = u.Structs[rec]
	}
	for _, f := range st.Fields {
		if f.Name == field {
			return f.Type.Name
		}
	}
	t.Fatalf("no %s.%s", rec, field)
	return ""
}

func fillCamera(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderCamera", "position", 1) +
		quat(t, u, "RenderCamera", "rotation", 1) +
		u32(t, u, "RenderCamera", "camera_id", "i * 7 + 1") +
		u32(t, u, "RenderCamera", "camera_type", "rem(i, 4)") +
		u32(t, u, "RenderCamera", "target_object_id", "i * 13 + 2") +
		f32(t, u, "RenderCamera", "fov", "0.5 * i + 60.0")
}

func fillShip(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderShip", "position", 2) +
		quat(t, u, "RenderShip", "rotation", 2) +
		u64(t, u, "RenderShip", "flags", "rem(i, 16)") +
		u32(t, u, "RenderShip", "object_id", "i * 3 + 11") +
		u32(t, u, "RenderShip", "target_object_id", "i * 5 + 7") +
		f32(t, u, "RenderShip", "thrust", "0.25 * i") +
		u8(t, u, "RenderShip", "object_sequence", "rem(i, 251)") +
		u8(t, u, "RenderShip", "ship_type", "rem(i, 4)") +
		u8(t, u, "RenderShip", "team", "rem(i, 5)") +
		u8(t, u, "RenderShip", "has_target_lock", "if(rem(i, 2) == 0, do: 1, else: 0)") +
		u8(t, u, "RenderShip", "predicted_explode", "if(rem(i, 3) == 0, do: 1, else: 0)")
}

func fillTurret(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return quat(t, u, "RenderTurret", "rotation", 3) +
		u64(t, u, "RenderTurret", "flags", "i * 17") +
		u32(t, u, "RenderTurret", "object_id", "i * 2 + 1") +
		u32(t, u, "RenderTurret", "parent_object_id", "div(i, 3)") +
		u32(t, u, "RenderTurret", "turret_index", "rem(i, 8)") +
		u32(t, u, "RenderTurret", "target_object_id", "i * 11") +
		u8(t, u, "RenderTurret", "object_sequence", "rem(i, 253)") +
		u8(t, u, "RenderTurret", "team", "rem(i, 5)") +
		u8(t, u, "RenderTurret", "has_target_lock", "if(rem(i, 5) == 0, do: 1, else: 0)")
}

func fillMissile(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderMissile", "position", 4) +
		quat(t, u, "RenderMissile", "rotation", 4) +
		u64(t, u, "RenderMissile", "flags", "i * 19") +
		u32(t, u, "RenderMissile", "object_id", "i * 23") +
		u8(t, u, "RenderMissile", "object_sequence", "rem(i, 249)") +
		u8(t, u, "RenderMissile", "missile_type", "rem(i, 3)") +
		u8(t, u, "RenderMissile", "team", "rem(i, 5)")
}

func fillDynamic(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderDynamicProp", "position", 5) +
		quat(t, u, "RenderDynamicProp", "rotation", 5) +
		u64(t, u, "RenderDynamicProp", "flags", "i * 29") +
		u32(t, u, "RenderDynamicProp", "object_id", "i * 31") +
		u8(t, u, "RenderDynamicProp", "object_sequence", "rem(i, 247)") +
		u8(t, u, "RenderDynamicProp", "prop_type", "rem(i, 4)") +
		u8(t, u, "RenderDynamicProp", "team", "rem(i, 5)")
}

func fillStatic(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderStaticProp", "position", 6) +
		quat(t, u, "RenderStaticProp", "rotation", 6) +
		f64(t, u, "RenderStaticProp", "scale", "0.5 + 0.25 * rem(i, 7)") +
		u64(t, u, "RenderStaticProp", "flags", "i * 37") +
		u32(t, u, "RenderStaticProp", "static_prop_id", "i * 41") +
		u8(t, u, "RenderStaticProp", "prop_type", "rem(i, 4)") +
		u8(t, u, "RenderStaticProp", "team", "rem(i, 5)")
}

func fillCosmetic(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderCosmeticProp", "position", 7) +
		quat(t, u, "RenderCosmeticProp", "rotation", 7) +
		f64(t, u, "RenderCosmeticProp", "scale", "0.25 + 0.125 * rem(i, 5)") +
		u64(t, u, "RenderCosmeticProp", "flags", "i * 43") +
		u32(t, u, "RenderCosmeticProp", "cosmetic_prop_id", "i * 47") +
		u8(t, u, "RenderCosmeticProp", "prop_sequence", "rem(i, 241)") +
		u8(t, u, "RenderCosmeticProp", "prop_type", "rem(i, 4)") +
		u8(t, u, "RenderCosmeticProp", "team", "rem(i, 5)")
}

func fillLaser(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderLaser", "start", 8) +
		vec3(t, u, "RenderLaser", "finish", 9) +
		f64(t, u, "RenderLaser", "t", "0.125 * rem(i, 8)") +
		u32(t, u, "RenderLaser", "laser_id", "i * 53") +
		u8(t, u, "RenderLaser", "laser_type", "rem(i, 3)") +
		u8(t, u, "RenderLaser", "team", "rem(i, 5)")
}

func fillExplosion(t *testing.T, u *ir.Unit, _ int64) string {
	t.Helper()
	return vec3(t, u, "RenderExplosion", "position", 10) +
		quat(t, u, "RenderExplosion", "rotation", 10) +
		f64(t, u, "RenderExplosion", "t", "0.0625 * rem(i, 16)") +
		u32(t, u, "RenderExplosion", "explosion_id", "i * 59") +
		u32(t, u, "RenderExplosion", "parent_object_id", "i * 61") +
		u8(t, u, "RenderExplosion", "explosion_type", "rem(i, 3)") +
		u8(t, u, "RenderExplosion", "team", "rem(i, 5)")
}

func vec3(t *testing.T, u *ir.Unit, rec, field string, salt int) string {
	t.Helper()
	s := itoa(int64(salt))
	return f64at(nest(t, u, rec, field, "x"), "1.5 * i + "+s) +
		f64at(nest(t, u, rec, field, "y"), "2.25 * i - "+s) +
		f64at(nest(t, u, rec, field, "z"), "0.5 * i + 2 * "+s)
}

func quat(t *testing.T, u *ir.Unit, rec, field string, salt int) string {
	t.Helper()
	s := itoa(int64(salt))
	return f64at(nest(t, u, rec, field, "x"), "0.25 * i + "+s) +
		f64at(nest(t, u, rec, field, "y"), "0.125 * i") +
		f64at(nest(t, u, rec, field, "z"), "0.0625 * i - "+s) +
		f64at(nest(t, u, rec, field, "w"), "1.0 - 0.5 * rem(i, 3)")
}

func u8(t *testing.T, u *ir.Unit, rec, field, expr string) string {
	return "    bin = Fill.u8(bin, at + " + itoa(recOff(t, u, rec, field)) + ", " + expr + ")\n"
}
func u32(t *testing.T, u *ir.Unit, rec, field, expr string) string {
	return "    bin = Fill.u32(bin, at + " + itoa(recOff(t, u, rec, field)) + ", " + expr + ")\n"
}
func u64(t *testing.T, u *ir.Unit, rec, field, expr string) string {
	return "    bin = Fill.u64(bin, at + " + itoa(recOff(t, u, rec, field)) + ", " + expr + ")\n"
}
func f32(t *testing.T, u *ir.Unit, rec, field, expr string) string {
	return "    bin = Fill.f32(bin, at + " + itoa(recOff(t, u, rec, field)) + ", " + expr + ")\n"
}
func f64(t *testing.T, u *ir.Unit, rec, field, expr string) string {
	return "    bin = Fill.f64(bin, at + " + itoa(recOff(t, u, rec, field)) + ", " + expr + ")\n"
}
func f64at(off int64, expr string) string {
	return "    bin = Fill.f64(bin, at + " + itoa(off) + ", " + expr + ")\n"
}
