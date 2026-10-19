package gotable

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

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

func measureArenaSizeFromRegionSource(t *testing.T) uint64 {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "regions_emitted.go", "package probe\n"+tableRegionSource, 0)
	if err != nil {
		t.Fatalf("parse tableRegionSource: %v", err)
	}
	var fieldNames []string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "TableArena" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("TableArena is not a struct")
			}
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					fieldNames = append(fieldNames, n.Name)
				}
			}
		}
	}
	wantFields := "Allocator,Locked,next,segments,storage,initialized,mutex,first"
	if got := strings.Join(fieldNames, ","); got != wantFields {
		t.Fatalf("TableArena fields in tableRegionSource = %q, want %q", got, wantFields)
	}

	type mirrorAllocator struct {
		Alloc func(size, align int64) []byte
		Free  func(memory []byte)
	}
	type mirrorSegment struct {
		data  []byte
		owned bool
	}
	type mirrorArena struct {
		Allocator   mirrorAllocator
		Locked      bool
		next        atomic.Uint64
		segments    [4096]atomic.Pointer[mirrorSegment]
		storage     [4096]mirrorSegment
		initialized bool
		mutex       sync.Mutex
		first       int64
	}
	size := uint64(unsafe.Sizeof(mirrorArena{}))
	if os.Getenv("SCHEMA_SLOW") != "" {
		runGenerated(t, `package probe
table Node { x int32 }
table Root { child *Node }
`, fmt.Sprintf(`package probe
import (
	"testing"
	"unsafe"
)
func TestArenaSize(t *testing.T) {
	if got := uint64(unsafe.Sizeof(TableArena{})); got != %d {
		t.Fatalf("unsafe.Sizeof(TableArena{}) = %%d, want %d", got)
	}
}
`, size, size))
	}
	return size
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

	tsMatch := regexp.MustCompile(`is outside the years (\d+) through (\d+)`).FindStringSubmatch(tableTimestampRuntime)
	if len(tsMatch) != 3 {
		t.Fatalf("TableTimestampError year range not found in tableTimestampRuntime")
	}
	tsMinYear, _ := strconv.ParseUint(tsMatch[1], 10, 64)
	tsMaxYear, _ := strconv.ParseUint(tsMatch[2], 10, 64)

	arenaSize := measureArenaSizeFromRegionSource(t)
	if !strings.Contains(string(usageBytes), "the arena occupies 163,896 bytes on a 64-bit target") {
		t.Fatalf("docs/USAGE.md prose does not state the 163,896-byte TableArena size")
	}

	if uint64(ir.TableMessageListMax) != uint64(math.MaxUint32) {
		t.Fatalf("ir.TableMessageListMax=%d != math.MaxUint32=%d", ir.TableMessageListMax, uint64(math.MaxUint32))
	}

	goLimits := map[string]uint64{
		"storage_cap":             uint64(ir.MaxSizeBytes),
		"int32_extent_cap":        uint64(math.MaxInt32),
		"blob_cap":                uint64(ir.TableMessageListMax),
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
