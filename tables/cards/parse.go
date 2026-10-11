// Parse builds a Record from the customer's admitted brief text: the text
// grammar of this card's TEXT GRAMMAR paragraph over the declarations of
// docs/SPEC-TABLES.md §2. Text is informational, not machine state: Parse
// reads prose once and never repairs a malformed historical brief.
package cards

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// cardHeaderKeys is nova-sprint pkg/swarm/lintheader.go cardHeaderKeys: the
// exact case-sensitive key set this parser refuses once prose has begun
// (docs/SPEC-TABLES.md §2: a header line before the first prose line).
var cardHeaderKeys = map[string]bool{
	"KIND": true, "PATHS": true, "TEST": true, "LEGS": true, "SOURCE": true,
	"START": true, "STOP": true, "DEPENDS-ON": true, "PRIORITY": true,
	"NEW": true, "SHARED": true, "REPO": true, "BASE": true, "tier": true,
	"RESULT": true,
}

// mappedHeaderKeys are the header keys the RESULT id/sha/tier and this card's
// declared fields absorb; every other KEY-shaped line is an opaque extra
// (docs/SPEC-TABLES.md §2).
var mappedHeaderKeys = map[string]bool{
	"REPO": true, "BASE": true, "KIND": true, "PRIORITY": true,
	"PATHS": true, "NEW": true, "SHARED": true, "DEPENDS-ON": true,
	"TEST": true, "DONE-WHEN": true, "DEADLINE": true, "SOURCE": true,
	"tier": true,
}

// renderHeaderOrder is the fixed declaration order Render emits mapped keys in
// (the TEXT GRAMMAR paragraph of this card).
var renderHeaderOrder = []string{
	"REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW", "SHARED",
	"DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE",
}

type resultLine struct {
	id, sha, tier string
}

// header is one recognized header line: its key, its value and the original
// 1-based line it stood on.
type header struct {
	key, value string
	line       int
}

// Parse reads admitted brief text into a Record (docs/SPEC-TABLES.md §2). It
// builds, validates and serializes in separate temporary generated storage and
// publishes only on complete checked success: a refusal returns a nil Record.
func Parse(text []byte) (*Record, error) {
	if !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 {
		return nil, errors.New("cards: text is not well-formed UTF-8 without NUL")
	}
	res, resultEnd, err := parseResult(text)
	if err != nil {
		return nil, err
	}
	headers, bodyStart, bodyLine, err := scanHeaders(text, resultEnd)
	if err != nil {
		return nil, err
	}
	if err := checkDuplicates(headers); err != nil {
		return nil, err
	}
	if err := checkStranded(text, bodyStart, bodyLine); err != nil {
		return nil, err
	}
	body := text[bodyStart:]
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("cards: body is %d bytes, past the %d-byte ceiling", len(body), MaxBodyBytes)
	}
	if err := checkAgreeTier(res.tier, headers); err != nil {
		return nil, err
	}
	return buildFrom(res, headers, body)
}

// parseResult reads line 1 as `RESULT: <id> sha=<sha> tier: <tier>` and
// validates its bounds (the TEXT GRAMMAR paragraph; docs/SPEC-TABLES.md §2.5,
// §4.2).
func parseResult(text []byte) (resultLine, int, error) {
	line, end := nextLine(text, 0)
	if !bytes.HasPrefix(line, []byte("RESULT:")) {
		return resultLine{}, 0, errors.New("cards: line 1 is not a RESULT: line")
	}
	fields := strings.Fields(string(line))
	if len(fields) != 5 || fields[0] != "RESULT:" || fields[2][:4] != "sha=" || fields[3] != "tier:" {
		return resultLine{}, 0, errors.New("cards: RESULT line is not `RESULT: <id> sha=<sha> tier: <tier>`")
	}
	res := resultLine{id: fields[1], sha: fields[2][4:], tier: fields[4]}
	if res.id == "" || len(res.id) > 64 {
		return resultLine{}, 0, fmt.Errorf("cards: RESULT id is %d bytes, past its 64-byte bound", len(res.id))
	}
	if !isHexSha(res.sha) {
		return resultLine{}, 0, fmt.Errorf("cards: RESULT sha %q is not 12 or 40 hex digits", res.sha)
	}
	if !validTier(res.tier) {
		return resultLine{}, 0, fmt.Errorf("cards: RESULT tier %q is not a declared tier", res.tier)
	}
	return res, end, nil
}

// scanHeaders walks the lines after RESULT. Blank lines are structural and do
// not end the header; the first nonblank non-key line starts prose. It returns
// the header list in arrival order, the body's start offset (the exact suffix
// after the last actual header line's terminator, or the RESULT terminator when
// no header fields) and the 1-based line the body begins at
// (the TEXT GRAMMAR paragraph).
func scanHeaders(text []byte, pos int) ([]header, int, int, error) {
	var headers []header
	lastEnd := pos
	lineNo := 1
	for pos < len(text) {
		lineNo++
		line, end := nextLine(text, pos)
		if isBlank(line) {
			pos = end
			continue
		}
		if k, v, ok := splitKey(line); ok {
			headers = append(headers, header{key: k, value: v, line: lineNo})
			lastEnd = end
			pos = end
			continue
		}
		return headers, lastEnd, lineNo, nil
	}
	return headers, lastEnd, lineNo, nil
}

// checkDuplicates refuses a repeated header key with both lines rather than
// silently dropping one (the TEXT GRAMMAR paragraph).
func checkDuplicates(headers []header) error {
	seen := map[string]int{}
	for _, h := range headers {
		if first, ok := seen[h.key]; ok {
			return fmt.Errorf("cards: duplicate header key %s on lines %d and %d", h.key, first, h.line)
		}
		seen[h.key] = h.line
	}
	return nil
}

// checkStranded refuses the exact customer set cardHeaderKeys when it appears
// KEY-shaped after prose has begun, naming the original 1-based line and key.
// It never applies the open header grammar as a blanket body prohibition
// (the STRANDED KEYS paragraph; docs/SPEC-TABLES.md §2).
func checkStranded(text []byte, pos, lineNo int) error {
	for pos < len(text) {
		line, end := nextLine(text, pos)
		lineNo++
		if k, _, ok := splitKey(line); ok && cardHeaderKeys[k] {
			return fmt.Errorf("cards: stranded key %s on line %d appears after prose began", k, lineNo)
		}
		pos = end
	}
	return nil
}

// checkAgreeTier refuses a separate `tier` line that disagrees with RESULT's
// (the HEADER MAPPING paragraph).
func checkAgreeTier(resultTier string, headers []header) error {
	for _, h := range headers {
		if h.key == "tier" && h.value != resultTier {
			return fmt.Errorf("cards: tier line %d says %q, RESULT says %q", h.line, h.value, resultTier)
		}
	}
	return nil
}

// buildFrom maps headers into a generated Card through the arena-backed
// builder, checks it with the shared checked-write helper, then measures and
// saves into owned wire storage that is published whole or not at all
// (docs/SPEC-TABLES.md §2.5, §6.2).
func buildFrom(res resultLine, headers []header, body []byte) (*Record, error) {
	var b CardBuilder
	if !b.Init() {
		return nil, errors.New("cards: arena initialization failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, res.id)
	setStr(root.Sha[:], &root.ShaLength, res.sha)
	root.Tier = tierValue(res.tier)
	var extras []header
	for _, h := range headers {
		switch h.key {
		case "REPO":
			if len(h.value) > 96 {
				return nil, overBound("REPO", len(h.value), 96)
			}
			setStr(root.Repo[:], &root.RepoLength, h.value)
		case "BASE":
			if len(h.value) > 96 {
				return nil, overBound("BASE", len(h.value), 96)
			}
			setStr(root.Base[:], &root.BaseLength, h.value)
		case "KIND":
			if len(h.value) > 24 {
				return nil, overBound("KIND", len(h.value), 24)
			}
			setStr(root.Kind[:], &root.KindLength, h.value)
		case "PRIORITY":
			p, ok := priorityValue(h.value)
			if !ok {
				return nil, fmt.Errorf("cards: PRIORITY %q is not a declared priority", h.value)
			}
			root.Priority = p
		case "PATHS":
			if err := addPathList(&b, &root.Paths, h.value, true, CardPathsAdd); err != nil {
				return nil, err
			}
		case "NEW":
			if err := addPathList(&b, &root.New, h.value, false, CardNewAdd); err != nil {
				return nil, err
			}
		case "SHARED":
			if err := addPathList(&b, &root.Shared, h.value, false, CardSharedAdd); err != nil {
				return nil, err
			}
		case "DEPENDS-ON":
			if err := addIDList(&b, &root.Needs, h.value); err != nil {
				return nil, err
			}
		case "TEST":
			if len(h.value) > 192 {
				return nil, overBound("TEST", len(h.value), 192)
			}
			setStr(root.Test[:], &root.TestLength, h.value)
		case "DONE-WHEN":
			TableStringEmplace(&b.Main, &root.DoneWhen, []byte(h.value))
		case "DEADLINE":
			n, ok := parseDeadline(h.value)
			if !ok {
				return nil, fmt.Errorf("cards: DEADLINE %q is not `finish within N minutes.`", h.value)
			}
			root.DeadlineMinutes = n
		case "SOURCE":
			TableStringEmplace(&b.Main, &root.Source, []byte(h.value))
		case "tier":
			// already validated to agree with RESULT.
		default:
			extras = append(extras, h)
		}
	}
	if len(extras) > MaxListEntries {
		return nil, fmt.Errorf("cards: extra has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", len(extras), MaxListEntries)
	}
	for _, h := range extras {
		if !validKeyGrammar(h.key) || len(h.key) > 32 {
			return nil, fmt.Errorf("cards: extra key %q on line %d is not a bounded declared key", h.key, h.line)
		}
		e := CardExtraAdd(&b.Main, &root.Extra)
		if e == nil {
			return nil, errors.New("cards: extra append failed")
		}
		setStr(e.Key[:], &e.KeyLength, h.key)
		TableStringEmplace(&b.Main, &e.Value, []byte(h.value))
	}
	if len(body) > 0 {
		TableStringEmplace(&b.Main, &root.Body, body)
	}
	if err := checkCard(root, &b.Arena); err != nil {
		return nil, err
	}
	if !b.Lock() {
		return nil, errors.New("cards: packing failed")
	}
	packed := b.AsConst()
	size := CardMeasure(packed)
	if size < 0 {
		return nil, errors.New("cards: measure refused the built card")
	}
	wire := make([]byte, size)
	if CardSave(packed, wire) != size {
		return nil, errors.New("cards: save did not fill the measured wire")
	}
	region := make([]byte, len(b.Region()))
	copy(region, b.Region())
	own := (*Card)(unsafe.Pointer(&region[0]))
	return &Record{wire: wire, region: region, root: own}, nil
}

// addPathList fills a []PathText from a comma-separated value, trimming each
// element; `-`, an empty value and (for PATHS alone) `none` mean an empty list
// (the HEADER MAPPING paragraph; docs/SPEC-TABLES.md §2.9).
func addPathList(b *CardBuilder, list *TableList[PathTextRow], value string, paths bool,
	add func(*TableWorker, *TableList[PathTextRow]) *PathTextRow) error {
	for _, elem := range splitList(value, paths) {
		e := add(&b.Main, list)
		if e == nil {
			return errors.New("cards: paths append failed")
		}
		if len(elem) > 192 {
			return overBound("PATHS element", len(elem), 192)
		}
		setStr(e.S[:], &e.SLength, elem)
	}
	return nil
}

// addIDList fills []CardID from a comma-separated value (docs/SPEC-TABLES.md §2.9).
func addIDList(b *CardBuilder, list *TableList[CardIDRow], value string) error {
	for _, elem := range splitList(value, false) {
		e := CardNeedsAdd(&b.Main, list)
		if e == nil {
			return errors.New("cards: needs append failed")
		}
		if len(elem) > 64 {
			return overBound("DEPENDS-ON element", len(elem), 64)
		}
		setStr(e.S[:], &e.SLength, elem)
	}
	return nil
}

// splitList turns a comma-separated list into trimmed elements. A `-` or empty
// whole value, and (for PATHS alone) `none`, yield no elements (the HEADER
// MAPPING paragraph).
func splitList(value string, paths bool) []string {
	t := strings.TrimSpace(value)
	if t == "" || t == "-" || (paths && t == "none") {
		return nil
	}
	parts := strings.Split(t, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseDeadline converts `finish within N minutes.` into the declared
// deadline_minutes field (the HEADER MAPPING paragraph; docs/SPEC-TABLES.md §2.5).
func parseDeadline(value string) (int32, bool) {
	const prefix = "finish within "
	const suffix = " minutes."
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return 0, false
	}
	n, err := parseInt32Flexible(value[len(prefix) : len(value)-len(suffix)])
	if err != nil || n < 0 || n > 100000 {
		return 0, false
	}
	return n, true
}

// nextLine returns the bytes of the line at pos without its LF or CRLF
// terminator, and the offset immediately after the terminator (the TEXT
// GRAMMAR paragraph).
func nextLine(text []byte, pos int) ([]byte, int) {
	i := bytes.IndexByte(text[pos:], '\n')
	if i < 0 {
		return text[pos:], len(text)
	}
	line := text[pos : pos+i]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, pos + i + 1
}

// isBlank reports a whitespace-only line, which may separate header lines
// without ending the header (the TEXT GRAMMAR paragraph).
func isBlank(line []byte) bool {
	for _, c := range line {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

// splitKey applies `^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$` without a terminator;
// leading whitespace is not a key (the TEXT GRAMMAR paragraph).
func splitKey(line []byte) (string, string, bool) {
	if len(line) == 0 || !isKeyStart(line[0]) {
		return "", "", false
	}
	i := 0
	for i < len(line) && line[i] != ':' {
		if !isKeyByte(line[i]) {
			return "", "", false
		}
		i++
	}
	if i >= len(line) { // no colon
		return "", "", false
	}
	key := string(line[:i])
	if !validKeyGrammar(key) {
		return "", "", false
	}
	value := line[i+1:]
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\t') {
		value = value[1:]
	}
	return key, string(value), true
}

// validKeyGrammar is `[A-Za-z][A-Za-z0-9-]*` (the TEXT GRAMMAR paragraph).
func validKeyGrammar(key string) bool {
	if key == "" || !isKeyStart(key[0]) {
		return false
	}
	for i := 1; i < len(key); i++ {
		if !isKeyByte(key[i]) {
			return false
		}
	}
	return true
}

func isKeyStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isKeyByte(c byte) bool {
	return isKeyStart(c) || (c >= '0' && c <= '9') || c == '-'
}

// isHexSha accepts exactly 12 or 40 hex digits (the TEXT GRAMMAR paragraph).
func isHexSha(s string) bool {
	if len(s) != 12 && len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// validTier reports a declared tier name (docs/SPEC-TABLES.md §4.2).
func validTier(s string) bool {
	switch s {
	case "flash", "pro", "heavy", "frontier":
		return true
	}
	return false
}

func tierValue(s string) Tier {
	switch s {
	case "flash":
		return Tierflash
	case "pro":
		return Tierpro
	case "heavy":
		return Tierheavy
	case "frontier":
		return Tierfrontier
	}
	return TierNone
}

func priorityValue(s string) (Priority, bool) {
	switch s {
	case "normal":
		return Prioritynormal, true
	case "high":
		return Priorityhigh, true
	case "critical":
		return Prioritycritical, true
	case "blocker":
		return Priorityblocker, true
	}
	return PriorityNone, false
}

func overBound(what string, got, want int) error {
	return fmt.Errorf("cards: %s is %d bytes, past its %d-byte bound", what, got, want)
}

func parseInt32Flexible(s string) (int32, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int64(s[i]-'0')
		if n > 1<<31 {
			return 0, errors.New("overflow")
		}
	}
	return int32(n), nil
}
