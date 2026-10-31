// cards.lobster — the Lobster card facade (tables/cards/cards.go, parse.go,
// render.go, edit.go, upgrade.go), the owning layer a customer imports.
//
// The generated table module (CardTable) and the runtime modules beside it
// (load, schema_save, json, retain_unknown, region_measure, descriptors;
// docs/PORT-LOBSTER.md) answer the wire and text forms over the runtime's
// record model; this facade is the thin owning layer: a CardRecord that keeps
// its own copy of the wire, checked writes that refuse before publishing, and
// the two customer ceilings the schema deliberately leaves unbounded
// (docs/SPEC-TABLES.md §2.5, §2.9). It is the Go leg's facade in Lobster's own
// spelling and answers the same nine surfaces byte for byte on the shared
// corpus (tla/CardHops.tla: ImplementationsAgree, HopOrderIrrelevant;
// tables/cards/conformance).
//
// THE ERROR IDIOM. A refusal is a CardError beside a nil answer, never an
// abort: its `kind` is one of the closed refusal classes
// tables/cards/conformance/FORMAT.md fixes (body-bound, list-bound,
// stranded-key, retain-lost, unknown-present, malformed), never free text, and
// its `message` says why. A read that completed answers its report beside the
// record, and a refusal after a read carries it as the error's report.
//
// THE IMPORT PATH. Lobster resolves `import .x` against this file's own
// directory, so the file is staged into the generated Card unit's directory
// (tables/cards/lobster/driver does it) and imported from there.
//
// A `*string` field (body, done, source, an extra's value) is the cell's blob,
// nil when null (docs/PORT-LOBSTER.md, THE LOAD). A string is a Lobster byte
// string, so a "well-formed" string is valid UTF-8 with no NUL (the runtime's
// text_valid, Go's utf8.Valid plus the NUL refusal), and every bound is a byte
// count.

import .CardTable
import .load
import .schema_save
import .json
import .retain_unknown
import .region_measure
import .descriptors

namespace cards

let ROOT = "Card"

// MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
// declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
// refuses a larger body before any bytes are published.
let MaxBodyBytes = 64 << 10

// MaxListEntries is the customer ceiling on each of a card's collection fields
// (paths/_new/shared/needs/extra/consumers), which the schema leaves unbounded
// (docs/SPEC-TABLES.md §2.9).
let MaxListEntries = 512

// MaxRetainBytes and MaxRetainIDs are the customer capacities of the retained
// byte store and the retained-id store; neither grows on demand
// (docs/SPEC-TABLES.md §6.6).
let MaxRetainBytes = 1 << 20
let MaxRetainIDs = 4096

// ---- errors -------------------------------------------------------------------

class CardError:
    message:string
    kind:string
    report:schema_rt.LoadReport? = nil

def refuse(message:string, kind:string) -> CardError:
    return CardError { message: message, kind: kind }

def refuse_with(message:string, kind:string, report:schema_rt.LoadReport) -> CardError:
    return CardError { message: message, kind: kind, report: report }

// ---- the record ---------------------------------------------------------------

// CardRecord owns the wire and the decoded Card. A Lobster string the facade
// keeps is never written in place, so no caller's buffer aliases the record's
// wire (docs/SPEC-TABLES.md §6.2).
class CardRecord:
    wire:string
    root:schema_rt.Record
    report:schema_rt.LoadReport

// Wire returns the record's wire bytes.
def Wire(r:CardRecord) -> string:
    return r.wire

// ---- the record model's fields ------------------------------------------------

def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)

def str_of(rec:schema_rt.Record, name:string) -> string:
    return slot(rec, name).cell.s

def set_str(rec:schema_rt.Record, name:string, v:string):
    let sl = slot(rec, name)
    sl.cell.s = v
    sl.count = length(v)

def blob_of(rec:schema_rt.Record, name:string) -> string?:
    return slot(rec, name).cell.blob

def set_blob(rec:schema_rt.Record, name:string, v:string?):
    slot(rec, name).cell.blob = v

def int_of(rec:schema_rt.Record, name:string) -> int:
    return slot(rec, name).cell.i

def set_int(rec:schema_rt.Record, name:string, v:int):
    slot(rec, name).cell.i = v

// enum_name is the variant name an enum field holds, or "" for None.
def enum_name(rec:schema_rt.Record, name:string) -> string:
    let sl = slot(rec, name)
    return schema_rt.variant_name(sl.fd, sl.cell.i)

// set_enum stores the variant named v (it is a declared variant: every caller
// checks it first).
def set_enum(rec:schema_rt.Record, name:string, v:string):
    let sl = slot(rec, name)
    for(sl.fd.vs) variant, i:
        if variant.name == v:
            sl.cell.i = i + 1
            return
    assert false

// rows is a collection field's element records in order.
def rows(rec:schema_rt.Record, name:string) -> [schema_rt.Record]:
    let sl = slot(rec, name)
    let out:[schema_rt.Record] = []
    var i = 0
    while i < sl.count and i < length(sl.elems):
        let t = sl.elems[i].tab
        assert t
        push(out, t)
        i++
    return out

// strs is a PathText or CardID list as its element strings.
def strs(rec:schema_rt.Record, name:string) -> [string]:
    let out:[string] = []
    for(rows(rec, name)) e:
        push(out, str_of(e, "s"))
    return out

def element_table(rec:schema_rt.Record, name:string) -> schema_rt.TableDescriptor:
    let e = slot(rec, name).fd.e
    assert e
    return schema_rt.table_of(e.t)

// set_rows replaces a collection field's elements.
def set_rows(rec:schema_rt.Record, name:string, elems:[schema_rt.Record]):
    let sl = slot(rec, name)
    let cells:[schema_rt.Value] = []
    for(elems) e:
        push(cells, schema_rt.Value { tab: e })
    sl.elems = cells
    sl.count = length(elems)

// set_strs replaces a PathText or CardID list with one element per string.
def set_strs(rec:schema_rt.Record, name:string, values:[string]):
    let d = element_table(rec, name)
    let elems:[schema_rt.Record] = []
    for(values) v:
        let e = schema_rt.new_record(d)
        set_str(e, "s", v)
        push(elems, e)
    set_rows(rec, name, elems)

// ---- UTF-8 and whitespace helpers -----------------------------------------------

def well_formed(s:string) -> bool:
    return schema_rt.text_valid(s, 0, length(s))

// Go's unicode.IsSpace set beyond ASCII, as UTF-8 byte sequences, for
// strings.TrimSpace.
let SPACE_SEQS = ["\xC2\x85", "\xC2\xA0", "\xE1\x9A\x80", "\xE2\x80\xA8", "\xE2\x80\xA9",
    "\xE2\x80\xAF", "\xE2\x81\x9F", "\xE3\x80\x80", "\xE2\x80\x80", "\xE2\x80\x81", "\xE2\x80\x82",
    "\xE2\x80\x83", "\xE2\x80\x84", "\xE2\x80\x85", "\xE2\x80\x86", "\xE2\x80\x87", "\xE2\x80\x88",
    "\xE2\x80\x89", "\xE2\x80\x8A"]

// space_at is the byte length of the space that starts at i, or 0.
def space_at(s:string, i:int) -> int:
    if i < 0 or i >= length(s):
        return 0
    let b = s[i]
    if b == 0x20 or (b >= 0x09 and b <= 0x0D):
        return 1
    if b < 0x80:
        return 0
    for(SPACE_SEQS) seq:
        if i + length(seq) <= length(s) and substring(s, i, length(seq)) == seq:
            return length(seq)
    return 0

// space_before is the byte length of the space that ends at j (inclusive), or 0.
def space_before(s:string, j:int) -> int:
    for([1, 2, 3]) n:
        let i = j - n + 1
        if i >= 0 and space_at(s, i) == n:
            return n
    return 0

// trim is strings.TrimSpace.
def trim(s:string) -> string:
    var i = 0
    var j = length(s) - 1
    while i <= j:
        let n = space_at(s, i)
        if n == 0:
            break
        i += n
    while j >= i:
        let n = space_before(s, j)
        if n == 0:
            break
        j -= n
    return substring(s, i, j - i + 1)

// or_empty is a nil string's empty spelling.
def or_empty(s:string?) -> string:
    if s:
        return s
    return ""

def has_crlf(s:string) -> bool:
    return find_string(s, "\r") >= 0 or find_string(s, "\n") >= 0

def is_letter(c:int) -> bool:
    return (c >= 'A' and c <= 'Z') or (c >= 'a' and c <= 'z')

def is_digit(c:int) -> bool:
    return c >= '0' and c <= '9'

// is_header_key is the admitted key grammar: one letter then letters, digits
// and hyphens (parse.go headerKeyRE).
def is_header_key(s:string) -> bool:
    if length(s) == 0 or not is_letter(s[0]):
        return false
    for(s) c:
        if not (is_letter(c) or is_digit(c) or c == '-'):
            return false
    return true

def is_hex(s:string) -> bool:
    if length(s) == 0:
        return false
    for(s) c:
        if not (is_digit(c) or (c >= 'a' and c <= 'f') or (c >= 'A' and c <= 'F')):
            return false
    return true

def contains(xs:[string], s:string) -> bool:
    for(xs) x:
        if x == s:
            return true
    return false

// split_commas splits on every comma, keeping empty segments.
def split_commas(v:string) -> [string]:
    let out:[string] = []
    var at = 0
    while true:
        let i = find_string(v, ",", at)
        if i < 0:
            push(out, substring(v, at, length(v) - at))
            return out
        push(out, substring(v, at, i - at))
        at = i + 1

// ---- the checked-write helper (tables/cards/cards.go checkCard) -----------------

def list_bound(name:string, n:int) -> CardError:
    return refuse("cards: {name} has {n} entries, past the {MaxListEntries}-entry ceiling (docs/SPEC-TABLES.md §2.9)",
        "list-bound " + name + " 512")

let RESERVED_HEADER_KEYS = ["RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW",
    "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE"]

def check_strings(root:schema_rt.Record, name:string) -> CardError?:
    let list = strs(root, name)
    if length(list) > MaxListEntries:
        return list_bound(name, length(list))
    for(list) s:
        if not well_formed(s):
            return refuse("cards: " + name + " has an element that is not well-formed UTF-8 without NUL", "malformed")
    return nil

// check_extras enforces the extras rules before any write publishes: the
// 512-entry ceiling, the admitted header key grammar, no CR or LF in a key or
// value, well-formed keys and values, and no key duplicating a mapped header
// name (HEADER MAPPING, docs/SPEC-TABLES.md §2.9).
def check_extras(root:schema_rt.Record) -> CardError?:
    let list = rows(root, "extra")
    if length(list) > MaxListEntries:
        return list_bound("extra", length(list))
    for(list) e:
        let key = str_of(e, "key")
        if not well_formed(key):
            return refuse("cards: extra has a key that is not well-formed UTF-8 without NUL", "malformed")
        if not is_header_key(key):
            return refuse("cards: extra key \"" + key + "\" is not an admitted header key", "malformed")
        if has_crlf(key):
            return refuse("cards: extra has a key with a CR or LF", "malformed")
        if contains(RESERVED_HEADER_KEYS, key):
            return refuse("cards: extra key \"" + key + "\" duplicates a mapped header key", "malformed")
        let value = blob_of(e, "value")
        if value:
            if not well_formed(value):
                return refuse("cards: extra has a value that is not well-formed UTF-8 without NUL", "malformed")
            if has_crlf(value):
                return refuse("cards: extra has a value with a CR or LF", "malformed")
    return nil

let CONSUMER_STRINGS = ["kind", "who", "route", "model", "outcome", "line"]

def check_consumers(root:schema_rt.Record) -> CardError?:
    let list = rows(root, "consumers")
    if length(list) > MaxListEntries:
        return list_bound("consumers", length(list))
    for(list) e:
        for(CONSUMER_STRINGS) name:
            if not well_formed(str_of(e, name)):
                return refuse("cards: consumers has a " + name + " that is not well-formed UTF-8 without NUL", "malformed")
    return nil

// check_card enforces the customer bounds and the well-formedness of the
// opaque body and every collection element before bytes are published
// (docs/SPEC-TABLES.md §2.5, §2.9).
def check_card(root:schema_rt.Record) -> CardError?:
    let body = blob_of(root, "body")
    if body:
        if length(body) > MaxBodyBytes:
            return refuse("cards: body is {length(body)} bytes, past the {MaxBodyBytes}-byte ceiling", "body-bound 65536")
        if not well_formed(body):
            return refuse("cards: body is not well-formed UTF-8 without NUL", "malformed")
    for(["paths", "_new", "shared", "needs"]) name:
        let err = check_strings(root, name)
        if err:
            return err
    let xerr = check_extras(root)
    if xerr:
        return xerr
    return check_consumers(root)

// save is the runtime's save behind the facade's one refusal.
def save(root:schema_rt.Record) -> string?, CardError?:
    let wire, why = schema_rt.save_reasoned(root)
    if why != "":
        return nil, refuse("cards: save refused the card: " + why, "malformed")
    return wire, nil

// ---- the owning facade (tables/cards/cards.go) -------------------------------------

// not_a_card is the measure's refusal (Go's CardLoadMeasure < 0): a wire whose
// framing cannot be opened or whose region the framing refuses.
def not_a_card(wire:string) -> bool:
    let n, whole, refused = schema_rt.load_measure(ROOT, wire)
    return refused or not whole or n < 0

// read_wire is the one tolerant read every facade read shares: the measure
// first (a wire that is not a card), then the load into a zeroed report.
def read_wire(wire:string) -> schema_rt.Record?, CardError?, schema_rt.LoadReport:
    let report = schema_rt.LoadReport {}
    if not_a_card(wire):
        return nil, refuse_with("cards: wire is not a card", "malformed", report), report
    let root = schema_rt.load_tolerant(ROOT, wire, report)
    if not root:
        return nil, refuse_with("cards: wire is malformed", "malformed", report), report
    return root, nil, report

// Load reads wire into a CardRecord (docs/SPEC-TABLES.md §6.2). Refusals and
// malformed wire are errors; tolerant events ride in the record's report. The
// ORIGINAL wire is kept, never rewritten, so an old input is preserved byte
// for byte.
def Load(wire:string) -> CardRecord?, CardError?:
    let root, err, report = read_wire(wire)
    if err:
        return nil, err
    assert root
    let cerr = check_card(root)
    if cerr:
        cerr.report = report
        return nil, cerr
    return CardRecord { wire: copy(wire), root: root, report: report }, nil

// FromJSON builds a CardRecord from JSON text (docs/SPEC-TABLES.md §16).
// Unknown, mismatched, clamped and malformed inputs are refused, as are the
// customer bounds and any body or element that is not well-formed UTF-8
// without NUL. Bytes are published only on complete success.
def FromJSON(text:string) -> CardRecord?, CardError?:
    let report = schema_rt.LoadReport {}
    let root = schema_rt.json_read(ROOT, text, report)
    if not root or report.malformed:
        return nil, refuse("cards: JSON is malformed", "malformed")
    if report.unknown > 0 or report.kind_mismatch > 0 or report.clamped > 0:
        return nil, refuse("cards: JSON has loss events (unknown={report.unknown} kindMismatch={report.kind_mismatch} clamped={report.clamped}): refused, never truncated",
            "malformed")
    let cerr = check_card(root)
    if cerr:
        return nil, cerr
    let wire, serr = save(root)
    if not wire:
        return nil, serr
    return CardRecord { wire: wire, root: root, report: report }, nil

// ToJSON writes the record's known-field JSON text. It is the known-field
// view, not a lossless archival rewrite (docs/SPEC-TABLES.md §16).
def ToJSON(record:CardRecord) -> string?, CardError?:
    let text, why = schema_rt.json_write(record.root)
    if why != "":
        return nil, refuse_with("cards: the card has no JSON spelling: " + why, "malformed", record.report)
    return text, nil

// ---- parse (tables/cards/parse.go) -------------------------------------------------

let STRANDED_KEYS = ["KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
    "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT"]

let TIERS = ["flash", "pro", "heavy", "frontier"]
let PRIORITIES = ["normal", "high", "critical", "blocker"]
let COLUMNS = ["waiting", "ready", "working", "review", "merging", "landed", "plan", "check"]

// A Line is one physical line: its content without the terminator (and
// without a CR before the LF), the offset just past its terminator, and its
// 1-based number.
class Line:
    content:string
    term_end:int
    num:int

// split_lines answers every physical line. A trailing LF does not invent a
// final empty line.
def split_lines(text:string) -> [Line]:
    let out:[Line] = []
    var i = 0
    var n = 1
    while i < length(text):
        let j = find_string(text, "\n", i)
        var line_end = length(text)
        var term_end = length(text)
        if j >= 0:
            line_end = j
            term_end = j + 1
        var content = substring(text, i, line_end - i)
        if length(content) > 0 and content[length(content) - 1] == '\r':
            content = substring(content, 0, length(content) - 1)
        push(out, Line { content: content, term_end: term_end, num: n })
        n++
        i = term_end
    return out

def is_regexp_space(c:int) -> bool:
    return c == ' ' or c == '\t' or c == '\n' or c == 12 or c == '\r'

// match_header recognizes a header key line: one letter then letters, digits
// and hyphens, a colon, optional ASCII whitespace and the rest as the value
// (parse.go headerLineRE). Prose, blank and any line with leading whitespace
// is not one: it answers the key "".
def match_header(content:string) -> string, string:
    let colon = find_string(content, ":")
    if colon <= 0:
        return "", ""
    let key = substring(content, 0, colon)
    if not is_header_key(key):
        return "", ""
    var v = colon + 1
    while v < length(content) and is_regexp_space(content[v]):
        v++
    return key, substring(content, v, length(content) - v)

def is_blank(content:string) -> bool:
    for(content) c:
        if not (c == ' ' or c == '\t' or c == '\r'):
            return false
    return true

def check_str_bound(name:string, value:string, bound:int) -> CardError?:
    if length(value) > bound:
        return refuse("cards: {name} is {length(value)} bytes, past the {bound}-byte bound", "malformed")
    return nil

// Parsed is the brief's header state while it is read.
class Parsed:
    id:string = ""
    sha:string = ""
    tier:string = ""
    repo:string = ""
    base:string = ""
    kind:string = ""
    priority:string = ""
    paths:[string] = []
    news:[string] = []
    shared:[string] = []
    needs:[string] = []
    test:string = ""
    done_when:string = ""
    source:string = ""
    deadline:int = 0
    extra_keys:[string] = []
    extra_values:[string] = []

// apply_result parses the RESULT value "id sha=<sha> tier: <tier>"
// (docs/SPEC-TABLES.md §2, §4).
def apply_result(p:Parsed, value:string) -> CardError?:
    let i = find_string_reverse(value, " tier:")
    if i < 0:
        return refuse("cards: RESULT line has no \" tier:\"", "malformed")
    let tier_str = trim(substring(value, i + 6, length(value) - i - 6))
    let rest = substring(value, 0, i)
    let j = find_string_reverse(rest, " sha=")
    if j < 0:
        return refuse("cards: RESULT line has no \" sha=\"", "malformed")
    let id_str = trim(substring(rest, 0, j))
    let sha_str = substring(rest, j + 5, length(rest) - j - 5)
    let err = check_str_bound("id", id_str, 64)
    if err:
        return err
    if not is_hex(sha_str) or (length(sha_str) != 12 and length(sha_str) != 40):
        return refuse("cards: RESULT sha \"" + sha_str + "\" is not 12 or 40 hex digits", "malformed")
    if not contains(TIERS, tier_str):
        return refuse("cards: RESULT tier: unknown tier \"" + tier_str + "\"", "malformed")
    p.id = id_str
    p.sha = sha_str
    p.tier = tier_str
    return nil

def bounded(name:string, value:string, bound:int, num:int) -> CardError?:
    let err = check_str_bound(name, value, bound)
    if err:
        return refuse("cards: {name} on line {num}: " + err.message, "malformed")
    return nil

// split_list splits a comma-separated list, trims elements, drops empty ones,
// and enforces the element bound and the entry ceiling (docs/SPEC-TABLES.md
// §2.9). A whole value of "-" or empty is an empty list; PATHS alone also
// takes the customer sentinel "none".
def split_list(value:string, bound:int, none_sentinel:bool, name:string, num:int) -> [string], CardError?:
    let v = trim(value)
    let out:[string] = []
    if v == "" or v == "-" or (none_sentinel and v == "none"):
        return out, nil
    for(split_commas(v)) raw:
        let part = trim(raw)
        if part != "":
            let err = check_str_bound(name + " element", part, bound)
            if err:
                return out, refuse("cards: {name} on line {num}: " + err.message, "malformed")
            push(out, part)
    if length(out) > MaxListEntries:
        return out, refuse("cards: {name} on line {num} has {length(out)} entries, past the {MaxListEntries}-entry ceiling",
            "malformed")
    return out, nil

// parse_deadline reads "finish within N minutes." (parse.go deadlineRE) with
// a checked conversion into deadline_minutes.
def parse_deadline(value:string, num:int) -> int, CardError?:
    let v = trim(value)
    let pre = "finish within "
    let post = " minutes."
    var digits = ""
    if length(v) > length(pre) + length(post) and substring(v, 0, length(pre)) == pre:
        if substring(v, length(v) - length(post), length(post)) == post:
            digits = substring(v, length(pre), length(v) - length(pre) - length(post))
    var all_digits = length(digits) > 0
    for(digits) c:
        if not is_digit(c):
            all_digits = false
    if not all_digits:
        return 0, refuse("cards: DEADLINE on line {num}: not of the form \"finish within N minutes.\"", "malformed")
    if length(digits) > 18:
        return 0, refuse("cards: DEADLINE on line {num}: minutes out of range", "malformed")
    let n, ok = string_to_int(digits, 10)
    if not ok or n > 100000:
        return 0, refuse("cards: DEADLINE on line {num}: minutes out of range", "malformed")
    return n, nil

def assign_mapped(p:Parsed, key:string, value:string, num:int) -> CardError?:
    if key == "REPO":
        let err = bounded("REPO", value, 96, num)
        if not err:
            p.repo = value
        return err
    if key == "BASE":
        let err = bounded("BASE", value, 96, num)
        if not err:
            p.base = value
        return err
    if key == "KIND":
        let err = bounded("KIND", value, 24, num)
        if not err:
            p.kind = value
        return err
    if key == "PRIORITY":
        let pr = trim(value)
        if not contains(PRIORITIES, pr):
            return refuse("cards: PRIORITY on line {num}: unknown priority \"" + pr + "\"", "malformed")
        p.priority = pr
        return nil
    if key == "PATHS":
        let l, err = split_list(value, 192, true, "PATHS", num)
        p.paths = l
        return err
    if key == "NEW":
        let l, err = split_list(value, 192, false, "NEW", num)
        p.news = l
        return err
    if key == "SHARED":
        let l, err = split_list(value, 192, false, "SHARED", num)
        p.shared = l
        return err
    if key == "DEPENDS-ON":
        let l, err = split_list(value, 64, false, "DEPENDS-ON", num)
        p.needs = l
        return err
    if key == "TEST":
        let err = bounded("TEST", value, 192, num)
        if not err:
            p.test = value
        return err
    if key == "DONE-WHEN":
        p.done_when = value
        return nil
    if key == "DEADLINE":
        let n, err = parse_deadline(value, num)
        if not err:
            p.deadline = n
        return err
    if key == "SOURCE":
        p.source = value
    return nil

// Seen is the line each mapped header key was first read on.
class Seen:
    keys:[string] = []
    lines:[int] = []

def seen_line(s:Seen, key:string) -> int:
    for(s.keys) k, i:
        if k == key:
            return s.lines[i]
    return 0

def duplicate(key:string, first:int, num:int) -> CardError:
    return refuse("cards: duplicate header key \"" + key + "\" on lines {first} and {num}", "malformed")

// apply_header dispatches one header line to its field, an agreeing tier, or
// an extra, refusing duplicates and CR/LF inside any single-line value.
def apply_header(p:Parsed, key:string, value:string, num:int, seen:Seen) -> CardError?:
    if has_crlf(value):
        return refuse("cards: " + key + " on line {num} has a CR or LF in its value", "malformed")
    if key == "tier":
        let first = seen_line(seen, "tier")
        if first > 0:
            return duplicate(key, first, num)
        push(seen.keys, "tier")
        push(seen.lines, num)
        let t = trim(value)
        if not contains(TIERS, t):
            return refuse("cards: tier on line {num}: unknown tier \"" + t + "\"", "malformed")
        if t != p.tier:
            return refuse("cards: tier on line {num} disagrees with RESULT", "malformed")
        return nil
    if key == "RESULT":
        return duplicate(key, seen_line(seen, "RESULT"), num)
    if contains(RESERVED_HEADER_KEYS, key):
        let first = seen_line(seen, key)
        if first > 0:
            return duplicate(key, first, num)
        push(seen.keys, key)
        push(seen.lines, num)
        return assign_mapped(p, key, value, num)
    let err = check_str_bound("extra key", key, 32)
    if err:
        return refuse("cards: line {num}: " + err.message, "malformed")
    push(p.extra_keys, key)
    push(p.extra_values, value)
    return nil

// build publishes the parsed card through the runtime's save and the checked
// write (docs/SPEC-TABLES.md §6.2). Bytes are published only on complete
// success.
def build(p:Parsed, body:string) -> CardRecord?, CardError?:
    let card = schema_rt.new_record(schema_rt.table_of(ROOT))
    set_str(card, "id", p.id)
    set_str(card, "sha", p.sha)
    set_enum(card, "tier", p.tier)
    set_str(card, "repo", p.repo)
    set_str(card, "_base", p.base)
    set_str(card, "kind", p.kind)
    if p.priority != "":
        set_enum(card, "priority", p.priority)
    set_strs(card, "paths", p.paths)
    set_strs(card, "_new", p.news)
    set_strs(card, "shared", p.shared)
    set_strs(card, "needs", p.needs)
    set_str(card, "test", p.test)
    if p.done_when != "":
        set_blob(card, "done", p.done_when)
    set_int(card, "deadline_minutes", p.deadline)
    if p.source != "":
        set_blob(card, "source", p.source)
    let kv = element_table(card, "extra")
    let extras:[schema_rt.Record] = []
    for(p.extra_keys) k, i:
        let e = schema_rt.new_record(kv)
        set_str(e, "key", k)
        set_blob(e, "value", p.extra_values[i])
        push(extras, e)
    set_rows(card, "extra", extras)
    if length(body) > 0:
        set_blob(card, "body", body)
    let cerr = check_card(card)
    if cerr:
        return nil, cerr
    let wire, serr = save(card)
    if not wire:
        return nil, serr
    return CardRecord { wire: wire, root: card, report: schema_rt.LoadReport {} }, nil

// Parse reads admitted brief text and publishes a typed CardRecord only on
// complete checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the
// exact suffix immediately after the last header line's terminator; header
// layout may canonicalize, body bytes never do.
def Parse(text:string) -> CardRecord?, CardError?:
    if not well_formed(text):
        return nil, refuse("cards: brief is not valid UTF-8 without NUL", "malformed")
    let lines = split_lines(text)
    if length(lines) == 0:
        return nil, refuse("cards: brief has no RESULT line", "malformed")
    let key, value = match_header(lines[0].content)
    if key != "RESULT":
        return nil, refuse("cards: line 1 is not a RESULT line", "malformed")
    let p = Parsed {}
    let err = apply_result(p, value)
    if err:
        return nil, err
    var last_header_end = lines[0].term_end
    let seen = Seen { keys: ["RESULT"], lines: [1] }
    var prose = false
    var k = 1
    while k < length(lines):
        let ln = lines[k]
        if not is_blank(ln.content):
            let hk, hv = match_header(ln.content)
            if hk == "":
                prose = true
            elif prose:
                if contains(STRANDED_KEYS, hk):
                    return nil, refuse("cards: stranded header key \"" + hk + "\" on line {ln.num}", "stranded-key " + hk)
            else:
                let aerr = apply_header(p, hk, hv, ln.num, seen)
                if aerr:
                    return nil, aerr
                last_header_end = ln.term_end
        k++
    return build(p, substring(text, last_header_end, length(text) - last_header_end))

// ---- render (tables/cards/render.go) --------------------------------------------------

def header_line(out:[string], key:string, value:string):
    push(out, key + ": " + value + "\n")

def render_card(root:schema_rt.Record) -> string?:
    let tier = enum_name(root, "tier")
    if not contains(TIERS, tier):
        return nil
    let out = ["RESULT: " + str_of(root, "id") + " sha=" + str_of(root, "sha") + " tier: " + tier + "\n"]
    if str_of(root, "repo") != "":
        header_line(out, "REPO", str_of(root, "repo"))
    if str_of(root, "_base") != "":
        header_line(out, "BASE", str_of(root, "_base"))
    if str_of(root, "kind") != "":
        header_line(out, "KIND", str_of(root, "kind"))
    let priority = enum_name(root, "priority")
    if int_of(root, "priority") != 0:
        if not contains(PRIORITIES, priority):
            return nil
        header_line(out, "PRIORITY", priority)
    for([["paths", "PATHS"], ["_new", "NEW"], ["shared", "SHARED"], ["needs", "DEPENDS-ON"]]) pair:
        let list = strs(root, pair[0])
        if length(list) > 0:
            header_line(out, pair[1], concat_string(list, ", "))
    if str_of(root, "test") != "":
        header_line(out, "TEST", str_of(root, "test"))
    let done = blob_of(root, "done")
    if done:
        header_line(out, "DONE-WHEN", done)
    let deadline = int_of(root, "deadline_minutes")
    if deadline != 0:
        push(out, "DEADLINE: finish within {deadline} minutes.\n")
    let source = blob_of(root, "source")
    if source:
        header_line(out, "SOURCE", source)
    for(rows(root, "extra")) e:
        let v = blob_of(e, "value")
        header_line(out, str_of(e, "key"), or_empty(v))
    let body = blob_of(root, "body")
    if body:
        push(out, body)
    return concat_string(out, "")

// Render reads the record's wire into a fresh load and answers its
// informational text (docs/SPEC-TABLES.md §6.2, §16).
def Render(record:CardRecord) -> string?, CardError?:
    let root, err, report = read_wire(record.wire)
    if err:
        return nil, err
    assert root
    let text = render_card(root)
    if not text:
        return nil, refuse_with("cards: the card has no text spelling", "malformed", report)
    return text, nil

// ---- upgrade and strict read (tables/cards/upgrade.go) --------------------------------

// loss_err names the report events that make a rewrite unsafe (§6.6); unknown,
// retained and widened alone are not loss (§4).
def loss_err(report:schema_rt.LoadReport) -> CardError?:
    if report.retain_lost > 0:
        return refuse_with("cards: upgrade: {report.retain_lost} retained field(s) lost, refusing to replace", "retain-lost", report)
    if report.kind_mismatch > 0:
        return refuse_with("cards: upgrade: {report.kind_mismatch} kind mismatch(es), refusing to replace", "malformed", report)
    if report.clamped > 0:
        return refuse_with("cards: upgrade: {report.clamped} clamped value(s), refusing to replace", "malformed", report)
    if report.malformed:
        return refuse_with("cards: upgrade: malformed wire, refusing to replace", "malformed", report)
    return nil

// upgrade_with is the retention seam Upgrade fixes the two customer
// capacities through; the refusal probes drive smaller caller-owned storage
// through the same path so a tiny capacity names retain-lost and answers nil
// while the input is preserved.
def upgrade_with(wire:string, retain_bytes:int, retain_ids:int) -> string?, CardError?, schema_rt.LoadReport:
    let report = schema_rt.LoadReport {}
    if not_a_card(wire):
        return nil, refuse_with("cards: upgrade: wire is not a card", "malformed", report), report
    let retain = schema_rt.Retain { capacity: retain_bytes, id_capacity: retain_ids }
    let root = schema_rt.load_retain(ROOT, wire, retain, report)
    // load_retain answers a fresh record where the tolerant read answered none
    if report.refused or report.missing_required > 0 or report.non_finite_float != "":
        return nil, refuse_with("cards: upgrade: wire is malformed", "malformed", report), report
    let cerr = check_card(root)
    if cerr:
        cerr.report = report
        return nil, cerr, report
    let saved = schema_rt.save_retain(root, retain, report)
    let lerr = loss_err(report)
    if lerr:
        return nil, lerr, report
    return saved, nil, report

// Upgrade rewrites wire through the retaining path so every field the current
// declaration cannot name is preserved at the end of its own body (§6.6). It
// answers nil and a named error when any value would be lost (retain-lost,
// kind mismatch, clamped, malformed) or the wire is refused; the input is
// never touched (tla/CardVersioning.tla: NoFieldLost, BodyUnchanged,
// RefusalKeepsOriginal, ShippedUpgradeSucceeds).
def Upgrade(wire:string) -> string?, CardError?, schema_rt.LoadReport:
    return upgrade_with(wire, MaxRetainBytes, MaxRetainIDs)

// ReadStrict reads wire and refuses any data the current declaration cannot
// support: unknown, kind mismatch, clamped, malformed and every load failure
// (docs/SPEC-TABLES.md §4).
def ReadStrict(wire:string) -> CardRecord?, CardError?:
    let record, err = Load(wire)
    if not record:
        return nil, err
    let report = record.report
    if report.unknown > 0:
        return nil, refuse_with("cards: strict: {report.unknown} unknown field(s), unsupported data", "unknown-present", report)
    if report.kind_mismatch > 0:
        return nil, refuse_with("cards: strict: {report.kind_mismatch} kind mismatch(es), unsupported data", "malformed", report)
    if report.clamped > 0:
        return nil, refuse_with("cards: strict: {report.clamped} clamped value(s), unsupported data", "malformed", report)
    if report.malformed:
        return nil, refuse_with("cards: strict: malformed wire, unsupported data", "malformed", report)
    return record, nil

// ---- the task hash (tables/cards/edit.go) -------------------------------------------

// SHA-256 (FIPS 180-4) in Lobster's 64-bit integers, every word masked to 32
// bits: Lobster has no digest builtin and the hash is part of the contract.
let SHA_K = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2]

let MASK32 = 0xffffffff

def rotr(x:int, n:int) -> int:
    return ((x >> n) | (x << (32 - n))) & MASK32

def sha256hex(message:string) -> string:
    var h0 = 0x6a09e667
    var h1 = 0xbb67ae85
    var h2 = 0x3c6ef372
    var h3 = 0xa54ff53a
    var h4 = 0x510e527f
    var h5 = 0x9b05688c
    var h6 = 0x1f83d9ab
    var h7 = 0x5be0cd19
    let bits = length(message) * 8
    let zeros = (55 - length(message)) % 64
    var padded = message + "\x80" + ensure_size("", if zeros < 0: zeros + 64 else: zeros, 0)
    var shift = 56
    while shift >= 0:
        let b = ensure_size("", 1, (bits >> shift) & 0xff)
        padded += b
        shift -= 8
    assert length(padded) % 64 == 0
    let w:[int] = []
    for(64):
        push(w, 0)
    var chunk = 0
    while chunk < length(padded):
        for(16) i:
            let at = chunk + i * 4
            w[i] = (padded[at] << 24) | (padded[at + 1] << 16) | (padded[at + 2] << 8) | padded[at + 3]
        var i = 16
        while i < 64:
            let s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >> 3)
            let s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >> 10)
            w[i] = (w[i - 16] + s0 + w[i - 7] + s1) & MASK32
            i++
        var a = h0
        var b = h1
        var c = h2
        var d = h3
        var e = h4
        var f = h5
        var g = h6
        var h = h7
        for(64) r:
            let S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
            let ch = (e & f) ^ ((~e & MASK32) & g)
            let t1 = (h + S1 + ch + SHA_K[r] + w[r]) & MASK32
            let S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
            let maj = (a & b) ^ (a & c) ^ (b & c)
            let t2 = (S0 + maj) & MASK32
            h = g
            g = f
            f = e
            e = (d + t1) & MASK32
            d = c
            c = b
            b = a
            a = (t1 + t2) & MASK32
        h0 = (h0 + a) & MASK32
        h1 = (h1 + b) & MASK32
        h2 = (h2 + c) & MASK32
        h3 = (h3 + d) & MASK32
        h4 = (h4 + e) & MASK32
        h5 = (h5 + f) & MASK32
        h6 = (h6 + g) & MASK32
        h7 = (h7 + h) & MASK32
        chunk += 64
    let words:[string] = []
    for([h0, h1, h2, h3, h4, h5, h6, h7]) x:
        push(words, number_to_string(x, 16, 8))
    return lowercase(concat_string(words, ""))

// refuse_lossy names the read events that make a record unsafe to hash or
// rewrite (docs/SPEC-TABLES.md §4, §6.6).
def refuse_lossy(report:schema_rt.LoadReport) -> CardError?:
    let lost = report.unknown > 0 or report.kind_mismatch > 0 or report.clamped > 0 or report.retain_lost > 0
    if lost or report.malformed:
        return refuse_with("cards: record has loss events (unknown={report.unknown} kindMismatch={report.kind_mismatch} clamped={report.clamped} retainLost={report.retain_lost} malformed={report.malformed}): refused, never hashed or rewritten",
            "unknown-present", report)
    return nil

// le64 is n as eight little-endian bytes.
def le64(n:int) -> string:
    let s = write_int64_le(ensure_size("", 8, 0), 0, n)
    return s

def frame(text:string) -> string:
    return le64(length(text)) + text

def frame_list(list:[string]) -> string:
    let parts = [le64(length(list))]
    for(list) s:
        push(parts, frame(s))
    return concat_string(parts, "")

// TaskHash answers the hex digest of a record's TASK: body, paths, new,
// shared, test and done in that order, each framed as an unsigned 64-bit
// little-endian byte length then its bytes, each list as a count then its
// length-prefixed elements; needs, tier, priority and machine fields are
// excluded (docs/SPEC-TABLES.md §5; tla/CardHops.tla: TaskHashStable). A
// malformed or lossy record is refused with an error, never hashed.
def TaskHash(record:CardRecord) -> string?, CardError?:
    let lossy = refuse_lossy(record.report)
    if lossy:
        return nil, lossy
    let root = record.root
    return sha256hex(frame(or_empty(blob_of(root, "body"))) + frame_list(strs(root, "paths")) +
        frame_list(strs(root, "_new")) + frame_list(strs(root, "shared")) + frame(str_of(root, "test")) +
        frame(or_empty(blob_of(root, "done")))), nil

// ---- the field edits (tables/cards/edit.go) ---------------------------------------------

def validate_strings(name:string, values:[string], bound:int) -> CardError?:
    for(values) s:
        if length(s) > bound:
            return refuse("cards: {name} element is {length(s)} bytes, past its {bound}-byte bound", "malformed")
        if not well_formed(s):
            return refuse("cards: " + name + " has an element that is not well-formed UTF-8 without NUL", "malformed")
    return nil

// edit is the shared atomic rewrite: check the current record is safe, reload
// it into a fresh independent decode, apply mutate to the root, validate the
// whole rebuilt card, pack it, and only on complete success swap the record's
// owned wire (docs/SPEC-TABLES.md §6.2). Any refusal leaves the original wire
// untouched (tla/CardHops.tla: EditDropsUnknown, BodyUnchanged).
def edit(record:CardRecord, mutate) -> CardError?:
    let lossy = refuse_lossy(record.report)
    if lossy:
        return lossy
    let report = schema_rt.LoadReport {}
    let root = schema_rt.load_tolerant(ROOT, record.wire, report)
    if not root:
        return refuse("cards: reload into the fresh decode failed", "malformed")
    let again = refuse_lossy(report)
    if again:
        return again
    mutate(root)
    let cerr = check_card(root)
    if cerr:
        return cerr
    let wire, serr = save(root)
    if not wire:
        return serr
    record.wire = wire
    record.root = root
    record.report = report
    return nil

def set_list(record:CardRecord, field:string, values:[string], bound:int) -> CardError?:
    let err = validate_strings(field, values, bound)
    if err:
        return err
    if length(values) > MaxListEntries:
        return list_bound(field, length(values))
    return edit(record) root:
        set_strs(root, field, values)

// SetNeeds replaces the card's DEPENDS-ON ids.
def SetNeeds(record:CardRecord, values:[string]) -> CardError?:
    return set_list(record, "needs", values, 64)

// SetPaths replaces the card's PATHS.
def SetPaths(record:CardRecord, values:[string]) -> CardError?:
    return set_list(record, "paths", values, 192)

def set_variant(record:CardRecord, field:string, allowed:[string], value:string) -> CardError?:
    if not contains(allowed, value):
        return refuse("cards: " + field + " " + value + " is not a declared variant (docs/SPEC-TABLES.md §8)", "malformed")
    return edit(record) root:
        set_enum(root, field, value)

// SetTier, SetPriority and SetColumn set one declared variant.
def SetTier(record:CardRecord, value:string) -> CardError?:
    return set_variant(record, "tier", TIERS, value)

def SetPriority(record:CardRecord, value:string) -> CardError?:
    return set_variant(record, "priority", PRIORITIES, value)

def SetColumn(record:CardRecord, value:string) -> CardError?:
    return set_variant(record, "column", COLUMNS, value)

// SetHolder sets the card's holder.
def SetHolder(record:CardRecord, holder:string) -> CardError?:
    let err = validate_strings("holder", [holder], 32)
    if err:
        return err
    return edit(record) root:
        set_str(root, "holder", holder)

// ---- the generated view (docs/SPEC-TABLES.md §8) -------------------------------------

// type_name is a field's schema element type, read off its descriptor: the
// shape, the wire kind and the named type.
def type_name(f:schema_rt.FieldDescriptor) -> string:
    let s = f.s
    if s == "str" or s == "sblob":
        return "string"
    if s == "bytes" or s == "bblob":
        return "bytes"
    if s == "enum" or s == "tab" or s == "ptr":
        return f.t
    if s == "bool":
        return "bool"
    if s == "float":
        return if f.f32: "float32" else: "float64"
    if s == "arr":
        let e = f.e
        assert e
        return type_name(e)
    if s == "int":
        let k = f.k
        if k >= 2 and k <= 5:
            return "int{8 << (k - 2)}"
        if k >= 6 and k <= 9:
            return "uint{8 << (k - 6)}"
    // a shape the Card unit does not declare: refused by name, never guessed
    return ["no view type for " + f.name + " shape " + s][1]

// View answers the generated view: every field of every table in the unit, in
// table-name order then declaration order, one line each (FORMAT.md "The
// view"). Every word comes from the unit view's descriptors.
def View() -> string:
    let tables = copy(schema_rt.unit_view().tables)
    // an insertion sort by name: the unit has a handful of tables
    var i = 1
    while i < length(tables):
        var j = i
        while j > 0 and tables[j].name < tables[j - 1].name:
            let t = tables[j]
            tables[j] = tables[j - 1]
            tables[j - 1] = t
            j--
        i++
    let out:[string] = []
    for(tables) t:
        let d = t.d
        assert d
        for(d.fields) f:
            let shape = if f.s == "arr": "list" elif f.s == "sblob" or f.s == "bblob": "blob" else: "value"
            let bound = if shape == "value" and f.bound > 0: string(f.bound) else: "-"
            push(out, t.name + " " + f.name + " " + type_name(f) + " " + shape + " " + bound + "\n")
    return concat_string(out, "")
