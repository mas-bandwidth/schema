// THE MESSAGE FORM (docs/SPEC-TABLES.md section 3.3): announcement and batch verbs.
package squirreltable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterSurface(Surface{Name: "message", Table: messageTable, Module: messageModule})
}

func messageModule(m *Module) {
	m.Runtime("message")
	ann := ir.TableAnnouncement(m.Unit)
	m.Printf("// The announcement is the unit's compile-time form-1 file (section 3.3).\n")
	m.Printf("_M.TableMessageBatchMax <- 256;\n")
	m.Printf("_M.TableMessageRefBitsHere <- %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(m.Unit))))
	m.Printf("_M.TableMessageEntriesHere <- %d;\n", len(ir.TableVocabulary(m.Unit)))
	m.Printf("_M.TableMessageIds <- [")
	for _, e := range ir.TableVocabulary(m.Unit) {
		m.Printf("%s,", squirrel.HexLit(e.Id))
	}
	m.Printf("];\n")
	m.Printf("_M.TableMessageAnnouncement <- blob(%d);\n", len(ann))
	for i, b := range ann {
		m.Printf("_M.TableMessageAnnouncement[%d] = 0x%02x;\n", i, b)
	}
	m.Printf("_M.AnnounceMeasure <- function() { return _M.TableMessageAnnouncement.len(); };\n")
	m.Printf("_M.Announce <- function(out) {\n")
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
