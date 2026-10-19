// THE HAND-EDITABLE TEXT FORM RUNTIME (docs/SPEC-TABLES.md §16.9): the
// strict s-expression authoring form whose document retains its source bytes.
// A successful LoadTextForm returns a document and its typed table value; the
// rewrite of an unedited document is the input byte for byte. This runtime is
// the Squirrel leg of the Go textform oracle (internal/codegen/gotable/textform.go
// §16.9). It is incomplete: it refuses every non-trivial document cleanly
// rather than mis-resolve it.
local _M = vargv[0];
local _require = vargv[1];

// ---- the document record (source bytes, spans, edits) ----
// A document is a table with the retained source bytes and the token spans
// that let a leaf edit replace only its own bytes (docs/SPEC-TABLES.md §16.9.3).
local _Doc = class {
  bytes = null;
  ok = false;
};

// ---- the strict lexer (docs/SPEC-TABLES.md §16.9.1) ----
local _err = null;
local _tokens = null;

local _lex = function(text) {
  _tokens = [];
  _err = null;
  local n = text.len();
  local i = 0;
  while (i < n) {
    local c = text[i];
    if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
      // gap : SP, TAB, LF, CRLF
      i += 1;
      if (c == '\r' && i < n && text[i] == '\n') i += 1;
    } else if (c == ';') {
      // line comment: to LF, CRLF or EOF
      while (i < n && text[i] != '\n' && text[i] != '\r') i += 1;
    } else if (c == '(' || c == ')') {
      _tokens.push({ kind = "delim", s = i, e = i + 1, ch = format("%c", c) });
      i += 1;
    } else if (c == '"') {
      // string with JSON escapes (RFC-8259-string)
      local s = i;
      i += 1;
      local done = false;
      while (i < n) {
        if (text[i] == '"') { done = true; i += 1; break; }
        if (text[i] == '\\') {
          i += 2;
        } else {
          i += 1;
        }
      }
      if (!done) { _err = "text_unterminated"; return null; }
      _tokens.push({ kind = "string", s = s, e = i });
    } else {
      // atom : number, true/false/null, keyword, name
      local s = i;
      while (i < n && text[i] != ' ' && text[i] != '\t' &&
             text[i] != '\n' && text[i] != '\r' &&
             text[i] != '(' && text[i] != ')' && text[i] != ';' &&
             text[i] != '"') i += 1;
      _tokens.push({ kind = "atom", s = s, e = i });
    }
  }
  return _tokens;
};

// ---- the surface ----
// LoadTextForm(T, rootName, text, limits, report) returns {doc, value} or
// null, setting report.refused and report.name on refusal. Limits are the
// base limits (docs/SPEC-TABLES.md §16.9.4: text_limit).
_M.LoadTextForm <- function(T, rootName, text, limits, report) {
  if (!("refused" in report)) report.refused <- false;
  if (!("name" in report)) report.name <- null;
  if (!("offset" in report)) report.offset <- 0;
  if (!("path" in report)) report.path <- null;
  if (text == null || text.len() == 0) {
    report.refused = true;
    report.name = "text_syntax";
    report.offset = 0;
    return null;
  }
  local toks = _lex(text);
  if (_err != null) {
    report.refused = true;
    report.name = _err;
    report.offset = text.len();
    return null;
  }
  // the full resolver is not implemented in the Squirrel leg yet; the only
  // accepted document is the empty record, which is refused as unrepresentable
  // rather than mis-read.
  report.refused = true;
  report.name = "text_syntax";
  report.offset = 0;
  return null;
};

// SaveTextForm(T, rootName, value) is not meaningful without a document; this
// leg refuses by returning null.
_M.SaveTextForm <- function(T, rootName, value) {
  return null;
};

// EditTextForm(T, rootName, doc, edits, limits, report) refuses all structural
// and compound edits as text_edit_conflict (docs/SPEC-TABLES.md §16.9.3).
_M.EditTextForm <- function(T, rootName, doc, edits, limits, report) {
  if (!("refused" in report)) report.refused <- false;
  if (!("name" in report)) report.name <- null;
  report.refused = true;
  report.name = "text_edit_conflict";
  return null;
};
