package darttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCookMapFieldExposesItsEntriesLikeGo holds the Dart cook of a MAP field to
// what a cooked map IS (docs/SPEC-TABLES.md §2.8, §7.2): an int64 self-relative
// delta to the entry array and an int32 count, the entries by value in
// ascending key order, and `Find` a binary search over them in place. The Go
// cook (the oracle) exposes every entry — `Roster.Count`, `Roster.At(i)` and
// `SquadRosterFind` — so the Dart cook must too: a count, an entry cursor per
// index, and a find by key, never the slot's first byte read as a scalar.
//
// The pure-Go half reads the generated text. The functional half (gated on the
// SDK; `make tables-dart-cook-map-field` runs it under SCHEMA_REQUIRE_CORPUS=1)
// has the Go cook write a Squad with a known roster, an absent map and an
// emptied one, plus a unit holding every key kind §2.8 admits (string(N),
// int64, uint64 — an enum key is refused by name), and requires the Dart
// programs in test/dart-tables to print the oracle's entries line for line.
func TestCookMapFieldExposesItsEntriesLikeGo(t *testing.T) {
	t.Parallel()
	lists := listsUnit(t)
	files, err := Generate(lists)
	if err != nil {
		t.Fatal(err)
	}
	cook := string(files["HoldersCook.dart"])
	row := dartClassBody(t, cook, "SquadRow")
	for _, line := range strings.Split(row, "\n") {
		if strings.Contains(line, "get roster =>") {
			t.Fatalf("the Dart cook reads the map field Squad.roster as ONE BYTE, not as its entries (§2.8, §7.2): %s", strings.TrimSpace(line))
		}
	}
	for _, want := range []string{
		"int get rosterCount {",
		"SquadRosterEntryRow rosterAt(int index, SquadRosterEntryRow into) {",
		"SquadRosterEntryRow? rosterFind(int key, SquadRosterEntryRow into) {",
	} {
		if !strings.Contains(row, want) {
			t.Fatalf("SquadRow does not expose the roster's entries as the Go cook does: no %q in\n%s", want, row)
		}
	}
	entry := dartClassBody(t, cook, "SquadRosterEntryRow")
	for _, want := range []string{"int get key => view.getUint8(at + 0);", "ItemRow value(ItemRow into) {"} {
		if !strings.Contains(entry, want) {
			t.Fatalf("the roster's entry cursor does not read %q:\n%s", want, entry)
		}
	}

	keys := unitFrom(t, mapKeysSchema)
	keyFiles, err := Generate(keys)
	if err != nil {
		t.Fatal(err)
	}
	keysRow := dartClassBody(t, string(keyFiles["ProbeCook.dart"]), "KeysRow")
	for _, want := range []string{
		"KeysNamesEntryRow? namesFind(List<int> key, KeysNamesEntryRow into) {",
		"KeysWideEntryRow? wideFind(int key, KeysWideEntryRow into) {",
		"KeysBigEntryRow? bigFind(int key, KeysBigEntryRow into) {",
		"int get namesCount {", "int get wideCount {", "int get bigCount {",
	} {
		if !strings.Contains(keysRow, want) {
			t.Fatalf("a string, int64 or uint64 key is not exposed as entries: no %q in\n%s", want, keysRow)
		}
	}

	t.Run("dart reads every entry the go cook wrote", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart and the Go toolchain (the Go cook is the oracle)")
		dart := dartBinary(t)
		for _, c := range []struct {
			name    string
			unit    *ir.Unit
			files   map[string][]byte
			oracle  string
			program string
		}{
			{"lists", lists, files, squadOracle, "cook_map_field.dart"},
			{"keys", keys, keyFiles, keysOracle, "cook_map_keys.dart"},
		} {
			dir := t.TempDir()
			want := goCookOracle(t, c.unit, c.oracle, dir)
			got := dartCookProgram(t, dart, c.files, c.program, dir)
			if want == "" || !strings.Contains(want, " entry ") {
				t.Fatalf("%s: the Go oracle printed no entries:\n%s", c.name, want)
			}
			if got != want {
				t.Fatalf("%s: the Dart cook's entries differ from the Go cook's:\n--- go\n%s\n--- dart\n%s", c.name, want, got)
			}
			t.Logf("%s: the Dart cook reads every map entry as the Go cook does: %d lines agree entry for entry",
				c.name, strings.Count(want, "\n"))
		}
	})
}

func listsUnit(t *testing.T) *ir.Unit {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "lists", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/lists: %v", err)
	}
	return loadUnit(t, schemas...)
}

// dartClassBody is one generated class's text, from its declaration to the
// closing brace at column zero.
func dartClassBody(t *testing.T, text, class string) string {
	t.Helper()
	head := "final class " + class + " {"
	at := strings.Index(text, head)
	if at < 0 {
		t.Fatalf("the generated cook declares no %s", class)
	}
	end := strings.Index(text[at:], "\n}\n")
	if end < 0 {
		t.Fatalf("%s has no closing brace", class)
	}
	return text[at : at+end+2]
}

// goCookOracle compiles the Go cook of a unit, runs the oracle body (which
// writes its cooks into dir and prints its ORACLE lines), and answers those
// lines.
func goCookOracle(t *testing.T, u *ir.Unit, oracle, dir string) string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	module := filepath.Join(dir, "go")
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["oracle_test.go"] = []byte("package " + u.Package + "\n" + oracle)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(module, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "TestCookMapOracle", "-v", ".")
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "COOK_OUT="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Go cook oracle failed: %v\n%s", err, out)
	}
	return oracleLines(string(out))
}

// dartCookProgram runs one test/dart-tables program over the generated Dart
// library and the cooks in dir, and answers its ORACLE lines.
func dartCookProgram(t *testing.T, dart string, files map[string][]byte, program, dir string) string {
	t.Helper()
	lib := filepath.Join(dir, "dart")
	if err := os.MkdirAll(lib, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(lib, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "dart-tables", program))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, program), source, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", program, dir)
	cmd.Dir = lib
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Dart program %s failed: %v\n%s", program, err, out)
	}
	return oracleLines(string(out))
}

func oracleLines(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ORACLE ") {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// The Squad oracle: keys inserted OUT of order so the ascending order is the
// map's and not the caller's, a replaced key, both ends of uint8, an absent
// map (never inserted) and an emptied one (inserted, then erased).
const squadOracle = `
import ("fmt";"os";"path/filepath";"testing";"unsafe")

func cookSquad(t *testing.T, b *SquadBuilder, name string) {
	if !b.Lock() { t.Fatal("lock " + name) }
	v := b.AsConst()
	n := SquadCookMeasure(v)
	if n < 0 { t.Fatal("measure " + name) }
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	buf := raw[off : off+uintptr(n)]
	if !SquadCookFrom(v, buf, TableByteOrderLittle) { t.Fatal("cook " + name) }
	if err := os.WriteFile(filepath.Join(os.Getenv("COOK_OUT"), name+".cook"), buf, 0o600); err != nil { t.Fatal(err) }
	var cook SquadCook
	if !SquadOpen(&cook, unsafe.Pointer(&buf[0]), int64(len(buf))) { t.Fatal("open " + name) }
	r := cook.Root()
	fmt.Printf("ORACLE %s count %d\n", name, r.Roster.Count)
	for i := int32(0); i < r.Roster.Count; i++ {
		e := r.Roster.At(i)
		fmt.Printf("ORACLE %s entry %d key %d value %d\n", name, i, e.Key, e.Value.Count)
	}
	for _, k := range []uint8{0, 3, 4, 77, 200, 255} {
		if hit := SquadRosterFind(nil, &r.Roster, k); hit != nil {
			fmt.Printf("ORACLE %s find %d %d\n", name, k, hit.Count)
		} else {
			fmt.Printf("ORACLE %s find %d none\n", name, k)
		}
	}
	fmt.Printf("ORACLE %s name %d\n", name, r.Name)
}

func TestCookMapOracle(t *testing.T) {
	var full SquadBuilder
	full.Init()
	defer full.Shutdown()
	r := full.GetRoot()
	r.Name = -7
	for _, kv := range [][2]int32{{200, 2000}, {3, 30}, {255, 2550}, {77, 1}, {0, -5}, {77, 770}} {
		SquadRosterInsert(&full.Main, &r.Roster, uint8(kv[0])).Count = kv[1]
	}
	cookSquad(t, &full, "full")

	var absent SquadBuilder
	absent.Init()
	defer absent.Shutdown()
	absent.GetRoot().Name = 11
	cookSquad(t, &absent, "absent")

	var empty SquadBuilder
	empty.Init()
	defer empty.Shutdown()
	e := empty.GetRoot()
	e.Name = 12
	SquadRosterInsert(&empty.Main, &e.Roster, 9).Count = 90
	if !SquadRosterErase(&empty.Arena, &e.Roster, 9) { t.Fatal("erase") }
	cookSquad(t, &empty, "empty")
}
`

// mapKeysSchema holds every key kind §2.8 admits that tables/lists and
// tables/examples do not: a bounded string, a signed and an unsigned 64-bit
// integer, with a record value and scalar values.
const mapKeysSchema = `package probe

fixed table Child { n int32 }

table Keys
{
    names map[string(8)]Child
    wide  map[int64]uint16
    big   map[uint64]int32
    after int32
}
`

const keysOracle = `
import ("fmt";"math";"os";"path/filepath";"testing";"unsafe")

func TestCookMapOracle(t *testing.T) {
	var b KeysBuilder
	b.Init()
	defer b.Shutdown()
	r := b.GetRoot()
	r.After = 99
	for i, k := range []string{"z", "a", "mm", "abcdefgh", "", "b"} {
		KeysNamesInsert(&b.Main, &r.Names, k).N = int32(i + 1)
	}
	for i, k := range []int64{7, math.MinInt64, -1, math.MaxInt64, 0} {
		*KeysWideInsert(&b.Main, &r.Wide, k) = uint16(100 + i)
	}
	for i, k := range []uint64{math.MaxUint64, 5, 1 << 63, 0} {
		*KeysBigInsert(&b.Main, &r.Big, k) = int32(-1 - i)
	}
	if !b.Lock() { t.Fatal("lock") }
	v := b.AsConst()
	n := KeysCookMeasure(v)
	if n < 0 { t.Fatal("measure") }
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	buf := raw[off : off+uintptr(n)]
	if !KeysCookFrom(v, buf, TableByteOrderLittle) { t.Fatal("cook") }
	if err := os.WriteFile(filepath.Join(os.Getenv("COOK_OUT"), "keys.cook"), buf, 0o600); err != nil { t.Fatal(err) }
	var cook KeysCook
	if !KeysOpen(&cook, unsafe.Pointer(&buf[0]), int64(len(buf))) { t.Fatal("open") }
	k := cook.Root()
	fmt.Printf("ORACLE names count %d\n", k.Names.Count)
	for i := int32(0); i < k.Names.Count; i++ {
		e := k.Names.At(i)
		fmt.Printf("ORACLE names entry %d key %q value %d\n", i, string(e.Key[:e.KeyLength]), e.Value.N)
	}
	for _, key := range []string{"", "a", "b", "mm", "z", "abcdefg", "abcdefgh"} {
		if hit := KeysNamesFind(nil, &k.Names, key); hit != nil {
			fmt.Printf("ORACLE names find %q %d\n", key, hit.N)
		} else {
			fmt.Printf("ORACLE names find %q none\n", key)
		}
	}
	fmt.Printf("ORACLE wide count %d\n", k.Wide.Count)
	for i := int32(0); i < k.Wide.Count; i++ {
		e := k.Wide.At(i)
		fmt.Printf("ORACLE wide entry %d key %d value %d\n", i, e.Key, e.Value)
	}
	for _, key := range []int64{math.MinInt64, -2, -1, 0, 1, 7, math.MaxInt64} {
		if hit := KeysWideFind(nil, &k.Wide, key); hit != nil {
			fmt.Printf("ORACLE wide find %d %d\n", key, *hit)
		} else {
			fmt.Printf("ORACLE wide find %d none\n", key)
		}
	}
	fmt.Printf("ORACLE big count %d\n", k.Big.Count)
	for i := int32(0); i < k.Big.Count; i++ {
		e := k.Big.At(i)
		fmt.Printf("ORACLE big entry %d key %d value %d\n", i, e.Key, e.Value)
	}
	for _, key := range []uint64{0, 1, 5, 6, 1 << 63, math.MaxUint64} {
		if hit := KeysBigFind(nil, &k.Big, key); hit != nil {
			fmt.Printf("ORACLE big find %d %d\n", key, *hit)
		} else {
			fmt.Printf("ORACLE big find %d none\n", key)
		}
	}
	fmt.Printf("ORACLE after %d\n", k.After)
}
`
