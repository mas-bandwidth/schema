package cqualify

// THE TYPE ESCAPE. A C-family emitter writes a declared type in its schema
// spelling, and a token pass after it (Qualify for C, the namespace pass of
// the C++ target) can only see tokens. A unit that declares `type uint8_t`,
// `type memcpy` or `type NULL` then hands that pass a header in which the
// user's name and the platform's are the same token, and the pass rewrites
// both: `typedef uint8_t probe_kind_t` became `typedef probe_uint8_t
// probe_kind_t`, every memcpy call became a cast to the user's struct, and
// every NULL became PROBE_NULL. No rule about which names may collide can fix
// that: an emitter's locals (value, stream, raw) are names too.
//
// So the emitter never writes a declared type in its schema spelling. Escape
// hands it a COPY of the unit in which every chosen type is spelled under a
// marker no schema can contain (`schemacesc3xuint8_t`), each derived name the
// emitter builds from it (write_schemacesc3xuint8_t, SCHEMACESC3X_UINT8_T_MAX)
// carries the marker with it, and the pass after the emitter is the one place
// that maps a marked spelling back. A token without the marker is the
// emitter's own and is never respelled because a user declared its name.
//
// The copy keeps every identity the wire reads: a table's declared name rides
// in Struct.SchemaName (ir.Struct.WireName hashes it), and string literals and
// comments are unmarked by the pass, so descriptors, reflection names and the
// text form keep the schema's spellings. Nothing marked may survive the pass:
// Leak refuses an output that still carries the marker, by name.

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Escapes maps every marked spelling an emitter can derive from an escaped
// type back to the spelling it derives from the declared name.
type Escapes struct {
	stem     string            // the marker's stem, absent from every string the unit carries
	declared map[string]string // marked name -> declared name
	replacer *strings.Replacer // every derived marked spelling -> its declared twin
	pairs    [][2]string       // the replacer's pairs, longest marked spelling first
}

// derivations are the spellings the C-family emitters build from a type name.
// Each is applied to the marked and to the declared name, so the pair carries
// whatever the emitter does to the name; a derivation missing here leaves the
// marker in the output, and Leak refuses that output by name.
var derivations = []func(string) string{
	func(s string) string { return s },
	ir.RustSnake,
	ir.RustConstName,
	ir.GoExportName,
	strings.ToUpper,
	strings.ToLower,
	func(s string) string { return ir.RustSnake(ir.GoExportName(s)) },
}

// Escape returns a copy of u in which every declared type for which want
// reports true (a packet type, a table, an enum, a flags or a union, and the
// map entries and text elements the checker generates) is spelled under its
// marker, and the
// table that maps the marked spellings back. A unit with no such type is
// returned as it is, with a nil table.
func Escape(u *ir.Unit, want func(name string) bool) (*ir.Unit, *Escapes) {
	var names []string
	seen := map[string]bool{}
	note := func(n string) {
		if n != "" && !seen[n] && want(n) {
			seen[n] = true
			names = append(names, n)
		}
	}
	for n := range u.Structs {
		note(n)
	}
	for n := range u.Tables {
		note(n)
	}
	for n := range u.Enums {
		note(n)
	}
	for n := range u.Flags {
		note(n)
	}
	for n := range u.Unions {
		note(n)
	}
	for n := range u.TableUnions {
		note(n)
	}
	if len(names) == 0 {
		return u, nil
	}
	sort.Strings(names)
	e := &Escapes{stem: stemFor(u), declared: map[string]string{}}
	// ONE marker for every name, ending in the separator: then a derived
	// spelling never gains or loses a separator at the seam (ir.RustSnake
	// inserts none after `_`), and any list an emitter sorts by a derived
	// spelling sorts the marked names exactly as it sorts the declared ones.
	// Two names whose derivations differ only at the seam (`Ab` and `_ab`
	// both snake to `<marker>ab`) cannot share it, and then each name takes
	// its own: the stem, its zero-padded rank, the separator
	marker := func(int) string { return e.stem + "_" }
	rename, pairs, ok := markAll(names, marker)
	if !ok {
		width := len(strconv.Itoa(len(names)))
		marker = func(i int) string { return fmt.Sprintf("%s%0*d_", e.stem, width, i) }
		rename, pairs, _ = markAll(names, marker)
	}
	for n, to := range rename {
		e.declared[to] = n
	}
	for k := range pairs {
		e.pairs = append(e.pairs, [2]string{k, pairs[k]})
	}
	// longest first, so a name never matches inside a longer one's spelling
	// (strings.Replacer tries its pairs in argument order at each position)
	sort.Slice(e.pairs, func(i, j int) bool {
		if len(e.pairs[i][0]) != len(e.pairs[j][0]) {
			return len(e.pairs[i][0]) > len(e.pairs[j][0])
		}
		return e.pairs[i][0] < e.pairs[j][0]
	})
	args := make([]string, 0, 2*len(e.pairs))
	for _, p := range e.pairs {
		args = append(args, p[0], p[1])
	}
	e.replacer = strings.NewReplacer(args...)
	return respellTypes(u, rename), e
}

// markAll spells each name under marker(rank) and derives every pair; ok is
// false when two derivations of different names land on one marked spelling.
func markAll(names []string, marker func(int) string) (rename, pairs map[string]string, ok bool) {
	rename = map[string]string{}
	pairs = map[string]string{}
	ok = true
	for i, n := range names {
		to := marker(i) + n
		rename[n] = to
		for _, d := range derivations {
			k, v := d(to), d(n)
			if prev, dup := pairs[k]; dup && prev != v {
				ok = false
			}
			pairs[k] = v
		}
	}
	return rename, pairs, ok
}

// Unescape maps every marked spelling in s back to its declared twin.
func (e *Escapes) Unescape(s string) string {
	if e == nil || !e.Marked(s) {
		return s
	}
	return e.replacer.Replace(s)
}

// Marked reports whether s carries the marker, in any case.
func (e *Escapes) Marked(s string) bool {
	return e != nil && strings.Contains(asciiLower(s), e.stem)
}

// asciiLower folds ASCII letters only, so byte offsets survive the fold (the
// marker is ASCII; strings.ToLower may change the length of other text).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Leak refuses an output that still carries the marker: a derivation the
// table does not know, or a marked token the pass after the emitter missed.
// Either is a defect of the compiler, never of the schema, and it is named.
func (e *Escapes) Leak(files map[string][]byte) error {
	if e == nil {
		return nil
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := string(files[n])
		i := strings.Index(asciiLower(s), e.stem)
		if i < 0 {
			continue
		}
		line := strings.Count(s[:i], "\n") + 1
		start := strings.LastIndexByte(s[:i], '\n') + 1
		end := len(s)
		if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
			end = i + j
		}
		return fmt.Errorf("internal error: the C-family type escape leaked into %s line %d (%q): an emitter derived a spelling cqualify.Escape does not map back; report it with the schema", n, line, s[start:end])
	}
	return nil
}

// Err returns err with every marked spelling in its message unmarked, so a
// refusal names the declared types. The original stays reachable through
// errors.Unwrap.
func (e *Escapes) Err(err error) error {
	if err == nil || e == nil || !e.Marked(err.Error()) {
		return err
	}
	return &unescapedError{msg: e.Unescape(err.Error()), err: err}
}

type unescapedError struct {
	msg string
	err error
}

func (u *unescapedError) Error() string { return u.msg }
func (u *unescapedError) Unwrap() error { return u.err }

// stemFor is a marker stem no string of the unit contains, in any case: every
// name, doc, tag, key, default and expression the IR holds is read, so a
// schema can never spell a marker by accident.
func stemFor(u *ir.Unit) string {
	var all strings.Builder
	collectStrings(reflect.ValueOf(u), map[uintptr]bool{}, &all)
	text := asciiLower(all.String())
	stem := "schemacesc"
	for strings.Contains(text, stem) {
		stem += "q"
	}
	return stem
}

func collectStrings(v reflect.Value, seen map[uintptr]bool, out *strings.Builder) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		collectStrings(v.Elem(), seen, out)
	case reflect.Interface:
		if !v.IsNil() {
			collectStrings(v.Elem(), seen, out)
		}
	case reflect.Struct:
		for _, f := range v.Fields() {
			collectStrings(f, seen, out)
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			out.Write(v.Bytes())
			out.WriteByte(0)
			return
		}
		for i := range v.Len() {
			collectStrings(v.Index(i), seen, out)
		}
	case reflect.Array:
		for i := range v.Len() {
			collectStrings(v.Index(i), seen, out)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			collectStrings(iter.Key(), seen, out)
			collectStrings(iter.Value(), seen, out)
		}
	case reflect.String:
		out.WriteString(v.String())
		out.WriteByte(0)
	}
}

// respellTypes returns a copy of u with every type in rename spelled under its
// new name and every reference following it. The caller's unit is never
// touched: another target may emit it next.
func respellTypes(u *ir.Unit, rename map[string]string) *ir.Unit {
	respell := func(name string) string {
		if to, ok := rename[name]; ok {
			return to
		}
		return name
	}
	structs := map[*ir.Struct]*ir.Struct{}
	unions := map[*ir.Union]*ir.Union{}
	enums := map[*ir.Enum]*ir.Enum{}
	flags := map[*ir.Flags]*ir.Flags{}
	fields := map[*ir.Field]*ir.Field{}
	var copyStruct func(*ir.Struct) *ir.Struct
	var copyUnion func(*ir.Union) *ir.Union
	var copyField func(*ir.Field) *ir.Field
	var copyItems func([]ir.Item) []ir.Item
	copyEnum := func(e *ir.Enum) *ir.Enum {
		if e == nil {
			return nil
		}
		if c, ok := enums[e]; ok {
			return c
		}
		c := *e
		if to, ok := rename[e.Name]; ok {
			c.SchemaName, c.Name = e.DeclaredName(), to
		}
		enums[e] = &c
		return &c
	}
	copyFlags := func(f *ir.Flags) *ir.Flags {
		if f == nil {
			return nil
		}
		if c, ok := flags[f]; ok {
			return c
		}
		c := *f
		if to, ok := rename[f.Name]; ok {
			c.SchemaName, c.Name = f.DeclaredName(), to
		}
		flags[f] = &c
		return &c
	}
	copyStruct = func(st *ir.Struct) *ir.Struct {
		if st == nil {
			return nil
		}
		if c, ok := structs[st]; ok {
			return c
		}
		c := *st
		structs[st] = &c
		if to, ok := rename[st.Name]; ok && (u.Structs[st.Name] == st || u.Tables[st.Name] == st) {
			c.SchemaName, c.Name = st.DeclaredName(), to
		}
		// MapEntryOf keeps the holder's DECLARED name, as on the Go escape's
		// copy: ir.MapEntryProjectionName hashes it and finds the holder by it
		c.Fields = make([]*ir.Field, len(st.Fields))
		for i, f := range st.Fields {
			c.Fields[i] = copyField(f)
		}
		c.Items = copyItems(st.Items)
		return &c
	}
	copyUnion = func(un *ir.Union) *ir.Union {
		if un == nil {
			return nil
		}
		if c, ok := unions[un]; ok {
			return c
		}
		c := *un
		unions[un] = &c
		if to, ok := rename[un.Name]; ok {
			c.SchemaName, c.Name = un.DeclaredName(), to
		}
		c.Variants = make([]ir.UnionVariant, len(un.Variants))
		for i, v := range un.Variants {
			v.Ref = copyStruct(v.Ref)
			v.F = copyField(v.F)
			v.Type = respell(v.Type)
			c.Variants[i] = v
		}
		return &c
	}
	copyDecl := func(d ir.Decl) ir.Decl {
		switch d := d.(type) {
		case *ir.Struct:
			return copyStruct(d)
		case *ir.Union:
			return copyUnion(d)
		case *ir.Enum:
			return copyEnum(d)
		case *ir.Flags:
			return copyFlags(d)
		}
		return d
	}
	copyField = func(f *ir.Field) *ir.Field {
		if f == nil {
			return nil
		}
		if c, ok := fields[f]; ok {
			return c
		}
		c := *f
		fields[f] = &c
		if f.Type.Ref != nil {
			c.Type.Ref = copyDecl(f.Type.Ref)
		}
		if f.Type.Kind == ir.TNamed {
			c.Type.Name = respell(f.Type.Name)
		}
		c.KeyEnum = respell(f.KeyEnum)
		c.KeyEnumRef = copyEnum(f.KeyEnumRef)
		c.MapEntry = copyStruct(f.MapEntry)
		return &c
	}
	copyItems = func(items []ir.Item) []ir.Item {
		if items == nil {
			return nil
		}
		out := make([]ir.Item, len(items))
		for i, it := range items {
			switch it := it.(type) {
			case *ir.FieldItem:
				out[i] = &ir.FieldItem{F: copyField(it.F)}
			case *ir.Branch:
				b := *it
				b.Then = copyItems(it.Then)
				b.Else = copyItems(it.Else)
				out[i] = &b
			default:
				out[i] = it // const, reserved, align: no type name
			}
		}
		return out
	}

	c := *u
	c.Structs = make(map[string]*ir.Struct, len(u.Structs))
	for k, st := range u.Structs {
		c.Structs[respell(k)] = copyStruct(st)
	}
	c.Tables = make(map[string]*ir.Struct, len(u.Tables))
	for k, st := range u.Tables {
		c.Tables[respell(k)] = copyStruct(st)
	}
	c.Unions = make(map[string]*ir.Union, len(u.Unions))
	for k, un := range u.Unions {
		c.Unions[respell(k)] = copyUnion(un)
	}
	c.TableUnions = make(map[string]*ir.Union, len(u.TableUnions))
	for k, un := range u.TableUnions {
		c.TableUnions[respell(k)] = copyUnion(un)
	}
	c.Enums = make(map[string]*ir.Enum, len(u.Enums))
	for k, e := range u.Enums {
		c.Enums[respell(k)] = copyEnum(e)
	}
	c.Flags = make(map[string]*ir.Flags, len(u.Flags))
	for k, f := range u.Flags {
		c.Flags[respell(k)] = copyFlags(f)
	}
	c.DeclFile = make(map[string]string, len(u.DeclFile))
	for k, base := range u.DeclFile {
		c.DeclFile[respell(k)] = base
	}
	c.Files = make([]*ir.File, len(u.Files))
	for i, f := range u.Files {
		cf := *f
		cf.Decls = make([]ir.Decl, len(f.Decls))
		for j, d := range f.Decls {
			cf.Decls[j] = copyDecl(d)
		}
		cf.Tables = make([]*ir.Struct, len(f.Tables))
		for j, st := range f.Tables {
			cf.Tables[j] = copyStruct(st)
		}
		cf.TableUnions = make([]*ir.Union, len(f.TableUnions))
		for j, un := range f.TableUnions {
			cf.TableUnions[j] = copyUnion(un)
		}
		c.Files[i] = &cf
	}
	return &c
}
