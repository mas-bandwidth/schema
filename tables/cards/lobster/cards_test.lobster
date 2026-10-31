// cards_test.lobster — the Lobster card facade's unit tests
// (tables/cards/cards_test.go, parse_test.go, edit_test.go; the Lua twin is
// tables/cards/lua/cards_test.lua). Each test reads committed files and
// in-memory strings only, with no clock, no socket and no process: the driver
// and conformance targets that start processes are the functional tier.
//
// `make cards-lobster-test` stages this file and the facade into the generated
// Card unit's directory, where Lobster resolves their imports, and runs it as
//
//   lobster <unit>/cards_test.lobster -- <repo root>

import .cards

let arguments = command_line_arguments()
assert length(arguments) >= 1
let repo_root = arguments[length(arguments) - 1]

let CASES = "tables/cards/conformance/cases/"
let EXPECTED = "tables/cards/conformance/expected/"

var passed = 0
var failed = 0
let failures:[string] = []

def check(ok:bool, what:string):
    if not ok:
        push(failures, what)

def eq(got:string, want:string, what:string):
    check(got == want, what + ": got \"" + got + "\", want \"" + want + "\"")

// refused checks a refusal by its class
def refused(accepted:bool, err:cards.CardError?, kind:string, what:string):
    check(not accepted, what + ": accepted, want refusal " + kind)
    if err:
        eq(err.kind, kind, what + " class")
    else:
        check(false, what + ": no error, want refusal " + kind)

// test runs one test body and counts it by the failures it left
def test(name:string, body):
    let before = length(failures)
    body()
    if length(failures) == before:
        passed++
    else:
        failed++
        var i = before
        while i < length(failures):
            print("FAIL " + name + ": " + failures[i])
            i++

def read(path:string) -> string:
    let data = read_file(repo_root + "/" + path)
    assert data
    return data

def json_card(fields:string) -> string:
    return "{{\"id\":\"x\"" + fields + "}}"

def message_of(err:cards.CardError?) -> string:
    if err:
        return err.message
    return ""

test("sha256 matches the FIPS 180-4 vectors"):
    eq(cards.sha256hex(""), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "empty")
    eq(cards.sha256hex("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "abc")
    eq(cards.sha256hex("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
        "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1", "two blocks")

test("TestCardRoundTripsThePopulatedFixture"):
    let wire = read(CASES + "card-populated.wire")
    let json = read(CASES + "card-populated.json")
    let record, err = cards.Load(wire)
    check(record != nil, "Load refused the fixture: " + message_of(err))
    if record:
        check(not record.report.malformed, "malformed")
        let text = cards.ToJSON(record)
        eq(cards.or_empty(text), json, "ToJSON reproduces card-populated.json")
        eq(cards.Wire(record), wire, "Wire")
    let again, aerr = cards.FromJSON(json)
    check(again != nil, "FromJSON refused the fixture: " + message_of(aerr))
    if again:
        eq(cards.Wire(again), wire, "FromJSON reproduces card-populated.wire")

test("TestCardBoundsRefuseBeforePublishing"):
    let at = cards.FromJSON(json_card(",\"body\":\"" + repeat_string("y", cards.MaxBodyBytes) + "\""))
    check(at != nil, "exactly 64 KiB body was refused")
    let over, err = cards.FromJSON(json_card(",\"body\":\"" + repeat_string("y", cards.MaxBodyBytes + 1) + "\""))
    refused(over != nil, err, "body-bound 65536", "64 KiB + 1 body")
    check(find_string(message_of(err), "65537") >= 0 and find_string(message_of(err), "65536") >= 0, message_of(err))
    for([["paths", "{{\"s\":\"p\"}}"], ["_new", "{{\"s\":\"n\"}}"], ["shared", "{{\"s\":\"s\"}}"],
            ["needs", "{{\"s\":\"d\"}}"], ["consumers", "{{\"kind\":\"k\"}}"]]) c:
        let field = c[0]
        let element = c[1]
        let full = json_card(",\"" + field + "\":[" + repeat_string(element + ",", cards.MaxListEntries - 1) + element + "]")
        let ok = cards.FromJSON(full)
        check(ok != nil, "exactly 512 entries in " + field + " was refused")
        let past = json_card(",\"" + field + "\":[" + repeat_string(element + ",", cards.MaxListEntries) + element + "]")
        let r, e = cards.FromJSON(past)
        refused(r != nil, e, "list-bound " + field + " 512", "513 entries in " + field)
        check(find_string(message_of(e), "513") >= 0, message_of(e))

test("TestParseRenderMatchesTheCorpus"):
    let record, err = cards.Parse(read(CASES + "normalized-valid.md"))
    check(record != nil, "normalized-valid did not parse: " + message_of(err))
    if record:
        eq(cards.Wire(record), read(EXPECTED + "normalized-valid.wire"), "parsed wire")
    let none, nerr = cards.Parse(read(CASES + "rejected-legacy.md"))
    let want = read(EXPECTED + "rejected-legacy.refused")
    refused(none != nil, nerr, substring(want, 0, length(want) - 1), "rejected-legacy")
    for(["card-populated", "initial-populated", "consumers-populated", "empty-consumers"]) name:
        let r = cards.Load(read(CASES + name + ".wire"))
        check(r != nil, "Load " + name)
        if r:
            let text = cards.Render(r)
            eq(cards.or_empty(text), read(EXPECTED + name + ".txt"), "render " + name)
    // a brief that is not UTF-8, or carries a NUL, or has no RESULT line
    let b1, e1 = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\xff")
    refused(b1 != nil, e1, "malformed", "invalid UTF-8")
    let b2, e2 = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\x00")
    refused(b2 != nil, e2, "malformed", "NUL")
    let b3, e3 = cards.Parse("KIND: x\n")
    refused(b3 != nil, e3, "malformed", "no RESULT line")
    // a stranded key after prose names the key
    let b4, e4 = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\nprose\nKIND: late\n")
    refused(b4 != nil, e4, "stranded-key KIND", "stranded key")

test("TestUpgradeMatchesTheCorpus"):
    for(["initial-populated", "consumers-populated", "empty-consumers", "future-note"]) name:
        let out, err = cards.Upgrade(read(CASES + name + ".wire"))
        check(out != nil, name + ": " + message_of(err))
        eq(cards.or_empty(out), read(EXPECTED + name + ".wire"), "upgrade " + name)
    let out, err = cards.Upgrade(read(CASES + "truncated.wire"))
    refused(out != nil, err, "malformed", "truncated")
    // a retained field the caller's storage cannot hold is named and refused,
    // and the input is never mutated (tla/CardVersioning.tla: RefusalKeepsOriginal)
    let future = read(CASES + "future-note.wire")
    let lost, lerr, report = cards.upgrade_with(future, 0, 0)
    refused(lost != nil, lerr, "retain-lost", "zero retention capacity")
    check(report.retain_lost > 0, "the report counts the loss")
    eq(future, read(CASES + "future-note.wire"), "input preserved")

// the independent byte vector: each text is its length as eight little-endian
// bytes then its bytes, each list its count then its framed elements
test("TestTaskHashFramesLogicalFields"):
    let a = cards.FromJSON(json_card(",\"body\":\"a\",\"paths\":[{{\"s\":\"bc\"}}]"))
    let b = cards.FromJSON(json_card(",\"body\":\"ab\",\"paths\":[{{\"s\":\"c\"}}]"))
    check(a != nil and b != nil, "the two framing cards")
    if a:
        let h = cards.TaskHash(a)
        eq(cards.or_empty(h), "49d2070600dfd1bc43252553ebe0c9ac99c1374c16186f8d5ed876e7f8c089ae", "body a / paths [bc]")
    if b:
        let h = cards.TaskHash(b)
        eq(cards.or_empty(h), "4d76000071d4b7c8ce523ac264a5fbc8cd06b405ae03626ee9ce244492326f39", "body ab / paths [c]")
    for(["card-populated", "initial-populated", "empty-consumers"]) name:
        let record = cards.Load(read(CASES + name + ".wire"))
        check(record != nil, "Load " + name)
        if record:
            let h = cards.TaskHash(record)
            eq(cards.or_empty(h), read(EXPECTED + name + ".hex"), "TaskHash " + name)
    let lossy = cards.Load(read(CASES + "future-note.wire"))
    check(lossy != nil, "Load future-note")
    if lossy:
        let hex, err = cards.TaskHash(lossy)
        refused(hex != nil, err, "unknown-present", "a lossy record")

test("TestFieldEditLeavesTheTaskHashAlone"):
    let wire = read(CASES + "card-populated.wire")
    let record = cards.Load(wire)
    check(record != nil, "Load card-populated")
    if record:
        let before = cards.TaskHash(record)
        let body0 = cards.or_empty(cards.blob_of(record.root, "body"))
        check(cards.SetNeeds(record, ["a", "b"]) == nil, "SetNeeds")
        check(cards.SetTier(record, "heavy") == nil, "SetTier")
        check(cards.SetPriority(record, "blocker") == nil, "SetPriority")
        check(cards.SetColumn(record, "landed") == nil, "SetColumn")
        check(cards.SetHolder(record, "someone") == nil, "SetHolder")
        check(cards.Wire(record) != wire, "the edits changed the wire")
        let after = cards.TaskHash(record)
        eq(cards.or_empty(after), cards.or_empty(before), "needs, tier, priority, column and holder edits keep the hash")
        eq(cards.or_empty(cards.blob_of(record.root, "body")), body0, "the body stays byte-equal")
        check(cards.SetPaths(record, ["x/y"]) == nil, "SetPaths")
        let moved = cards.TaskHash(record)
        check(cards.or_empty(moved) != cards.or_empty(before), "a paths edit changes the hash")
        // every refusal leaves the wire byte-equal
        let kept = cards.Wire(record)
        refused(false, cards.SetTier(record, "ultra"), "malformed", "an undeclared tier")
        refused(false, cards.SetHolder(record, repeat_string("h", 33)), "malformed", "a holder past its bound")
        let many:[string] = []
        for(cards.MaxListEntries + 1):
            push(many, "p")
        refused(false, cards.SetPaths(record, many), "list-bound paths 512", "513 paths")
        eq(cards.Wire(record), kept, "the wire after the refusals")
    // a record carrying an unknown field is never rewritten
    let future = cards.Load(read(CASES + "future-note.wire"))
    check(future != nil, "Load future-note")
    if future:
        refused(false, cards.SetNeeds(future, ["a"]), "unknown-present", "an edit over an unknown field")

test("TestReadStrictRefusesWhatTheDeclarationCannotName"):
    let record = cards.ReadStrict(read(CASES + "card-populated.wire"))
    check(record != nil, "a clean card was refused")
    let none, err = cards.ReadStrict(read(CASES + "future-note.wire"))
    refused(none != nil, err, "unknown-present", "an unknown field")

test("TestViewMatchesTheCorpus"):
    eq(cards.View(), read(EXPECTED + "view.txt"), "view")

print("cards (lobster): {passed} passed, {failed} failed")
assert failed == 0
