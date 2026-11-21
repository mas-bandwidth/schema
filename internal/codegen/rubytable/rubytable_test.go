package rubytable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func loadUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func loadUnitDir(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sources []check.SourceFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema") {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		raw, rerr := os.ReadFile(full)
		if rerr != nil {
			t.Fatal(rerr)
		}
		ast, perrs := parser.Parse(full, raw)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", full, perrs[0])
		}
		base := strings.TrimSuffix(entry.Name(), ".schema")
		sources = append(sources, check.SourceFile{
			Path: full, Name: entry.Name(), Base: base, Bytes: raw, AST: ast,
		})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}

func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runBothRubyPins(t *testing.T, dir, main string, inputs map[string][]byte) string {
	t.Helper()
	cur := string(rubyrun.Run(t, dir, main, inputs))
	flr := string(rubyrun.RunFloor(t, dir, main, inputs))
	if cur != flr {
		t.Fatalf("Ruby pins disagree:\ncurrent (4.0.9):\n%s\nfloor (3.4.12):\n%s", cur, flr)
	}
	return cur
}

func TestRubyVariableTablesAllSurfacesAndStrictAnalyze(t *testing.T) {
	slowtest.Gate(t, "both pinned Ruby interpreters and strict analyzers")

	const schemaSrc = `package vtdemo

const LIMIT = 42

enum Team
{
    Alpha
    Beta
}

enum Shade
{
    Red
    Blue
    OldShade | deprecated
}

flags Perks
{
    Fast
    Strong
    Clever
}

type Vec2
{
    x int16 = 3
    y int16 = -4
}

union PktChoice
{
    empty
    vec Vec2
    number_val int32
}

union ItemChoice
{
    none_arm
    count int64
    child *Node
    inline_vec Vec2
    nums [2]int32
}

fixed table Stats
{
    hp int32 = 100
    mp uint16 = 50
}

table Node
{
    id int32
    alias string(32)
    next *Node
    object_id uint32
}

table Root | integrity = "crc32c"
{
    flag bool = true
    tiny int8 = -5
    big uint64 = 9000
    huge int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    uhuge uint128
    fx fixed(16, 16) | min = -100, max = 100
    ufx ufixed(16, 16) | min = 0, max = 100
    bits_val bits(5)
    f32 float32 = 1.5
    cf32 float32 | min = -10.0, max = 10.0, resolution = 0.25
    f64 float64 = -0.0
    ts timestamp
    label string(16) = "hi"
    free_text []string
    raw bytes(8)
    free_bytes []uint8
    shade Shade = Blue
    perks Perks = { Fast, Clever }
    vec Vec2
    stats Stats
    head *Node
    shared *Node
    blob_b *bytes
    blob_b2 *bytes
    blob_s *string
    opt_i ?int32
    opt_st ?Stats
    opt_arr ?[..3]int32
    opt_map ?map[uint8]string(8)
    fixed_arr [2]int16
    counted_arr [1..3]uint8
    list_i []int32
    list_ptr []*Node
    words []string(8) | total = 32
    maybe_words [..3]?string(8)
    keyed [Team]int32
    scores map[string]int32
    pkt_u PktChoice
    pkt_u_arr [..2]PktChoice
    item_u ItemChoice
    item_u_arr []ItemChoice
    active bool
    if active
    {
        bonus int32 = 7
    }
    else
    {
        penalty int32 = 2
    }
}
`
	u := loadUnit(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	rubyrun.Analyze(t, dir, "vtdemo")

	probe := `# frozen_string_literal: true

require_relative 'vtdemo'

r = Vtdemo::Root.new
raise 'default flag' unless r.flag == true
raise 'default shade' unless r.shade == Vtdemo::Shade::Blue
raise 'default counted min' unless r.counted_arr == [0]

n1 = Vtdemo::Node.new
n1.id = 10
n1.alias = 'alpha'
n1.object_id = 777
n2 = Vtdemo::Node.new
n2.id = 20
n2.alias = 'beta'
n1.next = n2

r.head = n1
r.shared = n2
r.huge = -(1 << 100) + 12345
r.uhuge = (1 << 120) + 999
r.fx = -65536
r.ufx = 131072
r.bits_val = 19
r.cf32 = 3.5
r.f64 = -0.0
r.ts = 1_700_000_000_123_456_789
r.free_text = ['hello pure ruby 🌍', 'second unbounded']
r.raw = "\x01\x02\x03".b
r.free_bytes = [0xfe, 0xff, 0x00, 0x10]

shared_blob = Vtdemo::BlobRef.new("\xaa\xbb\xcc".b)
r.blob_b = shared_blob
r.blob_b2 = shared_blob
begin
  r.save
  raise shared BlobRef accepted
rescue Vtdemo::InvalidValue
  # expected blob_shared refusal
end
r.blob_b = "\xaa\xbb\xcc".b
r.blob_b2 = "\xdd\xee".b
r.blob_s = 'blob text'
r.opt_i = -99
r.opt_st = Vtdemo::Stats.new; r.opt_st.hp = 250
r.opt_arr = [11, 22]
r.opt_map = { 4 => 'four' }
r.fixed_arr = [100, -200]
r.counted_arr = [5, 6]
r.list_i = [1, 2, 3, 4]
r.list_ptr = [n1, nil, n2]
r.words = ['one', 'two']
r.maybe_words = [nil, 'three']
r.keyed[Vtdemo::Team::Alpha] = 111
r.keyed[Vtdemo::Team::Beta] = 222
r.scores = { 'b' => 2, 'a' => 1 }

r.pkt_u.type = Vtdemo::PktChoiceType::NumberVal
r.pkt_u.number_val = 4242
pu = Vtdemo::PktChoice.new
pu.type = Vtdemo::PktChoiceType::Vec
pu.vec.x = 9
pu.vec.y = -9
r.pkt_u_arr = [pu]

r.item_u.type = Vtdemo::ItemChoiceType::Child
r.item_u.child = n2
iu1 = Vtdemo::ItemChoice.new
iu1.type = Vtdemo::ItemChoiceType::Nums
iu1.nums = [7, 8]
r.item_u_arr = [iu1]
r.active = true
r.bonus = 99

# KeyedArray bounds check
begin
  r.keyed[0]
  raise 'accepted key 0'
rescue IndexError
  # expected
end

wire = r.save
raise "measure mismatch: #{r.measure} != #{wire.bytesize}" unless r.measure == wire.bytesize

rep = Vtdemo::LoadReport.new
loaded = Vtdemo::Root.load(wire, rep)
raise 'graph_eq mismatch' unless loaded == r
raise 'shared node lost' unless loaded.head.next.equal?(loaded.shared)
raise 'neg zero lost' unless (1.0 / loaded.f64).infinite? == -1

# Base64 text carrier round-trip
b64 = r.save_text
from_b64 = Vtdemo::Root.load_text(b64)
raise 'base64 round-trip mismatch' unless from_b64 == r

# Strict JSON round-trip
js = r.to_json
from_js = Vtdemo::Root.from_json(js)
raise 'json round-trip mismatch' unless from_js == r

# Hand-text S-expression parse + edit
doc = Vtdemo::Node.from_text_form("; lead comment\n(:id 5 :alias \"orig\" :object_id 9)\n")
raise 'textform parse' unless doc.value.id == 5 && doc.value.alias == 'orig'
doc = doc.edit('alias', '"updated"')
edited_src = doc.write
raise 'comment lost on edit' unless edited_src.include?('; lead comment')
re_doc = Vtdemo::Node.read_text(edited_src)
raise 'textform edit' unless re_doc.value.alias == 'updated' && re_doc.value.id == 5

# Cycle refusal on save
n2.next = n1
begin
  r.save
  raise 'cycle accepted'
rescue Vtdemo::InvalidValue
  n2.next = nil
end

# Deprecated enum refusal on save
r.shade = Vtdemo::Shade::OldShade
begin
  r.save
  raise 'deprecated enum accepted'
rescue Vtdemo::InvalidValue
  r.shade = Vtdemo::Shade::Blue
end

# Cook little + big endian + check_cook + open_cook
cooked_le = r.cook(order: 1)
raise 'cook_measure mismatch' unless r.cook_measure == cooked_le.bytesize
Vtdemo::Root.cook_check(cooked_le)
handle = Vtdemo::Root.cook_open(cooked_le)
raise 'cook root mismatch' unless handle.root.equal?(Vtdemo::Root)
n_cooked = n1.cook(order: 1)
n_handle = Vtdemo::Node.cook_open(n_cooked)
dump_le = Vtdemo::SchemaTable.dump_cook(n_handle)
raise 'empty cook dump' unless dump_le.include?('node 0 Node')
cooked_be = r.cook(order: 2)
raise 'big endian size mismatch' unless cooked_be.bytesize == cooked_le.bytesize

# UnitView check
uv = Vtdemo.unit_view
raise 'unit_view package' unless uv.package == 'vtdemo'
raise 'unit_view tables' unless uv.tables.map(&:name) == %w[Node Root Stats]

puts wire.unpack1('H*')
puts cooked_le.unpack1('H*')
`
	out := runBothRubyPins(t, dir, "probe.rb", map[string][]byte{"probe.rb": []byte(probe)})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected probe lines: %q", out)
	}

	// Verify Go tablewire decodes the exact CRC-sealed wire Ruby produced and re-encodes byte-identically!
	rbWire, err := hex.DecodeString(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	goRoot := model.New(model.Lookup("Root"))
	var goRep tabletext.Report
	if ok, derr := tablewire.Decode(model, goRoot, rbWire, &goRep); !ok || derr != nil {
		t.Fatalf("Go tablewire refused Ruby wire: ok=%v err=%v rep=%+v", ok, derr, goRep)
	}
	goWire, err := tablewire.Encode(model, goRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rbWire, goWire) {
		t.Fatalf("Ruby wire != Go canonical wire:\nrb=%x\ngo=%x", rbWire, goWire)
	}

	// Verify Go tablecook produces the exact same cooked image!
	rbCook, err := hex.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	goCook, cerr := tablecook.Cook(model, goRoot, tablecook.Options{})
	if cerr != nil {
		t.Fatalf("Go tablecook: %v", cerr)
	}
	if !bytes.Equal(rbCook, goCook) {
		t.Fatalf("Ruby cook != Go canonical cook:\nrb=%x\ngo=%x", rbCook, goCook)
	}
}

func TestRubyMessageFormAndRetainUnknown(t *testing.T) {
	slowtest.Gate(t, "both pinned Ruby interpreters and strict analyzers")

	const v1Src = `package msgret

fixed table Ping | integrity = "crc32c"
{
    seq uint32
    code int16
}

table Child
{
    x int32
}

table Holder
{
    keep int32
    child Child
}
`
	const v2Src = `package msgret

fixed table Ping | integrity = "crc32c"
{
    seq uint32
    code int16
    extra uint16
}

table Child
{
    x int32
    secret string(16)
}

table Holder
{
    keep int32
    child Child
    added string(16)
}
`
	u1 := loadUnit(t, v1Src)
	u2 := loadUnit(t, v2Src)

	f1, err := Generate(u1)
	if err != nil {
		t.Fatal(err)
	}
	dir1 := t.TempDir()
	writeTree(t, dir1, f1)
	rubyrun.Analyze(t, dir1, "msgret")

	f2, err := Generate(u2)
	if err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	writeTree(t, dir2, f2)
	rubyrun.Analyze(t, dir2, "msgret")

	// Produce v2 message batch + announcement and v2 Holder wire in dir2
	writerProbe := `# frozen_string_literal: true

require_relative 'msgret'

p1 = Msgret::Ping.new
p1.seq = 10
p1.code = -3
p1.extra = 99
p2 = Msgret::Ping.new
p2.seq = 20
p2.code = 7
p2.extra = 100
batch = Msgret.save_messages(Msgret::Ping, [p1, p2])
raise 'msg measure mismatch' unless Msgret.measure_messages(Msgret::Ping, [p1, p2]) == batch.bytesize

h = Msgret::Holder.new
h.keep = 42
h.child.x = 7
h.child.secret = 'child-unknown'
h.added = 'root-unknown'
hwire = h.save

puts Msgret::ANNOUNCEMENT.unpack1('H*')
puts batch.unpack1('H*')
puts hwire.unpack1('H*')
`
	wOut := strings.Split(strings.TrimSpace(runBothRubyPins(t, dir2, "writer.rb", map[string][]byte{"writer.rb": []byte(writerProbe)})), "\n")
	annBytes, _ := hex.DecodeString(wOut[0])
	batchBytes, _ := hex.DecodeString(wOut[1])
	hwireBytes, _ := hex.DecodeString(wOut[2])

	// Read v2 messages and retain v2 Holder unknown fields in dir1 (v1), mutate keep, save_retain!
	readerProbe := `# frozen_string_literal: true

require_relative 'msgret'

ann = File.binread('ann.bin')
batch = File.binread('batch.bin')
hwire = File.binread('hwire.bin')

strict_vocab = Msgret::MessageVocabulary.new(strict: true)
begin
  Msgret.announce_read(strict_vocab, ann)
  raise 'strict vocab accepted foreign build version'
rescue Msgret::BuildVersionMismatch
  # expected
end

vocab = Msgret::MessageVocabulary.new
Msgret.announce_read(vocab, ann)
mrep = Msgret::LoadReport.new
msgs = Msgret.load_messages(Msgret::Ping, vocab, batch, 16, mrep)
raise 'msg count' unless msgs.length == 2
raise 'msg values' unless msgs[0].seq == 10 && msgs[0].code == -3 && msgs[1].seq == 20 && msgs[1].code == 7
raise "msg unknown #{mrep.unknown}" unless mrep.unknown == 2

retain = Msgret::TableRetain.new
rrep = Msgret::LoadReport.new
holder = Msgret::Holder.load_retain(hwire, retain, rrep)
raise 'holder keep' unless holder.keep == 42 && holder.child.x == 7
raise "retain counts #{rrep.unknown}/#{rrep.retained}" unless rrep.unknown == 2 && rrep.retained == 2

holder.keep = 99
srep = Msgret::LoadReport.new
resaved = holder.save_retain(retain, srep)
raise 'measure_retain mismatch' unless holder.measure_retain(retain) == resaved.bytesize
raise 'retain_lost nonzero' unless srep.retain_lost == 0
puts resaved.unpack1('H*')
`
	rOut := strings.TrimSpace(runBothRubyPins(t, dir1, "reader.rb", map[string][]byte{
		"reader.rb": []byte(readerProbe),
		"ann.bin":   annBytes,
		"batch.bin": batchBytes,
		"hwire.bin": hwireBytes,
	}))
	resavedBytes, _ := hex.DecodeString(rOut)

	// Verify in v2 that both retained unknown fields ('child-unknown' and 'root-unknown') survived intact!
	verifyProbe := `# frozen_string_literal: true

require_relative 'msgret'

h = Msgret::Holder.load(File.binread('resaved.bin'))
raise "keep=#{h.keep}" unless h.keep == 99
raise "x=#{h.child.x}" unless h.child.x == 7
raise "secret=#{h.child.secret}" unless h.child.secret == 'child-unknown'
raise "added=#{h.added}" unless h.added == 'root-unknown'
puts 'ok'
`
	vOut := strings.TrimSpace(runBothRubyPins(t, dir2, "verify.rb", map[string][]byte{
		"verify.rb":   []byte(verifyProbe),
		"resaved.bin": resavedBytes,
	}))
	if vOut != "ok" {
		t.Fatalf("retain round-trip failed: %q", vOut)
	}
}

func TestRubyBlockManifestAndRealUnitsAnalyze(t *testing.T) {
	slowtest.Gate(t, "both pinned Ruby interpreters and strict analyzers")

	repo := filepath.Join("..", "..", "..")
	for _, relDir := range []string{
		filepath.Join("tables", "arms"),
		filepath.Join("tables", "blobs"),
		filepath.Join("tables", "block"),
		filepath.Join("tables", "lists"),
		filepath.Join("tables", "maps"),
		filepath.Join("tables", "pointers"),
		filepath.Join("tables", "scalars"),
		filepath.Join("tables", "stream"),
		filepath.Join("tables", "vocab"),
		filepath.Join("tables", "vocab9"),
		filepath.Join("tables", "widelist"),
	} {
		u := loadUnitDir(t, filepath.Join(repo, relDir))
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate %s: %v", relDir, err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		rubyrun.Analyze(t, dir, u.Package)
	}

	// Verify packet-only AddView + Analyze
	pktUnit := loadUnit(t, "package pktview\nconst K = 3\nenum E { A\nB }\ntype P { x int32\ne E }\n")
	pktFiles, err := ruby.Generate(pktUnit)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddView(pktUnit, pktFiles); err != nil {
		t.Fatal(err)
	}
	pktDir := t.TempDir()
	writeTree(t, pktDir, pktFiles)
	rubyrun.Analyze(t, pktDir, "pktview")
	pvOut := strings.TrimSpace(runBothRubyPins(t, pktDir, "pv.rb", map[string][]byte{
		"pv.rb": []byte("# frozen_string_literal: true\nrequire_relative 'pktview'\nuv = Pktview.unit_view\nputs \"#{uv.package}:#{uv.types.map(&:name).join(',')}:#{uv.enums.map(&:name).join(',')}\"\n"),
	}))
	if pvOut != "pktview:P:E" {
		t.Fatalf("unexpected pktview output: %q", pvOut)
	}

	// Verify every block image in MANIFEST.txt matches Go's canonical dump byte-for-byte!
	manifestRaw, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	manifestUnits := map[string]string{}
	for line := range strings.SplitSeq(string(manifestRaw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" {
			p := fields[2]
			if strings.HasSuffix(p, ".schema") {
				p = filepath.Dir(p)
			}
			manifestUnits[fields[1]] = p
		}
	}
	blockFilesByUnit := map[string]map[string][]byte{}
	blockPkgByUnit := map[string]string{}
	checkedBlocks := 0
	for line := range strings.SplitSeq(string(manifestRaw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "block" {
			continue
		}
		name, unitKey, wireRel, dumpRel := fields[1], fields[2], fields[3], fields[4]
		blockFiles, ok := blockFilesByUnit[unitKey]
		if !ok {
			bu := loadUnitDir(t, filepath.Join(repo, manifestUnits[unitKey]))
			var err error
			blockFiles, err = Generate(bu)
			if err != nil {
				t.Fatal(err)
			}
			blockFilesByUnit[unitKey] = blockFiles
			blockPkgByUnit[unitKey] = bu.Package
		}
		pkg := blockPkgByUnit[unitKey]
		modName := strings.ToUpper(pkg[:1]) + pkg[1:]
		image, err := os.ReadFile(filepath.Join(repo, wireRel))
		if err != nil {
			t.Fatal(err)
		}
		wantDump, err := os.ReadFile(filepath.Join(repo, dumpRel))
		if err != nil {
			t.Fatal(err)
		}
		firstLine, _, _ := strings.Cut(string(wantDump), "\n")
		parts := strings.Fields(firstLine)
		rootName := parts[1]

		dir := t.TempDir()
		writeTree(t, dir, blockFiles)
		probe := fmt.Sprintf("# frozen_string_literal: true\nrequire_relative '%s'\nh = %s::%s.block_open(File.binread('img.bin'))\n$stdout.write(%s::SchemaTable.block_dump(h))\n", pkg, modName, rootName, modName)
		got := runBothRubyPins(t, dir, "bprobe.rb", map[string][]byte{
			"bprobe.rb": []byte(probe),
			"img.bin":   image,
		})
		if got != string(wantDump) {
			t.Fatalf("block %s dump mismatch:\ngot:\n%s\nwant:\n%s", name, got, string(wantDump))
		}
		checkedBlocks++
	}
	if checkedBlocks == 0 {
		t.Fatal("MANIFEST.txt checked 0 block cases")
	}
	_ = sort.Strings
}
