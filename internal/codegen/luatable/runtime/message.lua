-- THE MESSAGE FORM'S RUNTIME (docs/SPEC-TABLES.md §3.3), form byte 2: the
-- announcement reader and the batch verbs, against the module's own descriptor
-- table (the vocabulary is the unit's own closure, emitted by messageform.go as
-- _M.MessageVocabulary in projection order). The wire is the bitpacked batch:
-- form byte 2, an eight-bit count (M-1), then M bodies as one bit stream, then
-- zero pad to the byte boundary.
--
-- A body is a sequence of fields, each a REFERENCE at refBits followed by a
-- payload at its declared width; reference 0 is the body terminator. Elision
-- and the declared defaults are the variable form's (§3). Damage is terminal
-- for the batch.
local M = {}

M.announceRead = function(module, vocab, data, report)
  error("message.announceRead: not yet implemented")
end

M.loadMessages = function(module, root, vocab, data, report)
  error("message.loadMessages: not yet implemented")
end

M.measureMessages = function(module, root, vocab, values)
  error("message.measureMessages: not yet implemented")
end

M.saveMessages = function(module, root, vocab, values)
  error("message.saveMessages: not yet implemented")
end

return M
