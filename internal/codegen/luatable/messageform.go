// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, in Lua: the
// ANNOUNCEMENT half. The announcement is a COMPILE-TIME CONSTANT of the unit,
// so this card emits it as a byte literal beside the entry-count and
// reference-width facts, and the reader and the batch verbs live in
// runtime/message.lua, emitted beside the module by the embed glob in
// luatable.go. The batch shape mirrors the JavaScript leg
// (internal/codegen/jstable/messageform.go, messagebatch.go).
package luatable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	messageFormByte     = 2
	messageBatchMax     = 256
	messageVersionID    = uint64(0xFFFFFFFFFFFFFFFE)
	messageVocabularyID = uint64(0xFFFFFFFFFFFFFFFD)
	messageNodeID       = uint64(0xFFFFFFFFFFFFFFFF)
)

func init() {
	RegisterPass(emitMessageForm)
}

// messageRoots is every fixed table of a unit the message form is emitted for:
// the fixed form's roots (docs/SPEC-TABLES.md §3.3: the message form is a
// FIXED table's; a plain table is refused by name).
func messageRoots(u *ir.Unit) []*ir.Struct {
	return fixedRoots(u)
}

// emitMessageForm emits the announcement constants and the message-form entry
// points for every fixed root (docs/SPEC-TABLES.md §3.3).
func emitMessageForm(m *Module) error {
	u := m.Unit
	roots := messageRoots(u)
	vocab := ir.TableVocabulary(u)
	if len(roots) == 0 {
		return nil
	}
	announce := ir.TableAnnouncement(u)
	refBits := ir.TableMessageRefBits(len(vocab))
	m.Printf("-- the message form (docs/SPEC-TABLES.md §3.3), byte %d: an announced\n", messageFormByte)
	m.Printf("-- batch of bitpacked fixed-table bodies; the runtime is runtime/message.lua\n")
	m.Printf("local M = require(\"message\")\n\n")
	m.Printf("_M.MessageBatchMax = %d\n", messageBatchMax)
	m.Printf("_M.MessageRefBits = %d\n", refBits)
	m.Printf("_M.MessageEntries = %d\n", len(vocab))
	m.Printf("_M.Announcement = %s\n\n", lua.ByteString(announce))
	m.Printf("_M.MessageVersionId = %s\n", idLit(messageVersionID))
	m.Printf("_M.MessageVocabularyId = %s\n", idLit(messageVocabularyID))
	m.Printf("_M.MessageNodeId = %s\n", idLit(messageNodeID))
	m.Printf("\n")
	// The resolved vocabulary the reader fills once per connection: a Lua
	// table of the unit's OWN entries in projection order, the exact capacity a
	// receiver that talks only to peers of its own schema declares.
	m.Printf("_M.MessageVocabulary = {}\n")
	for i, e := range vocab {
		m.Printf("_M.MessageVocabulary[%d] = { id = %s, kind = %d", i+1, idLit(e.Id), e.Kind)
		s := e.Shape
		if s.Packing != 0 {
			m.Printf(", packing = %d", s.Packing)
		}
		if s.Bits != 0 {
			m.Printf(", bits = %d", s.Bits)
		}
		if s.Max != 0 {
			m.Printf(", max = %d", s.Max)
		}
		if s.Min != 0 {
			m.Printf(", min = %d", s.Min)
		}
		if s.Elem != 0 {
			m.Printf(", elem = %d", s.Elem)
		}
		m.Printf(" }\n")
	}
	m.Printf("\n_M.MessageRoots = {}\n")
	for _, st := range roots {
		m.Printf("_M.MessageRoots[%q] = true\n", st.Name)
	}
	// THE RECEIVER BOUNDS ARE THE CALLER'S (docs/SPEC-TABLES.md §3.3): the
	// vocabulary field's byte bound and the entry capacity. Both are optional;
	// nil takes the conforming defaults, and the runner of the shared hostile
	// corpus states the numbers its manifest records.
	m.Printf("\nfunction _M.AnnounceRead(vocab, data, report, maxBytes, maxEntries)\n")
	m.Printf("  return M.announceRead(_M, vocab, data, report, maxBytes, maxEntries)\n")
	m.Printf("end\n\n")
	// THE PLAIN NAME IS THE STRICT READ; THE TOLERANT READ IS NAMED
	// (docs/SPEC-TABLES.md §6.5): LoadMessages passes the strict decision into
	// runtime/message.lua, and LoadMessagesTolerant keeps BASE's answer.
	m.Printf("function _M.LoadMessagesTolerant(root, vocab, data, report)\n")
	m.Printf("  return M.loadMessages(_M, root, vocab, data, report, false)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.LoadMessages(root, vocab, data, report)\n")
	m.Printf("  return M.loadMessages(_M, root, vocab, data, report, true)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.MeasureMessages(root, vocab, values)\n")
	m.Printf("  return M.measureMessages(_M, root, vocab, values)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.SaveMessages(root, vocab, values)\n")
	m.Printf("  return M.saveMessages(_M, root, vocab, values)\n")
	m.Printf("end\n\n")
	return nil
}
