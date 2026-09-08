package ctable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// The parallel family uses the same field emitters. Only named body/helper
// calls gain the retention context; primitive reads and writes stay unchanged.
// This pass operates on compiler-generated C identifiers, skipping comments
// and literals, and balances arguments before adding the context parameter.
func retainedCode(source string, names map[string]string, required ...string) string {
	hits := make(map[string]bool)
	result := retainedCodeWalk(source, names, hits)
	for _, name := range required {
		if !hits[name] {
			panic("missing generated retention anchor: " + name)
		}
	}
	return result
}

func retainedCodeWalk(source string, names map[string]string, hits map[string]bool) string {
	var out strings.Builder
	for i := 0; i < len(source); {
		if end := retainedLiteralEnd(source, i); end > i {
			out.WriteString(source[i:end])
			i = end
			continue
		}
		if !retainedIdent(source[i]) || source[i] >= '0' && source[i] <= '9' {
			out.WriteByte(source[i])
			i++
			continue
		}
		j := i + 1
		for j < len(source) && retainedIdent(source[j]) {
			j++
		}
		word := source[i:j]
		replacement, ok := names[word]
		open := j
		for open < len(source) && strings.ContainsRune(" \t\r\n", rune(source[open])) {
			open++
		}
		if !ok || open == len(source) || source[open] != '(' {
			out.WriteString(word)
			i = j
			continue
		}
		close, depth := open+1, 1
		for close < len(source) && depth > 0 {
			if end := retainedLiteralEnd(source, close); end > close {
				close = end
				continue
			}
			if source[close] == '(' {
				depth++
			}
			if source[close] == ')' {
				depth--
			}
			close++
		}
		if depth != 0 {
			panic("unbalanced generated retention call: " + word)
		}
		args := source[open+1 : close-1]
		hits[word] = true
		decl := strings.HasPrefix(strings.TrimSpace(args), "TableWriter") || strings.HasPrefix(strings.TrimSpace(args), "TableReader") || strings.HasPrefix(strings.TrimSpace(args), "TableMessageReader")
		out.WriteString(replacement)
		out.WriteString(source[j : open+1])
		out.WriteString(retainedCodeWalk(args, names, hits))
		if decl {
			out.WriteString(", TableRetainWalk retention")
		} else {
			out.WriteString(", retention")
		}
		out.WriteByte(')')
		i = close
		if decl {
			brace := i
			for brace < len(source) && strings.ContainsRune(" \t\r\n", rune(source[brace])) {
				brace++
			}
			if brace < len(source) && source[brace] == '{' {
				out.WriteString(source[i : brace+1])
				out.WriteString("\n    (void)retention;\n")
				i = brace + 1
			}
		}
	}
	return out.String()
}
func retainedIdent(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
func retainedLiteralEnd(s string, i int) int {
	if i+1 < len(s) && s[i:i+2] == "//" {
		if n := strings.IndexByte(s[i:], '\n'); n >= 0 {
			return i + n
		}
		return len(s)
	}
	if i+1 < len(s) && s[i:i+2] == "/*" {
		if n := strings.Index(s[i+2:], "*/"); n >= 0 {
			return i + 2 + n + 2
		}
		return len(s)
	}
	if s[i] != '\'' && s[i] != '"' {
		return i
	}
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if s[j] == quote {
			return j + 1
		}
	}
	return len(s)
}
func (g *tableGen) emitRetainBodies(members []*ir.Struct, unions []*ir.Union) {
	if !g.anyVariable {
		return
	}
	names := map[string]string{"table_writer_id": "table_retain_writer_id", "table_writer_probe": "table_retain_probe", "table_writer_rewind": "table_retain_rewind"}
	var required []string
	for _, st := range members {
		for _, verb := range []string{"save_body", "load_body", "load_message_body"} {
			required = append(required, g.api(st.Name, verb))
		}
	}
	for name := range ir.TableClosure(g.unit) {
		for _, verb := range []string{"save_body", "load_body", "load_message_body"} {
			fn := g.api(name, verb)
			names[fn] = fn + "_retain"
		}
	}
	vocabulary := ir.TableClosureVocabulary(g.unit)
	for _, file := range g.unit.Files {
		for _, d := range file.Decls {
			if un, ok := d.(*ir.Union); ok && vocabulary[un.Name] {
				g.retainUnionNames(names, un)
			}
		}
		for _, un := range file.TableUnions {
			g.retainUnionNames(names, un)
		}
	}
	for _, st := range members {
		for _, f := range st.Fields {
			fn := g.wireFieldHelper(st, f)
			names[fn] = fn + "_retain"
		}
	}
	previous := g.body.String()
	g.body.Reset()
	g.retain = true
	for _, st := range members {
		g.pf("static SCHEMA_UNUSED int %s(TableWriter *,const %s *);\n", g.api(st.Name, "save_body"), st.Name)
		g.pf("static SCHEMA_UNUSED int %s(TableReader *,%s *);\n", g.api(st.Name, "load_body"), st.Name)
	}
	g.emitUnionWireDeclarations(unions)
	g.emitMessageReadDeclarations(members, unions)
	for _, un := range unions {
		g.emitUnionWire(un)
		g.emitMessageUnionRead(un)
	}
	for _, st := range members {
		g.owner = st
		g.emitWireWrite(st)
		g.emitWireRead(st)
		g.emitMessageRead(st)
	}
	g.retain = false
	g.owner = nil
	code := g.body.String()
	g.body.Reset()
	g.body.WriteString(previous)
	g.body.WriteString(retainedCode(code, names, required...))
}
func (g *tableGen) retainUnionNames(names map[string]string, un *ir.Union) {
	for _, verb := range []string{"save", "load", "message_load"} {
		fn := g.unionWireName(un, verb)
		names[fn] = fn + "_retain"
	}
	for _, v := range un.Variants {
		fn := g.sym(un.Name, "wire_arm_"+v.Name)
		names[fn] = fn + "_retain"
	}
}
func retainHoldsBodies(f *ir.Field) bool {
	if f.IsMap() {
		return true
	}
	if f.Type.Pointer {
		return false
	}
	switch f.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}
func (g *tableGen) retainReadField(f *ir.Field, ordinal int, ind string) {
	if !g.retain {
		return
	}
	g.pf("%sretention=table_retain_step(body_keep,%d,0);\n", ind, ordinal)
	if retainHoldsBodies(f) && !f.IsMap() && !f.IsList() && f.Array == ir.ArrayNone {
		g.pf("%stable_retain_discard(body_keep,1,%d);\n", ind, ordinal)
	}
}
func (g *tableGen) retainReplace(f *ir.Field, ind string) {
	if !g.retain || !retainHoldsBodies(f) {
		return
	}
	// retention names this field. Its parent and ordinal identify the entire old
	// occurrence, including array elements absent from the new occurrence.
	g.pf("%s{TableRetainWalk parent_keep=retention;uint32_t ordinal=parent_keep.path.steps[--parent_keep.path.depth].ordinal;table_retain_discard(parent_keep,1,ordinal);}\n", ind)
}
func (g *tableGen) retainIndex(f *ir.Field, index, ind string) {
	if g.retain && retainHoldsBodies(f) {
		g.pf("%sretention=table_retain_index(retention,(uint32_t)(%s));\n", ind, index)
	}
}

func (g *tableGen) retainUnionDiscard() string {
	if g.retain {
		return "table_retain_discard(retention,0,0);"
	}
	return ""
}

// Unknown enum values, arms and keyed slots are excluded retention classes.
// Emit their event directly so whitespace changes cannot lose retain_lost.
func (g *tableGen) unknownEvent() string {
	if g.retain {
		return "r->report->unknown++;r->report->retain_lost += retention.retain != NULL;"
	}
	return "r->report->unknown++;"
}
