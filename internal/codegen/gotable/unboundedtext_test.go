package gotable

import (
	"testing"
)

// TestGoTableUnboundedTextListsAndMapKeys holds docs/SPEC-TABLES.md §2.13.1
// (unbounded text-list elements and map keys) in the Go backend, through a
// generated unit compiled and run in a t.TempDir():
//
//   - a table with []string, [..4]?string, [..2]string and map[string]int32
//     round-trips through the binary wire, base64 (§16.8) and JSON (§16.2)
//     with short values, its wire pinned byte for byte AND equal to the
//     string(8) twin's (the wire moves no byte), its ToJson the plain shape;
//   - with LONG values: a 70 KB element in each list, a 70 KB key and a
//     second sharing all but its last byte (two entries, key order kept),
//     each equal through binary, base64, JSON, the cook (§7) and
//     retain-unknown (§6.6), clamped counted zero; the map's Find, Each,
//     Erase and the index (Index, IndexFind) compare whole keys;
//   - [..2] given three elements clamps the count and ReadStrict refuses it
//     naming the field;
//   - the respell: string(8) bytes read by a []string reader are equal, a
//     nine-byte element read by string(8) clamps at a code point boundary
//     and counts clamped, and no kind moves;
//   - damaged images, each refused by name, ReadStrict returning the typed
//     error naming table and field: an element L past the end, a count past
//     what the bytes hold (allocations flat), ill-formed UTF-8 and a zero
//     byte in an element, a key L past the end, a key ill-formed and one
//     holding a zero byte, entries out of key order (malformed) and a
//     duplicate key (duplicate, counted), and a 70 KB element truncated
//     mid-element.
func TestGoTableUnboundedTextListsAndMapKeys(t *testing.T) {
	t.Parallel()
	runGenerated(t, unboundedTextSchema, unboundedTextProbe)
}

func TestGoTableSpecLimitRuntimeBounds(t *testing.T) {
	t.Parallel()
	if out, err := runGeneratedEdited(t, `package probe
table Node { child *Node }
table Root {
    names []string
    byname map[string]int32
    child *Node
}
fixed table Large { data bytes(65532) }
`, `package probe
import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"unsafe"
)

func TestBlobCapAtNamedLimit(t *testing.T) {
	t.Parallel()
	at := int64(0)
	if got := tableTextExtentAdd(&at, int64(tableBlobMaxBytes)); got != TableRefuseOk {
		t.Fatalf("blob_cap %d refused: %s", tableBlobMaxBytes, got)
	}
}

func TestBlobCapOnePastNamedLimitRefusesByName(t *testing.T) {
	t.Parallel()
	at := int64(0)
	if got := tableTextExtentAdd(&at, int64(tableBlobMaxBytes)+1); got != TableRefuseBlobOverSizeCap {
		t.Fatalf("blob_cap %d refusal = %s, want blob_over_size_cap", tableBlobMaxBytes+1, got)
	}
}

func TestArenaRangeAtCapacity(t *testing.T) {
	t.Parallel()
	cap := uint64(tableArenaCapacityBytes)
	if !tableArenaRangeWithinLimit(cap-1, cap) {
		t.Fatalf("arena capacity %d was refused", cap)
	}
}

func TestArenaRangeOnePastCapacityRefuses(t *testing.T) {
	t.Parallel()
	cap := uint64(tableArenaCapacityBytes)
	if tableArenaRangeWithinLimit(cap-1, cap+1) || tableArenaRangeWithinLimit(math.MaxUint64-1, 0) {
		t.Fatalf("arena accepted a range past capacity %d", cap)
	}
}

func TestMessageSkipAtNamedDepth(t *testing.T) {
	t.Parallel()
	v := TableVocabulary{RefBits: 1}
	r := TableBitReader{Buffer: []byte{0}}
	if !tableMessageSkipBody(&r, &v, 0, tableMessageMaxSkipDepth) {
		t.Fatalf("message_skip_depth %d was refused", tableMessageMaxSkipDepth)
	}
}

func TestMessageSkipOnePastNamedDepthRefuses(t *testing.T) {
	t.Parallel()
	v := TableVocabulary{RefBits: 1}
	r := TableBitReader{Buffer: []byte{0}}
	if tableMessageSkipBody(&r, &v, 0, tableMessageMaxSkipDepth+1) {
		t.Fatalf("message_skip_depth %d was accepted", tableMessageMaxSkipDepth+1)
	}
}

func TestJSONMaxDepthAtNamedLimit(t *testing.T) {
	t.Parallel()
	n := int(tableJsonMaxDepth)
	text := strings.Repeat("{\"child\":", n) + "{}" + strings.Repeat("}", n)
	var value Root
	var report TableReport
	if !RootFromJson(&value, []byte(text), &report) {
		t.Fatalf("json_max_depth %d was refused: %+v", tableJsonMaxDepth, report)
	}
}

func TestJSONMaxDepthOnePastNamedLimitRefuses(t *testing.T) {
	t.Parallel()
	n := int(tableJsonMaxDepth) + 1
	text := strings.Repeat("{\"child\":", n) + "{}" + strings.Repeat("}", n)
	var value Root
	var report TableReport
	if RootFromJson(&value, []byte(text), &report) {
		t.Fatalf("json_max_depth %d was accepted", tableJsonMaxDepth+1)
	}
}

func TestWrongCookMagicRefusesByName(t *testing.T) {
	t.Parallel()
	value := Root{}
	cooked := make([]byte, RootCookMeasure(&value))
	if !RootCookFrom(&value, cooked, TableByteOrderLittle) {
		t.Fatal("could not make cook fixture")
	}
	binary.LittleEndian.PutUint64(cooked, TableCookMagic^1)
	var opened RootCook
	err := opened.Open(unsafe.Pointer(&cooked[0]), int64(len(cooked)))
	if err != TableRefuseNotACook {
		t.Fatalf("wrong cook_magic refusal = %v, want %v", err, TableRefuseNotACook)
	}
}

func TestInt32ExtentCapAtNamedLimit(t *testing.T) {
	t.Parallel()
	carve := TableExtentCarve{Worker: &TableWorker{}}
	var holder tableContainer
	if !tableContainerFill(&carve, &holder, uint64(math.MaxInt32), 1, 1) {
		t.Fatalf("int32_extent_cap %d was refused", math.MaxInt32)
	}
}

func TestInt32ExtentCapOnePastNamedLimitRefuses(t *testing.T) {
	t.Parallel()
	carve := TableExtentCarve{Worker: &TableWorker{}}
	var holder tableContainer
	if tableContainerFill(&carve, &holder, uint64(math.MaxInt32)+1, 1, 1) {
		t.Fatalf("int32_extent_cap %d was accepted", uint64(math.MaxInt32)+1)
	}
}

func TestStorageCapAtNamedLimit(t *testing.T) {
	t.Parallel()
	got := tableMapIndexMeasure(tableMapIndexStorageMaxCount)
	if got < 0 {
		t.Fatalf("storage_cap %d was refused", tableMapIndexStorageMaxCount)
	}
}

func TestStorageCapOnePastNamedLimitRefuses(t *testing.T) {
	t.Parallel()
	if got := tableMapIndexMeasure(tableMapIndexStorageMaxCount+1); got != -1 {
		t.Fatalf("storage_cap %d measure = %d, want refusal", tableMapIndexStorageMaxCount+1, got)
	}
}

func TestFixedRecordBoundAtNamedLimit(t *testing.T) {
	t.Parallel()
	layout, why := tableFixedParseLayout(LargeFixedLayout)
	if why != "" {
		t.Fatalf("fixed_record_bound layout refused: %s", why)
	}
	root := tableFixedEntryAt(layout, 0)
	if root.Size != tableFixedRecordMaxBytes {
		t.Fatalf("fixed_record_bound fixture size = %d, want %d", root.Size, tableFixedRecordMaxBytes)
	}
}

func fixedRemapLayouts(n int32) (tableFixedLayoutView, tableFixedLayoutView, []TableFixedDst) {
	theirCount := int32(2) + n
	theirBytes := make([]byte, int(tableFixedLayoutHeaderBytes+theirCount*tableFixedEntryBytes))
	theirs := tableFixedLayoutView{Bytes: theirBytes, Count: theirCount}
	theirOrdinal := theirBytes[int(tableFixedLayoutHeaderBytes+tableFixedEntryBytes):]
	theirOrdinal[8] = 30
	binary.LittleEndian.PutUint32(theirOrdinal[9:], 1)
	binary.LittleEndian.PutUint32(theirOrdinal[13:], uint32(n))
	for i := int32(0); i < n; i++ {
		entry := theirBytes[int(tableFixedLayoutHeaderBytes+(2+i)*tableFixedEntryBytes):]
		binary.LittleEndian.PutUint64(entry, uint64(i+1))
	}
	myBytes := make([]byte, int(tableFixedLayoutHeaderBytes+2*tableFixedEntryBytes))
	mine := tableFixedLayoutView{Bytes: myBytes, Count: 2}
	myOrdinal := myBytes[int(tableFixedLayoutHeaderBytes+tableFixedEntryBytes):]
	myOrdinal[8] = 30
	binary.LittleEndian.PutUint32(myOrdinal[9:], 1)
	return theirs, mine, make([]TableFixedDst, 2)
}

func TestFixedRemapMaxAtNamedLimit(t *testing.T) {
	t.Parallel()
	n := int32(tableFixedRemapMax)
	theirs, mine, dst := fixedRemapLayouts(n)
	c := tableFixedCompiler{plan: make([]TableFixedEntry, 8192), capacity: 8192}
	tableFixedCompileEntry(&c, theirs, 1, 0, mine, 1, dst, 0, 0, 0, false)
	if c.overflow || c.count == 0 {
		t.Fatalf("fixed_remap_max %d was refused: count=%d overflow=%v", tableFixedRemapMax, c.count, c.overflow)
	}
}

func TestFixedRemapMaxOnePastRefusesByName(t *testing.T) {
	t.Parallel()
	n := int32(tableFixedRemapMax) + 1
	theirs, mine, dst := fixedRemapLayouts(n)
	c := tableFixedCompiler{plan: make([]TableFixedEntry, 8192), capacity: 8192}
	tableFixedCompileEntry(&c, theirs, 1, 0, mine, 1, dst, 0, 0, 0, false)
	if !c.overflow {
		t.Fatalf("fixed_remap_max %d did not refuse by the named limit", n)
	}
}

func TestTextFormDefaultByteLimitAtBound(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte("(\"" + strings.Repeat("x", tableTextFormDefaultBytes-4) + "\")")
	if len(input) != tableTextFormDefaultBytes {
		t.Fatalf("fixture bytes=%d want %d", len(input), tableTextFormDefaultBytes)
	}
	if _, _, err := tableTextFormParse(input, limits); err != nil {
		t.Fatalf("text_bytes_default %d refused: %v", tableTextFormDefaultBytes, err)
	}
}

func TestTextFormDefaultByteLimitOnePastRefusesByName(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte("(\"" + strings.Repeat("x", tableTextFormDefaultBytes-3) + "\")")
	if _, _, err := tableTextFormParse(input, limits); err == nil || err.Name != TableTextFormLimit {
		t.Fatalf("text_bytes_default %d refusal = %v, want %s", tableTextFormDefaultBytes+1, err, TableTextFormLimit)
	}
}

func TestTextFormDefaultTokenLimitAtBound(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte("(" + strings.Repeat("true ", tableTextFormDefaultTokens-2) + ")")
	if _, _, err := tableTextFormParse(input, limits); err != nil {
		t.Fatalf("text_tokens_default %d refused: %v", tableTextFormDefaultTokens, err)
	}
}

func TestTextFormDefaultTokenLimitOnePastRefusesByName(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte("(" + strings.Repeat("true ", tableTextFormDefaultTokens-1) + ")")
	if _, _, err := tableTextFormParse(input, limits); err == nil || err.Name != TableTextFormLimit {
		t.Fatalf("text_tokens_default %d refusal = %v, want %s", tableTextFormDefaultTokens+1, err, TableTextFormLimit)
	}
}

func TestTextFormDefaultNodeLimitAtBound(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte("(" + strings.Repeat("() ", tableTextFormDefaultNodes-1) + ")")
	if _, _, err := tableTextFormParse(input, limits); err != nil {
		t.Fatalf("text_nodes_default %d refused: %v", tableTextFormDefaultNodes, err)
	}
}

func TestTextFormDefaultNodeLimitOnePastRefusesByName(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte("(" + strings.Repeat("() ", tableTextFormDefaultNodes) + ")")
	if _, _, err := tableTextFormParse(input, limits); err == nil || err.Name != TableTextFormLimit {
		t.Fatalf("text_nodes_default %d refusal = %v, want %s", tableTextFormDefaultNodes+1, err, TableTextFormLimit)
	}
}

func TestTextFormDefaultAllocationAtBound(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	limits.Bytes = tableTextFormDefaultBytes + 8
	input := []byte("(\"" + strings.Repeat("x", tableTextFormDefaultAllocation) + "\")")
	if _, _, err := tableTextFormParse(input, limits); err != nil {
		t.Fatalf("text_allocation_default %d refused: %v", tableTextFormDefaultAllocation, err)
	}
}

func TestTextFormDefaultAllocationOnePastRefusesByName(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	limits.Bytes = tableTextFormDefaultBytes + 8
	input := []byte("(\"" + strings.Repeat("x", tableTextFormDefaultAllocation+1) + "\")")
	if _, _, err := tableTextFormParse(input, limits); err == nil || err.Name != TableTextFormLimit {
		t.Fatalf("text_allocation_default %d refusal = %v, want %s", tableTextFormDefaultAllocation+1, err, TableTextFormLimit)
	}
}

func TestTextFormDefaultDepthAtBound(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte(strings.Repeat("(", tableTextFormMaxDepth) + "true" + strings.Repeat(")", tableTextFormMaxDepth))
	if _, _, err := tableTextFormParse(input, limits); err != nil {
		t.Fatalf("text_depth_default %d refused: %v", tableTextFormMaxDepth, err)
	}
}

func TestTextFormDefaultDepthOnePastRefusesByName(t *testing.T) {
	t.Parallel()
	limits := TableTextFormDefaultLimits()
	input := []byte(strings.Repeat("(", tableTextFormMaxDepth+1) + "true" + strings.Repeat(")", tableTextFormMaxDepth+1))
	if _, _, err := tableTextFormParse(input, limits); err == nil || err.Name != TableTextFormLimit {
		t.Fatalf("text_depth_default %d refusal = %v, want %s", tableTextFormMaxDepth+1, err, TableTextFormLimit)
	}
}
`, nil, "-timeout", "600s"); err != nil {
		t.Fatalf("runtime bound probes: %v\n%s", err, out)
	}
}

const unboundedTextSchema = `package probe

table Where
{
    rules []string
    first [..4]?string
    capped [..2]string
    byname map[string]int32
}

table Bounded
{
    rules []string(8)
    first [..4]?string(8)
    capped [..2]string(8)
    byname map[string(8)]int32
}
`

const unboundedTextProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unsafe"
)

// whereGolden is the pinned wire of rules ["a","b"], first ["x",null],
// capped ["c"] and byname {"k1":1,"k2":2}: form byte 1; rules as kind 14 over
// element kind 12, each element its own L; first as kind 14 over element
// kind 13; capped as kind 14 over element kind 12; byname as kind 14 over
// entry bodies, each key under kind 12; the zero reference; the id table. It
// equals the Bounded twin's bytes, which the test checks first.
const whereGolden = "01010e060c0201610162020e0a0d0205030c0178000100040e040c010163050e1c0d020c060c026b31030401000000000c060c026b320304020000000000c05351286a4f1267411d6f997fedd789ea0ce83094fde47ce86f9207409840f48f702bcb07334de7ec105b36194ac93d0600000000000000"

func compact(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}

func toJson(t *testing.T, v *Where) []byte {
	t.Helper()
	n := WhereToJsonMeasure(v)
	if n < 0 {
		t.Fatal("ToJsonMeasure refused")
	}
	buf := make([]byte, n)
	if WhereToJson(v, buf) != n {
		t.Fatal("ToJson")
	}
	return buf
}

func save(t *testing.T, v *Where, arena ...TableWriteContext) []byte {
	t.Helper()
	n := WhereMeasure(v, arena...)
	if n < 0 {
		t.Fatal("Measure refused")
	}
	buf := make([]byte, n)
	if WhereSave(v, buf, arena...) != n {
		t.Fatal("Save")
	}
	return buf
}

func load(t *testing.T, wire []byte) (*Where, TableReport) {
	t.Helper()
	var report TableReport
	size := WhereLoadMeasure(wire)
	if size < 0 {
		return nil, report
	}
	region := make([]byte, size+64)
	at := 0
	for uintptr(unsafe.Pointer(&region[at]))%64 != 0 {
		at++
	}
	return WhereLoadTolerant(region[at:], wire, &report), report
}

type whereText struct {
	rules, first, capped []string // "\x00absent" marks an absent element
	keys                 []string
	values               []int32
}

func textOf(t *testing.T, v *Where, arena *TableArena) whereText {
	t.Helper()
	var out whereText
	var a []*TableArena
	if arena != nil {
		a = []*TableArena{arena}
	}
	for i := int32(0); i < v.Rules.Count; i++ {
		p, ok := WhereRulesTextAt(v, i, a...)
		if !ok {
			t.Fatalf("rules[%d] absent", i)
		}
		out.rules = append(out.rules, string(p))
	}
	for i := int32(0); i < v.FirstCount; i++ {
		p, ok := WhereFirstTextAt(v, i, a...)
		if !ok {
			out.first = append(out.first, "\x00absent")
			continue
		}
		out.first = append(out.first, string(p))
	}
	for i := int32(0); i < v.CappedCount; i++ {
		p, ok := WhereCappedTextAt(v, i, a...)
		if !ok {
			t.Fatalf("capped[%d] absent", i)
		}
		out.capped = append(out.capped, string(p))
	}
	WhereBynameEach(&v.Byname, func(k string, value *int32) bool {
		out.keys = append(out.keys, k)
		out.values = append(out.values, *value)
		return true
	}, a...)
	return out
}

func same(t *testing.T, what string, got, want whereText) {
	t.Helper()
	g, w := strings.Join(got.rules, "|")+"/"+strings.Join(got.first, "|")+"/"+strings.Join(got.capped, "|")+"/"+strings.Join(got.keys, "|"), strings.Join(want.rules, "|")+"/"+strings.Join(want.first, "|")+"/"+strings.Join(want.capped, "|")+"/"+strings.Join(want.keys, "|")
	if g != w || len(got.values) != len(want.values) {
		t.Fatalf("%s: the text moved (%d vs %d bytes)", what, len(g), len(w))
	}
	for i := range got.values {
		if got.values[i] != want.values[i] {
			t.Fatalf("%s: value %d moved", what, i)
		}
	}
}

func build(t *testing.T, b *WhereBuilder, in whereText) *Where {
	t.Helper()
	if !b.Init() {
		t.Fatal("init")
	}
	v := b.GetRoot()
	for _, s := range in.rules {
		if !WhereRulesAppend(&b.Main, v, s) {
			t.Fatal("rules append")
		}
	}
	for _, s := range in.first {
		ok := false
		if s == "\x00absent" {
			ok = WhereFirstAppendAbsent(&b.Main, v)
		} else {
			ok = WhereFirstAppend(&b.Main, v, s)
		}
		if !ok {
			t.Fatal("first append")
		}
	}
	for _, s := range in.capped {
		if !WhereCappedAppend(&b.Main, v, s) {
			t.Fatal("capped append")
		}
	}
	for i, k := range in.keys {
		p := WhereBynameInsert(&b.Main, &v.Byname, k)
		if p == nil {
			t.Fatalf("insert %d", i)
		}
		*p = in.values[i]
	}
	return v
}

// roundTrip holds one value through every carrier and answers its wire.
func roundTrip(t *testing.T, what string, in whereText) []byte {
	t.Helper()
	var b WhereBuilder
	v := build(t, &b, in)
	defer b.Shutdown()
	same(t, what+" builder", textOf(t, v, &b.Arena), in)
	wire := save(t, v, &b.Arena)
	// BINARY: the region load
	got, report := load(t, wire)
	if got == nil || report != (TableReport{}) {
		t.Fatalf("%s: load %+v", what, report)
	}
	same(t, what+" region", textOf(t, got, nil), in)
	if again := save(t, got); !bytes.Equal(again, wire) {
		t.Fatalf("%s: wire round trip moved", what)
	}
	// the strict builder read
	strict, err := WhereReadStrict(wire)
	if err != nil {
		t.Fatalf("%s: ReadStrict %v", what, err)
	}
	same(t, what+" strict", textOf(t, strict, nil), in)
	// BASE64
	b64, err := WhereSaveText(v, &b.Arena)
	if err != nil || b64 != base64.StdEncoding.EncodeToString(wire) {
		t.Fatalf("%s: SaveText %v", what, err)
	}
	back, err := WhereLoadText(b64, nil)
	if err != nil || !bytes.Equal(save(t, back), wire) {
		t.Fatalf("%s: LoadText %v", what, err)
	}
	// JSON
	jtext := toJson(t, got)
	var jb WhereBuilder
	jb.Init()
	defer jb.Shutdown()
	report = TableReport{}
	if !WhereFromJson(&jb, jtext, &report) || report != (TableReport{}) {
		t.Fatalf("%s: FromJson %+v", what, report)
	}
	same(t, what+" json", textOf(t, jb.GetRoot(), &jb.Arena), in)
	if !bytes.Equal(save(t, jb.GetRoot(), &jb.Arena), wire) {
		t.Fatalf("%s: the JSON round trip moved the wire", what)
	}
	// RETAIN-UNKNOWN (§6.6)
	var retain TableRetain
	report = TableReport{}
	size := WhereLoadMeasure(wire)
	kept := WhereLoadRetain(make([]byte, size+4096), wire, &retain, &report)
	if kept == nil || report.Malformed || report.Clamped != 0 {
		t.Fatalf("%s: retain load %+v", what, report)
	}
	rbuf := make([]byte, len(wire)+64)
	if n := WhereSaveRetain(kept, &retain, rbuf, &report); n < 0 || !bytes.Equal(rbuf[:n], wire) {
		t.Fatalf("%s: retain round trip %d", what, n)
	}
	// THE COOK (§7), from the builder and from the region
	for i, src := range []*Where{got, v} {
		var arena []TableWriteContext
		if i == 1 {
			arena = []TableWriteContext{&b.Arena}
		}
		cn := WhereCookMeasure(src, arena...)
		if cn < 0 {
			t.Fatalf("%s: cook measure refused", what)
		}
		cooked := make([]byte, cn+64)
		at := 0
		for uintptr(unsafe.Pointer(&cooked[at]))%64 != 0 {
			at++
		}
		cooked = cooked[at : at+int(cn)]
		if !WhereCookFrom(src, cooked, TableByteOrderLittle, arena...) {
			t.Fatalf("%s: cook", what)
		}
		var cook WhereCook
		if !WhereOpen(&cook, unsafe.Pointer(&cooked[0]), cn) {
			t.Fatalf("%s: cook open", what)
		}
		croot := (*Where)(cook.Region)
		same(t, what+" cook", textOf(t, croot, nil), in)
		if !bytes.Equal(save(t, croot), wire) {
			t.Fatalf("%s: the cook moved the wire", what)
		}
	}
	// THE MAP over whole keys: Find in the builder and the region, the index
	for i, k := range in.keys {
		if p := WhereBynameFind(&b.Arena, &v.Byname, k); p == nil || *p != in.values[i] {
			t.Fatalf("%s: builder Find %d", what, i)
		}
		if p := WhereBynameFind(nil, &got.Byname, k); p == nil || *p != in.values[i] {
			t.Fatalf("%s: region Find %d", what, i)
		}
	}
	if len(in.keys) > 0 {
		if WhereBynameFind(nil, &got.Byname, in.keys[0]+"\x01") != nil || WhereBynameFind(nil, &got.Byname, in.keys[0][:len(in.keys[0])-1]) != nil {
			t.Fatalf("%s: a key differing in length found an entry", what)
		}
		storage := make([]byte, WhereBynameIndexMeasure(&got.Byname)+8)
		at := 0
		for uintptr(unsafe.Pointer(&storage[at]))%4 != 0 {
			at++
		}
		index := WhereBynameIndex(&got.Byname, storage[at:])
		for i, k := range in.keys {
			if p := WhereBynameIndexFind(index, &got.Byname, k); p == nil || *p != in.values[i] {
				t.Fatalf("%s: IndexFind %d", what, i)
			}
		}
	}
	return wire
}

func TestGoTableUnboundedTextListsAndMapKeys(t *testing.T) {
	short := whereText{rules: []string{"a", "b"}, first: []string{"x", "\x00absent"}, capped: []string{"c"}, keys: []string{"k1", "k2"}, values: []int32{1, 2}}
	wire := roundTrip(t, "short", short)

	// THE WIRE MOVES NO BYTE: the string(8) twin writes the same bytes
	var bb BoundedBuilder
	bb.Init()
	defer bb.Shutdown()
	bv := bb.GetRoot()
	BoundedRulesAppend(&bb.Main, bv, "a")
	BoundedRulesAppend(&bb.Main, bv, "b")
	BoundedFirstAppend(bv, "x")
	BoundedFirstAppendAbsent(bv)
	BoundedCappedAppend(bv, "c")
	*BoundedBynameInsert(&bb.Main, &bv.Byname, "k1") = 1
	*BoundedBynameInsert(&bb.Main, &bv.Byname, "k2") = 2
	bn := BoundedMeasure(bv, &bb.Arena)
	bwire := make([]byte, bn)
	BoundedSave(bv, bwire, &bb.Arena)
	if !bytes.Equal(bwire, wire) {
		t.Fatalf("the unbounded spelling moved a byte\n got %x\nwant %x", wire, bwire)
	}
	if got := hex.EncodeToString(wire); got != whereGolden {
		t.Fatalf("wire\n got %s\nwant %s", got, whereGolden)
	}
	if !bytes.Contains(wire, []byte{14, 6, 12, 2, 1, 'a', 1, 'b'}) {
		t.Fatalf("rules is not kind 14 over element kind 12: %x", wire)
	}
	got, _ := load(t, wire)
	if j := compact(t, toJson(t, got)); j != ` + "`" + `{"rules":["a","b"],"first":["x",null],"capped":["c"],"byname":{"k1":1,"k2":2}}` + "`" + ` {
		t.Fatalf("ToJson %s", j)
	}

	// LONG VALUES: 70 KB elements and keys, two keys sharing all but the last byte
	long := strings.Repeat("é", 35000)
	keyA := strings.Repeat("k", 70000) + "a"
	keyB := strings.Repeat("k", 70000) + "b"
	big := whereText{rules: []string{long, "z"}, first: []string{"\x00absent", long + "1"}, capped: []string{long + "2"}, keys: []string{keyA, keyB}, values: []int32{7, 8}}
	bigWire := roundTrip(t, "long", big)
	got, report := load(t, bigWire)
	if got == nil || report.Clamped != 0 || got.Byname.Count != 2 {
		t.Fatalf("long: %+v", report)
	}
	// the map's builder surface over whole keys: Erase one, the other stays
	var eb WhereBuilder
	ev := build(t, &eb, big)
	defer eb.Shutdown()
	if !WhereBynameErase(&eb.Arena, &ev.Byname, keyA) || WhereBynameFind(&eb.Arena, &ev.Byname, keyA) != nil || WhereBynameFind(&eb.Arena, &ev.Byname, keyB) == nil || ev.Byname.Count != 1 {
		t.Fatal("Erase of one 70 KB key moved the other")
	}
	if p := WhereBynameInsert(&eb.Main, &ev.Byname, keyB); p == nil || *p != 0 {
		t.Fatal("Insert of an existing key does not replace its value")
	}

	// THE WRITE SIDE: the count bound, a zero byte, ill-formed UTF-8
	var wb WhereBuilder
	wv := build(t, &wb, whereText{capped: []string{"p", "q"}})
	defer wb.Shutdown()
	if WhereCappedAppend(&wb.Main, wv, "r") {
		t.Fatal("[..2] took a third element")
	}
	if WhereRulesAppend(&wb.Main, wv, "a\x00") || WhereRulesAppend(&wb.Main, wv, "\xff") || WhereFirstAppend(&wb.Main, wv, "\xc3") {
		t.Fatal("Append took a zero byte or ill-formed UTF-8")
	}
	if WhereBynameInsert(&wb.Main, &wv.Byname, "\xff") != nil || WhereBynameInsert(&wb.Main, &wv.Byname, "k\x00") != nil {
		t.Fatal("Insert took an ill-formed key or one holding a zero byte")
	}

	// HAND-BUILT IMAGES, each by its framing
	ids := map[string]uint64{"rules": WhereTableFields[0].Id, "first": WhereTableFields[1].Id, "capped": WhereTableFields[2].Id, "byname": WhereTableFields[3].Id}
	entry := WhereTableFields[3].Table()
	keyId, valueId, valueKind := entry.Fields[0].Id, entry.Fields[1].Id, entry.Fields[1].Kind
	type elem struct {
		raw   string
		claim int // an L that claims more than raw holds
	}
	leb := func(v uint64) []byte {
		var b []byte
		for v >= 0x80 {
			b = append(b, byte(v)|0x80)
			v >>= 7
		}
		return append(b, byte(v))
	}
	list := func(field string, count int, elems ...elem) func(w *TableWriter) {
		return func(w *TableWriter) {
			var body []byte
			body = append(body, 12)
			body = append(body, leb(uint64(count))...)
			for _, e := range elems {
				n := len(e.raw)
				if e.claim > 0 {
					n = e.claim
				}
				body = append(body, leb(uint64(n))...)
				body = append(body, e.raw...)
			}
			w.Id(ids[field])
			w.Put8(14)
			w.PutLeb(uint64(len(body)))
			w.Raw(body)
		}
	}
	file := func(parts ...func(w *TableWriter)) []byte {
		var tids TableIds
		out := make([]byte, 1<<20)
		w := TableWriter{Buffer: out, Ids: &tids}
		w.Put8(1)
		for _, p := range parts {
			p(&w)
		}
		w.Put8(0)
		w.Trailer()
		return out[:w.Offset]
	}
	// a map's entries need their key and value ids in the same id table
	entries := func(keys ...elem) func(w *TableWriter) {
		return func(w *TableWriter) {
			var tids TableIds
			scratch := make([]byte, 1<<20)
			inner := TableWriter{Buffer: scratch, Ids: w.Ids}
			_ = tids
			inner.Put8(13)
			inner.PutLeb(uint64(len(keys)))
			for i, k := range keys {
				ebuf := make([]byte, 1<<19)
				e := TableWriter{Buffer: ebuf, Ids: w.Ids}
				n := len(k.raw)
				if k.claim > 0 {
					n = k.claim
				}
				e.Id(keyId)
				e.Put8(12)
				e.PutLeb(uint64(n))
				e.Raw([]byte(k.raw))
				e.Id(valueId)
				e.Put8(valueKind)
				e.Raw([]byte{byte(i + 1), 0, 0, 0})
				e.Put8(0)
				inner.PutLeb(uint64(e.Offset))
				inner.Raw(ebuf[:e.Offset])
			}
			w.Id(ids["byname"])
			w.Put8(14)
			w.PutLeb(uint64(inner.Offset))
			w.Raw(scratch[:inner.Offset])
		}
	}
	// the hand-built short image reads equal to the written one
	if g, _ := load(t, file(list("rules", 2, elem{raw: "a"}, elem{raw: "b"}), entries(elem{raw: "k1"}, elem{raw: "k2"}))); g == nil || g.Rules.Count != 2 || g.Byname.Count != 2 {
		t.Fatal("the hand-built image does not read")
	}

	// [..2] GIVEN THREE: the count clamps, named
	three := file(list("capped", 3, elem{raw: "p"}, elem{raw: "q"}, elem{raw: "r"}))
	if g, r := load(t, three); g == nil || g.CappedCount != 2 || r.Clamped != 1 || r.Malformed {
		t.Fatalf("[..2] given three: %+v", r)
	}
	if _, err := WhereReadStrict(three); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), "Where.capped") {
		t.Fatalf("ReadStrict [..2] given three: %v", err)
	}

	// DAMAGED IMAGES, refused by name
	for _, c := range []struct {
		name, field string
		image       []byte
	}{
		{"element L past the end", "Where.rules", file(list("rules", 1, elem{raw: "abc", claim: 9}))},
		{"element ill-formed UTF-8", "Where.rules", file(list("rules", 1, elem{raw: "\xc3"}))},
		{"element with a zero byte", "Where.rules", file(list("rules", 1, elem{raw: "a\x00b"}))},
		{"bounded-count element ill-formed", "Where.capped", file(list("capped", 1, elem{raw: "\xff"}))},
		{"key L past the end", "Where.byname", file(entries(elem{raw: "k1", claim: 9}))},
		{"key ill-formed", "Where.byname", file(entries(elem{raw: "\xc3\x28"}))},
		{"key with a zero byte", "Where.byname", file(entries(elem{raw: "k\x00"}))},
		{"entries out of key order", "Where.byname", file(entries(elem{raw: "k2"}, elem{raw: "k1"}))},
		{"70 KB element truncated mid-element", "Where.rules", file(list("rules", 1, elem{raw: long[:35000], claim: len(long)}))},
	} {
		g, r := load(t, c.image)
		if !r.Malformed && g != nil {
			t.Fatalf("%s: not malformed: %+v", c.name, r)
		}
		_, err := WhereReadStrict(c.image)
		var named TableMalformedField
		if !errors.Is(err, TableRefuseMalformed) || !errors.As(err, &named) || named.Field != c.field || err.Error() != "malformed: "+c.field {
			t.Fatalf("%s: ReadStrict %v, want malformed naming %s", c.name, err, c.field)
		}
	}
	// a duplicate key is §2.8's event: last wins, counted, not malformed
	dup := file(entries(elem{raw: "k1"}, elem{raw: "k1"}))
	if g, r := load(t, dup); g == nil || r.Duplicate != 1 || r.Malformed || g.Byname.Count != 1 {
		t.Fatalf("duplicate key: %+v", r)
	}
	// A COUNT PAST WHAT THE BYTES HOLD is refused from the framing before
	// anything is allocated: LoadMeasure answers count_over_length, and the
	// load into the caller's region allocates nothing, as for a clean image
	claim := file(list("rules", 1<<20, elem{raw: "a"}))
	if n, err := WhereLoadMeasureReason(claim); n >= 0 || !errors.Is(err, TableRefuseCountOverLength) {
		t.Fatalf("count past the bytes: %d %v", n, err)
	}
	region := make([]byte, 1<<16)
	var r TableReport
	clean := file(list("rules", 2, elem{raw: "a"}, elem{raw: "b"}))
	if a := testing.AllocsPerRun(20, func() { r = TableReport{}; WhereLoadTolerant(region, claim, &r) }); a != 0 || !r.Malformed {
		t.Fatalf("count past the bytes: %v allocations, %+v", a, r)
	}
	if a := testing.AllocsPerRun(20, func() { r = TableReport{}; WhereLoad(region, clean, &r) }); a != 0 {
		t.Fatalf("a clean image: %v allocations", a)
	}

	// THE RESPELL (§2.13.1, §4): string(8) bytes read under []string are
	// equal; a nine-byte element read under string(8) clamps at a code point
	// boundary, counted; the kind is 12 in both directions
	narrow := file(list("rules", 2, elem{raw: "12345678"}, elem{raw: ""}))
	var rr TableReport
	bregion := make([]byte, 1<<16)
	if g, r := load(t, narrow); g == nil || r != (TableReport{}) || g.Rules.Count != 2 {
		t.Fatalf("string(8) bytes under []string: %+v", r)
	} else if p, _ := WhereRulesTextAt(g, 0); string(p) != "12345678" {
		t.Fatalf("respelled element %q", p)
	}
	wide := file(list("rules", 1, elem{raw: "1234567é"}))
	bg := BoundedLoadTolerant(bregion, wide, &rr)
	if bg == nil || rr.Clamped != 1 || rr.Malformed || rr.KindMismatch != 0 {
		t.Fatalf("nine bytes under string(8): %+v", rr)
	}
	if p, _ := BoundedRulesTextAt(bg, 0); string(p) != "1234567" {
		t.Fatalf("clamp not at a code point boundary: %q", p)
	}
}
`
