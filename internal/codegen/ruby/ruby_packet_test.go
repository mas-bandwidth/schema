package ruby

import (
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func repoRoot(t testing.TB) string {
	t.Helper()
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func loadRubyUnit(t testing.TB, paths ...string) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			tree, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			sources = append(sources, check.SourceFile{
				Path: name, Name: filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: tree,
			})
		}
	}
	u, errs := check.Unit(sources)
	if len(errs) > 0 {
		t.Fatalf("check %v: %v", paths, errs[0])
	}
	return u
}

func declName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Const:
		return d.Name
	case *ir.Enum:
		return d.Name
	case *ir.Flags:
		return d.Name
	case *ir.Struct:
		return d.Name
	case *ir.Union:
		return d.Name
	}
	return ""
}

func rootClosure(u *ir.Unit, roots ...string) *ir.Unit {
	byName := map[string]ir.Decl{}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			byName[declName(d)] = d
		}
	}
	keep := map[string]bool{}
	var add func(string)
	add = func(name string) {
		if name == "" || keep[name] {
			return
		}
		d, ok := byName[name]
		if !ok {
			return
		}
		keep[name] = true
		switch d := d.(type) {
		case *ir.Struct:
			for _, f := range d.Fields {
				if f.Type.Kind == ir.TNamed {
					add(f.Type.Name)
				}
			}
		case *ir.Union:
			for _, v := range d.Variants {
				add(v.Type)
			}
		}
	}
	for _, root := range roots {
		add(root)
	}
	out := &ir.Unit{
		Package: u.Package, ProtocolId: u.ProtocolId,
		DeclFile: map[string]string{}, Consts: map[string]*ir.Const{},
		Enums: map[string]*ir.Enum{}, Flags: map[string]*ir.Flags{},
		Structs: map[string]*ir.Struct{}, Unions: map[string]*ir.Union{},
	}
	for _, f := range u.Files {
		var decls []ir.Decl
		for _, d := range f.Decls {
			if !keep[declName(d)] {
				continue
			}
			decls = append(decls, d)
			name := declName(d)
			out.DeclFile[name] = f.Base
			switch d := d.(type) {
			case *ir.Const:
				out.Consts[d.Name] = d
			case *ir.Enum:
				out.Enums[d.Name] = d
			case *ir.Flags:
				out.Flags[d.Name] = d
			case *ir.Struct:
				out.Structs[d.Name] = d
			case *ir.Union:
				out.Unions[d.Name] = d
			}
		}
		if len(decls) > 0 {
			out.Files = append(out.Files, &ir.File{Base: f.Base, Path: f.Path, Decls: decls})
		}
	}
	return out
}

type packetPin struct {
	name  string
	paths []string
	root  string
	note  string
}

// wireDir is the directory under testdata/wire that holds the pin's golden.
func (p packetPin) wireDir() string {
	paths := strings.Join(p.paths, "|")
	if strings.Contains(paths, "packet-defaults") || strings.Contains(paths, "constant-defaults") {
		return "packet-defaults"
	}
	return ""
}

func packetPins() []packetPin {
	examples := []string{"examples"}
	ludicrous := []string{"examples128"}
	bench := []string{"bench/corpus/Bench.schema"}
	real := []string{"bench/corpus/RealWorld.schema"}
	defaults := []string{"test/packet-defaults/Defaults.schema"}
	// the C and C++ writers' constant defaults (O-01, O-02), pinned beside
	// the packet-defaults goldens
	constant := func(name string) []string {
		return []string{"test/conformance/packet/testdata/constant-defaults/" + name + "/Defaults.schema"}
	}
	return []packetPin{
		{"shipcreate_flags", examples, "ShipCreate", ""},
		{"chat", examples, "Chat", ""},
		{"compressed_probe", examples, "CompressedProbe", ""},
		{"inputpacket", examples, "InputPacket", ""},
		{"probearray", examples, "ProbeArray", ""},
		{"probebits", examples, "ProbeBits", ""},
		{"probecollider", examples, "ProbeCollider", ""},
		{"probe_header", examples, "ProbeHeader", ""},
		{"testdata", examples, "TestData", ""},
		{"rigidbody_at_rest", examples, "RigidBody", ""},
		{"rigidbody_moving", examples, "RigidBody", ""},
		{"clauses", examples, "", "concatenated multi-root pin"},
		{"joins", examples, "", "concatenated multi-root pin"},
		{"degenerate", examples, "", "concatenated multi-root pin"},
		{"ludicrous_state", ludicrous, "LudicrousState", ""},
		{"ludicrous_state_untargeted", ludicrous, "LudicrousState", ""},
		{"degenerate_probe", ludicrous, "DegenerateProbe", ""},
		{"unsigned_probe", ludicrous, "UnsignedProbe", ""},
		{"bench_bits", bench, "BenchBits", ""},
		{"bench_ints", bench, "BenchInts", ""},
		{"bench_mixed", bench, "BenchMixed", ""},
		{"bench_packet", bench, "BenchPacket", ""},
		{"bench_table", nil, "", "table-wire pin"},
		{"real_packet", real, "RealPacket", ""},
		{"sample-defaults", defaults, "Sample", ""},
		{"sample-empty", defaults, "Sample", ""},
		{"sample-short", defaults, "Sample", ""},
		{"conditional-on", defaults, "Conditional", ""},
		{"conditional-off", defaults, "Conditional", ""},
		{"batch-defaults", defaults, "Batch", ""},
		{"zero-count", defaults, "ZeroCount", ""},
		{"choice-sample", defaults, "Choice", ""},
		{"hexadecimal-addition", constant("hexadecimal-addition"), "Value", ""},
		{"unsigned-subtraction", constant("unsigned-subtraction"), "Value", ""},
	}
}

func topLevelFields(u *ir.Unit, root string) (intField, boolField string, intMin, intMax *big.Int) {
	st, ok := u.Structs[root]
	if !ok {
		return "", "", nil, nil
	}
	for _, f := range st.Fields {
		switch {
		case intField == "" && f.Guard == "" && f.Array == ir.ArrayNone && f.Type.Kind == ir.TInt:
			intField = f.Name
			intMin, intMax = intBounds(f)
		case boolField == "" && f.Guard == "" && f.Array == ir.ArrayNone && f.Type.Kind == ir.TBool:
			boolField = f.Name
		}
	}
	return intField, boolField, intMin, intMax
}

func packetProbe(u *ir.Unit, root, intField, boolField string, intMin, intMax *big.Int) string {
	mod := ModuleName(u.Package)
	qualRoot := mod + "::" + root
	intChecks := ""
	if intField != "" {
		above := new(big.Int).Add(intMax, big.NewInt(1))
		below := new(big.Int).Sub(intMin, big.NewInt(1))
		intChecks = fmt.Sprintf(`
[%s, %s].each do |bad|
  v = %s.new
  v.%s = bad
  begin
    v.write
    puts "FAILED: an out-of-range int was written"
    exit 1
  rescue %s::InvalidValue
    nil
  end
end
v = %s.new
v.%s = true
begin
  v.write
  puts "FAILED: a bool where an int is declared was written"
  exit 1
rescue %s::InvalidValue
  nil
end
`, above.String(), below.String(), qualRoot, intField, mod, qualRoot, intField, mod)
	}
	boolChecks := ""
	if boolField != "" {
		boolChecks = fmt.Sprintf(`
v = %s.new
v.%s = 1
begin
  v.write
  puts "FAILED: an int where a bool is declared was written"
  exit 1
rescue %s::InvalidValue
  nil
end
`, qualRoot, boolField, mod)
	}
	return fmt.Sprintf(`# frozen_string_literal: true

require_relative %q

data = File.binread("input.bin")
begin
  value, bits = %s.read_bits(data)
rescue %s::Refused => e
  puts "FAILED: the pin was refused: #{e.reason}"
  exit 1
end
if ((bits + 7) / 8) != data.bytesize
  puts "FAILED: read_bits consumed #{bits} bits on #{data.bytesize} bytes"
  exit 1
end
if %s.read(data) != value
  puts "FAILED: read(data) disagrees with read_bits(data)"
  exit 1
end
begin
  again = value.write
rescue %s::InvalidValue => e
  puts "FAILED: the read value was refused on write: #{e}"
  exit 1
end
if again != data
  puts "FAILED: write-back differs"
  puts "got  #{again.unpack1("H*")}"
  puts "want #{data.unpack1("H*")}"
  exit 1
end
# Single-frame stream check: trailing byte refuses serialize_end.
r_trail = %s::ReadStream.new(data + "\x00".b)
v_trail = %s.new
if v_trail._s_read(r_trail) && r_trail.serialize_end
  puts "FAILED: a trailing byte was accepted by serialize_end"
  exit 1
end
if data.bytesize > 0 && bits > 8 * (data.bytesize - 1)
  begin
    %s.read(data.byteslice(0, data.bytesize - 1))
    puts "FAILED: a short read was accepted"
    exit 1
  rescue %s::Refused
    nil
  end
end
fresh = %s.new
if fresh.inspect.empty?
  puts "FAILED: empty inspect"
  exit 1
end
unless fresh == %s.new
  puts "FAILED: a fresh value does not equal its own constructor"
  exit 1
end
%s
%s
puts "ok"
`, u.Package, qualRoot, mod, qualRoot, mod, mod, qualRoot, qualRoot, mod, qualRoot, qualRoot, intChecks, boolChecks)
}

func TestRubyPacketClose(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	known := map[string]bool{"bench_table.bin": true}
	for _, pin := range packetPins() {
		key := pin.name + ".bin"
		if dir := pin.wireDir(); dir != "" {
			key = dir + "/" + key
		}
		if known[key] && pin.name != "bench_table" {
			t.Errorf("duplicate packet pin %s", key)
		}
		known[key] = true
	}
	for _, dir := range []string{"", "packet-defaults"} {
		matches, err := filepath.Glob(filepath.Join(root, "testdata", "wire", dir, "*.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatal("matches is empty: the loop below would check nothing")
		}
		for _, p := range matches {
			key := filepath.ToSlash(filepath.Join(dir, filepath.Base(p)))
			if !known[key] {
				t.Errorf("uncovered packet pin %s", key)
			}
		}
	}
	allPacketPins := packetPins()
	if len(allPacketPins) == 0 {
		t.Fatal("packetPins() is empty: the loop below would check nothing")
	}
	for _, pin := range allPacketPins {
		if pin.name == "bench_table" {
			continue
		}
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			if pin.note != "" {
				packetCloseMulti(t, root, pin.name)
				return
			}
			paths := make([]string, len(pin.paths))
			for i, p := range pin.paths {
				paths[i] = filepath.Join(root, filepath.FromSlash(p))
			}
			u := loadRubyUnit(t, paths...)
			pruned := rootClosure(u, pin.root)
			files, err := Generate(pruned)
			if err != nil {
				t.Fatal(err)
			}
			intField, boolField, intMin, intMax := topLevelFields(pruned, pin.root)
			probe := packetProbe(pruned, pin.root, intField, boolField, intMin, intMax)
			pinDir := filepath.Join(root, "testdata", "wire")
			pinDir = filepath.Join(pinDir, pin.wireDir())
			data, err := os.ReadFile(filepath.Join(pinDir, pin.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range []struct {
				name string
				fn   func(testing.TB, string, string, map[string][]byte) []byte
			}{{"current", rubyrun.Run}, {"floor", rubyrun.RunFloor}} {
				t.Run(run.name, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					writeTree(t, dir, files)
					got := strings.TrimSpace(string(run.fn(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(probe), "input.bin": data})))
					if got != "ok" {
						t.Fatalf("%s: %s", pin.name, got)
					}
				})
			}
		})
	}
}

func packetCloseMulti(t *testing.T, root, name string) {
	t.Helper()
	u := loadRubyUnit(t, filepath.Join(root, "examples"))
	var roots []string
	var sequence string
	switch name {
	case "degenerate":
		roots = strings.Fields("Vec2 SpanF64 SpanU64 SpanI64 SpanOne SpanChunk SpanTail SpanTwice Trio TrioSole TrioFirst TrioStraddle")
		sequence = `
kinds = [Example::Vec2, Example::SpanF64, Example::SpanU64, Example::SpanI64, Example::SpanOne, Example::SpanChunk, Example::SpanTail, Example::SpanTwice, Example::Trio, Example::TrioSole, Example::TrioFirst, Example::TrioStraddle]
bit_off = 0
values = kinds.map do |klass|
  v, consumed = klass.read_bits(data, bit_offset: bit_off)
  bit_off += consumed
  v
end
stream = Example::ReadStream.new(data)
kinds.each do |klass|
  v = klass.new
  raise "read #{klass}" unless v._s_read(stream)
end
raise "end #{stream.refusal}" unless stream.serialize_end
raise "read_bits total #{bit_off} != #{stream.bits_processed}" if bit_off != stream.bits_processed
w = Example::WriteStream.new(((data.bytesize + 7) / 8) * 8)
values.each do |v|
  v._s_validate(v.class.name)
  v._s_write(w)
end
w.flush
raise "write-back differs" if w.data != data
`
	case "clauses":
		roots = strings.Fields("W13 W17 W26 W1 W52 W50 F13 ArrTri3 ArrEleven HoldsEmptyUnion Strs ArrNested Sole")
		sequence = `
rows = []
add = ->(klass, bits) { rows << [klass, (bits + 7) / 8] }
[0, 1, 3, 4, 5, 7, 12].each { |c| add.call(Example::W13, 4 + (13 * c)) }
[0, 1, 2, 3, 4, 9].each { |c| add.call(Example::W17, 4 + (17 * c)) }
[0, 1, 2, 3, 6].each { |c| add.call(Example::W26, 3 + (26 * c)) }
[0, 1, 3, 4, 5, 20].each { |c| add.call(Example::W1, 5 + c) }
4.times { |c| add.call(Example::W52, 2 + (52 * c)) }
4.times { |c| add.call(Example::W50, 2 + (50 * c)) }
add.call(Example::F13, 91)
[0, 1, 3, 4, 5, 10].each { |c| add.call(Example::ArrTri3, 4 + (3 * c)) }
add.call(Example::ArrEleven, 99)
3.times { add.call(Example::HoldsEmptyUnion, 14) }
[27, 155, 75].each { |bits| add.call(Example::Strs, bits) }
5.times { |c| add.call(Example::ArrNested, 11 + (11 * c)) }
add.call(Example::Sole, 13)
`
	case "joins":
		roots = strings.Fields("ArmsAgree ArmsDisagree ArmEmpty ArmAlign ArmsNested ArmArray HoldsUneven ArrUneven RegainAfterAlign")
		sequence = `
rows = []
add = ->(klass, bits) { rows << [klass, (bits + 7) / 8] }
2.times do |f|
  add.call(Example::ArmsAgree, 24)
  add.call(Example::ArmsDisagree, f != 0 ? 24 : 16)
  add.call(Example::ArmEmpty, f != 0 ? 32 : 16)
  add.call(Example::ArmAlign, f != 0 ? 56 : 24)
  add.call(Example::ArmAlign, 24)
end
2.times do |o|
  2.times { |i| add.call(Example::ArmsNested, o != 0 ? (i != 0 ? 40 : 16) : 23) }
end
2.times do |f|
  4.times { |c| add.call(Example::ArmArray, f != 0 ? (15 + (13 * c)) : 22) }
end
[18, 21, 55].each { |bits| add.call(Example::HoldsUneven, bits) }
[0, 5, 44, 49].each { |n| add.call(Example::ArrUneven, 10 + n) }
4.times do |c|
  [0, 4].each { |sl| add.call(Example::RegainAfterAlign, (((5 + 2 + (13 * c) + 3 + 7) / 8) * 8) + (8 * sl) + 84) }
end
`
	default:
		t.Fatalf("unknown concatenated golden %s", name)
	}
	pruned := rootClosure(u, roots...)
	files, err := Generate(pruned)
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf("# frozen_string_literal: true\n\nrequire_relative %q\n\ndata = File.binread(\"input.bin\")\n%s", u.Package, sequence)
	if name != "degenerate" {
		probe += `
off = 0
rows.each do |klass, size|
  segment = data.byteslice(off, size)
  raise "short segment #{klass}" if segment.nil? || segment.bytesize != size
  value = klass.read(segment)
  raise "write-back #{klass}" if value.write != segment
  from_stream, _ = klass.read_bits(data, bit_offset: off * 8)
  raise "read_bits offset #{klass}" if from_stream != value
  off += size
end
raise "trailing bytes #{data.bytesize - off}" if off != data.bytesize
`
	}
	probe += "\nputs \"ok\"\n"
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", rubyrun.Run}, {"floor", rubyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(probe), "input.bin": data})))
			if got != "ok" {
				t.Fatal(got)
			}
		})
	}
}

func TestRubyPacketCloseNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "examples"))
	pruned := rootClosure(u, "ShipCreate")
	files, err := Generate(pruned)
	if err != nil {
		t.Fatal(err)
	}
	base := pruned.DeclFile["ShipCreate"]
	module := pruned.Package + "/" + base + ".rb"
	original := string(files[module])
	marker := "self.health = _s_ri.value"
	if strings.Count(original, marker) != 1 {
		t.Fatalf("generated reader health marker missing or ambiguous in %s", module)
	}
	files[module] = []byte(strings.Replace(original, marker, "self.health = 1 + _s_ri.value", 1))
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	intField, boolField, intMin, intMax := topLevelFields(pruned, "ShipCreate")
	probe := packetProbe(pruned, "ShipCreate", intField, boolField, intMin, intMax)
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, _, err := rubyrun.Try(t, pin.exe(t), dir, "main.rb", map[string][]byte{"main.rb": []byte(probe), "input.bin": data})
			if err == nil || !strings.Contains(out, "FAILED: write-back differs") {
				t.Fatalf("control did not fail on write-back: %v\n%s", err, out)
			}
		})
	}
}

func TestRubyPacketCloseAnalyzer(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, rel := range []string{
		"examples",
		"examples128",
		"bench/corpus/Bench.schema",
		"test/packet-defaults/Defaults.schema",
		"test/packet-finite/ruby/Finite.schema",
		"test/packet-void/ruby/Ruby.schema",
	} {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			u := loadRubyUnit(t, filepath.Join(root, filepath.FromSlash(rel)))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			rubyrun.Analyze(t, dir, u.Package)
		})
	}
}

func TestRubyPacketCloseAnalyzerNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "examples"))
	baseFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	targetFile := u.Package + "/" + u.DeclFile["ShipCreate"] + ".rb"

	t.Run("steep", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		src := string(files[targetFile])
		marker := "@health = "
		if !strings.Contains(src, marker) {
			t.Fatalf("missing %q in %s", marker, targetFile)
		}
		files[targetFile] = []byte(strings.Replace(src, marker, "@health = \"bad_type\" || ", 1))
		dir := t.TempDir()
		writeTree(t, dir, files)
		step, out, err := rubyrun.TryAnalyze(t, dir, u.Package)
		if err == nil || step != "steep" || !strings.Contains(out, filepath.Base(targetFile)) {
			t.Fatalf("steep did not catch planted type fault (%s, %v):\n%s", step, err, out)
		}
	})

	t.Run("rubocop", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		src := string(files[targetFile])
		files[targetFile] = []byte(strings.TrimSuffix(src, "end\n") + "  def self._s_rubocop_fault(unused_arg)\n    nil\n  end\nend\n")
		dir := t.TempDir()
		writeTree(t, dir, files)
		step, out, err := rubyrun.TryAnalyze(t, dir, u.Package)
		if err == nil || step != "rubocop" || !strings.Contains(out, "Lint/UnusedMethodArgument") {
			t.Fatalf("rubocop did not catch planted lint fault (%s, %v):\n%s", step, err, out)
		}
	})
}

func TestRubyPacketDefaults(t *testing.T) {
	t.Parallel()
	packetDefaultsProbe(t, false)
}

func TestRubyPacketDefaultsNegativeControl(t *testing.T) {
	t.Parallel()
	packetDefaultsProbe(t, true)
}

func packetDefaultsProbe(t *testing.T, fault bool) {
	t.Helper()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "test", "packet-defaults", "Defaults.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, corpus := range []string{"examples", "examples128"} {
		additional, err := Generate(loadRubyUnit(t, filepath.Join(root, corpus)))
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, additional)
	}
	if fault {
		name := u.Package + "/Defaults.rb"
		code := string(files[name])
		marker := `@token = "\\n\\t".b.freeze`
		if strings.Count(code, marker) != 1 {
			t.Fatalf("default token marker missing in %s: %s", name, marker)
		}
		files[name] = []byte(strings.Replace(code, marker, `@token = "wrong".b.freeze`, 1))
	}
	driver, err := os.ReadFile(filepath.Join(root, "test", "packet-defaults", "ruby", "main.rb"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"main.rb": driver}
	// Every .bin of the pin directory goes to the driver, which fails on any it
	// did not read: a pin added to the directory is never skipped.
	bins, err := filepath.Glob(filepath.Join(root, "testdata", "wire", "packet-defaults", "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bins) == 0 {
		t.Fatal("no pins in testdata/wire/packet-defaults")
	}
	for _, bin := range bins {
		data, err := os.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		inputs[filepath.Base(bin)] = data
	}
	// The constant-default pins: both schemas declare package arithmetic, so each
	// generates into its own tree, constant-<name>/, beside the driver.
	constantSchemas, err := filepath.Glob(filepath.Join(root, "test", "conformance", "packet", "testdata", "constant-defaults", "*", "Defaults.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(constantSchemas) != 2 {
		t.Fatalf("want the 2 constant-default schemas, found %d", len(constantSchemas))
	}
	for _, schema := range constantSchemas {
		additional, err := Generate(loadRubyUnit(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		for rel, data := range additional {
			files["constant-"+filepath.Base(filepath.Dir(schema))+"/"+rel] = data
		}
	}
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, stderr, err := rubyrun.Try(t, pin.exe(t), dir, "main.rb", inputs)
			if fault {
				if err == nil || !strings.Contains(stderr, "constructor text bytes") {
					t.Fatalf("planted default did not fail by name: %v\n%s\n%s", err, out, stderr)
				}
			} else if err != nil || stderr != "" || strings.TrimSpace(out) != "ok" {
				t.Fatalf("default probe: %v\n%s\n%s", err, out, stderr)
			}
		})
	}
}

func TestRubyPacketFinite(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "test", "packet-finite", "ruby", "Finite.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(root, "test", "packet-finite", "ruby", "main.rb")
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out := string(rubyrun.RunArgs(t, pin.exe(t), dir, driverPath, []string{dir}, nil))
			if !strings.Contains(out, "OK: packet finite Ruby, 86 vectors") {
				t.Fatalf("unexpected finite output: %s", out)
			}
		})
	}
}

func failedFiniteVectors(stderr string) []string {
	rx := regexp.MustCompile(`(?m)^FAILED: ([^:]+):`)
	var got []string
	for _, m := range rx.FindAllStringSubmatch(stderr, -1) {
		got = append(got, m[1])
	}
	slices.Sort(got)
	return got
}

func TestRubyPacketFiniteNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "test", "packet-finite", "ruby", "Finite.schema"))
	baseFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(root, "test", "packet-finite", "ruby", "main.rb")

	t.Run("finite-read", func(t *testing.T) {
		t.Parallel()
		wantBytes, err := os.ReadFile(filepath.Join(root, "test", "packet-finite", "ruby", "read.expected"))
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Fields(string(wantBytes))
		slices.Sort(want)

		files := maps.Clone(baseFiles)
		core := string(files["finite/serialize/core.rb"])
		sabotaged := strings.ReplaceAll(core, "return refuse(NON_FINITE_FLOAT) if bits.allbits?(", "return refuse(NON_FINITE_FLOAT) if false && bits.allbits?(")
		if sabotaged == core {
			t.Fatal("could not locate NON_FINITE_FLOAT read check in serialize/core.rb")
		}
		files["finite/serialize/core.rb"] = []byte(sabotaged)

		for _, pin := range []struct {
			name string
			exe  func(testing.TB) string
		}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
			t.Run(pin.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				_, stderr, err := rubyrun.TryArgs(t, pin.exe(t), dir, driverPath, []string{dir}, nil, nil)
				if err == nil {
					t.Fatal("sabotaged finite-read passed")
				}
				got := failedFiniteVectors(stderr)
				if !slices.Equal(got, want) {
					t.Fatalf("finite-read failed vectors:\ngot  %v\nwant %v\nstderr:\n%s", got, want, stderr)
				}
			})
		}
	})

	t.Run("finite-write", func(t *testing.T) {
		t.Parallel()
		wantBytes, err := os.ReadFile(filepath.Join(root, "test", "packet-finite", "ruby", "write.expected"))
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Fields(string(wantBytes))
		slices.Sort(want)

		files := maps.Clone(baseFiles)
		schemaRB := string(files["finite/_schema.rb"])
		sabSchema := strings.ReplaceAll(schemaRB, "unless value.finite?", "unless true")
		sabSchema = strings.ReplaceAll(sabSchema, "unless rounded.finite?", "unless true")
		if sabSchema == schemaRB {
			t.Fatal("could not locate finite checks in finite/_schema.rb")
		}
		files["finite/_schema.rb"] = []byte(sabSchema)

		finiteRB := string(files["finite/Finite.rb"])
		// Bypass inline range guards ONLY on bare float32/float64 (not compressed_float)
		sabMod := regexp.MustCompile(`unless (_s_v\d+)\.is_a\?\(::Float\) && [^\n]+\n(\s+::Finite\._s_validate_float(?:32|64))`).ReplaceAllString(finiteRB, "unless $1.is_a?(::Float)\n$2")
		files["finite/Finite.rb"] = []byte(sabMod)

		core := string(files["finite/serialize/core.rb"])
		sabCore := strings.ReplaceAll(core, "Serialize.float_bits_finite?(bits)", "true")
		sabCore = strings.ReplaceAll(sabCore, "Serialize.double_bits_finite?(bits)", "true")
		sabCore = strings.ReplaceAll(sabCore, "unless value.finite?", "unless true")
		files["finite/serialize/core.rb"] = []byte(sabCore)

		files["finite/_schema.rb"] = []byte(strings.Replace(
			string(files["finite/_schema.rb"]),
			"def self._s_validate_compressed_float(value, path, min, max)\n    rounded = _s_finite_float32(value, path)",
			"def self._s_validate_compressed_float(value, path, min, max)\n    raise InvalidValue.new(path, \"not finite\") unless value.is_a?(::Float) && value.finite?\n    rounded = [value].pack(\"e\").unpack1(\"e\")\n    raise InvalidValue.new(path, \"not finite\") unless rounded.finite?",
			1,
		))

		for _, pin := range []struct {
			name string
			exe  func(testing.TB) string
		}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
			t.Run(pin.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				_, stderr, err := rubyrun.TryArgs(t, pin.exe(t), dir, driverPath, []string{dir}, nil, nil)
				if err == nil {
					t.Fatal("sabotaged finite-write passed")
				}
				got := failedFiniteVectors(stderr)
				if !slices.Equal(got, want) {
					t.Fatalf("finite-write failed vectors:\ngot  %v\nwant %v\nstderr:\n%s", got, want, stderr)
				}
			})
		}
	})
}

func TestRubyPacketVoid(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "test", "packet-void", "ruby", "Ruby.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(root, "test", "packet-void", "ruby", "main.rb")
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out := strings.TrimSpace(string(rubyrun.RunArgs(t, pin.exe(t), dir, driverPath, []string{dir}, nil)))
			if out != "ok" {
				t.Fatalf("packet-void output: %q", out)
			}
		})
	}
}

func TestRubyPacketVoidNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "test", "packet-void", "ruby", "Ruby.schema"))
	baseFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(root, "test", "packet-void", "ruby", "main.rb")

	t.Run("unused-tag-admitted", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		src := string(files["packetvoidruby/Ruby.rb"])
		marker := "return false unless stream.serialize_int(_s_ri, 0, 2)"
		if strings.Count(src, marker) != 1 {
			t.Fatalf("Tags tag-read marker missing or ambiguous")
		}
		files["packetvoidruby/Ruby.rb"] = []byte(strings.Replace(src, marker, "return false unless stream.serialize_int(_s_ri, 0, 3)", 1))
		for _, pin := range []struct {
			name string
			exe  func(testing.TB) string
		}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
			t.Run(pin.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				_, stderr, err := rubyrun.TryArgs(t, pin.exe(t), dir, driverPath, []string{dir}, nil, nil)
				if err == nil || !strings.Contains(stderr, "FAILED: read tags-tag-3 was accepted") {
					t.Fatalf("sabotaged tag range did not fail tags-tag-3: %v\n%s", err, stderr)
				}
			})
		}
	})

	t.Run("extra-bit-on-void-arm", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		src := string(files["packetvoidruby/Ruby.rb"])
		marker := "stream.serialize_int(_s_ri, 0, 3)\n      if self.type == 2"
		if strings.Count(src, marker) != 1 {
			t.Fatalf("Mixed write marker missing")
		}
		files["packetvoidruby/Ruby.rb"] = []byte(strings.Replace(
			src,
			marker,
			"stream.serialize_int(_s_ri, 0, 3)\n      if self.type == 1\n        _s_ri.value = 1\n        stream.serialize_bits(_s_ri, 1)\n      elsif self.type == 2",
			1,
		))
		for _, pin := range []struct {
			name string
			exe  func(testing.TB) string
		}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
			t.Run(pin.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				_, stderr, err := rubyrun.TryArgs(t, pin.exe(t), dir, driverPath, []string{dir}, nil, nil)
				if err == nil || !strings.Contains(stderr, "FAILED: write mixed-ping") {
					t.Fatalf("sabotaged void arm write did not fail mixed-ping: %v\n%s", err, stderr)
				}
			})
		}
	})
}

func TestRubyPacketReadBitsAndOwnership(t *testing.T) {
	t.Parallel()
	const src = `package packstream

type Tiny
{
    flag bool
    code bits(3)
}

type BlobBox
{
    label string(8)
    raw   bytes(8)
    f32   float32
    f64   float64
}
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Pack.schema")
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadRubyUnit(t, schemaPath)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	rubyrun.Analyze(t, func() string {
		ad := t.TempDir()
		writeTree(t, ad, files)
		return ad
	}(), u.Package)

	probe := `# frozen_string_literal: true

require_relative "packstream"

# 1. Composable bit-packed stream via read_bits at non-zero unaligned offsets.
t1 = Packstream::Tiny.new
t1.flag = true
t1.code = 5

b = Packstream::BlobBox.new
b.label = "hi"
b.raw = "\x11\x22\x33".b
b.f32 = -0.0
b.f64 = 0.0

t2 = Packstream::Tiny.new
t2.flag = false
t2.code = 3

w = Packstream::WriteStream.new(64)
t1._s_write(w)
b._s_write(w)
t2._s_write(w)
w.flush
buf = w.data.dup # mutable input buffer

r1, n1 = Packstream::Tiny.read_bits(buf, bit_offset: 0)
raise "t1 bits #{n1}" unless n1 == 4 && r1 == t1

rb, nb = Packstream::BlobBox.read_bits(buf, bit_offset: n1)
raise "b mismatch" unless rb == b && nb > 0

r2, n2 = Packstream::Tiny.read_bits(buf, bit_offset: n1 + nb)
raise "t2 bits #{n2}" unless n2 == 4 && r2 == t2

# 2. Mutating caller input buffer never mutates decoded bytes(N) field.
buf.setbyte(0, 0xff)
buf.b.length.times { |i| buf.setbyte(i, 0xaa) }
raise "bytes(N) pinned caller buffer" unless rb.raw == "\x11\x22\x33".b && rb.raw.encoding == Encoding::BINARY

# 3. Float -0.0 vs +0.0 bit equality.
pos = Packstream::BlobBox.new
pos.label = "hi"
pos.raw = "\x11\x22\x33".b
pos.f32 = 0.0
pos.f64 = 0.0
raise "-0.0 equaled +0.0" if rb == pos

# 4. Invalid bit_offset and non-String input raise Refused.
[-1, (buf.bytesize * 8) + 1, "0", nil].each do |bad_off|
  begin
    Packstream::Tiny.read_bits(buf, bit_offset: bad_off)
    raise "accepted bad bit_offset #{bad_off.inspect}"
  rescue Packstream::Refused
    nil
  end
end
begin
  Packstream::Tiny.read(123)
  raise "accepted non-String input"
rescue Packstream::Refused
  nil
end

# 5. Text and bytes write contract refusals (NUL, invalid UTF-8, wrong encoding).
bad = Packstream::BlobBox.new
bad.label = "a\x00b"
begin
  bad.write
  raise "accepted NUL in string(N)"
rescue Packstream::InvalidValue
  nil
end

bad = Packstream::BlobBox.new
bad.label = (+"\xff\xfe").force_encoding(Encoding::UTF_8)
begin
  bad.write
  raise "accepted invalid UTF-8"
rescue Packstream::InvalidValue
  nil
end

bad = Packstream::BlobBox.new
bad.label = (+"abc").force_encoding(Encoding::BINARY)
begin
  bad.write
  raise "accepted BINARY string in string(N)"
rescue Packstream::InvalidValue
  nil
end

bad = Packstream::BlobBox.new
bad.raw = "abc" # UTF-8, not ASCII-8BIT
begin
  bad.write
  raise "accepted UTF-8 string in bytes(N)"
rescue Packstream::InvalidValue
  nil
end

puts "ok"
`
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", rubyrun.Run}, {"floor", rubyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			d := t.TempDir()
			writeTree(t, d, files)
			got := strings.TrimSpace(string(run.fn(t, d, "main.rb", map[string][]byte{"main.rb": []byte(probe)})))
			if got != "ok" {
				t.Fatal(got)
			}
		})
	}
}

func TestRubyPacketShapeBench(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadRubyUnit(t, filepath.Join(root, "bench", "corpus", "Bench.schema"))
	straightFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	descFiles, err := GenerateDescriptor(u)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "bench_mixed.bin"))
	if err != nil {
		t.Fatal(err)
	}
	packed, err := os.ReadFile(filepath.Join(root, "bench", "corpus", "variants", "bench_mixed.variants.bin"))
	if err != nil {
		t.Fatal(err)
	}
	compareScript := `# frozen_string_literal: true

require_relative "bench"

golden = File.binread("bench_mixed.bin")
packed = File.binread("bench_mixed.variants.bin")
size = packed.bytesize / 64
variants = Array.new(64) { |i| packed.byteslice(i * size, size) }
raise "variant 0 != golden" if variants[0] != golden

values = variants.map { |d| Bench::BenchMixed.read(d) }
values.each_with_index do |v, i|
  raise "variant #{i} round-trip mismatch" if v.write != variants[i]
end

yjit = defined?(RubyVM::YJIT) && RubyVM::YJIT.enabled?
puts "yjit=#{yjit} variants=#{values.length} bytes=#{size}"
`
	interp := rubyrun.Interpreter(t)
	jits := []string{"--disable-yjit"}
	if probeOut, _, probeErr := rubyrun.TryArgs(t, interp, t.TempDir(), "-e", []string{"print(!defined?(RubyVM::YJIT).nil?)"}, nil, nil); probeErr == nil && strings.TrimSpace(probeOut) == "true" {
		jits = append(jits, "--yjit")
	}
	for _, shape := range []struct {
		name  string
		files map[string][]byte
	}{
		{"straight-line", straightFiles},
		{"descriptor-walk", descFiles},
	} {
		if len(jits) == 0 {
			t.Fatal("jits is empty: the loop below would check nothing")
		}
		for _, jitFlag := range jits {
			t.Run(shape.name+"/"+jitFlag, func(t *testing.T) {
				dir := t.TempDir()
				writeTree(t, dir, shape.files)
				inputs := map[string][]byte{
					"compare_shape.rb":         []byte(compareScript),
					"bench_mixed.bin":          golden,
					"bench_mixed.variants.bin": packed,
				}
				for name, data := range inputs {
					if werr := os.WriteFile(filepath.Join(dir, name), data, 0o644); werr != nil {
						t.Fatal(werr)
					}
				}
				stdout, stderr, rerr := rubyrun.TryArgs(t, interp, dir, jitFlag, []string{"compare_shape.rb"}, nil, nil)
				if rerr != nil || stderr != "" {
					t.Fatalf("%s %s: %v\nstderr:\n%s\nstdout:\n%s", shape.name, jitFlag, rerr, stderr, stdout)
				}
				t.Logf("%s (%s):\n%s", shape.name, jitFlag, strings.TrimSpace(stdout))
			})
		}
	}
}
