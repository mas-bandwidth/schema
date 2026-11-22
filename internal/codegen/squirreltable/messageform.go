// THE MESSAGE FORM (docs/SPEC-TABLES.md section 3.3): announcement and batch verbs.
package squirreltable

import (
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterSurface(Surface{Name: "message", Table: messageTable, Module: messageModule})
}

func messageModule(m *Module) {
	m.Runtime("message")
	ann := tablewire.Announce(m.Unit)
	m.Printf("// The announcement is the unit's compile-time form-1 file (section 3.3).\n")
	m.Printf("_M.TableMessageBatchMax <- 256;\n")
	m.Printf("_M.TableMessageRefBitsHere <- %d;\n", ir.TableMessageRefBits(len(tablewire.MessageVocabulary(m.Unit))))
	m.Printf("_M.TableMessageEntriesHere <- %d;\n", len(tablewire.MessageVocabulary(m.Unit)))
	m.Printf("_M.TableMessageIds <- [")
	for _, e := range tablewire.MessageVocabulary(m.Unit) {
		m.Printf("%s,", squirrel.HexLit(e.Id))
	}
	m.Printf("];\n")
	slots := tablewire.MessageVocabularySlots(m.Unit)
	tnames := make([]string, 0, len(m.Unit.Tables)+len(m.Unit.Structs))
	byName := map[string]*ir.Struct{}
	for name, st := range m.Unit.Tables {
		if st != nil && st.IsTable && st.FixedDeclared && !st.IsMapEntry() {
			tnames = append(tnames, name)
			byName[name] = st
		}
	}
	for name, st := range m.Unit.Structs {
		if st != nil && !st.IsMapEntry() {
			tnames = append(tnames, name)
			byName[name] = st
		}
	}
	sort.Strings(tnames)
	m.Printf("_M.TableMessageFieldSlots <- {")
	for _, name := range tnames {
		st := byName[name]
		m.Printf(" [%s] = [", squirrel.StringLit([]byte(st.Name)))
		for _, fld := range st.Fields {
			m.Printf("%d,", slots[ir.TableFieldEntry(fld).Key()])
		}
		m.Printf("],")
	}
	m.Printf(" };\n")
	unames := make([]string, 0, len(m.Unit.TableUnions))
	for name := range m.Unit.TableUnions {
		unames = append(unames, name)
	}
	sort.Strings(unames)
	m.Printf("_M.TableMessageArmSlots <- {")
	for _, name := range unames {
		un := m.Unit.TableUnions[name]
		m.Printf(" [%s] = [", squirrel.StringLit([]byte(un.Name)))
		for _, v := range un.Variants {
			m.Printf("%d,", slots[ir.TableArmEntry(v).Key()])
		}
		m.Printf("],")
	}
	m.Printf(" };\n")
	m.Printf("_M.TableMessageAnnouncement <- blob(%d);\n", len(ann))
	for i, b := range ann {
		m.Printf("_M.TableMessageAnnouncement[%d] = 0x%02x;\n", i, b)
	}
	// R8: the writer refuses a vocabulary past the readers' default entry maximum.
	tooLarge := len(ir.TableVocabulary(m.Unit)) > ir.TableVocabularyMaxEntries
	m.Printf("_M.AnnounceMeasure <- function() { if (%t) return -1; return _M.TableMessageAnnouncement.len(); };\n", tooLarge)
	m.Printf("_M.Announce <- function(out) {\n")
	m.Printf("  if (%t) return -1;\n", tooLarge)
	m.Printf("  if (out.len() < _M.TableMessageAnnouncement.len()) return -1;\n")
	m.Printf("  for (local i = 0; i < _M.TableMessageAnnouncement.len(); i++) out[i] = _M.TableMessageAnnouncement[i];\n")
	m.Printf("  return _M.TableMessageAnnouncement.len();\n};\n")
	m.Printf("_M.AnnounceRead <- function(v, bytes, report) { return _rt_message.AnnounceRead(_M, v, bytes, report); };\n\n")
}

// messageTable emits a FIXED table's batch verbs. The message form is fixed
// tables only (section 3.3): a variable table has no message verb at all.
func messageTable(m *Module, st *ir.Struct) {
	if !st.IsTable || !st.FixedDeclared {
		return
	}
	m.Runtime("message")
	m.Printf("// Message bodies use the announced vocabulary and one batch bitstream (section 3.3).\n")
	m.Printf("_M.%sMeasureMessages <- function(values, report) { return _rt_message.MeasureMessages(_M, %s, values, report); };\n", st.Name, squirrel.StringLit([]byte(st.Name)))
	m.Printf("_M.%sSaveMessages <- function(values, out, report) { return _rt_message.SaveMessages(_M, %s, values, out, report); };\n", st.Name, squirrel.StringLit([]byte(st.Name)))
	m.Printf("_M.%sLoadMessages <- function(storage, v, bytes, report) { return _rt_message.LoadMessages(_M, %s, storage, v, bytes, report); };\n\n", st.Name, squirrel.StringLit([]byte(st.Name)))
}
