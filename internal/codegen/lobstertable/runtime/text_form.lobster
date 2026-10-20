// text_form.lobster — the hand-editable text form (docs/SPEC-TABLES.md §16.9;
// docs/PORT-LOBSTER.md, THE CARD MAP). A first slice: the restricted
// s-expression grammar of §16.9.1, read into the load's Record, kept with its
// source bytes so a rewrite is the input (§16.9.3), and one leaf edit by the
// token's span. The typed mapping here covers bool, int, string/N, enum,
// nested by-value tables and bounded arrays of those; the rest of §16.9.2's
// kinds are named as not carried in RESULT.md.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_save
import .schema_tables

namespace schema_rt

// TNode is one parsed token (§16.9.1): a list, a string, a number, a keyword,
// or one of true/false/null, with its source span.
class TNode
class TextSpan
class TextDoc
class TextParser

class TNode:
    kind:string
    lo:int
    hi:int
    text:string = ""
    items:[TNode] = []

// TextSpan is one scalar token's source range: the path names the field and
// lo/hi its bytes in the document (§16.9.3).
class TextSpan:
    path:string
    lo:int
    hi:int

// TextDoc is a document AND its typed table value (§16.9.3): the retained
// bytes, the root record, the leaf spans, and the root table's name.
class TextDoc:
    text:string
    rec:Record
    spans:[TextSpan]
    root:string

class TextParser:
    s:string
    n:int
    i:int
    fail:string = ""
    spans:[TextSpan] = []

def t_set_fail(p:TextParser, name:string):
    if p.fail == "":
        p.fail = name

def t_gap(p:TextParser):
    while p.i < p.n:
        let c = p.s[p.i]
        if c == 32 or c == 9 or c == 10 or c == 13:
            p.i++
        elif c == 59:
            while p.i < p.n and p.s[p.i] != 10:
                p.i++
        else:
            return

def t_name_char(c:int) -> bool:
    return (c >= 65 and c <= 90) or (c >= 97 and c <= 122) or c == 95 or (c >= 48 and c <= 57) or c == 45 or c == 47 or c == 46

def t_string(p:TextParser) -> TNode?:
    let lo = p.i
    p.i++
    var out = ""
    while p.i < p.n:
        let c = p.s[p.i]
        if c == 34:
            p.i++
            return TNode { kind: "str", lo: lo, hi: p.i, text: out }
        if c == 92:
            p.i++
            if p.i >= p.n:
                t_set_fail(p, "text_unterminated")
                return nil
            let e = p.s[p.i]
            if e == 110:
                out += "\n"
            elif e == 116:
                out += "\t"
            elif e == 114:
                out += "\r"
            elif e == 34:
                out += "\""
            elif e == 92:
                out += "\\"
            else:
                t_set_fail(p, "text_encoding")
                return nil
            p.i++
        else:
            if c < 0x20:
                t_set_fail(p, "text_encoding")
                return nil
            out += substring(p.s, p.i, 1)
            p.i++
    t_set_fail(p, "text_unterminated")
    return nil

def t_parse_value(p:TextParser) -> TNode?:
    t_gap(p)
    if p.i >= p.n:
        t_set_fail(p, "text_unterminated")
        return nil
    let c = p.s[p.i]
    if c == 40:
        let lo = p.i
        p.i++
        let items:[TNode] = []
        while true:
            t_gap(p)
            if p.i >= p.n:
                t_set_fail(p, "text_unterminated")
                return nil
            if p.s[p.i] == 41:
                p.i++
                return TNode { kind: "list", lo: lo, hi: p.i, items: items }
            let v = t_parse_value(p)
            if not v:
                return nil
            push(items, v)
    if c == 34:
        return t_string(p)
    if c == 58:
        let lo = p.i
        p.i++
        let ns = p.i
        while p.i < p.n and t_name_char(p.s[p.i]):
            p.i++
        if p.i == ns:
            t_set_fail(p, "text_syntax")
            return nil
        return TNode { kind: "kw", lo: lo, hi: p.i, text: substring(p.s, ns, p.i - ns) }
    if t_name_char(c) or c == 45:
        let lo = p.i
        while p.i < p.n and t_name_char(p.s[p.i]):
            p.i++
        let tok = substring(p.s, lo, p.i - lo)
        if tok == "true" or tok == "false":
            return TNode { kind: "bool", lo: lo, hi: p.i, text: tok }
        if tok == "null":
            return TNode { kind: "null", lo: lo, hi: p.i, text: tok }
        return TNode { kind: "num", lo: lo, hi: p.i, text: tok }
    t_set_fail(p, "text_syntax")
    return nil

def t_int(tok:string) -> int:
    var i = 0
    var neg = false
    if i < length(tok) and tok[i] == 45:
        neg = true
        i++
    var v = 0
    while i < length(tok):
        v = v * 10 + (tok[i] - 48)
        i++
    return if neg: -v else: v

def t_field_of(d:TableDescriptor, key:string) -> FieldDescriptor?:
    for(d.fields) f:
        if f.json == key:
            return f
    return nil

def t_key_text(n:TNode) -> string:
    return n.text

def t_set_scalar(p:TextParser, sl:Slot, v:TNode, path:string) -> bool:
    let f = sl.fd
    let s = f.s
    if s == "bool":
        if v.kind != "bool":
            t_set_fail(p, "text_kind_mismatch")
            return false
        sl.cell.b = v.text == "true"
    elif s == "int":
        if v.kind != "num":
            t_set_fail(p, "text_kind_mismatch")
            return false
        sl.cell.i = t_int(v.text)
    elif s == "str" or s == "text" or s == "bytes":
        if v.kind != "str":
            t_set_fail(p, "text_kind_mismatch")
            return false
        sl.cell.s = v.text
        sl.count = length(v.text)
    elif s == "enum":
        if v.kind != "kw" and v.kind != "str":
            t_set_fail(p, "text_kind_mismatch")
            return false
        var found = false
        for(f.vs) ev:
            if ev.name == v.text:
                sl.cell.i = ev.id
                found = true
        if not found:
            t_set_fail(p, "text_unknown_variant")
            return false
    else:
        t_set_fail(p, "text_kind_mismatch")
        return false
    push(p.spans, TextSpan { path: path, lo: v.lo, hi: v.hi })
    return true

def t_fill(p:TextParser, n:TNode, d:TableDescriptor, path:string) -> Record?:
    if n.kind != "list":
        t_set_fail(p, "text_kind_mismatch")
        return nil
    let rec = new_record(d)
    var k = 0
    while k < length(n.items):
        if k + 1 >= length(n.items):
            t_set_fail(p, "text_syntax")
            return nil
        let key = t_key_text(n.items[k])
        if n.items[k].kind != "kw" and n.items[k].kind != "str":
            t_set_fail(p, "text_syntax")
            return nil
        let f = t_field_of(d, key)
        if not f:
            t_set_fail(p, "text_unknown_key")
            return nil
        let sl = rec.fields[f.o]
        let v = n.items[k + 1]
        let child = if path == "": key else: path + "." + key
        if f.s == "tab":
            let inner = t_fill(p, v, table_of(f.t), child)
            if not inner:
                return nil
            sl.cell.tab = inner
        elif f.s == "arr":
            if v.kind != "list":
                t_set_fail(p, "text_kind_mismatch")
                return nil
            let e = elem_of(f)
            sl.elems = []
            for(v.items) it:
                let tmp = Slot { fd: e, present: false, count: 0, cell: Value {}, elems: [], entries: [] }
                if not t_set_scalar(p, tmp, it, child):
                    return nil
                push(sl.elems, tmp.cell)
            if f.array == "fixed" and length(sl.elems) != f.bound:
                t_set_fail(p, "text_bound")
                return nil
            if f.array != "fixed" and length(sl.elems) > f.bound:
                t_set_fail(p, "text_bound")
                return nil
            sl.count = length(sl.elems)
        else:
            if not t_set_scalar(p, sl, v, child):
                return nil
        k += 2
    for(d.fields) f:
        if f.required:
            var seen = false
            for(n.items) it, j:
                if j % 2 == 0 and it.text == f.json:
                    seen = true
            if not seen:
                t_set_fail(p, "text_missing_required")
                return nil
    return rec

// text_load reads one hand-editable document (§16.9.1-§16.9.2): the document
// AND its typed Record, or nil with no usable pair (§16.9.4).
def text_load(root:string, text:string) -> TextDoc?:
    let d = lookup_table(root)
    if not d:
        return nil
    let p = TextParser { s: text, n: length(text), i: 0 }
    let node = t_parse_value(p)
    if not node or p.fail != "":
        return nil
    t_gap(p)
    if p.i != p.n:
        return nil
    let rec = t_fill(p, node, d, "")
    if not rec or p.fail != "":
        return nil
    return TextDoc { text: text, rec: rec, spans: p.spans, root: root }

// text_save writes a document back: the retained bytes (§16.9.3).
def text_save(doc:TextDoc) -> string:
    return doc.text

// text_edit replaces one existing scalar's token span and reads the result
// back; every other byte is the input's (§16.9.3).
def text_edit(doc:TextDoc, path:string, token:string) -> TextDoc?:
    for(doc.spans) sp:
        if sp.path == path:
            let out = substring(doc.text, 0, sp.lo) + token + substring(doc.text, sp.hi, length(doc.text) - sp.hi)
            return text_load(doc.root, out)
    return nil
