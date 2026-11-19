package main

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// schemaKeywords lists identifiers that the .schema parser itself reserves as
// keywords or built-in type tokens and therefore cannot appear bare as field
// or declaration names. Target-language keywords that .schema permits ARE kept
// in our minefield on purpose!
var schemaKeywords = map[string]bool{
	"package": true, "import": true, "const": true, "type": true, "table": true,
	"fixed": true, "enum": true, "flags": true, "union": true, "map": true,
	"if": true, "else": true, "true": true, "false": true, "void": true,
	"bool": true, "string": true, "bytes": true, "bits": true, "timestamp": true,
	"int8": true, "int16": true, "int32": true, "int64": true, "int128": true,
	"uint8": true, "uint16": true, "uint32": true, "uint64": true, "uint128": true,
	"float32": true, "float64": true, "ufixed": true,
}

// rustVarShadowed lists Rust stdlib prelude names that rusttable intentionally
// excludes from variable-wire emission (docs/SPEC-TABLES.md §11).
var rustVarShadowed = map[string]bool{
	"Any": true, "Box": true, "Clone": true, "Copy": true, "Debug": true,
	"Default": true, "HashMap": true, "Layout": true, "Option": true,
	"PartialEq": true, "Rc": true, "Result": true, "String": true, "Vec": true,
}

// fieldMinefield contains legal-under-SPEC-§4.6 cross-language runtime helper
// names, Go-escaped keywords, companion suffixes, casing edge cases, and
// 1-letter identifiers that stress codegen across all 18 languages.
var fieldMinefield = []string{
	"tag", "kind", "size", "len", "length", "count", "items", "data",
	"buf", "buffer", "value", "val", "ok", "err", "error", "result",
	"id", "root", "view", "msg", "cls", "func", "chan", "defer",
	"fallthrough", "select", "go", "to", "present", "out_val",
	"snake_case_name", "MixCase99", "a", "x", "next", "prev",
	"key", "slot", "index", "offset", "limit", "cap", "flags_val",
	"state", "mode", "code", "label", "report", "retain", "vocab",
	"plan", "stream", "reader", "writer", "bytes_val", "object_id",
	"wire", "json", "text", "table_id",
}

// variantMinefield contains PascalCase/identifier variants that collide with
// target-language runtime types, Go keywords, or enum/union helper methods.
var variantMinefield = []string{
	"Tag", "Kind", "Size", "Len", "Length", "Items", "Data",
	"Buf", "Buffer", "Value", "Val", "Ok", "Err", "Error", "Result",
	"Id", "Root", "View", "Msg", "Cls", "Func", "Chan", "Defer",
	"To", "Present", "OutVal", "MixCase99", "Alpha", "Beta", "Gamma",
	"Record", "Stream", "Reader", "Writer", "Report", "Retain", "Plan", "Hash",
}

// armMinefield contains union arm names from the cross-language minefield.
var armMinefield = []string{
	"tag", "kind", "size", "len", "length", "count", "items", "data",
	"buf", "buffer", "value", "val", "ok", "err", "error", "result",
	"id", "root", "view", "msg", "cls", "func", "chan", "defer",
	"fallthrough", "select", "go", "to", "present", "out_val",
	"snake_case_name", "MixCase99", "report", "retain", "vocab", "plan",
}

// LineageBreachSpec describes how to mutate a valid V1 Form-3 image so that its
// bytes satisfy V2's current bounds while violating V1's historical writer
// bounds recorded in V2's schema.lock.
type LineageBreachSpec struct {
	FieldV1   string
	RawOffset int    // byte offset inside V1 FixedRoot Form-3 image (computed from IR)
	RawWidth  int    // byte width in V1 FixedRoot Form-3 image
	GoodLE    []byte // little-endian legal bytes proving RawOffset hits this field
	BadLE     []byte // little-endian replacement bytes violating V1 writer bound
}

// SchemaPair holds one generated (V1, V2, V2.schema.lock) adversarial pair.
type SchemaPair struct {
	Index       int
	Name        string // e.g. "tg01"
	PkgV1       string // e.g. "tg01v1"
	PkgV2       string // e.g. "tg01v2"
	FileBase    string // e.g. "Tg01"
	DirV1       string
	DirV2       string
	V1Text      string
	V2Text      string
	LockText    string
	UnitV1      *ir.Unit
	UnitV2      *ir.Unit
	FixRootV1   string
	FixRootV2   string
	BlkRootV1   string
	BlkRootV2   string
	VarRootV1   string
	VarRootV2   string
	BreachSpecs []LineageBreachSpec
}

type namePool struct {
	rng  *rand.Rand
	used map[string]bool
}

func newNamePool(rng *rand.Rand) *namePool {
	return &namePool{
		rng:  rng,
		used: map[string]bool{},
	}
}

func (p *namePool) pick(candidates []string, fallbackPrefix string) string {
	start := p.rng.IntN(len(candidates))
	for i := range len(candidates) {
		c := candidates[(start+i)%len(candidates)]
		if schemaKeywords[c] || rustVarShadowed[c] || p.used[strings.ToLower(c)] {
			continue
		}
		p.used[strings.ToLower(c)] = true
		return c
	}
	for n := 1; ; n++ {
		c := fmt.Sprintf("%s_%d", fallbackPrefix, n)
		if !p.used[strings.ToLower(c)] {
			p.used[strings.ToLower(c)] = true
			return c
		}
	}
}

type scalarWidenChoice struct {
	v1Type string
	v1Qual string
	v2Type string
	v2Qual string
	defLit string
	// For historical V1 writer-bound breach in Form 3:
	hasBreach bool
	breachVal int64 // fits V2, outside V1 writer range!
	v1Bytes   int
}

func formatScalarDecl(name, typ, defLit, qual, wasOld string) string {
	var q []string
	if qual != "" {
		q = append(q, qual)
	}
	if wasOld != "" {
		q = append(q, fmt.Sprintf("was = %q", wasOld))
	}
	if len(q) > 0 {
		return fmt.Sprintf("    %s %s = %s | %s\n", name, typ, defLit, strings.Join(q, ", "))
	}
	return fmt.Sprintf("    %s %s = %s\n", name, typ, defLit)
}

var fixedScalarWidenings = []scalarWidenChoice{
	{
		v1Type:    "int8",
		v1Qual:    "min = -20, max = 20",
		v2Type:    "int32",
		v2Qual:    "min = -5000, max = 5000",
		defLit:    "-7",
		hasBreach: true,
		breachVal: 55,
		v1Bytes:   1,
	},
	{
		v1Type:    "int16",
		v1Qual:    "min = -200, max = 200",
		v2Type:    "int64",
		v2Qual:    "min = -100000, max = 100000",
		defLit:    "-123",
		hasBreach: true,
		breachVal: 500,
		v1Bytes:   2,
	},
	{
		v1Type:    "uint8",
		v1Qual:    "min = 5, max = 50",
		v2Type:    "uint32",
		v2Qual:    "min = 0, max = 50000",
		defLit:    "12",
		hasBreach: true,
		breachVal: 120,
		v1Bytes:   1,
	},
	{
		v1Type:    "bits(5)",
		v2Type:    "bits(37)",
		defLit:    "19",
		hasBreach: true,
		breachVal: 45, // > 31 (2^5 - 1), fits in uint8 V1 storage byte!
		v1Bytes:   1,
	},
	{
		v1Type:    "fixed(28, 4)",
		v1Qual:    "min = -100, max = 100",
		v2Type:    "fixed(124, 4)",
		v2Qual:    "min = -100000, max = 100000",
		defLit:    "-12.5",
		hasBreach: true,
		breachVal: 3200, // 200.0 * 16 = 3200 > 100.0 * 16 = 1600!
		v1Bytes:   4,
	},
	{
		v1Type:    "float32",
		v2Type:    "float64",
		defLit:    "1.5",
		hasBreach: false,
	},
	{
		v1Type:    "int32",
		v1Qual:    "min = -1000, max = 1000",
		v2Type:    "int64",
		v2Qual:    "min = -1000000, max = 1000000",
		defLit:    "-42",
		hasBreach: true,
		breachVal: 5000,
		v1Bytes:   4,
	},
	{
		v1Type:    "ufixed(12, 4)",
		v1Qual:    "min = 10, max = 500",
		v2Type:    "ufixed(124, 4)",
		v2Qual:    "min = 0, max = 100000",
		defLit:    "77.5",
		hasBreach: true,
		breachVal: 16000, // 1000.0 * 16 = 16000 > 500 * 16 = 8000, fits uint16!
		v1Bytes:   2,
	},
}

var escapedDefaultStrings = []string{
	`"hi 'q' α"`,
	`"path\dir 'x'"`,
	`"café #{v} 99"`,
	`"slot ${x} %s"`,
	`"slash\end 'z'"`,
	`"Ω-delta [0]/"`,
}

// GenerateSchemaPairs deterministically builds count adversarial (V1, V2, lock)
// schema pairs in outRoot, validates them through compiler.Load, and writes
// V2's 2-generation schema.lock via compiler.UpdateSchemaLock.
func GenerateSchemaPairs(outRoot string, seed uint64, count int) ([]*SchemaPair, error) {
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	pairs := make([]*SchemaPair, 0, count)
	for idx := range count {
		pair, err := buildOnePair(outRoot, idx, rng)
		if err != nil {
			return nil, fmt.Errorf("schema pair %d: %w", idx, err)
		}
		pairs = append(pairs, pair)
	}
	return pairs, nil
}

func buildOnePair(outRoot string, idx int, rng *rand.Rand) (*SchemaPair, error) {
	tag := fmt.Sprintf("tg%02d", idx+1)
	pkgV1 := tag + "v1"
	pkgV2 := tag + "v2"
	fileBase := fmt.Sprintf("Tg%02d", idx+1)

	declPool := newNamePool(rng)
	enumName := declPool.pick([]string{"Mode", "Grade", "Phase", "Tier", "Channel", "Route"}, "Enum")
	flagsName := declPool.pick([]string{"Perms", "Mask", "Gates", "Badges", "Traits"}, "Flags")
	subFixName := declPool.pick([]string{"Cell", "Patch", "Span", "Stamp", "Metric"}, "SubFix")
	blkRowName := declPool.pick([]string{"BlkRow", "SlotRow", "TrackRow", "TileRow"}, "BlkRow")
	fixUnionName := declPool.pick([]string{"FixPick", "Choice", "Action", "Dispatch"}, "FixPick")
	varNodeName := declPool.pick([]string{"NodeRec", "BranchRec", "StepRec", "LinkRec"}, "NodeRec")
	varSubName := declPool.pick([]string{"SubBox", "ItemBox", "LeafBox", "MetaBox"}, "SubBox")
	varUnionName := declPool.pick([]string{"VarPick", "EventPick", "PayloadPick"}, "VarPick")
	mapValName := declPool.pick([]string{"Bucket", "Holder", "EntryBox", "Envelope"}, "Bucket")

	// Reserve root names
	fixRootV1 := "FixRoot"
	fixRootV2 := "FixRoot"
	fixRootWas := ""
	if idx%3 == 1 {
		// Note: Form 3 FixedLoad matches by layout hash on the table name in schema.lock!
		// Let's check whether `was =` on a `fixed table` carries schema.lock lineage across table renames:
		// Keep FixRoot stable for Form 3 schema.lock lineage, and exercise `table ... | was = ...` on VarRoot!
		_ = fixRootWas
	}
	blkRootV1 := "BlkRoot"
	blkRootV2 := "BlkRoot"
	varRootV1 := "VarRoot"
	varRootV2 := "VarRoot"
	varRootWasAttr := ""
	if idx%2 == 1 {
		varRootV2 = "VarRootNext"
		varRootWasAttr = ` | was = "VarRoot"`
	}

	// Enum variants (4 in V1 -> rename variant #2 with `was` + append 1 variant in V2; default is variant #2 or #3!)
	evPool := newNamePool(rng)
	// Avoid "None" as an enum variant name because generated Go/C#/etc. emit <Enum>None for 0!
	evCand := make([]string, 0, len(variantMinefield))
	for _, v := range variantMinefield {
		if v != "None" {
			evCand = append(evCand, v)
		}
	}
	ev0 := evPool.pick(evCand, "V0")
	ev1Old := evPool.pick(evCand, "V1")
	ev1New := evPool.pick(evCand, "V1R")
	ev2 := evPool.pick(evCand, "V2")
	ev3Add := evPool.pick(evCand, "V3")

	// Flags bits (3 in V1 -> +2 in V2; multi-bit default!)
	flPool := newNamePool(rng)
	fl0 := flPool.pick(evCand, "F0")
	fl1 := flPool.pick(evCand, "F1")
	fl2 := flPool.pick(evCand, "F2")
	fl3Add := flPool.pick(evCand, "F3")
	fl4Add := flPool.pick(evCand, "F4")

	// SubFix fields (fixed struct used inside FixRoot, FixUnion, Keyed array, and VarPick!)
	sfPool := newNamePool(rng)
	sfAOld := sfPool.pick(fieldMinefield, "sfa")
	sfANew := sfPool.pick(fieldMinefield, "sfa_r")
	sfB := sfPool.pick(fieldMinefield, "sfb")
	sfCAdd := sfPool.pick(fieldMinefield, "sfc")
	sfStrDef := escapedDefaultStrings[idx%len(escapedDefaultStrings)]

	// BlkRow fields (union-free, pointer-free for Block)
	brPool := newNamePool(rng)
	brFlag := brPool.pick(fieldMinefield, "brf")
	brVal := brPool.pick(fieldMinefield, "brv")
	brTxt := brPool.pick(fieldMinefield, "brt")

	// FixUnion arms: void arm + widened scalar arm (renamed with `was`!) + SubFix struct arm + V2 added arm!
	fuPool := newNamePool(rng)
	fuVoid := fuPool.pick(armMinefield, "arm_void")
	fuNumOld := fuPool.pick(armMinefield, "arm_num")
	fuNumNew := fuPool.pick(armMinefield, "arm_num_r")
	fuStruct := fuPool.pick(armMinefield, "arm_sub")
	fuAdd := fuPool.pick(armMinefield, "arm_extra")

	// Pick two distinct scalar widenings for FixRoot so every pair exercises multiple widenings + historical writer bounds!
	wChoice1 := fixedScalarWidenings[(idx*2)%len(fixedScalarWidenings)]
	wChoice2 := fixedScalarWidenings[(idx*2+3)%len(fixedScalarWidenings)]
	if wChoice2.v1Type == wChoice1.v1Type {
		wChoice2 = fixedScalarWidenings[(idx*2+1)%len(fixedScalarWidenings)]
	}

	// FixRoot fields (drawn from the cross-language identifier minefield!)
	frPool := newNamePool(rng)
	frWiden1Old := frPool.pick(fieldMinefield, "fw1")
	frWiden1New := frPool.pick(fieldMinefield, "fw1_r")
	frWiden2 := frPool.pick(fieldMinefield, "fw2")
	frEnum := frPool.pick(fieldMinefield, "fenum")
	frFlags := frPool.pick(fieldMinefield, "fflags")
	frStr := frPool.pick(fieldMinefield, "fstr")
	frBytes := frPool.pick(fieldMinefield, "fbytes")
	frI64Big := frPool.pick(fieldMinefield, "fi64")
	frU64Big := frPool.pick(fieldMinefield, "fu64")
	frI128 := frPool.pick(fieldMinefield, "fi128")
	frU128 := frPool.pick(fieldMinefield, "fu128")
	frQuant := frPool.pick(fieldMinefield, "fquant")
	frBits64 := frPool.pick(fieldMinefield, "fbits64")
	frOptSub := frPool.pick(fieldMinefield, "foptsub")
	frFixArr := frPool.pick(fieldMinefield, "ffixarr")
	frCntArr := frPool.pick(fieldMinefield, "fcntarr")
	frKeyed := frPool.pick(fieldMinefield, "fkeyed")
	frUnion := frPool.pick(fieldMinefield, "funion")
	frAddV2 := frPool.pick(fieldMinefield, "faddv2")
	frAddStrV2 := frPool.pick(fieldMinefield, "faddstr")

	// BlkRoot fields (union-free, pointer-free -> emits Block surface!)
	bkPool := newNamePool(rng)
	bkLead := bkPool.pick(fieldMinefield, "bklead")
	bkWide := bkPool.pick(fieldMinefield, "bkwide")
	bkRows := bkPool.pick(fieldMinefield, "bkrows")
	bkKeyed := bkPool.pick(fieldMinefield, "bkkeyed")
	bkOpt := bkPool.pick(fieldMinefield, "bkopt")
	bkAdd := bkPool.pick(fieldMinefield, "bkadd")

	// VarNode (recursive/DAG pointer target `*NodeRec`)
	vnPool := newNamePool(rng)
	vnLabelOld := vnPool.pick(fieldMinefield, "vnl")
	vnLabelNew := vnPool.pick(fieldMinefield, "vnl_r")
	vnMetric := vnPool.pick(fieldMinefield, "vnm")
	vnNext := vnPool.pick(fieldMinefield, "vnn")
	vnBlob := vnPool.pick(fieldMinefield, "vnb")
	vnAdd := vnPool.pick(fieldMinefield, "vna")

	// VarPick union (void + scalar + fixed-struct SubFix + variable-table VarSub!)
	vuPool := newNamePool(rng)
	vuVoid := vuPool.pick(armMinefield, "varm_void")
	vuScalarOld := vuPool.pick(armMinefield, "varm_sc")
	vuScalarNew := vuPool.pick(armMinefield, "varm_sc_r")
	vuFixSub := vuPool.pick(armMinefield, "varm_fix")
	vuVarSub := vuPool.pick(armMinefield, "varm_var")
	vuAdd := vuPool.pick(armMinefield, "varm_add")

	// VarSub (variable table holding VarPick -> used inside `[Enum]VarSub` inside `Bucket` inside `map[string]Bucket`!)
	vsPool := newNamePool(rng)
	vsPick := vsPool.pick(fieldMinefield, "vspick")
	vsScore := vsPool.pick(fieldMinefield, "vsscore")
	vsAdd := vsPool.pick(fieldMinefield, "vsadd")

	// MapVal (`Bucket`: variable table holding `[Enum]VarSub`, which holds `VarPick`, which holds `SubFix`!)
	mvPool := newNamePool(rng)
	mvByMode := mvPool.pick(fieldMinefield, "mvbymode")
	mvTag := mvPool.pick(fieldMinefield, "mvtag")
	mvAdd := mvPool.pick(fieldMinefield, "mvadd")

	// VarRoot fields
	vrPool := newNamePool(rng)
	vrGuard := vrPool.pick(fieldMinefield, "vrguard")
	vrHeadOld := vrPool.pick(fieldMinefield, "vrhead")
	vrHeadNew := vrPool.pick(fieldMinefield, "vrhead_r")
	vrAlias := vrPool.pick(fieldMinefield, "vralias")
	vrMapStr := vrPool.pick(fieldMinefield, "vrmapstr")
	vrMapI64 := vrPool.pick(fieldMinefield, "vrmapi64")
	vrList := vrPool.pick(fieldMinefield, "vrlist")
	vrBounded := vrPool.pick(fieldMinefield, "vrbounded")
	vrText := vrPool.pick(fieldMinefield, "vrtext")
	vrData := vrPool.pick(fieldMinefield, "vrdata")
	vrEnum := vrPool.pick(fieldMinefield, "vrenum")
	vrFlags := vrPool.pick(fieldMinefield, "vrflags")
	vrGuardedVal := vrPool.pick(fieldMinefield, "vrguarded")
	vrAddV2 := vrPool.pick(fieldMinefield, "vraddv2")
	vrAddPtrV2 := vrPool.pick(fieldMinefield, "vraddptr")

	strDef2 := escapedDefaultStrings[(idx+2)%len(escapedDefaultStrings)]
	i64Def := int64(9007199254740993) + int64(idx*17)
	if idx%2 == 1 {
		i64Def = -i64Def
	}
	u64Def := uint64(9223372036854775809) + uint64(idx*31)
	i128Def := "-17014118346046923173168730371588410572"
	u128Def := "34028236692093846346337460743176821145" // > 2^64, fits u128!

	render := func(v2 bool) string {
		var b strings.Builder
		pkg := pkgV1
		if v2 {
			pkg = pkgV2
		}
		fmt.Fprintf(&b, "package %s\n\n", pkg)

		// Enum
		fmt.Fprintf(&b, "enum %s {\n", enumName)
		fmt.Fprintf(&b, "    %s\n", ev0)
		if v2 {
			fmt.Fprintf(&b, "    %s | was = %q\n", ev1New, ev1Old)
		} else {
			fmt.Fprintf(&b, "    %s\n", ev1Old)
		}
		fmt.Fprintf(&b, "    %s\n", ev2)
		if v2 {
			fmt.Fprintf(&b, "    %s\n", ev3Add)
		}
		b.WriteString("}\n\n")

		// Flags
		fmt.Fprintf(&b, "flags %s {\n", flagsName)
		fmt.Fprintf(&b, "    %s\n    %s\n    %s\n", fl0, fl1, fl2)
		if v2 {
			fmt.Fprintf(&b, "    %s\n    %s\n", fl3Add, fl4Add)
		}
		b.WriteString("}\n\n")

		// SubFix fixed table (carries its own 2-generation lineage in schema.lock!)
		fmt.Fprintf(&b, "fixed table %s {\n", subFixName)
		if v2 {
			fmt.Fprintf(&b, "    %s int64 = -19 | was = %q\n", sfANew, sfAOld)
		} else {
			fmt.Fprintf(&b, "    %s int16 = -19\n", sfAOld)
		}
		fmt.Fprintf(&b, "    %s string(24) = %s\n", sfB, sfStrDef)
		if v2 {
			fmt.Fprintf(&b, "    %s uint32 = 777\n", sfCAdd)
		}
		b.WriteString("}\n\n")

		// BlkRow fixed table
		fmt.Fprintf(&b, "fixed table %s {\n", blkRowName)
		fmt.Fprintf(&b, "    %s bool = true\n", brFlag)
		if v2 {
			fmt.Fprintf(&b, "    %s int64 = -99\n", brVal)
		} else {
			fmt.Fprintf(&b, "    %s int32 = -99\n", brVal)
		}
		fmt.Fprintf(&b, "    %s string(16) = \"row\"\n", brTxt)
		b.WriteString("}\n\n")

		// FixUnion (in Form 3 schema.lock: existing arm keeps its payload type while renaming via `was` + appending a new arm!)
		fmt.Fprintf(&b, "union %s {\n", fixUnionName)
		fmt.Fprintf(&b, "    %s\n", fuVoid)
		if v2 {
			fmt.Fprintf(&b, "    %s int16 | was = %q\n", fuNumNew, fuNumOld)
		} else {
			fmt.Fprintf(&b, "    %s int16\n", fuNumOld)
		}
		fmt.Fprintf(&b, "    %s %s\n", fuStruct, subFixName)
		if v2 {
			fmt.Fprintf(&b, "    %s string(16)\n", fuAdd)
		}
		b.WriteString("}\n\n")

		// FixRoot (Form 1, Form 2, Form 3 + schema.lock lineage + JSON!)
		fmt.Fprintf(&b, "fixed table %s {\n", fixRootV1)
		if v2 {
			b.WriteString(formatScalarDecl(frWiden1New, wChoice1.v2Type, wChoice1.defLit, wChoice1.v2Qual, frWiden1Old))
			b.WriteString(formatScalarDecl(frWiden2, wChoice2.v2Type, wChoice2.defLit, wChoice2.v2Qual, ""))
		} else {
			b.WriteString(formatScalarDecl(frWiden1Old, wChoice1.v1Type, wChoice1.defLit, wChoice1.v1Qual, ""))
			b.WriteString(formatScalarDecl(frWiden2, wChoice2.v1Type, wChoice2.defLit, wChoice2.v1Qual, ""))
		}
		fmt.Fprintf(&b, "    %s %s = %s\n", frEnum, enumName, ev2)
		fmt.Fprintf(&b, "    %s %s = { %s, %s }\n", frFlags, flagsName, fl0, fl2)
		fmt.Fprintf(&b, "    %s string(32) = %s\n", frStr, strDef2)
		fmt.Fprintf(&b, "    %s bytes(12) = \"ab9\"\n", frBytes)
		fmt.Fprintf(&b, "    %s int64 = %d\n", frI64Big, i64Def)
		fmt.Fprintf(&b, "    %s uint64 = %d\n", frU64Big, u64Def)
		fmt.Fprintf(&b, "    %s int128 = %s | min = -34028236692093846346337460743176821145, max = 34028236692093846346337460743176821145\n", frI128, i128Def)
		fmt.Fprintf(&b, "    %s uint128 = %s\n", frU128, u128Def)
		fmt.Fprintf(&b, "    %s float32 = -2.5 | min = -10.0, max = 10.0, resolution = 0.25\n", frQuant)
		fmt.Fprintf(&b, "    %s bits(64) = 0x8000000000000005\n", frBits64)
		fmt.Fprintf(&b, "    %s ?%s\n", frOptSub, subFixName)
		fmt.Fprintf(&b, "    %s [2]%s\n", frFixArr, subFixName)
		if v2 {
			fmt.Fprintf(&b, "    %s [..6]%s\n", frCntArr, blkRowName)
		} else {
			fmt.Fprintf(&b, "    %s [..3]%s\n", frCntArr, blkRowName)
		}
		fmt.Fprintf(&b, "    %s [%s]int32\n", frKeyed, enumName)
		fmt.Fprintf(&b, "    %s %s\n", frUnion, fixUnionName)
		if v2 {
			fmt.Fprintf(&b, "    %s int64 = -424242\n", frAddV2)
			fmt.Fprintf(&b, "    %s string(20) = \"v2-added\"\n", frAddStrV2)
		}
		b.WriteString("}\n\n")

		// BlkRoot (Block projection fixed root!)
		fmt.Fprintf(&b, "fixed table %s {\n", blkRootV1)
		fmt.Fprintf(&b, "    %s bool = true\n", bkLead)
		fmt.Fprintf(&b, "    %s uint64 = 42\n", bkWide)
		if v2 {
			fmt.Fprintf(&b, "    %s [..8]%s\n", bkRows, blkRowName)
		} else {
			fmt.Fprintf(&b, "    %s [..4]%s\n", bkRows, blkRowName)
		}
		fmt.Fprintf(&b, "    %s [%s]uint16\n", bkKeyed, enumName)
		fmt.Fprintf(&b, "    %s ?%s\n", bkOpt, subFixName)
		if v2 {
			fmt.Fprintf(&b, "    %s float64 = 3.25\n", bkAdd)
		}
		b.WriteString("}\n\n")

		// VarNode (DAG/recursive node table)
		fmt.Fprintf(&b, "table %s {\n", varNodeName)
		if v2 {
			fmt.Fprintf(&b, "    %s string(24) = \"node-def\" | was = %q\n", vnLabelNew, vnLabelOld)
			fmt.Fprintf(&b, "    %s float64 = 2.25\n", vnMetric)
		} else {
			fmt.Fprintf(&b, "    %s string(24) = \"node-def\"\n", vnLabelOld)
			fmt.Fprintf(&b, "    %s float32 = 2.25\n", vnMetric)
		}
		fmt.Fprintf(&b, "    %s *%s\n", vnNext, varNodeName)
		fmt.Fprintf(&b, "    %s *string\n", vnBlob)
		if v2 {
			fmt.Fprintf(&b, "    %s int64 = 999\n", vnAdd)
		}
		b.WriteString("}\n\n")

		// VarSub & VarPick: structs inside unions inside [Enum]Table inside map[string]V!
		fmt.Fprintf(&b, "union %s {\n", varUnionName)
		fmt.Fprintf(&b, "    %s\n", vuVoid)
		if v2 {
			fmt.Fprintf(&b, "    %s int64 | was = %q\n", vuScalarNew, vuScalarOld)
		} else {
			fmt.Fprintf(&b, "    %s int32\n", vuScalarOld)
		}
		fmt.Fprintf(&b, "    %s %s\n", vuFixSub, subFixName)
		fmt.Fprintf(&b, "    %s %s\n", vuVarSub, varNodeName)
		if v2 {
			fmt.Fprintf(&b, "    %s string(20)\n", vuAdd)
		}
		b.WriteString("}\n\n")

		fmt.Fprintf(&b, "table %s {\n", varSubName)
		fmt.Fprintf(&b, "    %s %s\n", vsPick, varUnionName)
		if v2 {
			fmt.Fprintf(&b, "    %s int64 = -77\n", vsScore)
			fmt.Fprintf(&b, "    %s bool = true\n", vsAdd)
		} else {
			fmt.Fprintf(&b, "    %s int32 = -77\n", vsScore)
		}
		b.WriteString("}\n\n")

		fmt.Fprintf(&b, "table %s {\n", mapValName)
		fmt.Fprintf(&b, "    %s [%s]%s\n", mvByMode, enumName, varSubName)
		fmt.Fprintf(&b, "    %s string(16) = \"bucket\"\n", mvTag)
		if v2 {
			fmt.Fprintf(&b, "    %s int32 = 101\n", mvAdd)
		}
		b.WriteString("}\n\n")

		// VarRoot
		rName := varRootV1
		rAttr := ""
		if v2 {
			rName = varRootV2
			rAttr = varRootWasAttr
		}
		fmt.Fprintf(&b, "table %s%s {\n", rName, rAttr)
		fmt.Fprintf(&b, "    %s bool = true\n", vrGuard)
		if v2 {
			fmt.Fprintf(&b, "    %s *%s | was = %q\n", vrHeadNew, varNodeName, vrHeadOld)
		} else {
			fmt.Fprintf(&b, "    %s *%s\n", vrHeadOld, varNodeName)
		}
		fmt.Fprintf(&b, "    %s *%s\n", vrAlias, varNodeName)
		fmt.Fprintf(&b, "    %s map[string]%s\n", vrMapStr, mapValName)
		fmt.Fprintf(&b, "    %s map[int64]int32\n", vrMapI64)
		fmt.Fprintf(&b, "    %s []%s\n", vrList, varSubName)
		fmt.Fprintf(&b, "    %s [..4]%s\n", vrBounded, varUnionName)
		fmt.Fprintf(&b, "    %s *string\n", vrText)
		fmt.Fprintf(&b, "    %s *bytes\n", vrData)
		fmt.Fprintf(&b, "    %s %s = %s\n", vrEnum, enumName, ev2)
		fmt.Fprintf(&b, "    %s %s = { %s, %s }\n", vrFlags, flagsName, fl1, fl2)
		fmt.Fprintf(&b, "    if %s {\n", vrGuard)
		if v2 {
			fmt.Fprintf(&b, "        %s int128 = 321 | min = -1000000, max = 1000000\n", vrGuardedVal)
		} else {
			fmt.Fprintf(&b, "        %s int32 = 321 | min = -1000, max = 1000\n", vrGuardedVal)
		}
		b.WriteString("    }\n")
		if v2 {
			fmt.Fprintf(&b, "    %s string(24) = %s\n", vrAddV2, sfStrDef)
			fmt.Fprintf(&b, "    %s *%s\n", vrAddPtrV2, varNodeName)
		}
		b.WriteString("}\n")
		return b.String()
	}

	v1Text := render(false)
	v2Text := render(true)

	dirV1 := filepath.Join(outRoot, pkgV1)
	dirV2 := filepath.Join(outRoot, pkgV2)
	if err := os.MkdirAll(dirV1, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dirV2, 0o755); err != nil {
		return nil, err
	}

	v1Path := filepath.Join(dirV1, fileBase+".schema")
	if err := os.WriteFile(v1Path, []byte(v1Text), 0o644); err != nil {
		return nil, err
	}

	// Build 2-generation schema.lock in dirV2 by writing V1 first (under V2's
	// package name? Wait: does UpdateSchemaLock care about package name? Let's
	// check: schema.lock keys tables by table name + layout hash, independent of
	// package name! Writing V1 with pkgV2 into dirV2 first, running UpdateSchemaLock,
	// then overwriting with V2Text and running UpdateSchemaLock again produces a
	// completely clean 2-generation schema.lock!)
	v2SchemaPath := filepath.Join(dirV2, fileBase+".schema")
	v1InV2Pkg := strings.Replace(v1Text, "package "+pkgV1+"\n", "package "+pkgV2+"\n", 1)
	if err := os.WriteFile(v2SchemaPath, []byte(v1InV2Pkg), 0o644); err != nil {
		return nil, err
	}

	c := compiler.New()
	c.FormatInPlace = false

	u1, err := c.Load([]string{v1Path})
	if err != nil {
		return nil, fmt.Errorf("load V1 (%s): %w\n%s", pkgV1, err, v1Text)
	}

	v2Paths, err := compiler.GatherPaths([]string{dirV2})
	if err != nil {
		return nil, err
	}
	u2First, err := c.Load(v2Paths)
	if err != nil {
		return nil, fmt.Errorf("load V1-in-V2 (%s): %w", pkgV2, err)
	}
	if _, _, err := compiler.UpdateSchemaLock(u2First, v2Paths); err != nil {
		return nil, fmt.Errorf("first UpdateSchemaLock (%s): %w", pkgV2, err)
	}

	if err := os.WriteFile(v2SchemaPath, []byte(v2Text), 0o644); err != nil {
		return nil, err
	}
	u2, err := c.Load(v2Paths)
	if err != nil {
		return nil, fmt.Errorf("load V2 (%s): %w\n%s", pkgV2, err, v2Text)
	}
	if _, _, err := compiler.UpdateSchemaLock(u2, v2Paths); err != nil {
		return nil, fmt.Errorf("second UpdateSchemaLock (%s): %w", pkgV2, err)
	}

	// Reload u2 so u2.Lock carries the updated 2-generation schema.lock!
	u2, err = c.Load(v2Paths)
	if err != nil {
		return nil, fmt.Errorf("reload V2 with lock (%s): %w", pkgV2, err)
	}
	lockRaw, err := os.ReadFile(filepath.Join(dirV2, lockfile.FileName))
	if err != nil {
		return nil, err
	}
	lk, err := lockfile.Parse(filepath.Join(dirV2, lockfile.FileName), lockRaw)
	if err != nil {
		return nil, err
	}
	if got := len(lockfile.Lineage(lk, fixRootV2)); got != 2 {
		return nil, fmt.Errorf("%s: expected 2 lineage entries for %s, got %d", pkgV2, fixRootV2, got)
	}

	// Compute exact Form-3 byte offsets for historical V1 writer-bound breach cases!
	var breaches []LineageBreachSpec
	fixStV1 := u1.Tables[fixRootV1]
	findBreach := func(fieldName string, wc scalarWidenChoice) {
		if !wc.hasBreach {
			return
		}
		off := int64(0)
		for _, sf := range fixStV1.Fields {
			if sf.Name == fieldName {
				raw := make([]byte, wc.v1Bytes)
				good := make([]byte, wc.v1Bytes)
				v := uint64(wc.breachVal)
				gv := uint64(15)
				if strings.Contains(wc.v1Type, "fixed(") {
					gv = 320 // 20.0 * 16, inside [-100..100] and [10..500]
				}
				for bIdx := range wc.v1Bytes {
					raw[bIdx] = byte(v >> (8 * bIdx))
					good[bIdx] = byte(gv >> (8 * bIdx))
				}
				breaches = append(breaches, LineageBreachSpec{
					FieldV1:   fieldName,
					RawOffset: int(off),
					RawWidth:  wc.v1Bytes,
					GoodLE:    good,
					BadLE:     raw,
				})
				break
			}
			off += ir.TableFixedFieldBytes(sf)
		}
	}
	_ = big.NewInt
	findBreach(frWiden1Old, wChoice1)
	findBreach(frWiden2, wChoice2)

	return &SchemaPair{
		Index:       idx,
		Name:        tag,
		PkgV1:       pkgV1,
		PkgV2:       pkgV2,
		FileBase:    fileBase,
		DirV1:       dirV1,
		DirV2:       dirV2,
		V1Text:      v1Text,
		V2Text:      v2Text,
		LockText:    string(lockRaw),
		UnitV1:      u1,
		UnitV2:      u2,
		FixRootV1:   fixRootV1,
		FixRootV2:   fixRootV2,
		BlkRootV1:   blkRootV1,
		BlkRootV2:   blkRootV2,
		VarRootV1:   varRootV1,
		VarRootV2:   varRootV2,
		BreachSpecs: breaches,
	}, nil
}
