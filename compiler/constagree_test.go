package compiler

// Every target's generated constant, and every integer default, must be the
// value the schema checker computed. Schema folds integer expressions in
// arbitrary precision; a target that renders the author's expression
// symbolically evaluates it in its own arithmetic instead — C++ in the
// declared type of each referenced constexpr, C by the literal typing of
// each #define's tokens, Rust in the type the literals infer — and where that
// arithmetic wraps, overflows or converts, the generated value is not the
// checked one. These tests hold the property over a corpus of the shapes
// that broke it (unsigned wrap, a hexadecimal literal C types unsigned,
// division and remainder of negatives, promotion of narrow types, the int64
// extremes) and over random expressions:
//
//   - the fast half evaluates the C and C++ backends' emitted text with C's
//     own rules (constagree_ceval_test.go) and checks the Rust and C#
//     literal-only renderings, with no toolchain;
//   - the slow half compiles and runs each target that renders expressions
//     symbolically — C, C++, Go, JavaScript, Rust, C#, Dart and Java — and
//     compares what the program prints with the checker. The targets not
//     here always emit the folded literal (Elixir renders symbolically, in
//     arbitrary-precision integers with div/rem, so its value is the fold).
//
// The schema language has no shift operators, so shifts are not in the
// corpus.

import (
	"bufio"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const constAgreeConsts = `package agree

const Start uint32 = 1
const Offset = Start - 2
const Half = (Start - 2) / 2
const Twice = Start * 2
const Next = 0xffffffff + 1
const Mask = 0xffffffff
const MaskTwice = Mask * 2
const MaskNeg = -0xffffffff
const HighBit = 0x80000000
const HighBitLess = 0x80000000 - 1
const HighBitThird = -0x80000000 / 3
const HighGap = 0x80000000 - 0x80000001
const Ceiling uint32 = 0xffffffff
const CeilingWrap = Ceiling + 1
const CeilingNeg = Ceiling / -1
const CeilingHalf uint32 = 0xffffffff - 0x80000000
const Small int32 = 2000000000
const SmallSum = Small + Small
const Floor int32 = -2147483648
const FloorLess = Floor - 1
const Byte uint8 = 200
const ByteSum = Byte + Byte
const ByteNeg = Byte - 300
const Narrow uint8 = 300 - 100
const Short int16 = -30000
const ShortSquare = Short * Short
const Big uint64 = 10
const BigNeg = Big - 11
const BigThird = (Big - 20) / 3
const Wide = 3000000000 * 3
const NegDiv = -7 / 2
const NegRem = -7 % 2
const DivNeg = 7 / -2
const RemNeg = 7 % -2
const NegNegDiv = -7 / -2
const StartRem = -Start % 2
const Bottom = -9223372036854775807 - 1
const BottomBack = -(-Bottom)
const Huge = 9007199254740993
const Top uint64 = 0xffffffffffffffff
const Low8 int8 = -127 - 1
const Low16 int16 = -32767 - 1
const Low32 int32 = -2147483647 - 1
const Low64 int64 = -9223372036854775807 - 1
const Rem8 = Low8 % -1
const Rem16 = Low16 % -1
const Rem32 = Low32 % (-1)
const Rem64 = Low64 % (-1)
const Div8 = Low8 / -1
const Div16 = Low16 / -1
const Div32 = Low32 / -1
const Rem8Literal int8 = (-127 - 1) % (-1)
const Rem16Literal int16 = (-32767 - 1) % (-1)
const Rem32Literal int32 = (-2147483647 - 1) % (-1)
const Rem64Literal = (-9223372036854775807 - 1) % (-1)
const BottomRem = Bottom % -1
`

const constAgreeTypes = `
type Defaults
{
    a int64 = Offset
    b int64 = Start - 2
    c int64 = 0xffffffff + 1
    d int64 = Mask * 2
    e int64 = Small + Small
    f int64 = (Big - 20) / 3
    g int64 = Ceiling + 1
    h uint8 = 300 - 100
    i int64 = 0x80000000 - 0x80000001
    j uint32 = Ceiling - 1
    k int8 = (-127 - 1) % (-1)
    l int16 = Low16 % -1
    m int32 = (-2147483647 - 1) % (-1)
    n int64 = Low64 % (-1)
}
`

// checkedInts is every integer constant of u by name, and every integer
// default of every struct as "Struct.field", at the checker's value.
func checkedInts(u *ir.Unit) map[string]*big.Int {
	out := map[string]*big.Int{}
	for name, c := range u.Consts {
		if !c.IsFloat && c.Int != nil {
			out[name] = c.Int
		}
	}
	for name, st := range u.Structs {
		for _, f := range st.Fields {
			if f.DefInt != nil {
				out[name+"."+f.Name] = f.DefInt
			}
		}
	}
	return out
}

func sortedKeys(m map[string]*big.Int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// screamingSnake is the corpus names' C / Java / Rust spelling (the corpus
// names are plain CamelCase words, so the general rule is not needed).
func screamingSnake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(rune(name[i-1])) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func agreeText(t *testing.T, u *ir.Unit, lang, suffix string) string {
	t.Helper()
	files, err := New().Generate(u, lang, Options{})
	if err != nil {
		t.Fatalf("generate %s: %v", lang, err)
	}
	var names []string
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.Write(files[name])
	}
	return b.String()
}

var (
	cDefineLine   = regexp.MustCompile(`(?m)^#define ([A-Z0-9_]+) (.*)$`)
	cDefaultLine  = regexp.MustCompile(`(?m)^\s+value\.([a-z0-9_]+) = (.*);$`)
	cppConstLine  = regexp.MustCompile(`(?m)^inline constexpr ([a-z0-9_]+) ([A-Za-z0-9_]+) = (.*);`)
	cppMemberLine = regexp.MustCompile(`(?m)^\s+([a-z0-9_]+) ([a-z0-9_]+) = (.*);`)
	cComment      = regexp.MustCompile(`/\*.*?\*/`)
)

// cValues evaluates the generated C header's constants and Defaults
// initializers with C's rules, keyed as checkedInts keys them.
func cValues(t *testing.T, u *ir.Unit) map[string]cValue {
	t.Helper()
	text := agreeText(t, u, "c", ".h")
	macros := map[string]string{}
	for _, m := range cDefineLine.FindAllStringSubmatch(text, -1) {
		macros[m[1]] = strings.TrimSpace(cComment.ReplaceAllString(m[2], ""))
	}
	var expand func(name string, depth int) (cValue, error)
	expand = func(name string, depth int) (cValue, error) {
		body, ok := macros[name]
		if !ok || depth > 64 {
			return cValue{}, fmt.Errorf("no macro %s", name)
		}
		return evalC(body, func(n string) (cValue, error) { return expand(n, depth+1) })
	}
	prefix := strings.ToUpper(u.Package) + "_"
	out := map[string]cValue{}
	for name, c := range u.Consts {
		if c.IsFloat {
			continue
		}
		v, err := expand(prefix+screamingSnake(name), 0)
		if err != nil {
			t.Errorf("C %s: %v", name, err)
			continue
		}
		out[name] = v
	}
	for _, m := range cDefaultLine.FindAllStringSubmatch(text, -1) {
		v, err := evalC(m[2], func(n string) (cValue, error) { return expand(n, 0) })
		if err != nil {
			t.Errorf("C default %s = %s: %v", m[1], m[2], err)
			continue
		}
		out["Defaults."+m[1]] = v
	}
	return out
}

// cppValues evaluates the generated C++ header's constexpr constants and the
// Defaults member initializers with C++'s rules.
func cppValues(t *testing.T, u *ir.Unit) map[string]cValue {
	t.Helper()
	text := agreeText(t, u, "cpp", ".h")
	type decl struct {
		typ  cType
		expr string
	}
	decls := map[string]decl{}
	for _, m := range cppConstLine.FindAllStringSubmatch(text, -1) {
		typ, ok := cppDeclType(m[1])
		if !ok {
			continue
		}
		decls[m[2]] = decl{typ, m[3]}
	}
	var resolve func(name string, depth int) (cValue, error)
	resolve = func(name string, depth int) (cValue, error) {
		d, ok := decls[name]
		if !ok || depth > 64 {
			return cValue{}, fmt.Errorf("no constexpr %s", name)
		}
		v, err := evalC(d.expr, func(n string) (cValue, error) { return resolve(n, depth+1) })
		if err != nil {
			return v, err
		}
		// the initializer converts to the declared type, and a reference
		// carries that type
		return cValue{d.typ.convert(v.v), d.typ}, nil
	}
	out := map[string]cValue{}
	for name, c := range u.Consts {
		if c.IsFloat {
			continue
		}
		d, ok := decls[name]
		if !ok {
			t.Errorf("C++ %s: no constexpr declaration", name)
			continue
		}
		// the initializer's own value, before the conversion to the declared
		// type: a wrapped value that happens to convert back is still wrong
		raw, err := evalC(d.expr, func(n string) (cValue, error) { return resolve(n, 0) })
		if err != nil {
			t.Errorf("C++ %s = %s: %v", name, d.expr, err)
			continue
		}
		out[name] = raw
	}
	if _, rest, found := strings.Cut(text, "struct Defaults\n{"); found {
		body, _, closed := strings.Cut(rest, "};")
		if !closed {
			t.Errorf("C++: struct Defaults has no closing }; in the generated header")
			return out
		}
		for _, m := range cppMemberLine.FindAllStringSubmatch(body, -1) {
			v, err := evalC(m[3], func(n string) (cValue, error) { return resolve(n, 0) })
			if err != nil {
				t.Errorf("C++ default %s = %s: %v", m[2], m[3], err)
				continue
			}
			out["Defaults."+m[2]] = v
		}
	}
	return out
}

func compareAgree(t *testing.T, target string, want map[string]*big.Int, got map[string]cValue, src string) {
	t.Helper()
	for _, name := range sortedKeys(want) {
		g, ok := got[name]
		if !ok {
			if cCarries(name, ".") {
				t.Errorf("%s: default %s was not found in the generated text", target, name)
			}
			continue // an evaluation error was already reported
		}
		if g.v.Cmp(want[name]) != 0 {
			t.Errorf("%s: %s evaluates to %s, the checker says %s", target, name, g.v, want[name])
		}
	}
	if t.Failed() && src != "" {
		t.Logf("schema:\n%s", src)
	}
}

func TestGeneratedConstantsAgreeWithCheckerC(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, constAgreeConsts+constAgreeTypes)
	compareAgree(t, "C", checkedInts(u), cValues(t, u), "")
}

func TestGeneratedConstantsAgreeWithCheckerCpp(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, constAgreeConsts+constAgreeTypes)
	compareAgree(t, "C++", checkedInts(u), cppValues(t, u), "")
}

// The literal-only renderings: Rust evaluates a literal-only expression in
// the type its literals infer from the context, and C# types a literal past
// INT32_MAX uint; where either leaves the type, the checked value is folded.
func TestGeneratedLiteralExpressionsFoldWhereTheTargetWouldNot(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, constAgreeConsts+constAgreeTypes)
	rust := agreeText(t, u, "rust", ".rs")
	for _, want := range []string{"pub const NARROW: u8 = 200;", "h: 200,"} {
		if !cCarries(rust, want) {
			t.Errorf("Rust: want %q (the u8 literal 300 is out of range)", want)
		}
	}
	cs := agreeText(t, u, "cs", ".cs")
	for _, want := range []string{"public const long HighGap = -1;", "value.I = -1;"} {
		if !cCarries(cs, want) {
			t.Errorf("C#: want %q (uint arithmetic would refuse it)", want)
		}
	}
}

// randomConstCorpus writes a schema of typed base constants at the edges of
// their types and untyped constants over random expressions of literals
// (decimal and hexadecimal, inside and past int32 and uint32) and earlier
// constants, keeping only expressions whose value is a legal untyped
// constant.
func randomConstCorpus(seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	type konst struct {
		name string
		v    *big.Int
	}
	var consts []konst
	var b strings.Builder
	b.WriteString("package agree\n\n")
	name := func(i int) string { return fmt.Sprintf("Gen%c%c", 'a'+byte(i/26), 'a'+byte(i%26)) }
	storages := []struct {
		name   string
		lo, hi *big.Int
	}{
		{"int8", big.NewInt(-128), big.NewInt(127)},
		{"uint8", big.NewInt(0), big.NewInt(255)},
		{"int16", big.NewInt(-32768), big.NewInt(32767)},
		{"uint16", big.NewInt(0), big.NewInt(65535)},
		{"int32", cInt.lo(), cInt.hi()},
		{"uint32", big.NewInt(0), cUint.hi()},
		{"int64", cLong.lo(), cLong.hi()},
		{"uint64", big.NewInt(0), cUlong.hi()},
	}
	literal := func(v *big.Int) string {
		if v.Sign() >= 0 && rng.Intn(2) == 0 {
			return fmt.Sprintf("0x%x", v)
		}
		if v.Sign() < 0 {
			return "-" + new(big.Int).Neg(v).String()
		}
		return v.String()
	}
	edge := func(lo, hi *big.Int) *big.Int {
		switch rng.Intn(5) {
		case 0:
			return new(big.Int).Set(lo)
		case 1:
			return new(big.Int).Set(hi)
		case 2:
			return new(big.Int).Add(lo, big.NewInt(int64(rng.Intn(3))))
		case 3:
			return new(big.Int).Sub(hi, big.NewInt(int64(rng.Intn(3))))
		}
		span := new(big.Int).Sub(hi, lo)
		return new(big.Int).Add(lo, new(big.Int).Rand(rng, new(big.Int).Add(span, big.NewInt(1))))
	}
	for range 8 {
		s := storages[rng.Intn(len(storages))]
		v := edge(s.lo, s.hi)
		n := name(len(consts))
		fmt.Fprintf(&b, "const %s %s = %s\n", n, s.name, literal(v))
		consts = append(consts, konst{n, v})
	}
	leafValues := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(7), big.NewInt(100),
		cInt.hi(), new(big.Int).Add(cInt.hi(), big.NewInt(1)), cUint.hi(), new(big.Int).Add(cUint.hi(), big.NewInt(1)),
		big.NewInt(3000000000), cLong.hi()}
	limit := new(big.Int).Lsh(big.NewInt(1), 80)
	var expr func(depth int) (string, *big.Int, bool)
	expr = func(depth int) (string, *big.Int, bool) {
		if depth == 0 || rng.Intn(3) == 0 {
			if len(consts) > 0 && rng.Intn(2) == 0 {
				k := consts[rng.Intn(len(consts))]
				return k.name, k.v, true
			}
			v := leafValues[rng.Intn(len(leafValues))]
			if rng.Intn(2) == 0 {
				return fmt.Sprintf("0x%x", v), v, true
			}
			return v.String(), v, true
		}
		switch rng.Intn(6) {
		case 0:
			s, v, ok := expr(depth - 1)
			if !ok {
				return "", nil, false
			}
			return "-" + s, new(big.Int).Neg(v), true
		case 1:
			s, v, ok := expr(depth - 1)
			return "(" + s + ")", v, ok
		}
		xs, x, ok1 := expr(depth - 1)
		ys, y, ok2 := expr(depth - 1)
		if !ok1 || !ok2 {
			return "", nil, false
		}
		op := []string{"+", "-", "*", "/", "%"}[rng.Intn(5)]
		v := new(big.Int)
		switch op {
		case "+":
			v.Add(x, y)
		case "-":
			v.Sub(x, y)
		case "*":
			v.Mul(x, y)
		case "/", "%":
			if y.Sign() == 0 {
				return "", nil, false
			}
			if op == "/" {
				v.Quo(x, y)
			} else {
				v.Rem(x, y)
			}
		}
		if new(big.Int).Abs(v).Cmp(limit) > 0 {
			return "", nil, false
		}
		// a binary operand that is itself unary or binary is parenthesized,
		// so the schema text keeps the tree's shape
		return "(" + xs + ") " + op + " (" + ys + ")", v, true
	}
	for len(consts) < 48 {
		s, v, ok := expr(3)
		if !ok || !cLong.holds(v) {
			continue
		}
		n := name(len(consts))
		fmt.Fprintf(&b, "const %s = %s\n", n, s)
		consts = append(consts, konst{n, v})
	}
	return b.String()
}

func TestGeneratedConstantsAgreeWithCheckerRandom(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 24; seed++ {
		src := randomConstCorpus(seed)
		u := unitFromSource(t, src)
		want := checkedInts(u)
		compareAgree(t, fmt.Sprintf("C (seed %d)", seed), want, cValues(t, u), src)
		compareAgree(t, fmt.Sprintf("C++ (seed %d)", seed), want, cppValues(t, u), src)
		if t.Failed() {
			return
		}
	}
}

// ---- the slow half: compile, run, compare ----

func TestGeneratedConstantsCompileToCheckerValues(t *testing.T) {
	slowtest.Gate(t, "the C, C++, Go, Node, Rust, .NET, Dart and Java toolchains (each compiles and runs the generated constants)")
	t.Parallel()
	withTypes := unitFromSource(t, constAgreeConsts+constAgreeTypes)
	constsOnly := unitFromSource(t, constAgreeConsts)
	want := checkedInts(withTypes)
	wantConsts := checkedInts(constsOnly)

	t.Run("c", func(t *testing.T) {
		t.Parallel()
		runCFamilyProbe(t, withTypes, want, "c")
	})
	t.Run("cpp", func(t *testing.T) {
		t.Parallel()
		runCFamilyProbe(t, withTypes, want, "cpp")
	})
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		goBin := agreeTool(t, "go")
		dir := agreeGenerate(t, constsOnly, "go", "agree")
		var main strings.Builder
		main.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"agreeprobe/agree\"\n)\n\nfunc main() {\n")
		for _, name := range sortedKeys(wantConsts) {
			fmt.Fprintf(&main, "\tfmt.Println(%q, agree.%s)\n", name, name)
		}
		main.WriteString("}\n")
		agreeWrite(t, filepath.Join(dir, "main.go"), main.String())
		agreeWrite(t, filepath.Join(dir, "go.mod"), "module agreeprobe\n\ngo 1.21\n")
		cmd := testproc.Command(t, goBin, "run", ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
		agreePrinted(t, "Go", wantConsts, agreeRun(t, cmd))
	})
	t.Run("js", func(t *testing.T) {
		t.Parallel()
		node := agreeTool(t, "node")
		dir := agreeGenerate(t, constsOnly, "js", "")
		agreeWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`+"\n")
		var probe strings.Builder
		probe.WriteString("import * as k from \"./Probe.js\";\n")
		for _, name := range sortedKeys(wantConsts) {
			fmt.Fprintf(&probe, "console.log(%q, String(k.%s));\n", name, name)
		}
		agreeWrite(t, filepath.Join(dir, "probe.js"), probe.String())
		agreePrinted(t, "JavaScript", wantConsts, agreeRun(t, testproc.Command(t, node, filepath.Join(dir, "probe.js"))))
	})
	t.Run("rust", func(t *testing.T) {
		t.Parallel()
		rustc := agreeTool(t, "rustc")
		lines := agreeLines(agreeText(t, constsOnly, "rust", ".rs"), regexp.MustCompile(`^pub const [A-Z0-9_]+: [iu](8|16|32|64|size) = .*;$`))
		var src strings.Builder
		for _, l := range lines {
			src.WriteString(l + "\n")
		}
		src.WriteString("fn main() {\n")
		for _, name := range sortedKeys(wantConsts) {
			fmt.Fprintf(&src, "    println!(\"{} {}\", %q, %s);\n", name, ir.RustConstName(name))
		}
		src.WriteString("}\n")
		dir := t.TempDir()
		agreeWrite(t, filepath.Join(dir, "main.rs"), src.String())
		bin := filepath.Join(dir, "probe")
		agreeRun(t, testproc.Command(t, rustc, "--edition", "2021", "-A", "warnings", "-o", bin, filepath.Join(dir, "main.rs")))
		agreePrinted(t, "Rust", wantConsts, agreeRun(t, testproc.Command(t, bin)))
	})
	t.Run("cs", func(t *testing.T) {
		t.Parallel()
		dotnet := findDotnet()
		if dotnet == "" {
			t.Skip("dotnet unavailable")
		}
		dir := agreeGenerate(t, constsOnly, "cs", "")
		agreeWrite(t, filepath.Join(dir, "probe.csproj"), `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
  </PropertyGroup>
</Project>
`)
		var prog strings.Builder
		prog.WriteString("class Program {\n    static void Main() {\n")
		for _, name := range sortedKeys(wantConsts) {
			fmt.Fprintf(&prog, "        System.Console.WriteLine(%q + \" \" + Agree.Schema.%s);\n", name, name)
		}
		prog.WriteString("    }\n}\n")
		agreeWrite(t, filepath.Join(dir, "Program.cs"), prog.String())
		cmd := testproc.Command(t, dotnet, "run", "--configuration", "Release")
		cmd.Dir = dir
		agreePrinted(t, "C#", wantConsts, agreeRun(t, cmd))
	})
	t.Run("dart", func(t *testing.T) {
		t.Parallel()
		dart := agreeTool(t, "dart")
		lines := agreeLines(agreeText(t, constsOnly, "dart", ".dart"), regexp.MustCompile(`^const int [A-Za-z0-9_]+ = .*;$`))
		var src strings.Builder
		for _, l := range lines {
			src.WriteString(l + "\n")
		}
		src.WriteString("void main() {\n")
		for _, name := range sortedKeys(wantConsts) {
			fmt.Fprintf(&src, "  print('%s ${%s}');\n", name, lowerFirst(name))
		}
		src.WriteString("}\n")
		dir := t.TempDir()
		agreeWrite(t, filepath.Join(dir, "probe.dart"), src.String())
		got := agreeRun(t, testproc.Command(t, dart, "run", filepath.Join(dir, "probe.dart")))
		// Dart's int is a signed 64-bit integer: a uint64 constant past
		// INT64_MAX is its bit pattern (dartIntLit), printed negative
		for name, v := range wantConsts {
			if v.Sign() > 0 && !v.IsInt64() {
				got = strings.Replace(got, name+" "+cLong.convert(v).String(), name+" "+v.String(), 1)
			}
		}
		agreePrinted(t, "Dart", wantConsts, got)
	})
	t.Run("java", func(t *testing.T) {
		t.Parallel()
		java := agreeTool(t, "java")
		lines := agreeLines(agreeText(t, constsOnly, "java", ".java"), regexp.MustCompile(`^    public static final (int|long) [A-Z0-9_]+ = .*;$`))
		var src strings.Builder
		src.WriteString("public class Probe {\n")
		for _, l := range lines {
			src.WriteString(l + "\n")
		}
		src.WriteString("    public static void main(String[] args) {\n")
		for _, name := range sortedKeys(wantConsts) {
			v := wantConsts[name]
			if v.Sign() > 0 && !v.IsInt64() {
				fmt.Fprintf(&src, "        System.out.println(%q + \" \" + Long.toUnsignedString(%s));\n", name, screamingSnake(name))
				continue
			}
			fmt.Fprintf(&src, "        System.out.println(%q + \" \" + %s);\n", name, screamingSnake(name))
		}
		src.WriteString("    }\n}\n")
		dir := t.TempDir()
		agreeWrite(t, filepath.Join(dir, "Probe.java"), src.String())
		agreePrinted(t, "Java", wantConsts, agreeRun(t, testproc.Command(t, java, filepath.Join(dir, "Probe.java"))))
	})
}

// runCFamilyProbe compiles the generated data header with a probe printing
// every constant and every Defaults field: whether the expression's type is
// unsigned, then its value read signed and unsigned — so a wrapped unsigned
// value is caught even where a cast would hide it.
func runCFamilyProbe(t *testing.T, u *ir.Unit, want map[string]*big.Int, lang string) {
	t.Helper()
	compiler, ext, std, show := "cc", ".c", "-std=c99", "CC"
	if lang == "cpp" {
		compiler, ext, std, show = "c++", ".cpp", "-std=c++17", "CXX"
	}
	if env := os.Getenv(show); env != "" {
		compiler = env
	}
	compiler = agreeTool(t, compiler)
	dir := agreeGenerate(t, u, lang, "")
	var src strings.Builder
	src.WriteString("#include <stdio.h>\n#include \"Probe.h\"\n")
	src.WriteString("#define SHOW( name, x ) printf( \"%s %d %lld %llu\\n\", name, ( ( x ) * 0 - 1 ) > 0, (long long) ( x ), (unsigned long long) ( x ) )\n")
	src.WriteString("int main( void )\n{\n")
	if lang == "c" {
		src.WriteString("    agree_defaults_t d = agree_new_defaults();\n")
	} else {
		src.WriteString("    agree::Defaults d;\n")
	}
	for _, name := range sortedKeys(want) {
		switch {
		case strings.HasPrefix(name, "Defaults."):
			fmt.Fprintf(&src, "    SHOW( %q, d.%s );\n", name, strings.TrimPrefix(name, "Defaults."))
		case lang == "c":
			fmt.Fprintf(&src, "    SHOW( %q, AGREE_%s );\n", name, screamingSnake(name))
		default:
			fmt.Fprintf(&src, "    SHOW( %q, agree::%s );\n", name, name)
		}
	}
	src.WriteString("    return 0;\n}\n")
	path := filepath.Join(dir, "probe"+ext)
	agreeWrite(t, path, src.String())
	bin := filepath.Join(dir, "probe")
	agreeRun(t, testproc.Command(t, compiler, std, "-Wall", "-Wextra", "-Werror", "-Wno-unused-function", "-I"+dir, path, "-o", bin))
	out := agreeRun(t, testproc.Command(t, bin))
	got := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 4 {
			t.Fatalf("probe line %q", sc.Text())
		}
		if f[1] == "1" {
			got[f[0]] = f[3] // an unsigned expression: its value is the unsigned reading
		} else {
			got[f[0]] = f[2]
		}
	}
	for _, name := range sortedKeys(want) {
		if got[name] != want[name].String() {
			t.Errorf("%s: %s is %s in the compiled program, the checker says %s", lang, name, got[name], want[name])
		}
	}
}

func agreePrinted(t *testing.T, target string, want map[string]*big.Int, out string) {
	t.Helper()
	got := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			got[f[0]] = f[1]
		}
	}
	for _, name := range sortedKeys(want) {
		if got[name] != want[name].String() {
			t.Errorf("%s: %s is %q in the compiled program, the checker says %s", target, name, got[name], want[name])
		}
	}
}

func agreeTool(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s unavailable: %v", name, err)
	}
	return p
}

// writeGenerated writes u's generated files for lang into a fresh directory
// (into its subdirectory sub when sub is not empty) and returns the directory.
func agreeGenerate(t *testing.T, u *ir.Unit, lang, sub string) string {
	t.Helper()
	files, err := New().Generate(u, lang, Options{})
	if err != nil {
		t.Fatalf("generate %s: %v", lang, err)
	}
	dir := t.TempDir()
	into := filepath.Join(dir, sub)
	for name, data := range files {
		p := filepath.Join(into, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func agreeWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func agreeRun(t *testing.T, cmd *testproc.Cmd) string {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(cmd.Args, " "), err, out)
	}
	return string(out)
}

func agreeLines(text string, re *regexp.Regexp) []string {
	var out []string
	for l := range strings.SplitSeq(text, "\n") {
		if re.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}
