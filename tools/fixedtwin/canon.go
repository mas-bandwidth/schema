package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Token map: C spelling → canonical. C++ is rewritten toward the same
// canonical form. Documented in bench/paired/TWIN.md. Longest from-string
// first for the linear replace pass.
type token struct{ from, to string }

var identMap = []token{
	{"SCHEMA_TABLE_FIXED_NO_GUARD", "kTableFixedNoGuard"},
	{"SCHEMA_TABLE_RESTRICT", "TABLE_RESTRICT"},
	{"SCHEMA_BENCH_TABLE_INLINE", "TABLE_FIXED_INLINE"},
	{"SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE", "layout_record_too_large"},
	{"SCHEMA_TABLE_LAYOUT_COUNT_MISMATCH", "layout_count_mismatch"},
	{"SCHEMA_TABLE_LAYOUT_KIND_UNKNOWN", "layout_kind_unknown"},
	{"SCHEMA_TABLE_LAYOUT_KIND_INVALID", "layout_kind_invalid"},
	{"SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH", "layout_size_mismatch"},
	{"SCHEMA_TABLE_LAYOUT_TREE_UNCLOSED", "layout_tree_unclosed"},
	{"SCHEMA_TABLE_LAYOUT_TOO_DEEP", "layout_too_deep"},
	{"SCHEMA_TABLE_LAYOUT_MALFORMED", "layout_malformed"},
	{"SCHEMA_TABLE_VOCABULARY_TOO_LARGE", "vocabulary_too_large"},
	{"SCHEMA_TABLE_SECOND_ANNOUNCEMENT", "second_announcement"},
	{"SCHEMA_TABLE_MESSAGE_FORM_AS_FILE", "message_form_as_file"},
	{"SCHEMA_TABLE_BATCH_TOO_LARGE", "batch_too_large"},
	{"SCHEMA_TABLE_PLAN_TOO_LARGE", "plan_too_large"},
	{"SCHEMA_TABLE_PREVIOUS_FORM", "previous_form"},
	{"SCHEMA_TABLE_NO_VOCABULARY", "no_vocabulary"},
	{"SCHEMA_TABLE_NEWER_FORM", "newer_form"},
	{"SCHEMA_TABLE_NO_LAYOUT", "no_layout"},
	{"static SCHEMA_UNUSED", "inline"},
	{"TABLE_FIXED_INLINE", "inline"},
}

// table_fixed_putf32 is TableFixedPutF32, not TableFixedPutf32.
var tableFixedSpecial = map[string]string{
	"putf32": "PutF32", "putf64": "PutF64",
	"put8": "Put8", "put16": "Put16", "put32": "Put32", "put64": "Put64",
	"get32": "Get32", "get64": "Get64",
	"hash_of": "HashOf", "copy_run": "CopyRun",
	"signed_kind": "SignedKind", "widens": "Widens",
	"entry_zero": "EntryZero", "layout_entry_zero": "LayoutEntryZero",
	"layout_view_zero": "LayoutViewZero", "compiler_init": "CompilerInit",
	"entry_at": "EntryAt", "union_arm_bytes": "UnionArmBytes",
	"tag_bytes": "TagBytes", "leaf_size": "LeafSize",
	"ordinal_width": "OrdinalWidth", "known_kind": "KnownKind",
	"check_entry": "CheckEntry", "parse_layout": "ParseLayout",
	"match_children": "MatchChildren", "compile_entry": "CompileEntry",
	"lay_table": "LayTable",
	"put128_u":  "Put128U", "put128_i": "Put128I",
	"cmp128_u": "Cmp128U", "cmp128_i": "Cmp128I",
}

var (
	reTableFixed      = regexp.MustCompile(`\btable_fixed_[a-z0-9_]+\b`)
	reLineComment     = regexp.MustCompile(`(?m)//.*?$`)
	reBlockComment    = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reTrailingComma   = regexp.MustCompile(`,(\s*})`)
	reEnumTyped       = regexp.MustCompile(`enum\s*:\s*uint8_t`)
	reConstexpr       = regexp.MustCompile(`constexpr\s+\w+\s+(\w+)\s*=\s*([^;]+);`)
	reEnumSingle      = regexp.MustCompile(`enum\s*\{\s*(\w+)\s*=\s*([^,}]+)\s*\}\s*;`)
	reTypedefStruct   = regexp.MustCompile(`typedef struct (\w+)\s*\{`)
	reStructClose     = regexp.MustCompile(`\}\s*(TableFixed\w+)\s*;`)
	reFieldDefault    = regexp.MustCompile(`(?m)^(\s*(?:u?int(?:8|16|32|64)_t|bool)\s+\w+)\s*=\s*(?:0u?|1u?|false|true|kTableFixedNoGuard|layout_malformed)\s*;`)
	reUint8Lit        = regexp.MustCompile(`\(uint8_t\)\s*(\d+)`)
	rePtrDefault      = regexp.MustCompile(`(?m)^(\s*\S+\s*\*\s*\w+)\s*=\s*NULL\s*;`)
	reForInt          = regexp.MustCompile(`\bint i;\s*for \( i =`)
	reForI32          = regexp.MustCompile(`\bint32_t i;\s*for \( i =`)
	reForI64          = regexp.MustCompile(`\bint64_t i;\s*for \( i =`)
	reForU32k         = regexp.MustCompile(`\buint32_t k;\s*for \( k =`)
	reForU32j         = regexp.MustCompile(`\buint32_t j;\s*for \( j =`)
	reForDeclJK       = regexp.MustCompile(`\buint32_t j, k;\s*`)
	reStarClamped     = regexp.MustCompile(`\(\*(clamped|widened)\)\+\+`)
	reStarClampedAdd  = regexp.MustCompile(`\(\*(clamped|widened)\)\s*\+=`)
	reSkipBraceOpen   = regexp.MustCompile(`n = their_n < my_n \? their_n : my_n;\s*\{`)
	reSkipBraceClose  = regexp.MustCompile(`c\.skip_clamp = prev_skip;\s*\}\s*break;`)
	reCLayBoundsPtr   = regexp.MustCompile(`uint8_t \* at;\s*`)
	reCLayBoundsBody  = regexp.MustCompile(`at = \(uint8_t \*\) \(void \*\) \( \(uint8_t \*\) c\.plan \+ \(uint32_t\) \( total - c\.pool \) \);\s*memcpy\( at, &lo, 8 \);\s*memcpy\( at \+ 8, &hi, 8 \);\s*return \(uint32_t\) \( total - c\.pool \);`)
	reCppLayBoundsDst = regexp.MustCompile(`uint8_t \* dst = \(uint8_t \*\) \(void \*\) \( \(uint8_t \*\) c\.plan \+ at \);`)
	rePtrApply        = regexp.MustCompile(`(TableFixed(?:Apply|EntryLands)\(\s*)&plan\[i\]`)
	reAmpCounters     = regexp.MustCompile(`,\s*&clamped,\s*&widened\s*\)`)
	reStaticAssert    = regexp.MustCompile(`SCHEMA_TABLE_STATIC_ASSERT\s*\(\s*\w+\s*,\s*`)
	reOffsetof        = regexp.MustCompile(`\boffsetof\s*\(`)
	reBuiltinOff      = regexp.MustCompile(`\(uint32_t\)\s*__builtin_offsetof\s*\(`)
	reSpaces          = regexp.MustCompile(`[ \t]+`)
	reZeroFn          = regexp.MustCompile(`(?s)inline TableFixed(?:Entry|LayoutEntry|LayoutView) TableFixed(?:EntryZero|LayoutEntryZero|LayoutViewZero)\s*\(\s*void\s*\)\s*\{.*?\}`)
	reInitFn          = regexp.MustCompile(`(?s)inline void TableFixedCompilerInit\s*\([^)]*\)\s*\{.*?\}`)
	reRAIIcOpen       = regexp.MustCompile(`c(?:->|\.)depth\+\+\s*;\s*do\s*\{`)
	reRAIIcClose      = regexp.MustCompile(`\}\s*while\s*\(\s*0\s*\)\s*;\s*c(?:->|\.)depth--;`)
	reZeroCallEntry   = regexp.MustCompile(`TableFixedEntry\s+\w+\s*=\s*TableFixedEntryZero\s*\(\s*\)\s*;`)
	reZeroCallLayout  = regexp.MustCompile(`TableFixedLayoutEntry\s+\w+\s*=\s*TableFixedLayoutEntryZero\s*\(\s*\)\s*;`)
	reZeroCallView    = regexp.MustCompile(`TableFixedLayoutView\s+\w+\s*=\s*TableFixedLayoutViewZero\s*\(\s*\)\s*;`)
	reInitCall        = regexp.MustCompile(`TableFixedCompilerInit\s*\(\s*&c\s*\)\s*;`)
	reCReasons        = regexp.MustCompile(`(?s)enum\s*\{\s*no_layout\s*=\s*7,.*?previous_form\s*=\s*17\s*\}\s*;`)
	reCMessageGuard   = regexp.MustCompile(`(?s)#ifndef MESSAGE_REASONS.*?\#endif`)
	// OWED by the C leg (docs/FIXED-FORM-ALGORITHM.md §5). C++ first; these
	// blocks have no C twin yet. Strip them so the gate stays green; TWIN.md
	// names each row with the §5 section the C card implements from.
	reOwedPresentCase    = regexp.MustCompile(`(?s)case kTableFixedPresent:\s*\{\s*dst\[p\.dst\] = 1;\s*break;\s*\}`)
	reOwedPresentCompile = regexp.MustCompile(`(?s)if \( me\.kind == 35 && te\.kind != 35 \)\s*\{.*?return;\s*\}`)
	reOwedEnumWiden      = regexp.MustCompile(`(?s)if \( te\.size < me\.size \)\s*\{\s*TableFixedEntry e;\s*e\.src = their_at; e\.dst = at; e\.size = te\.size; e\.dstsize = \(uint8_t\) me\.size;\s*e\.guard = guard; e\.arg = arg; e\.op = kTableFixedWiden; e\.sign = 0;\s*TableFixedPush\( c, e \);\s*break;\s*\}`)
	reOwedKnownLayout    = regexp.MustCompile(`(?s)struct TableFixedKnownLayout\s*\{.*?\};`)
	reOwedArgLaneRemap   = regexp.MustCompile(`(?s)const uint32_t n = te\.children;\s+const uint32_t at_map = TableFixedLayTable\( c, NULL, \(int32_t\) n \);\s+if \( c\.overflow \) \{ break; \}\s+uint16_t \* map = \(uint16_t \*\) \(void \*\) \( \(uint8_t \*\) c\.plan \+ at_map \);\s+for \( uint32_t j = 0; j < n; \+\+j \)\s*\{.*?map\[1 \+ j\] = landed;\s*\}\s*TableFixedEntry e;\s*e\.src = their_at; e\.dst = at; e\.size = te\.size; e\.guard = guard; e\.op = kTableFixedOrdinal;\s*e\.arg = arg; e\.dstsize = \(uint8_t\) me\.size;\s*e\.aux = at_map;\s*TableFixedPush\( c, e \);`)
	reOwedByteLaneRemap  = regexp.MustCompile(`(?s)uint16_t remap\[256\];.*?TableFixedLayTable\( c, remap, \(int32_t\) n \);\s*TableFixedPush\( c, e \);`)
	reOwedLayTableNull   = regexp.MustCompile(`if \( values != NULL \)\s*\{\s*for \( int32_t i = 0; i < n; \+\+i \) \{ dst\[1 \+ i\] = values\[i\]; \}\s*\}`)
	reOwedNestedNoneArgw = regexp.MustCompile(`const uint8_t inner_argw = c\.argw;\s*if \( guard != kTableFixedNoGuard \) \{ c\.argw = saved_argw \? saved_argw : 1u; \}\s*`)
	reOwedNestedNoneRest = regexp.MustCompile(`TableFixedPush\( c, none \);\s*c\.argw = inner_argw;`)
	reOwedNestedRestamp  = regexp.MustCompile(`(?s)const int32_t restamp_from = c\.count;\s*`)
	reOwedNestedRewrite  = regexp.MustCompile(`(?s)if \( guard != kTableFixedNoGuard \)\s*\{\s*const uint8_t outer_w = saved_argw \? saved_argw : 1u;\s*for \( int32_t i = restamp_from; i < c\.count; \+\+i \)\s*\{\s*if \( c\.plan\[i\]\.guard == their_at \)\s*\{\s*c\.plan\[i\]\.guard = guard;\s*c\.plan\[i\]\.arg = arg;\s*c\.plan\[i\]\.argw = outer_w;\s*\}\s*\}\s*\}`)
)

func tableFixedIdent(name string) string {
	rest := strings.TrimPrefix(name, "table_fixed_")
	if spec, ok := tableFixedSpecial[rest]; ok {
		return "TableFixed" + spec
	}
	parts := strings.Split(rest, "_")
	var b strings.Builder
	b.WriteString("TableFixed")
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}

func stripComments(s string) string {
	s = reBlockComment.ReplaceAllString(s, "\n")
	s = reLineComment.ReplaceAllString(s, "")
	return s
}

func stripPreprocessor(s string) string {
	var out []string
	depth := 0
	for line := range strings.SplitSeq(s, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "#if") || strings.HasPrefix(trim, "#ifdef") || strings.HasPrefix(trim, "#ifndef"):
			depth++
			continue
		case strings.HasPrefix(trim, "#endif"):
			if depth > 0 {
				depth--
			}
			continue
		case strings.HasPrefix(trim, "#else") || strings.HasPrefix(trim, "#elif"):
			continue
		case depth > 0:
			continue
		case strings.HasPrefix(trim, "#define"):
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func applyIdents(s string) string {
	s = reTableFixed.ReplaceAllStringFunc(s, tableFixedIdent)
	for _, t := range identMap {
		s = strings.ReplaceAll(s, t.from, t.to)
	}
	return s
}

func expandEnums(s string) string {
	s = reEnumTyped.ReplaceAllString(s, "enum")
	s = reConstexpr.ReplaceAllString(s, "$1 = $2;")
	s = reEnumSingle.ReplaceAllString(s, "$1 = $2;")
	// enum { a = 0, b = 1, c = 2 } → a = 0; b = 1; c = 2;
	reMulti := regexp.MustCompile(`(?s)enum\s*\{([^}]*)\}\s*;?`)
	s = reMulti.ReplaceAllStringFunc(s, func(m string) string {
		inner := reMulti.FindStringSubmatch(m)[1]
		parts := strings.Split(inner, ",")
		var lines []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			lines = append(lines, p+";")
		}
		return strings.Join(lines, "\n")
	})
	return s
}

func stripZeroing(s string) string {
	s = reZeroFn.ReplaceAllString(s, "")
	s = reInitFn.ReplaceAllString(s, "")
	s = reZeroCallEntry.ReplaceAllString(s, "TableFixedEntry e;")
	s = reZeroCallLayout.ReplaceAllString(s, "TableFixedLayoutEntry out;")
	s = reZeroCallView.ReplaceAllString(s, "TableFixedLayoutView view;")
	s = reInitCall.ReplaceAllString(s, "")
	return s
}

func stripNamed(s string) string {
	// Named remaining #2: the two 128-bit stores (and C's compare pair).
	s = stripFuncsWithPrefix(s, "inline void TableFixedPut128")
	s = stripFuncsWithPrefix(s, "inline int TableFixedCmp128")
	// Named remaining #1: RAII guard vs inc/dec.
	s = stripCppDepthGuard(s)
	s = reRAIIcOpen.ReplaceAllString(s, "DEPTH_GUARD;\n")
	s = reRAIIcClose.ReplaceAllString(s, "\n")
	return s
}

func stripOwedC(s string) string {
	// OWED by the C leg. Not a named remaining: C has no spelling of these
	// yet. C++ first; legs from algorithm §5 after. Do not port C here.
	// §5.2 present op enumerator
	s = strings.ReplaceAll(s, "kTableFixedPresent = 7;", "")
	s = strings.ReplaceAll(s, "kTableFixedPresent = 7,", "")
	// §5.1 fixed(I,F) / ufixed(I,F) ladders in Widens
	s = strings.ReplaceAll(s, "if ( from >= 20 && from <= 24 && to >= 20 && to <= 24 ) { return to > from; }", "")
	s = strings.ReplaceAll(s, "if ( from >= 25 && from <= 29 && to >= 25 && to <= 29 ) { return to > from; }", "")
	// §5.1 SignedKind covering signed fixed(I,F)
	s = strings.ReplaceAll(s, "return ( kind >= 2 && kind <= 5 ) || ( kind >= 20 && kind <= 24 );", "return kind >= 2 && kind <= 5;")
	// §5.2 Apply case and compile T into ?T
	s = reOwedPresentCase.ReplaceAllString(s, "")
	s = reOwedPresentCompile.ReplaceAllString(s, "")
	// §5.2 grown ordinal width as widen
	s = reOwedEnumWiden.ReplaceAllString(s, "")
	// §5.3 known-layout table LOAD selects by hash
	s = reOwedKnownLayout.ReplaceAllString(s, "")
	// owed 6: arg is full width (bill §12.7). C still has a byte lane.
	s = strings.ReplaceAll(s, "uint64_t arg", "uint8_t arg")
	s = strings.ReplaceAll(s, "TableFixedTagAt( src, p.guard, p.argw ) != p.arg", "TableFixedTagAt( src, p.guard, p.argw ) != (uint64_t) p.arg")
	s = strings.ReplaceAll(s, "tag.arg = (uint64_t) j + 1u;", "tag.arg = (uint8_t) ( j + 1 );")
	s = strings.ReplaceAll(s, "mine, my_arm, dst, at, their_at, (uint64_t) j + 1u", "mine, my_arm, dst, at, their_at, (uint8_t) ( j + 1 )")
	s = strings.ReplaceAll(s, "if ( n < 0 || n > 65535 ) { c.overflow = true; return 0; }", "")
	s = reOwedLayTableNull.ReplaceAllString(s, "for ( int32_t i = 0; i < n; ++i ) { dst[1 + i] = values[i]; }")
	s = reOwedArgLaneRemap.ReplaceAllString(s, "OWED_REMAP;")
	s = reOwedByteLaneRemap.ReplaceAllString(s, "OWED_REMAP;")
	// owed 7: nested union answers to the outer tag; C still loses it.
	s = reOwedNestedNoneArgw.ReplaceAllString(s, "")
	s = reOwedNestedNoneRest.ReplaceAllString(s, "TableFixedPush( c, none );")
	s = reOwedNestedRestamp.ReplaceAllString(s, "")
	s = reOwedNestedRewrite.ReplaceAllString(s, "")
	return s
}

func stripFuncsWithPrefix(s, prefix string) string {
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			return s
		}
		open := strings.Index(s[i:], "{")
		if open < 0 {
			return s
		}
		end := matchingBrace(s, i+open)
		if end < 0 {
			return s
		}
		s = s[:i] + "\nNAMED_128;\n" + s[end+1:]
	}
}

func stripCppDepthGuard(s string) string {
	const mark = "struct TableFixedDepth"
	for {
		i := strings.Index(s, mark)
		if i < 0 {
			return s
		}
		open := strings.Index(s[i:], "{")
		if open < 0 {
			return s
		}
		abs := i + open
		depth := 0
		end := -1
		for k := abs; k < len(s); k++ {
			switch s[k] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = k
					k = len(s)
				}
			}
		}
		if end < 0 {
			return s
		}
		rest := s[end+1:]
		// `} depth_guard( c ); (void) depth_guard;`
		rest = strings.TrimSpace(rest)
		if j := strings.Index(rest, ";"); j >= 0 {
			rest = rest[j+1:]
		}
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "(void)") {
			if j := strings.Index(rest, ";"); j >= 0 {
				rest = rest[j+1:]
			}
		}
		s = s[:i] + "\nDEPTH_GUARD;\n" + rest
	}
}

func rewriteKindMismatchBreaks(s string) string {
	// After DEPTH_GUARD, C's kind-mismatch prelude uses break (out of do/while)
	// where C++ uses return. That spelling is named remaining #1.
	idx := strings.Index(s, "DEPTH_GUARD")
	if idx < 0 {
		return s
	}
	sw := strings.Index(s[idx:], "switch ( me.kind )")
	if sw < 0 {
		return s
	}
	head := s[:idx]
	mid := s[idx : idx+sw]
	tail := s[idx+sw:]
	mid = strings.ReplaceAll(mid, "break;", "return;")
	return head + mid + tail
}

func normalizeSyntax(s string) string {
	s = strings.ReplaceAll(s, "->", ".")
	s = strings.ReplaceAll(s, ".as.", ".")
	s = reStarClamped.ReplaceAllString(s, "$1++")
	s = reStarClampedAdd.ReplaceAllString(s, "$1 +=")
	s = reTrailingComma.ReplaceAllString(s, "$1")
	s = reTypedefStruct.ReplaceAllString(s, "struct $1 {")
	s = reStructClose.ReplaceAllString(s, "};")
	s = reFieldDefault.ReplaceAllString(s, "$1;")
	s = rePtrDefault.ReplaceAllString(s, "$1;")
	s = reForInt.ReplaceAllString(s, "for ( int i =")
	s = reForI32.ReplaceAllString(s, "for ( int32_t i =")
	s = reForI64.ReplaceAllString(s, "for ( int64_t i =")
	s = reForU32k.ReplaceAllString(s, "for ( uint32_t k =")
	s = reForU32j.ReplaceAllString(s, "for ( uint32_t j =")
	s = reForDeclJK.ReplaceAllString(s, "")
	s = rePtrApply.ReplaceAllString(s, "${1}plan[i]")
	s = reAmpCounters.ReplaceAllString(s, ", clamped, widened )")
	s = reOffsetof.ReplaceAllString(s, "__builtin_offsetof(")
	s = reBuiltinOff.ReplaceAllString(s, "__builtin_offsetof(")
	s = reStaticAssert.ReplaceAllString(s, "static_assert(")
	// drop the extra closing paren SCHEMA_TABLE_STATIC_ASSERT introduced? the macro is (tag, cond, msg) → static_assert(cond, msg) so we ate tag and its comma; the closing paren stays. Good.

	repls := []token{
		{"inline int TableFixedWidens", "inline bool TableFixedWidens"},
		{"inline int TableFixedSignedKind", "inline bool TableFixedSignedKind"},
		{"inline int TableFixedLeafSize", "inline bool TableFixedLeafSize"},
		{"inline int TableFixedOrdinalWidth", "inline bool TableFixedOrdinalWidth"},
		{"inline int TableFixedKnownKind", "inline bool TableFixedKnownKind"},
		{"inline int TableFixedParseLayout", "inline bool TableFixedParseLayout"},
		{"const TableFixedEntry * p", "const TableFixedEntry REF p"},
		{"const TableFixedEntry & p", "const TableFixedEntry REF p"},
		{"const TableFixedEntry * e", "const TableFixedEntry REF e"},
		{"const TableFixedEntry & e", "const TableFixedEntry REF e"},
		{"int32_t * clamped, int32_t * widened", "int32_t REF clamped, int32_t REF widened"},
		{"int32_t & clamped, int32_t & widened", "int32_t REF clamped, int32_t REF widened"},
		{"const TableFixedLayoutView * ", "const TableFixedLayoutView REF "},
		{"const TableFixedLayoutView & ", "const TableFixedLayoutView REF "},
		{"TableFixedCheck * c", "TableFixedCheck REF c"},
		{"TableFixedCheck & c", "TableFixedCheck REF c"},
		{"TableFixedCompiler * c", "TableFixedCompiler REF c"},
		{"TableFixedCompiler & c", "TableFixedCompiler REF c"},
		{"TableFixedPlanCache * cache", "TableFixedPlanCache REF cache"},
		{"TableFixedPlanCache & cache", "TableFixedPlanCache REF cache"},
		{"TableFixedLayoutView * out", "TableFixedLayoutView REF out"},
		{"TableFixedLayoutView & out", "TableFixedLayoutView REF out"},
		{"int * why", "TableMessageReason REF why"},
		{"TableMessageReason & why", "TableMessageReason REF why"},
		{"int * is_leaf", "bool REF is_leaf"},
		{"bool & is_leaf", "bool REF is_leaf"},
		{"const TableFixedDst * d", "const TableFixedDst REF d"},
		{"const TableFixedDst & d", "const TableFixedDst REF d"},
		{"inline inline void", "inline void"},
		{"int want_guarded;", "bool want_guarded;"},
		{"int overflow;", "bool overflow;"},
		{"int hostile;", "bool hostile;"},
		{"int skip_clamp;", "bool skip_clamp;"},
		{"int named;", "bool named;"},
		{"int kids_are_variants;", "bool kids_are_variants;"},
		{"int is_leaf;", "bool is_leaf;"},
		{"int reason;", "TableMessageReason why;"},
		{"int bad;", "bool bad;"},
		{"c.reason", "c.why"},
		{"int why =", "TableMessageReason why ="},
		{"int why;", "TableMessageReason why;"},
		{"want_guarded = 0", "want_guarded = false"},
		{"want_guarded = 1", "want_guarded = true"},
		{"overflow = 1", "overflow = true"},
		{"hostile = 1", "hostile = true"},
		{"skip_clamp = 1", "skip_clamp = true"},
		{"skip_clamp = 0", "skip_clamp = false"},
		{"named = 1", "named = true"},
		{"named = 0", "named = false"},
		{"bad = 1", "bad = true"},
		{"bad = 0", "bad = false"},
		{"kids_are_variants = 1", "kids_are_variants = true"},
		{"kids_are_variants = 0", "kids_are_variants = false"},
		{"is_leaf = 1", "is_leaf = true"},
		{"is_leaf = 0", "is_leaf = false"},
		{"*is_leaf = 1", "is_leaf = true"},
		{"*is_leaf = 0", "is_leaf = false"},
		{"*is_leaf = true", "is_leaf = true"},
		{"*is_leaf = false", "is_leaf = false"},
		{"(uint8_t) 1", "1u"},
		{"(uint8_t) 0", "0u"},
		{"(uint8_t) 8", "8u"},
		{"(uint8_t) kTableFixedWidenF", "kTableFixedWidenF"},
		{"(uint8_t) kTableFixedWiden", "kTableFixedWiden"},
		{"(uint8_t) kTableFixedClamp", "kTableFixedClamp"},
		{"const uint16_t * remap", "const uint16_t * table"},
		{"remap[0]", "table[0]"},
		{"remap[raw]", "table[raw]"},
		{"uint16_t map[256]", "uint16_t map[256]"},
		{"const double wide", "const double d"},
		{", &wide,", ", &d,"},
		{"uint16_t * slots", "uint16_t * dst"},
		{"slots[0]", "dst[0]"},
		{"slots[1", "dst[1"},
		{"int bare, counted;", "bool bare; bool counted;"},
		{"bare = ( e.size % first_size ) == 0u;", "const bool bare = ( e.size % first_size ) == 0u;"},
		{"counted = e.size >= 4u && ( ( e.size - 4u ) % first_size ) == 0u;", "const bool counted = e.size >= 4u && ( ( e.size - 4u ) % first_size ) == 0u;"},
		{"kTableFixedRecordMaxBytes = 65536;", "kTableFixedRecordMaxBytes = 65536u;"},
		{"(uint32_t) remap[0]", "(uint32_t) table[0]"},
		{"*why = layout_malformed;", "why = layout_malformed;"},
		{"*why = layout_count_mismatch;", "why = layout_count_mismatch;"},
		{"*why = layout_kind_invalid;", "why = layout_kind_invalid;"},
		{"*why = layout_record_too_large;", "why = layout_record_too_large;"},
		{"*why = layout_tree_unclosed;", "why = layout_tree_unclosed;"},
	}
	for _, t := range repls {
		s = strings.ReplaceAll(s, t.from, t.to)
	}
	for _, fn := range []string{
		"inline bool TableFixedWidens",
		"inline bool TableFixedSignedKind",
		"inline bool TableFixedLeafSize",
		"inline bool TableFixedOrdinalWidth",
		"inline bool TableFixedKnownKind",
		"inline bool TableFixedParseLayout",
	} {
		s = rewriteReturnsInFunc(s, fn)
	}
	s = rewriteFunc(s, "inline bool TableFixedLeafSize", func(body string) string {
		return strings.ReplaceAll(body, "default: break;", "")
	})
	more := []token{
		{"(uint32_t) table[0]", "table[0]"},
		{"for ( i = guarded;", "for ( int32_t i = guarded;"},
		{"const TableFixedEntry REF p = &plan[i];", "const TableFixedEntry REF p = plan[i];"},
		{"TableFixedFail( TableFixedCheck REF c, int why )", "TableFixedFail( TableFixedCheck REF c, TableMessageReason why )"},
		{"int kids_are_variants = true;", "bool kids_are_variants = true;"},
		{"int is_leaf = false;", "bool is_leaf = false;"},
		{"int is_leaf = true;", "bool is_leaf = true;"},
		{"( *c.layout,", "( c.layout,"},
		{"(*c.layout,", "( c.layout,"},
		{"( *c.layout ", "( c.layout "},
		{"TableFixedEntryAt( *c.layout,", "TableFixedEntryAt( c.layout,"},
		{"= layout_malformed;", ";"},
		{"= NULL;", ";"},
	}
	for _, t := range more {
		s = strings.ReplaceAll(s, t.from, t.to)
	}
	s = normalizeLocals(s)
	s = rewriteLayBounds(s)
	s = reUint8Lit.ReplaceAllString(s, "${1}u")
	s = reSkipBraceOpen.ReplaceAllString(s, "n = their_n < my_n ? their_n : my_n;")
	s = reSkipBraceClose.ReplaceAllString(s, "c.skip_clamp = prev_skip; break;")
	s = orderCompileEntry(s)
	amps := []token{
		{"&is_leaf", "is_leaf"},
		{"&view", "view"},
		{"&mine", "mine"},
		{"&why", "why"},
		{"&dst[mi]", "dst[mi]"},
		{"( &c,", "( c,"},
		{"(&c,", "( c,"},
		{"*why =", "why ="},
		{"*out =", "out ="},
		{"(uint64_t) kTableFixedRecordMaxBytes", "kTableFixedRecordMaxBytes"},
		{"(int64_t) my_layout_bytes", "my_layout_bytes"},
		{"const bool ", ""},
		{"uint16_t remap[256]", "uint16_t map[256]"},
		{"remap[", "map["},
		{", remap,", ", map,"},
	}
	for _, t := range amps {
		s = strings.ReplaceAll(s, t.from, t.to)
	}
	s = regexp.MustCompile(`\s+;`).ReplaceAllString(s, ";")
	s = strings.ReplaceAll(s, "kTableFixedRecordMaxBytes = 65536;", "kTableFixedRecordMaxBytes = 65536u;")
	return s
}

func rewriteLayBounds(s string) string {
	// C names the pool pointer `at` and returns the offset expression twice.
	// C++ names the offset `at` and the pointer `dst`. Same 16 bytes.
	return rewriteFunc(s, "inline uint32_t TableFixedLayBounds", func(body string) string {
		body = reCLayBoundsPtr.ReplaceAllString(body, "")
		body = reCLayBoundsBody.ReplaceAllString(body, "at = (uint32_t) ( total - c.pool ); dst = (uint8_t *) (void *) ( (uint8_t *) c.plan + at ); memcpy( dst, &lo, 8 ); memcpy( dst + 8, &hi, 8 ); return at;")
		body = reCppLayBoundsDst.ReplaceAllString(body, "dst = (uint8_t *) (void *) ( (uint8_t *) c.plan + at );")
		return body
	})
}

func orderCompileEntry(s string) string {
	return rewriteFunc(s, "inline void TableFixedCompileEntry", func(body string) string {
		const aux = "aux_at = my_at + d.aux;"
		body = strings.ReplaceAll(body, aux, "")
		const at = "at = my_at + d.dst;"
		body = strings.Replace(body, at, at+" "+aux, 1)
		// C hoists my_arm then stamps argw; C++ stamps argw then declares my_arm.
		const myArm = "my_arm = mi + 1;"
		const saved = "saved_argw = c.argw;"
		if strings.Contains(body, saved) && strings.Contains(body, myArm) {
			body = strings.ReplaceAll(body, myArm, "")
			body = strings.Replace(body, saved, saved+" "+myArm, 1)
		}
		return body
	})
}

func rewriteFunc(s, signature string, fn func(string) string) string {
	start := 0
	for {
		i := strings.Index(s[start:], signature)
		if i < 0 {
			return s
		}
		i += start
		open := strings.Index(s[i:], "{")
		if open < 0 {
			return s
		}
		abs := i + open
		semi := strings.Index(s[i:], ";")
		if semi >= 0 && i+semi < abs {
			// forward declaration
			start = i + len(signature)
			continue
		}
		end := matchingBrace(s, abs)
		if end < 0 {
			return s
		}
		return s[:abs] + fn(s[abs:end+1]) + s[end+1:]
	}
}

func matchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

var localType = regexp.MustCompile(`(?m)^\s*(?:const\s+)?(?:unsigned\s+)?(?:u?int(?:8|16|32|64)_t|uint8_t|bool|int|TableFixedLayoutEntry|TableFixedLayoutView|TableFixedEntry|TableFixedDst|TableFixedCheck|TableMessageReason)\s+(\w+)\s*;\s*$`)
var localInit = regexp.MustCompile(`\b(?:const\s+)?(?:unsigned\s+)?(?:u?int(?:8|16|32|64)_t|uint8_t|bool|int|TableFixedLayoutEntry|TableFixedLayoutView|TableFixedEntry|TableFixedDst|TableFixedCheck|TableMessageReason)\s+(\w+)\s*=`)

func normalizeLocals(s string) string {
	// Each inline function: drop C90 hoisted `Type name;` and peel types off
	// `Type name =` so C++'s declare-at-use matches C's assign-later.
	idx := 0
	var b strings.Builder
	for {
		i := strings.Index(s[idx:], "inline ")
		if i < 0 {
			b.WriteString(s[idx:])
			break
		}
		i += idx
		b.WriteString(s[idx:i])
		open := strings.Index(s[i:], "{")
		if open < 0 {
			b.WriteString(s[i:])
			break
		}
		abs := i + open
		end := matchingBrace(s, abs)
		if end < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i:abs])
		body := s[abs : end+1]
		body = localType.ReplaceAllString(body, "")
		body = localInit.ReplaceAllString(body, "$1 =")
		b.WriteString(body)
		idx = end + 1
	}
	return b.String()
}

func rewriteReturnsInFunc(s, signature string) string {
	start := strings.Index(s, signature)
	if start < 0 {
		return s
	}
	open := strings.Index(s[start:], "{")
	if open < 0 {
		return s
	}
	abs := start + open
	end := matchingBrace(s, abs)
	if end < 0 {
		return s
	}
	body := s[abs : end+1]
	body = strings.ReplaceAll(body, "return 1;", "return true;")
	body = strings.ReplaceAll(body, "return 0;", "return false;")
	return s[:abs] + body + s[end+1:]
}

func dropCOnlyRestatement(s string) string {
	s = reCMessageGuard.ReplaceAllString(s, "")
	s = reCReasons.ReplaceAllString(s, "")
	return s
}

var dropStatements = map[string]bool{
	"first_size = 0, second_size = 0;":   true,
	"under = 0, over = 0;":               true,
	"e.dstsize = 0;":                     true,
	"kids_are_variants = true;":          true,
	"is_leaf = false;":                   true,
	"named = false;":                     true,
	"c.bad = false;":                     true,
	"c.why;":                             true,
	"view = TableFixedLayoutViewZero();": true,
	"e = TableFixedEntryZero();":         true,
	"bool bare;":                         true,
	"bool counted;":                      true,
	"TableFixedCompiler c;":              true,
	"widest = 0;":                        true,
	"sum = 0;":                           true,
	"out = 0;":                           true,
	"split = 0;":                         true,
	"DEPTH_GUARD;":                       true,
	"NAMED_128;":                         true,
}

func collapse(s string) []string {
	// Newlines are not meaning: C splits Put32 across lines and C++ does not.
	// Flatten, then cut after ; { } so a leftover is still a line.
	s = strings.ReplaceAll(s, "\n", " ")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	var lines []string
	var b strings.Builder
	flush := func() {
		line := strings.TrimSpace(b.String())
		b.Reset()
		if line != "" && !dropStatements[line] {
			lines = append(lines, line)
		}
	}
	for _, r := range s {
		switch r {
		case ';', '{', '}':
			b.WriteRune(r)
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return lines
}

func canonicalize(src string) []string {
	s := stripComments(src)
	s = strings.ReplaceAll(s, "#define SCHEMA_TABLE_FIXED_NO_GUARD 0xFFFFFFFFu", "kTableFixedNoGuard = 0xFFFFFFFFu;")
	s = strings.ReplaceAll(s, "#define SCHEMA_TABLE_FIXED_NO_GUARD 0xFFFFFFFF", "kTableFixedNoGuard = 0xFFFFFFFFu;")
	s = stripPreprocessor(s)
	s = applyIdents(s)
	s = dropCOnlyRestatement(s)
	s = expandEnums(s)
	s = strings.ReplaceAll(s, "kTableFixedNoGuard = 0xFFFFFFFFu;", "")
	s = stripZeroing(s)
	s = stripNamed(s)
	s = stripOwedC(s)
	s = rewriteKindMismatchBreaks(s)
	s = normalizeSyntax(s)
	return collapse(s)
}

func extractRuntime(src string) (string, error) {
	start := strings.Index(src, "kTableFixedForm")
	if start < 0 {
		return "", fmt.Errorf("kTableFixedForm not found")
	}
	if nl := strings.LastIndex(src[:start], "\n"); nl >= 0 {
		start = nl + 1
	}
	body := src[start:]
	// C ends at table_bits_to_float (after the 128-bit pair). C++ ends at the
	// namespace close after Put128; table_bits_to_float sits BEFORE the C++ runtime.
	if i := strings.Index(body, "table_bits_to_float"); i > 0 {
		if j := strings.LastIndex(body[:i], "static SCHEMA_UNUSED float"); j >= 0 && i-j < 40 {
			body = body[:j]
		}
	}
	if i := strings.Index(body, "} // namespace bench"); i > 0 {
		body = body[:i]
	}
	return body, nil
}

func extractArray(src, cName, cppName string) (string, error) {
	for _, name := range []string{cName, cppName} {
		needle := name + "[]"
		i := strings.Index(src, needle)
		if i < 0 {
			continue
		}
		open := strings.Index(src[i:], "{")
		if open < 0 {
			continue
		}
		abs := i + open
		depth := 0
		for k := abs; k < len(src); k++ {
			switch src[k] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return src[abs : k+1], nil
				}
			}
		}
	}
	return "", fmt.Errorf("array %s / %s not found", cName, cppName)
}
