package cqualify

// THE C++ PASS. The C++ target declares a schema's types inside `namespace
// <package>` under their schema spellings, and its own code in that namespace
// names the platform's types, functions and macros unqualified: uint8_t,
// memcpy, NULL. A unit that declares `type uint8_t` hides ::uint8_t from every
// later use in the namespace, so the emitter's own `uint8_t x` names the
// user's struct; `type memcpy` turns every memcpy call into a constructor
// call; and `type NULL` declares `struct __null`.
//
// The C++ spelling of each such type stays its schema name wherever C++ can
// say it, so the pass runs on the type escape (Escape): the emitters see the
// colliding types under the marker, and after them
//
//   - a marked token is the user's, and is written back in its schema
//     spelling, or with a trailing `_` when that spelling is a macro the
//     platform defines (a macro cannot be declared as a type: `NULL_`);
//   - an unmarked token that spells a colliding libc name is the emitter's own
//     and is written `::name` where it names the global — always for a type
//     (uint8_t, size_t, FILE), and for a function at a call (memcpy( after an
//     operator or a keyword, never a declarator) — so the user's type and the
//     platform's are both reachable in the same namespace;
//   - string literals and comments carry the schema spelling, unmarked.

import "strings"

// cppMacros are the macros the C and C++ standard headers a generated C++
// header reaches define: a type spelled like one cannot be declared, so the
// C++ spelling of such a type takes a trailing `_`. Object-like macros expand
// everywhere; the function-like ones (assert, offsetof, INT8_C, ...) expand at
// any `(`, which a value-initialised `T()` supplies. `linux` and `unix` are
// predefined under the GNU dialects.
var cppMacros = func() map[string]bool {
	m := map[string]bool{}
	for _, words := range []string{
		// <stddef.h> <stdio.h> <stdlib.h> <time.h> <locale.h> <signal.h>
		`NULL offsetof EOF BUFSIZ FILENAME_MAX FOPEN_MAX L_tmpnam TMP_MAX SEEK_CUR
		 SEEK_END SEEK_SET stdin stdout stderr EXIT_FAILURE EXIT_SUCCESS RAND_MAX
		 MB_CUR_MAX CLOCKS_PER_SEC TIME_UTC LC_ALL LC_COLLATE LC_CTYPE LC_MONETARY
		 LC_NUMERIC LC_TIME SIGABRT SIGFPE SIGILL SIGINT SIGSEGV SIGTERM SIG_DFL
		 SIG_ERR SIG_IGN`,
		// <assert.h> <errno.h> <setjmp.h> <stdarg.h>
		`assert static_assert errno EDOM EILSEQ ERANGE setjmp va_arg va_copy va_end
		 va_start`,
		// <math.h> <float.h>
		`HUGE_VAL HUGE_VALF HUGE_VALL INFINITY NAN FP_INFINITE FP_NAN FP_NORMAL
		 FP_SUBNORMAL FP_ZERO FP_ILOGB0 FP_ILOGBNAN MATH_ERRNO MATH_ERREXCEPT
		 math_errhandling fpclassify isfinite isinf isnan isnormal signbit
		 isgreater isgreaterequal isless islessequal islessgreater isunordered
		 FLT_RADIX FLT_ROUNDS FLT_EVAL_METHOD DECIMAL_DIG FLT_DIG DBL_DIG LDBL_DIG
		 FLT_EPSILON DBL_EPSILON LDBL_EPSILON FLT_MANT_DIG DBL_MANT_DIG
		 LDBL_MANT_DIG FLT_MAX DBL_MAX LDBL_MAX FLT_MIN DBL_MIN LDBL_MIN
		 FLT_MAX_EXP DBL_MAX_EXP LDBL_MAX_EXP FLT_MIN_EXP DBL_MIN_EXP LDBL_MIN_EXP
		 FLT_MAX_10_EXP DBL_MAX_10_EXP LDBL_MAX_10_EXP FLT_MIN_10_EXP
		 DBL_MIN_10_EXP LDBL_MIN_10_EXP FLT_TRUE_MIN DBL_TRUE_MIN LDBL_TRUE_MIN`,
		// <limits.h>
		`CHAR_BIT CHAR_MAX CHAR_MIN SCHAR_MAX SCHAR_MIN UCHAR_MAX SHRT_MAX SHRT_MIN
		 USHRT_MAX INT_MAX INT_MIN UINT_MAX LONG_MAX LONG_MIN ULONG_MAX LLONG_MAX
		 LLONG_MIN ULLONG_MAX MB_LEN_MAX`,
		// <stdint.h>
		`INT8_MAX INT16_MAX INT32_MAX INT64_MAX INT8_MIN INT16_MIN INT32_MIN
		 INT64_MIN UINT8_MAX UINT16_MAX UINT32_MAX UINT64_MAX INTMAX_MAX INTMAX_MIN
		 UINTMAX_MAX INTPTR_MAX INTPTR_MIN UINTPTR_MAX PTRDIFF_MAX PTRDIFF_MIN
		 SIZE_MAX SIG_ATOMIC_MAX SIG_ATOMIC_MIN WCHAR_MAX WCHAR_MIN WINT_MAX
		 WINT_MIN INT8_C INT16_C INT32_C INT64_C UINT8_C UINT16_C UINT32_C UINT64_C
		 INTMAX_C UINTMAX_C`,
		// <inttypes.h>'s format macros the runtime may pull in
		`PRId8 PRId16 PRId32 PRId64 PRIu8 PRIu16 PRIu32 PRIu64 PRIx8 PRIx16 PRIx32
		 PRIx64 PRIX8 PRIX16 PRIX32 PRIX64 SCNd32 SCNd64 SCNu32 SCNu64`,
		// predefined under -std=gnu++
		`linux unix`,
	} {
		for w := range strings.FieldsSeq(words) {
			m[w] = true
		}
	}
	return m
}()

// CppMacro reports whether name is a macro the C++ target's headers see.
func CppMacro(name string) bool { return cppMacros[name] }

// CppCollides reports whether a declared type spelled name collides with the
// platform in generated C++: a macro, or a libc identifier the emitters' own
// code may name unqualified.
func CppCollides(name string, libc func(string) bool) bool {
	return cppMacros[name] || libc(name)
}

// cppTypeNames are the libc identifiers that name a type: every *_t and these.
// A type is never an emitter's local, so each bare use of one is qualified.
var cppTypeNames = map[string]bool{
	"FILE": true, "lconv": true, "tm": true, "timespec": true, "jmp_buf": true, "va_list": true,
}

// QualifyCpp is the pass after the C++ emitters when Escape marked any type:
// see the comment at the top of this file. libc reports the global names (the
// checker's set, check.LibcIdentifier).
func QualifyCpp(files map[string][]byte, esc *Escapes, libc func(string) bool) (map[string][]byte, error) {
	if esc == nil {
		return files, nil
	}
	declared := map[string]bool{}
	for _, n := range esc.declared {
		declared[n] = true
	}
	// a colliding libc name the unit declares: its bare uses are the emitter's
	global := map[string]bool{}
	for n := range declared {
		if libc(n) && !cppMacros[n] {
			global[n] = true
		}
	}
	out := make(map[string][]byte, len(files))
	for name, data := range files {
		if !isCpp(name) {
			out[name] = []byte(esc.Unescape(string(data)))
			continue
		}
		out[name] = []byte(cppRewrite(string(data), esc, declared, global))
	}
	if err := esc.Leak(out); err != nil {
		return nil, err
	}
	return out, nil
}

func isCpp(name string) bool {
	for _, ext := range []string{".h", ".hpp", ".hh", ".cpp", ".cc", ".cxx", ".inl"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// cppSpelling is the C++ spelling of a declared spelling: itself, or with
// trailing underscores past every macro and every declared type.
func cppSpelling(s string, declared map[string]bool) string {
	if !cppMacros[s] {
		return s
	}
	s += "_"
	for declared[s] || cppMacros[s] {
		s += "_"
	}
	return s
}

func cppRewrite(src string, esc *Escapes, declared, global map[string]bool) string {
	toks := tokenize(src)
	var b strings.Builder
	b.Grow(len(src) + len(src)/16)
	for i, t := range toks {
		switch t.kind {
		case tIdent:
			if esc.Marked(t.text) {
				b.WriteString(cppSpelling(esc.Unescape(t.text), declared))
				continue
			}
			if global[t.text] && namesGlobal(toks, i) {
				b.WriteString("::")
			}
			b.WriteString(t.text)
		case tString, tComment:
			b.WriteString(esc.Unescape(t.text))
		default:
			b.WriteString(t.text)
		}
	}
	return b.String()
}

// namesGlobal reports whether the bare libc identifier at i is a use of the
// platform's name rather than a name the emitter declares itself: outside a
// directive other than #define (a #define's own name stays), not after :: . or
// ->, and for a function only at a call: followed by ( and preceded by an
// operator, a bracket or a keyword, never by a type (which makes it a
// declarator: `int64_t exp = ...` is the emitter's local).
func namesGlobal(toks []token, i int) bool {
	t := toks[i]
	if t.directive >= 0 && !definesBody(toks, i) {
		return false
	}
	p := prevAny(toks, i)
	if p >= 0 {
		switch toks[p].text {
		case "::", ".", "->":
			return false
		case ":":
			if p > 0 && toks[p-1].text == ":" {
				return false
			}
		}
	}
	if cppTypeNames[t.text] || strings.HasSuffix(t.text, "_t") {
		return true
	}
	n := nextAny(toks, i)
	if n < 0 || toks[n].text != "(" {
		return false
	}
	if p < 0 {
		return true
	}
	switch toks[p].kind {
	case tIdent:
		switch toks[p].text {
		case "return", "case", "else", "do", "throw", "co_return", "co_yield", "sizeof", "decltype", "noexcept":
			return true
		}
		return false
	case tPunct:
		switch toks[p].text {
		case "*", "&", "~", ">":
			return false
		}
		return true
	}
	return false
}

// definesBody reports whether the directive token at i sits in a #define's
// replacement list (past the macro's name and parameter list).
func definesBody(toks []token, i int) bool {
	d := toks[i].directive
	j := 0
	for j < len(toks) && toks[j].directive != d {
		j++
	}
	// # define NAME [ ( params ) ] body
	var sig []int
	for k := j; k < len(toks) && toks[k].directive == d && k <= i; k++ {
		if toks[k].kind != tSpace && toks[k].kind != tComment {
			sig = append(sig, k)
		}
	}
	if len(sig) < 3 || toks[sig[1]].text != "define" {
		return false
	}
	nameAt := sig[2]
	if i <= nameAt {
		return false
	}
	// a function-like macro's ( follows its name with no space
	if nameAt+1 < len(toks) && toks[nameAt+1].text == "(" {
		for k := nameAt + 1; k < i; k++ {
			if toks[k].text == ")" {
				return true
			}
		}
		return false
	}
	return true
}

// prevAny and nextAny skip space and comments, inside a directive too.
func prevAny(toks []token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].kind != tSpace && toks[j].kind != tComment {
			return j
		}
	}
	return -1
}

func nextAny(toks []token, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].kind != tSpace && toks[j].kind != tComment {
			return j
		}
	}
	return -1
}
