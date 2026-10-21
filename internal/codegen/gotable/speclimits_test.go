package gotable

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var limitMarkerRe = regexp.MustCompile(`<!--\s*limit\s+name=([a-z0-9_]+)\s+value=([^\s>]+)\s*-->`)

func parseLimitLiteral(raw string) (uint64, error) {
	if after, ok := strings.CutPrefix(raw, "2^"); ok {
		minusOne := false
		if base, hasMinus := strings.CutSuffix(after, "-1"); hasMinus {
			after = base
			minusOne = true
		}
		exp, err := strconv.ParseUint(after, 10, 64)
		if err != nil || exp > 63 {
			return 0, fmt.Errorf("invalid power-of-two limit %q", raw)
		}
		v := uint64(1) << exp
		if minusOne {
			v--
		}
		return v, nil
	}
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		return strconv.ParseUint(raw[2:], 16, 64)
	}
	return strconv.ParseUint(raw, 10, 64)
}

func extractRegexpUint(t *testing.T, src, pattern, label string) uint64 {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	if len(m) < 2 {
		t.Fatalf("%s: pattern %q not found", label, pattern)
	}
	v, err := parseLimitLiteral(m[1])
	if err != nil {
		t.Fatalf("%s: parse %q: %v", label, m[1], err)
	}
	return v
}

type arenaTypeResolver struct {
	t        *testing.T
	segments int
	named    map[string]reflect.Type
}

func (r *arenaTypeResolver) arrayLen(expr ast.Expr) int {
	switch e := expr.(type) {
	case *ast.BasicLit:
		v, err := strconv.ParseUint(e.Value, 0, 64)
		if err != nil {
			r.t.Fatalf("arena type: bad array length %q: %v", e.Value, err)
		}
		return int(v)
	case *ast.Ident:
		if e.Name == "tableArenaSegments" {
			return r.segments
		}
	}
	r.t.Fatalf("arena type: unsupported array length expression")
	return 0
}

func (r *arenaTypeResolver) funcType(ft *ast.FuncType) reflect.Type {
	var in, out []reflect.Type
	add := func(list *ast.FieldList, dst *[]reflect.Type) {
		if list == nil {
			return
		}
		for _, f := range list.List {
			typ := r.resolve(f.Type)
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				*dst = append(*dst, typ)
			}
		}
	}
	add(ft.Params, &in)
	add(ft.Results, &out)
	return reflect.FuncOf(in, out, false)
}

func (r *arenaTypeResolver) resolve(expr ast.Expr) reflect.Type {
	switch e := expr.(type) {
	case *ast.Ident:
		switch e.Name {
		case "bool":
			return reflect.TypeOf(false)
		case "string":
			return reflect.TypeFor[string]()
		case "int":
			return reflect.TypeFor[int]()
		case "int8":
			return reflect.TypeFor[int8]()
		case "int16":
			return reflect.TypeFor[int16]()
		case "int32":
			return reflect.TypeFor[int32]()
		case "int64":
			return reflect.TypeFor[int64]()
		case "uint":
			return reflect.TypeFor[uint]()
		case "uint8", "byte":
			return reflect.TypeFor[byte]()
		case "uint16":
			return reflect.TypeFor[uint16]()
		case "uint32":
			return reflect.TypeFor[uint32]()
		case "uint64":
			return reflect.TypeFor[uint64]()
		case "uintptr":
			return reflect.TypeFor[uintptr]()
		case "float32":
			return reflect.TypeFor[float32]()
		case "float64":
			return reflect.TypeFor[float64]()
		}
		if typ, ok := r.named[e.Name]; ok {
			return typ
		}
		r.t.Fatalf("arena type: unknown identifier %q", e.Name)
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok {
			break
		}
		switch pkg.Name + "." + e.Sel.Name {
		case "sync.Mutex":
			return reflect.TypeFor[sync.Mutex]()
		case "atomic.Uint64":
			return reflect.TypeFor[atomic.Uint64]()
		}
		r.t.Fatalf("arena type: unsupported selector %s.%s", pkg.Name, e.Sel.Name)
	case *ast.StarExpr:
		return reflect.PointerTo(r.resolve(e.X))
	case *ast.ArrayType:
		elem := r.resolve(e.Elt)
		if e.Len == nil {
			return reflect.SliceOf(elem)
		}
		return reflect.ArrayOf(r.arrayLen(e.Len), elem)
	case *ast.IndexExpr:
		sel, ok := e.X.(*ast.SelectorExpr)
		if !ok {
			break
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "atomic" || sel.Sel.Name != "Pointer" {
			break
		}
		return reflect.PointerTo(r.resolve(e.Index))
	case *ast.FuncType:
		return r.funcType(e)
	}
	r.t.Fatalf("arena type: unsupported type expression %T", expr)
	return nil
}

func (r *arenaTypeResolver) structType(name string, file *ast.File) reflect.Type {
	var st *ast.StructType
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			st, _ = ts.Type.(*ast.StructType)
		}
	}
	if st == nil {
		r.t.Fatalf("arena type: struct %s not found", name)
	}
	var fields []reflect.StructField
	for _, f := range st.Fields.List {
		typ := r.resolve(f.Type)
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			fields = append(fields, reflect.StructField{Name: fmt.Sprintf("F%d", len(fields)), Type: typ})
		}
	}
	return reflect.StructOf(fields)
}

func measureArenaSizeFromRegionSource(t *testing.T) uint64 {
	t.Helper()
	segments := extractRegexpUint(t, tableRegionSource, `const tableArenaSegments = (\d+)`, "tableArenaSegments")
	if segments < 1 || segments > 1<<24 {
		t.Fatalf("implausible tableArenaSegments=%d", segments)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "regions_emitted.go", "package probe\n"+tableRegionSource, 0)
	if err != nil {
		t.Fatalf("parse tableRegionSource: %v", err)
	}
	r := &arenaTypeResolver{t: t, segments: int(segments), named: make(map[string]reflect.Type)}
	r.named["tableArenaSegment"] = r.structType("tableArenaSegment", file)
	r.named["TableAllocator"] = r.structType("TableAllocator", file)
	return uint64(r.structType("TableArena", file).Size())
}

func int32GuardOperand(t *testing.T, src string) uint64 {
	t.Helper()
	m := regexp.MustCompile(`func tableContainerFill\([^\n]*?count>([^ {\n]+)`).FindStringSubmatch(src)
	if len(m) != 2 {
		t.Fatalf("int32_extent_cap: tableContainerFill's count guard operand not found")
	}
	expr := m[1]
	switch {
	case expr == "math.MaxInt32":
		return uint64(math.MaxInt32)
	case strings.HasPrefix(expr, "math.MaxInt32-"):
		d, err := strconv.ParseUint(strings.TrimPrefix(expr, "math.MaxInt32-"), 10, 64)
		if err != nil {
			t.Fatalf("int32_extent_cap: parse guard operand %q: %v", expr, err)
		}
		return uint64(math.MaxInt32) - d
	case strings.HasPrefix(expr, "math.MaxInt32+"):
		d, err := strconv.ParseUint(strings.TrimPrefix(expr, "math.MaxInt32+"), 10, 64)
		if err != nil {
			t.Fatalf("int32_extent_cap: parse guard operand %q: %v", expr, err)
		}
		return uint64(math.MaxInt32) + d
	}
	v, err := parseLimitLiteral(expr)
	if err != nil {
		t.Fatalf("int32_extent_cap: parse guard operand %q: %v", expr, err)
	}
	return v
}

func checkSpecLimits(docs map[string]string, goLimits map[string]uint64) []string {
	var problems []string
	seen := make(map[string]uint64)
	for path, content := range docs {
		matches := limitMarkerRe.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			name, lit := m[1], m[2]
			val, err := parseLimitLiteral(lit)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: marker %s has invalid value %q: %v", path, name, lit, err))
				continue
			}
			if prev, dup := seen[name]; dup && prev != val {
				problems = append(problems, fmt.Sprintf("%s: duplicate marker %s with conflicting values %d vs %d", path, name, prev, val))
				continue
			}
			seen[name] = val
			goVal, ok := goLimits[name]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: marker %s has no Go limit entry", path, name))
				continue
			}
			if val != goVal {
				problems = append(problems, fmt.Sprintf("%s: limit %s spec=%d (%s) != go=%d (0x%x)", path, name, val, lit, goVal, goVal))
			}
		}
	}
	for name := range goLimits {
		if _, ok := seen[name]; !ok {
			problems = append(problems, fmt.Sprintf("Go limit %s has no <!-- limit name=%s ... --> marker in docs", name, name))
		}
	}
	return problems
}

func TestSpecLimitsMatchGo(t *testing.T) {
	t.Parallel()

	specBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatalf("read SPEC-TABLES.md: %v", err)
	}
	usageBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "USAGE.md"))
	if err != nil {
		t.Fatalf("read USAGE.md: %v", err)
	}
	fixedRuntimeBytes, err := os.ReadFile("fixedruntime.go")
	if err != nil {
		t.Fatalf("read fixedruntime.go: %v", err)
	}
	messageBytes, err := os.ReadFile("message.go")
	if err != nil {
		t.Fatalf("read message.go: %v", err)
	}
	jsonBytes, err := os.ReadFile("json.go")
	if err != nil {
		t.Fatalf("read json.go: %v", err)
	}

	fixedRuntimeSrc := string(fixedRuntimeBytes)
	messageSrc := string(messageBytes)
	jsonSrc := string(jsonBytes)

	lebMaxBytes := extractRegexpUint(t, tableWireSource, `for i := 0; i < (\d+); i\+\+`, "leb128_max_bytes")
	fixedRecordBoundRt := extractRegexpUint(t, fixedRuntimeSrc, `const tableFixedRecordMaxBytes uint32 = (\d+)`, "fixedruntime.tableFixedRecordMaxBytes")
	if fixedRecordBoundRt != uint64(ir.TableFixedRecordMaxBytes) {
		t.Fatalf("fixed_record_bound: fixedruntime=%d != ir.TableFixedRecordMaxBytes=%d", fixedRecordBoundRt, ir.TableFixedRecordMaxBytes)
	}
	fixedMaxDepthRt := extractRegexpUint(t, fixedRuntimeSrc, `const tableFixedMaxDepth int32 = (\d+)`, "fixedruntime.tableFixedMaxDepth")
	if fixedMaxDepthRt != uint64(ir.TableFixedMaxDepth) {
		t.Fatalf("fixed_max_depth: fixedruntime=%d != ir.TableFixedMaxDepth=%d", fixedMaxDepthRt, ir.TableFixedMaxDepth)
	}
	fixedRemapMax := extractRegexpUint(t, fixedRuntimeSrc, `const tableFixedRemapMax uint32 = (0x[0-9A-Fa-f]+|\d+)`, "fixed_remap_max")
	retainWalkDepth := extractRegexpUint(t, goRetainRuntime, `const tableRetainWalkDepthMax = (\d+)`, "retain_walk_depth")
	jsonMaxDepth := extractRegexpUint(t, jsonSrc, `tableJsonMaxDepth\s*=\s*(\d+)`, "json_max_depth")
	textFormMaxDepth := extractRegexpUint(t, tableTextFormSource, `const tableTextFormMaxDepth = (\d+)`, "tableTextFormMaxDepth")
	if jsonMaxDepth != textFormMaxDepth {
		t.Fatalf("json_max_depth=%d != tableTextFormMaxDepth=%d", jsonMaxDepth, textFormMaxDepth)
	}
	messageSkipDepth := extractRegexpUint(t, messageSrc, `func tableMessageSkip\([^\n]*\n\s*if depth>(\d+)\{return false\}`, "message_skip_depth")
	messageSkipBodyDepth := extractRegexpUint(t, messageSrc, `func tableMessageSkipBody\([^\n]*\{if depth>(\d+)\{return false\}`, "message_skip_body_depth")
	if messageSkipDepth != messageSkipBodyDepth {
		t.Fatalf("message_skip_depth=%d != message_skip_body_depth=%d", messageSkipDepth, messageSkipBodyDepth)
	}

	tfMatch := regexp.MustCompile(`TableTextFormLimits\{Bytes:\s*(\d+),\s*Tokens:\s*(\d+),\s*Nodes:\s*(\d+),\s*Allocation:\s*(\d+),`).FindStringSubmatch(tableTextFormSource)
	if len(tfMatch) != 5 {
		t.Fatalf("TableTextFormDefaultLimits literal not found in tableTextFormSource")
	}
	tfBytes, _ := strconv.ParseUint(tfMatch[1], 10, 64)
	tfTokens, _ := strconv.ParseUint(tfMatch[2], 10, 64)
	tfNodes, _ := strconv.ParseUint(tfMatch[3], 10, 64)
	tfAlloc, _ := strconv.ParseUint(tfMatch[4], 10, 64)

	minGuard := regexp.MustCompile(`time.Unix\(0,\s*(-1<<63)\)`).FindStringSubmatch(tableTimestampRuntime)
	maxGuard := regexp.MustCompile(`time.Unix\(0,\s*(1<<63-1)\)`).FindStringSubmatch(tableTimestampRuntime)
	if len(minGuard) != 2 || minGuard[1] != "-1<<63" {
		t.Fatalf("tableTimestampNanos min setter guard is not -1<<63")
	}
	if len(maxGuard) != 2 || maxGuard[1] != "1<<63-1" {
		t.Fatalf("tableTimestampNanos max setter guard is not 1<<63-1")
	}
	minBound := time.Unix(0, -1<<63).UTC()
	maxBound := time.Unix(0, 1<<63-1).UTC()
	tsMinYearVal := minBound.Year()
	if time.Date(tsMinYearVal, time.January, 1, 0, 0, 0, 0, time.UTC).Before(minBound) {
		tsMinYearVal++
	}
	tsMaxYearVal := maxBound.Year()
	if time.Date(tsMaxYearVal, time.January, 1, 0, 0, 0, 0, time.UTC).After(maxBound) {
		tsMaxYearVal--
	}
	tsMinYear := uint64(tsMinYearVal)
	tsMaxYear := uint64(tsMaxYearVal)

	arenaSize := measureArenaSizeFromRegionSource(t)
	if !strings.Contains(string(usageBytes), "the arena occupies 163,896 bytes on a 64-bit target") {
		t.Fatalf("docs/USAGE.md prose does not state the 163,896-byte TableArena size")
	}

	regionBytes, err := os.ReadFile("regions.go")
	if err != nil {
		t.Fatalf("read regions.go: %v", err)
	}
	unboundedBytes, err := os.ReadFile("unboundedtext.go")
	if err != nil {
		t.Fatalf("read unboundedtext.go: %v", err)
	}
	listsBytes, err := os.ReadFile("lists.go")
	if err != nil {
		t.Fatalf("read lists.go: %v", err)
	}
	int32ExtentCap := int32GuardOperand(t, string(listsBytes))
	blobCapGuard := extractRegexpUint(t, string(regionBytes), `len\(body\.Buffer\)\)>(0x[0-9a-fA-F]+)`, "blob_cap_guard")
	blobCapExtent := extractRegexpUint(t, string(unboundedBytes), `uint64\(n\)>(0x[0-9a-fA-F]+)`, "blob_cap_extent_guard")
	if blobCapGuard != blobCapExtent {
		t.Fatalf("blob capacity guards disagree: regions=%d unboundedtext=%d", blobCapGuard, blobCapExtent)
	}

	goLimits := map[string]uint64{
		"storage_cap":             uint64(ir.MaxSizeBytes),
		"int32_extent_cap":        int32ExtentCap,
		"blob_cap":                blobCapGuard,
		"leb128_max_bytes":        lebMaxBytes,
		"fixed_record_bound":      uint64(ir.TableFixedRecordMaxBytes),
		"fixed_max_depth":         uint64(ir.TableFixedMaxDepth),
		"fixed_remap_max":         fixedRemapMax,
		"retain_walk_depth":       retainWalkDepth,
		"json_max_depth":          jsonMaxDepth,
		"message_skip_depth":      messageSkipDepth,
		"cook_magic":              cookMagic,
		"block_magic":             ir.BlockMagic,
		"timestamp_min_year":      tsMinYear,
		"timestamp_max_year":      tsMaxYear,
		"text_default_max_bytes":  tfBytes,
		"text_default_max_tokens": tfTokens,
		"text_default_max_nodes":  tfNodes,
		"text_default_max_alloc":  tfAlloc,
		"go_arena_bytes":          arenaSize,
	}

	docs := map[string]string{
		"docs/SPEC-TABLES.md": string(specBytes),
		"docs/USAGE.md":       string(usageBytes),
	}
	if problems := checkSpecLimits(docs, goLimits); len(problems) > 0 {
		t.Fatalf("spec limits out of sync with Go:\n  %s", strings.Join(problems, "\n  "))
	}

	t.Run("reversed_witness", func(t *testing.T) {
		mutated := map[string]string{
			"docs/SPEC-TABLES.md": strings.Replace(string(specBytes), "<!-- limit name=json_max_depth value=128 -->", "<!-- limit name=json_max_depth value=64 -->", 1),
			"docs/USAGE.md":       strings.Replace(string(usageBytes), "<!-- limit name=go_arena_bytes value=163896 -->", "<!-- limit name=go_arena_bytes value=163888 -->", 1),
		}
		problems := checkSpecLimits(mutated, goLimits)
		joined := strings.Join(problems, "\n")
		if !strings.Contains(joined, "limit json_max_depth spec=64 (64) != go=128") {
			t.Fatalf("reversed witness did not report mutated json_max_depth; got:\n%s", joined)
		}
		if !strings.Contains(joined, "limit go_arena_bytes spec=163888 (163888) != go=163896") {
			t.Fatalf("reversed witness did not report mutated go_arena_bytes; got:\n%s", joined)
		}
	})
}
