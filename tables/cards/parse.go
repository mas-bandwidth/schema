// parse.go — the admitted brief text, parsed once into a typed Record.
//
// Parse reads the customer's readable brief prose and turns the header lines
// into the c01 Card fields, then serializes the result through the generated
// builder and checked write (docs/SPEC-TABLES.md §2, §6.2). The body is the
// exact suffix after the last header line's terminator, preserved byte for
// byte; text and JSON are informational views, never machine-state storage.
package cards

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// headerLineRE is the admitted header grammar: a key of one letter then
// letters/digits/hyphens, a colon, optional whitespace, and the rest as the
// value. Anchored at the start, so leading whitespace is not a key, and applied
// to a line with no LF/CRLF terminator.
var headerLineRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$`)

// headerKeyRE is the admitted key grammar alone: one letter then
// letters/digits/hyphens, the same key rule headerLineRE applies. The shared
// checked-write validation uses it to refuse an extra whose key Render could
// not spell as a header line (TEXT GRAMMAR).
var headerKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

// deadlineRE is the customer spelling DEADLINE accepts: "finish within N
// minutes." with a checked conversion into deadline_minutes.
var deadlineRE = regexp.MustCompile(`^finish within ([0-9]+) minutes\.$`)

// strandedKeys is the customer's exact case-sensitive set refused once prose
// has begun (nova-sprint pkg/swarm/lintheader.go cardHeaderKeys). Other
// KEY-shaped body lines, such as NOTE:, are opaque and preserved.
var strandedKeys = map[string]bool{
	"KIND": true, "PATHS": true, "TEST": true, "LEGS": true, "SOURCE": true,
	"START": true, "STOP": true, "DEPENDS-ON": true, "PRIORITY": true, "NEW": true,
	"SHARED": true, "REPO": true, "BASE": true, "tier": true, "RESULT": true,
}

// reservedHeaderKeys is the mapped header vocabulary, including RESULT and the
// agreeing tier line. An extra must not duplicate one of these names.
var reservedHeaderKeys = map[string]bool{
	"RESULT": true, "tier": true,
	"REPO": true, "BASE": true, "KIND": true, "PRIORITY": true,
	"PATHS": true, "NEW": true, "SHARED": true, "DEPENDS-ON": true,
	"TEST": true, "DONE-WHEN": true, "DEADLINE": true, "SOURCE": true,
}

// kv is one extra header entry, kept in arrival order.
type kv struct {
	key   string
	value string
}

// parsedCard is the intermediate, temporary result of reading the header lines
// before any generated storage exists.
type parsedCard struct {
	id       string
	sha      string
	tier     Tier
	repo     string
	base     string
	kind     string
	priority Priority
	stream   string
	paths    []string
	news     []string
	shared   []string
	needs    []string
	test     string
	doneWhen string
	source   string
	deadline int32
	extras   []kv
}

// lineRef is one physical line: its content without the LF/CRLF terminator and
// the byte offset just past its terminator (where the body could begin).
type lineRef struct {
	content []byte
	termEnd int
	num     int
}

// Parse reads admitted brief text and publishes a typed Record only on complete
// checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the exact suffix
// immediately after the last header line's terminator; header layout may
// canonicalize, body bytes never do.
func Parse(text []byte) (*Record, error) {
	if !utf8.Valid(text) {
		return nil, errors.New("cards: brief is not valid UTF-8")
	}
	if bytes.IndexByte(text, 0) >= 0 {
		return nil, errors.New("cards: brief contains a NUL byte")
	}

	lines := splitLines(text)
	if len(lines) == 0 {
		return nil, errors.New("cards: brief has no RESULT line")
	}

	key, value, ok := matchHeader(lines[0].content)
	if !ok || key != "RESULT" {
		return nil, errors.New("cards: line 1 is not a RESULT line")
	}
	p := &parsedCard{}
	if err := p.applyResult(value); err != nil {
		return nil, err
	}

	lastHeaderEnd := lines[0].termEnd
	seen := map[string]int{"RESULT": 1}
	prose := false
	for _, ln := range lines[1:] {
		if isBlank(ln.content) {
			continue
		}
		k, v, ok := matchHeader(ln.content)
		if !ok {
			prose = true
			continue
		}
		if prose {
			if strandedKeys[k] {
				return nil, fmt.Errorf("cards: stranded header key %q on line %d", k, ln.num)
			}
			continue
		}
		if err := applyHeader(p, k, v, ln.num, seen); err != nil {
			return nil, err
		}
		lastHeaderEnd = ln.termEnd
	}

	body := text[lastHeaderEnd:]
	return p.build(body)
}

// splitLines returns every physical line with its content and its terminator
// end offset. A trailing LF does not invent a final empty line.
func splitLines(text []byte) []lineRef {
	var refs []lineRef
	i, n := 0, 1
	for i < len(text) {
		start := i
		j := bytes.IndexByte(text[start:], '\n')
		lineEnd, termEnd := len(text), len(text)
		if j >= 0 {
			lineEnd = start + j
			termEnd = lineEnd + 1
		}
		content := text[start:lineEnd]
		if len(content) > 0 && content[len(content)-1] == '\r' {
			content = content[:len(content)-1]
		}
		refs = append(refs, lineRef{content: content, termEnd: termEnd, num: n})
		n++
		i = termEnd
	}
	return refs
}

// matchHeader recognizes a header key line; ok is false for prose, blank
// (handled by the caller) or any line with leading whitespace.
func matchHeader(line []byte) (key, value string, ok bool) {
	m := headerLineRE.FindSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return string(m[1]), string(m[2]), true
}

// applyResult parses the RESULT value "id sha=<sha> tier: <tier>" and validates
// each part (docs/SPEC-TABLES.md §2, §4).
func (p *parsedCard) applyResult(value string) error {
	i := strings.LastIndex(value, " tier:")
	if i < 0 {
		return errors.New("cards: RESULT line has no \" tier:\"")
	}
	tierStr := strings.TrimSpace(value[i+len(" tier:"):])
	rest := value[:i]
	j := strings.LastIndex(rest, " sha=")
	if j < 0 {
		return errors.New("cards: RESULT line has no \" sha=\"")
	}
	idStr := strings.TrimSpace(rest[:j])
	shaStr := rest[j+len(" sha="):]
	if err := checkStrBound("id", idStr, 64); err != nil {
		return err
	}
	if !isHex(shaStr) || (len(shaStr) != 12 && len(shaStr) != 40) {
		return fmt.Errorf("cards: RESULT sha %q is not 12 or 40 hex digits", shaStr)
	}
	tier, err := parseTier(tierStr)
	if err != nil {
		return fmt.Errorf("cards: RESULT tier: %v", err)
	}
	p.id = idStr
	p.sha = shaStr
	p.tier = tier
	return nil
}

// applyHeader dispatches one header line to its field, an agreeing tier, or an
// extra, refusing duplicates and CR/LF inside any single-line value.
func applyHeader(p *parsedCard, key, value string, num int, seen map[string]int) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("cards: %s on line %d has a CR or LF in its value", key, num)
	}
	if key == "tier" {
		if prev, ok := seen["tier"]; ok {
			return fmt.Errorf("cards: duplicate header key %q on lines %d and %d", key, prev, num)
		}
		seen["tier"] = num
		t, err := parseTier(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("cards: tier on line %d: %v", num, err)
		}
		if t != p.tier {
			return fmt.Errorf("cards: tier on line %d disagrees with RESULT", num)
		}
		return nil
	}
	if key == "RESULT" {
		return fmt.Errorf("cards: duplicate header key %q on lines %d and %d", key, seen["RESULT"], num)
	}
	if _, mapped := reservedHeaderKeys[key]; mapped {
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("cards: duplicate header key %q on lines %d and %d", key, prev, num)
		}
		seen[key] = num
		return assignMapped(p, key, value, num)
	}
	if err := checkStrBound("extra key", key, 32); err != nil {
		return fmt.Errorf("cards: line %d: %v", num, err)
	}
	p.extras = append(p.extras, kv{key: key, value: value})
	return nil
}

// assignMapped fills one mapped header field with its checked value.
func assignMapped(p *parsedCard, key, value string, num int) error {
	switch key {
	case "REPO":
		return assignStr(&p.repo, value, 96, "REPO", num)
	case "BASE":
		return assignStr(&p.base, value, 96, "BASE", num)
	case "KIND":
		return assignStr(&p.kind, value, 24, "KIND", num)
	case "PRIORITY":
		pr, err := parsePriority(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("cards: PRIORITY on line %d: %v", num, err)
		}
		p.priority = pr
	case "PATHS":
		el, err := splitListChecked(value, 192, true, "PATHS", num)
		if err != nil {
			return err
		}
		p.paths = el
	case "NEW":
		el, err := splitListChecked(value, 192, false, "NEW", num)
		if err != nil {
			return err
		}
		p.news = el
	case "SHARED":
		el, err := splitListChecked(value, 192, false, "SHARED", num)
		if err != nil {
			return err
		}
		p.shared = el
	case "DEPENDS-ON":
		el, err := splitListChecked(value, 64, false, "DEPENDS-ON", num)
		if err != nil {
			return err
		}
		p.needs = el
	case "TEST":
		return assignStr(&p.test, value, 192, "TEST", num)
	case "DONE-WHEN":
		p.doneWhen = value
	case "DEADLINE":
		n, err := parseDeadline(value)
		if err != nil {
			return fmt.Errorf("cards: DEADLINE on line %d: %v", num, err)
		}
		p.deadline = n
	case "SOURCE":
		p.source = value
	}
	return nil
}

func assignStr(dst *string, value string, bound int, name string, num int) error {
	if err := checkStrBound(name, value, bound); err != nil {
		return fmt.Errorf("cards: %s on line %d: %v", name, num, err)
	}
	*dst = value
	return nil
}

func checkStrBound(name, value string, bound int) error {
	if len(value) > bound {
		return fmt.Errorf("cards: %s is %d bytes, past the %d-byte bound", name, len(value), bound)
	}
	return nil
}

// splitListChecked splits a comma-separated list, trims elements, drops empty
// ones, and enforces the element bound and the entry ceiling (docs/SPEC-TABLES.md
// §2.9). A whole value of "-" or empty is an empty list; PATHS alone also takes
// the customer sentinel "none".
func splitListChecked(value string, bound int, noneSentinel bool, name string, num int) ([]string, error) {
	v := strings.TrimSpace(value)
	if v == "" || v == "-" {
		return nil, nil
	}
	if noneSentinel && v == "none" {
		return nil, nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if err := checkStrBound(name+" element", part, bound); err != nil {
			return nil, fmt.Errorf("cards: %s on line %d: %v", name, num, err)
		}
		out = append(out, part)
	}
	if len(out) > MaxListEntries {
		return nil, fmt.Errorf("cards: %s on line %d has %d entries, past the %d-entry ceiling", name, num, len(out), MaxListEntries)
	}
	return out, nil
}

func parseTier(s string) (Tier, error) {
	switch s {
	case "flash":
		return Tierflash, nil
	case "pro":
		return Tierpro, nil
	case "heavy":
		return Tierheavy, nil
	case "frontier":
		return Tierfrontier, nil
	}
	return TierNone, fmt.Errorf("unknown tier %q", s)
}

func parsePriority(s string) (Priority, error) {
	switch s {
	case "normal":
		return Prioritynormal, nil
	case "high":
		return Priorityhigh, nil
	case "critical":
		return Prioritycritical, nil
	case "blocker":
		return Priorityblocker, nil
	}
	return PriorityNone, fmt.Errorf("unknown priority %q", s)
}

func parseDeadline(s string) (int32, error) {
	m := deadlineRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, errors.New("not of the form \"finish within N minutes.\"")
	}
	n, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil || n < 0 || n > 100000 {
		return 0, errors.New("minutes out of range")
	}
	return int32(n), nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isBlank(line []byte) bool {
	for _, b := range line {
		if b != ' ' && b != '\t' && b != '\r' {
			return false
		}
	}
	return true
}

// build publishes the parsed card through the generated builder and checked
// write (docs/SPEC-TABLES.md §6.2). Bytes are published only on complete
// success; a refused write mutates nothing.
func (p *parsedCard) build(body []byte) (*Record, error) {
	var b CardBuilder
	if !b.Init() {
		return nil, errors.New("cards: arena initialization failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	assignFixed(root.Id[:], &root.IdLength, p.id)
	assignFixed(root.Sha[:], &root.ShaLength, p.sha)
	root.Tier = p.tier
	assignFixed(root.Repo[:], &root.RepoLength, p.repo)
	assignFixed(root.Base[:], &root.BaseLength, p.base)
	assignFixed(root.Kind[:], &root.KindLength, p.kind)
	root.Priority = p.priority
	assignFixed(root.Stream[:], &root.StreamLength, p.stream)
	for _, e := range p.paths {
		row := CardPathsAdd(&b.Main, &root.Paths)
		if row == nil {
			return nil, errors.New("cards: builder refused a paths element")
		}
		assignFixed(row.S[:], &row.SLength, e)
	}
	for _, e := range p.news {
		row := CardNewAdd(&b.Main, &root.New)
		if row == nil {
			return nil, errors.New("cards: builder refused a new element")
		}
		assignFixed(row.S[:], &row.SLength, e)
	}
	for _, e := range p.shared {
		row := CardSharedAdd(&b.Main, &root.Shared)
		if row == nil {
			return nil, errors.New("cards: builder refused a shared element")
		}
		assignFixed(row.S[:], &row.SLength, e)
	}
	for _, e := range p.needs {
		row := CardNeedsAdd(&b.Main, &root.Needs)
		if row == nil {
			return nil, errors.New("cards: builder refused a needs element")
		}
		assignFixed(row.S[:], &row.SLength, e)
	}
	assignFixed(root.Test[:], &root.TestLength, p.test)
	if p.doneWhen != "" {
		TableStringEmplace(&b.Main, &root.DoneWhen, []byte(p.doneWhen))
	}
	root.DeadlineMinutes = p.deadline
	if p.source != "" {
		TableStringEmplace(&b.Main, &root.Source, []byte(p.source))
	}
	for _, e := range p.extras {
		row := CardExtraAdd(&b.Main, &root.Extra)
		if row == nil {
			return nil, errors.New("cards: builder refused an extra element")
		}
		assignFixed(row.Key[:], &row.KeyLength, e.key)
		TableStringEmplace(&b.Main, &row.Value, []byte(e.value))
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

func assignFixed(dst []byte, n *int32, s string) {
	copy(dst, s)
	*n = int32(len(s))
}

// rowStr reads a generated string(N) buffer's used bytes as a Go string.
func rowStr(buf []byte, n int32) string {
	if n < 0 {
		n = 0
	}
	if int(n) > len(buf) {
		n = int32(len(buf))
	}
	return string(buf[:n])
}

// collectPaths and collectIDs read an unbounded list of table elements into
// their string spellings, in stored order.
func collectPaths(list *TableList[PathTextRow]) []string {
	var out []string
	CardPathsEach(list, func(e *PathTextRow) bool {
		out = append(out, rowStr(e.S[:], e.SLength))
		return true
	})
	return out
}

func collectIDs(list *TableList[CardIDRow]) []string {
	var out []string
	CardNeedsEach(list, func(e *CardIDRow) bool {
		out = append(out, rowStr(e.S[:], e.SLength))
		return true
	})
	return out
}
