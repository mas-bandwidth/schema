package js

import (
	"regexp"
	"slices"
	"sort"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

const declProbe = `package declprobe

const Limit = 4

enum Team { Red, Blue }

flags Caps { Fly, Swim }

type Leaf
{
    id    int32 | min = 0, max = 100
    big   uint64
    team  Team
    caps  Caps
    name  string(8)
    grid  [Limit]uint8
    marks [..Limit]int16
}

union Pick
{
    leaf Leaf
    idle
}

type Root
{
    flag  bool
    pick  Pick
    rows  [2]Leaf
}

type Empty
{
}
`

var (
	jsExport   = regexp.MustCompile(`(?m)^export (?:const|function|class) (\w+)`)
	declExport = regexp.MustCompile(`(?m)^export declare (?:const|function|class) (\w+)`)
	jsMember   = regexp.MustCompile(`this\.(\w+) =`)
	declMember = regexp.MustCompile(`(?m)^  (\w+): `)
)

func names(re *regexp.Regexp, text string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// Every module ships its declarations, and they declare exactly what the
// module exports and exactly the storage members its constructors lay down.
func TestDeclarationsMatchTheModules(t *testing.T) {
	t.Parallel()
	ast, perrs := parser.Parse("Probe.schema", []byte(declProbe))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(declProbe), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range []string{"Probe", "ProbeFlat"} {
		js, decl := string(out[mod+".js"]), string(out[mod+".d.ts"])
		if decl == "" {
			t.Fatalf("%s.d.ts is not emitted: %v", mod, keys(out))
		}
		if a, b := names(jsExport, js), names(declExport, decl); !slices.Equal(a, b) {
			t.Errorf("%s: the declarations export %v, the module %v", mod, b, a)
		}
	}
	js, decl := string(out["Probe.js"]), string(out["Probe.d.ts"])
	for _, class := range []string{"Leaf", "Root", "Pick"} {
		body := regexp.MustCompile(`(?s)export class ` + class + ` \{\n  constructor\(\) \{\n(.*?)\n  \}\n\}`).FindStringSubmatch(js)
		dbody := regexp.MustCompile(`(?ms)^export declare class ` + class + ` \{\n(.*?)^\}`).FindStringSubmatch(decl)
		if body == nil || dbody == nil {
			t.Fatalf("class %s: not found in the module or its declarations", class)
		}
		if a, b := names(jsMember, body[1]), names(declMember, dbody[1]); !slices.Equal(a, b) {
			t.Errorf("class %s: declared members %v, constructed %v", class, b, a)
		}
	}
	for _, want := range []string{
		"  id: number;", "  big: bigint;", "  team: Team;", "  caps: bigint;", "  name: Uint8Array;",
		"  nameLength: number;", "  grid: Uint8Array;", "  marks: number[];", "  marksCount: number;",
		"  type: PickType;", "  leaf: Leaf;", "  rows: Leaf[];",
		"export type Team = (typeof Team)[keyof typeof Team];",
		"export declare const TeamMax: 2;",
		"export declare function WriteRoot(stream: SerializeStream, value: Root): boolean;",
		"export declare function ZeroEmpty(value?: Empty): void;",
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(decl) {
			t.Errorf("Probe.d.ts lacks %q", want)
		}
	}
	flat := string(out["ProbeFlat.d.ts"])
	for _, want := range []string{
		`import type { Empty, Leaf, Pick, Root } from "./Probe.js";`,
		"export declare function ReadRootFlat(view: DataView, numBits: number, value: Root): boolean;",
		"export declare function WritePickFlat(view: DataView, value: Pick): number;",
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(flat) {
			t.Errorf("ProbeFlat.d.ts lacks %q", want)
		}
	}
}
