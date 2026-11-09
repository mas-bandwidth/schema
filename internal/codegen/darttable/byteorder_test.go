package darttable

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestDartByteOrderIsExplicit holds docs/PORTING.md M12 on the Dart leg: every
// multi-byte read and write the generated Dart makes on a ByteData names its
// byte order, so the host's order is never consulted. A ByteData accessor
// called without an Endian argument reads in BIG-endian order on every host,
// which a test on one machine cannot tell from a correct read of a palindromic
// value, so the rule is held on the emitted text: every
// get/set{Int,Uint,Float}{16,32,64} call over every corpus unit's generated
// files carries `Endian.` inside its argument list, or, in the cook writer,
// the `order` parameter the target's order arrives in. The foreign-file half of
// M12 (the magic, then the order word) is `tables-dart-order` and the
// `cook-foreign` and `block-foreign` conformance surfaces.
//
// Its negative control, TestDartByteOrderIsExplicitNegativeControl, strips the
// order argument from one generated call and requires the scan red on it.
func TestDartByteOrderIsExplicit(t *testing.T) {
	files := byteOrderCorpus(t)
	calls := 0
	for _, name := range sortedNames(files) {
		n, bad := byteOrderScan(string(files[name]))
		calls += n
		for _, b := range bad {
			t.Errorf("%s: a multi-byte ByteData call names no byte order: %s", name, b)
		}
	}
	if calls == 0 {
		t.Fatal("the scan found no multi-byte ByteData call at all: it is reading the wrong files")
	}
	t.Logf("%d multi-byte ByteData calls over %d generated files, every one naming its byte order", calls, len(files))
}

// TestDartByteOrderIsExplicitNegativeControl is the gate's reversed witness
// (tables-dart-byte-order-negative-control): one generated getUint32 call
// loses its Endian.little, the host-order (big-endian) read Dart defaults to,
// and the same scan must name that call.
func TestDartByteOrderIsExplicitNegativeControl(t *testing.T) {
	files := byteOrderCorpus(t)
	sabotaged := ""
	for _, name := range sortedNames(files) {
		s := string(files[name])
		loc := regexp.MustCompile(`\.getUint32\(([^()]*), Endian\.little\)`).FindStringSubmatchIndex(s)
		if loc == nil {
			continue
		}
		files[name] = []byte(s[:loc[0]] + ".getUint32(" + s[loc[2]:loc[3]] + ")" + s[loc[1]:])
		sabotaged = name
		break
	}
	if sabotaged == "" {
		t.Fatal("the sabotage matched nothing: no generated getUint32(..., Endian.little) to strip")
	}
	_, bad := byteOrderScan(string(files[sabotaged]))
	if len(bad) != 1 {
		t.Fatalf("with one call's byte order stripped from %s, the scan named %d calls, want exactly 1: %v", sabotaged, len(bad), bad)
	}
}

var byteDataCall = regexp.MustCompile(`\.(get|set)(Int|Uint|Float)(16|32|64)\(`)

// byteOrderScan counts the multi-byte ByteData calls in one Dart source and
// returns each whose argument list (to its matching parenthesis) names no
// Endian.
func byteOrderScan(src string) (int, []string) {
	var bad []string
	locs := byteDataCall.FindAllStringIndex(src, -1)
	for _, loc := range locs {
		depth, end := 1, len(src)
		for i := loc[1]; i < len(src); i++ {
			switch src[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				end = i
				break
			}
		}
		args := src[loc[1]:end]
		// the cook writer takes the target's order as its `order` parameter
		// (M12: "The cook writer takes the target's order as a parameter")
		last := strings.TrimRight(strings.TrimSpace(args), ",")
		last = strings.TrimSpace(last[strings.LastIndex(last, ",")+1:])
		if !strings.Contains(args, "Endian.") && last != "order" {
			line := strings.Count(src[:loc[0]], "\n") + 1
			bad = append(bad, fmt.Sprintf("line %d: %s%s)", line, src[loc[0]:loc[1]], args))
		}
	}
	return len(locs), bad
}

// byteOrderCorpus is every generated Dart file of every corpus unit under
// tables/, keyed unit/file.
func byteOrderCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	dirs, err := filepath.Glob(filepath.Join(root, "tables", "*"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, dir := range dirs {
		schemas, _ := filepath.Glob(filepath.Join(dir, "*.schema"))
		if len(schemas) == 0 {
			continue
		}
		u := loadDirUnit(t, dir)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for name, src := range files {
			if strings.HasSuffix(name, ".dart") {
				out[filepath.Base(dir)+"/"+name] = src
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no generated Dart file: the corpus moved")
	}
	return out
}

func sortedNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
