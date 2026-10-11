// Package cards — parse.go owns the card's brief text grammar: one RESULT line,
// a header region of key lines and structural blank lines, and an exact body
// suffix. Text is an admission view read once, never machine-state storage
// (card API AND OWNERSHIP).
package cards

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse reads a card's brief text once and publishes a Record only on complete
// checked success (card API AND OWNERSHIP; card TEXT GRAMMAR). It parses line 1
// as the RESULT line, maps the header region into declared fields and extras,
// keeps the exact body suffix, refuses stranded keys after prose, and then
// builds/validates/serializes through the generated builder into defensive
// storage.
func Parse(text []byte) (*Record, error) {
	lines := splitTextLines(text)
	if len(lines) == 0 {
		return nil, errors.New("cards: empty brief has no RESULT line")
	}
	p := &textParser{seen: make(map[string]int)}
	if err := p.parseResult(lines[0].content); err != nil {
		return nil, err
	}
	bodyStart := lines[0].end
	proseStart := -1
	for i := 1; i < len(lines); i++ {
		content := lines[i].content
		if isBlankLine(content) {
			continue
		}
		key, value, ok := matchHeaderLine(content)
		if !ok {
			proseStart = i
			break
		}
		if err := p.applyHeader(key, value, i+1); err != nil {
			return nil, err
		}
		bodyStart = lines[i].end
	}
	body := text[bodyStart:]
	if proseStart >= 0 {
		for i := proseStart; i < len(lines); i++ {
			if key, _, ok := matchHeaderLine(lines[i].content); ok && strandedHeaderKey(string(key)) {
				return nil, fmt.Errorf("cards: stranded header key %q after prose on line %d", key, i+1)
			}
		}
	}
	return p.build(body)
}

// textParser carries the fields a brief's headers resolve into while the
// header region is walked, and later publishes them through the builder.
type textParser struct {
	id           string
	sha          string
	tier         Tier
	repo         string
	base         string
	kind         string
	priority     Priority
	paths        []string
	new          []string
	shared       []string
	needs        []string
	test         string
	doneWhen     *string
	deadline     int32
	deadlineSet  bool
	source       *string
	extras       []extraKV
	seen         map[string]int
}

type extraKV struct {
	key   string
	value string
}

// mappedHeaderKey reports whether key is one of the exact case-sensitive keys a
// brief maps into a declared Card field (card HEADER MAPPING). An extra whose
// key collides here would render a second line for the same field.
func mappedHeaderKey(key string) bool {
	switch key {
	case "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW", "SHARED",
		"DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE", "RESULT", "tier":
		return true
	}
	return false
}

// strandedHeaderKey reports whether key is in the customer's cardHeaderKeys
// set, the exact case-sensitive keys refused when one appears as a key-shaped
// line after prose begins (card STRANDED KEYS; nova-sprint
// pkg/swarm/lintheader.go).
func strandedHeaderKey(key string) bool {
	switch key {
	case "KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP",
		"DEPENDS-ON", "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT":
		return true
	}
	return false
}

// validHeaderKey reports whether key matches the header key grammar
// ^[A-Za-z][A-Za-z0-9-]*$ (card TEXT GRAMMAR).
func validHeaderKey(key string) bool {
	if key == "" || !isAlphaByte(key[0]) {
		return false
	}
	for i := 1; i < len(key); i++ {
		if !isKeyByte(key[i]) {
			return false
		}
	}
	return true
}

func isAlphaByte(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isKeyByte(c byte) bool  { return isAlphaByte(c) || c >= '0' && c <= '9' || c == '-' }

// textLine is one line's content (terminator removed) and the byte offset just
// past its terminator, so the body suffix can be taken by offset (card TEXT
// GRAMMAR).
type textLine struct {
	content []byte
	end     int
}

// splitTextLines splits text on '\n', accepting a preceding '\r' as the CRLF
// terminator, and records the offset past each terminator (card TEXT GRAMMAR).
func splitTextLines(text []byte) []textLine {
	var lines []textLine
	start := 0
	for start < len(text) {
		i := bytes.IndexByte(text[start:], '\n')
		if i < 0 {
			content := stripCR(text[start:])
			lines = append(lines, textLine{content: content, end: len(text)})
			break
		}
		end := start + i
		lines = append(lines, textLine{content: stripCR(text[start:end]), end: end + 1})
		start = end + 1
	}
	return lines
}

func stripCR(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\r' {
		return b[:n-1]
	}
	return b
}

func isBlankLine(content []byte) bool {
	for _, c := range content {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

func trimSpaceBytes(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// matchHeaderLine recognizes a header line ^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$
// (card TEXT GRAMMAR), returning the key and the whitespace-trimmed value.
// Leading whitespace is not a key; a nonblank line that is not a key starts
// prose.
func matchHeaderLine(content []byte) (key, value []byte, ok bool) {
	if len(content) == 0 || !isAlphaByte(content[0]) {
		return nil, nil, false
	}
	i := 1
	for i < len(content) && isKeyByte(content[i]) {
		i++
	}
	if i >= len(content) || content[i] != ':' {
		return nil, nil, false
	}
	return content[:i], trimSpaceBytes(content[i+1:]), true
}

// parseResult parses line 1's RESULT grammar `RESULT: <id> sha=<sha> tier:
// <tier>`, validating the id string bound, the sha (12 or 40 hex digits) and
// the tier enum (card TEXT GRAMMAR).
func (p *textParser) parseResult(line []byte) error {
	if !bytes.HasPrefix(line, []byte("RESULT:")) {
		return errors.New("cards: line 1 is not a RESULT line")
	}
	rest := trimSpaceBytes(line[len("RESULT:"):])
	i := bytes.Index(rest, []byte(" sha="))
	if i < 0 {
		return errors.New("cards: RESULT line has no sha= clause")
	}
	id := string(trimSpaceBytes(rest[:i]))
	rest2 := rest[i+len(" sha="):]
	j := bytes.Index(rest2, []byte(" tier:"))
	if j < 0 {
		return errors.New("cards: RESULT line has no tier: clause")
	}
	sha := string(trimSpaceBytes(rest2[:j]))
	tierText := string(trimSpaceBytes(rest2[j+len(" tier:"):]))
	if err := checkBoundedString("id", id, 64, 1); err != nil {
		return err
	}
	if err := checkSha(sha); err != nil {
		return err
	}
	tier, err := parseTier(tierText)
	if err != nil {
		return fmt.Errorf("cards: %v on line 1", err)
	}
	p.id = id
	p.sha = sha
	p.tier = tier
	p.seen["RESULT"] = 1
	return nil
}

// checkSha holds the sha rule: exactly 12 or 40 hex digits (card TEXT GRAMMAR).
func checkSha(sha string) error {
	if len(sha) != 12 && len(sha) != 40 {
		return fmt.Errorf("cards: sha %q is %d bytes, want 12 or 40 hex digits", sha, len(sha))
	}
	for i := 0; i < len(sha); i++ {
		c := sha[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return fmt.Errorf("cards: sha %q is not hexadecimal", sha)
		}
	}
	return nil
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
	return TierNone, fmt.Errorf("cards: unknown tier %q", s)
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
	return PriorityNone, fmt.Errorf("cards: unknown priority %q", s)
}

// applyHeader maps one header line into a declared field or an extra, refusing
// duplicate keys and embedded CR/LF in a single-line value (card HEADER
// MAPPING, STRANDED KEYS).
func (p *textParser) applyHeader(key, value []byte, line int) error {
	ks := string(key)
	if prev, dup := p.seen[ks]; dup {
		return fmt.Errorf("cards: duplicate header key %q on line %d (first on line %d)", ks, line, prev)
	}
	if bytes.IndexByte(value, '\r') >= 0 || bytes.IndexByte(value, '\n') >= 0 {
		return fmt.Errorf("cards: header %q has a CR or LF in its single-line value on line %d", ks, line)
	}
	switch ks {
	case "REPO":
		if err := p.setStringField(&p.repo, value, "REPO", 96, line); err != nil {
			return err
		}
	case "BASE":
		if err := p.setStringField(&p.base, value, "BASE", 96, line); err != nil {
			return err
		}
	case "KIND":
		if err := p.setStringField(&p.kind, value, "KIND", 24, line); err != nil {
			return err
		}
	case "PRIORITY":
		pr, err := parsePriority(string(value))
		if err != nil {
			return fmt.Errorf("cards: %v on line %d", err, line)
		}
		p.priority = pr
	case "PATHS":
		list, err := parseList(value, "PATHS", 192, true, line)
		if err != nil {
			return err
		}
		p.paths = list
	case "NEW":
		list, err := parseList(value, "NEW", 192, false, line)
		if err != nil {
			return err
		}
		p.new = list
	case "SHARED":
		list, err := parseList(value, "SHARED", 192, false, line)
		if err != nil {
			return err
		}
		p.shared = list
	case "DEPENDS-ON":
		list, err := parseList(value, "DEPENDS-ON", 64, false, line)
		if err != nil {
			return err
		}
		p.needs = list
	case "TEST":
		if err := p.setStringField(&p.test, value, "TEST", 192, line); err != nil {
			return err
		}
	case "DONE-WHEN":
		s := string(value)
		p.doneWhen = &s
	case "DEADLINE":
		n, err := parseDeadline(value, line)
		if err != nil {
			return err
		}
		p.deadline = n
		p.deadlineSet = true
	case "SOURCE":
		s := string(value)
		p.source = &s
	case "tier":
		tier, err := parseTier(string(value))
		if err != nil {
			return fmt.Errorf("cards: %v on line %d", err, line)
		}
		if tier != p.tier {
			return fmt.Errorf("cards: tier line %q disagrees with RESULT tier %q on line %d", value, EnumNameTier(p.tier), line)
		}
	default:
		if err := checkBoundedString(ks, string(value), 32, line); err != nil {
			return err
		}
		if !validHeaderKey(ks) {
			return fmt.Errorf("cards: extra key %q is not a valid header key on line %d", ks, line)
		}
		p.extras = append(p.extras, extraKV{key: ks, value: string(value)})
	}
	p.seen[ks] = line
	return nil
}

// setStringField validates a mapped string header value against its declared
// string(N) bound and UTF-8 well-formedness, never truncating (card HEADER
// MAPPING: "Refuse over-bound keys/values ... no dropped/truncated header").
func (p *textParser) setStringField(dst *string, value []byte, key string, bound, line int) error {
	s := string(value)
	if err := checkBoundedString(key, s, bound, line); err != nil {
		return err
	}
	*dst = s
	return nil
}

// checkBoundedString holds the over-bound and UTF-8 rules shared by mapped
// string fields, list elements and extra keys (card HEADER MAPPING;
// docs/SPEC-TABLES.md §2.5).
func checkBoundedString(name, s string, bound, line int) error {
	if len(s) > bound {
		return fmt.Errorf("cards: %s is %d bytes, past the string(%d) bound on line %d", name, len(s), bound, line)
	}
	if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return fmt.Errorf("cards: %s is not well-formed UTF-8 without NUL on line %d", name, line)
	}
	return nil
}

// parseList splits a comma-separated header value, trims each element and drops
// empty ones; `-`, empty and (for PATHS alone) the customer sentinel `none`
// mean an empty list (card HEADER MAPPING). Every element is refused, never
// truncated, past the declared element bound.
func parseList(value []byte, key string, bound int, noneSentinel bool, line int) ([]string, error) {
	s := strings.TrimSpace(string(value))
	if s == "" || s == "-" || (noneSentinel && s == "none") {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		e := strings.TrimSpace(part)
		if e == "" {
			continue
		}
		if err := checkBoundedString(key, e, bound, line); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// parseDeadline accepts exactly `finish within N minutes.` and converts N into
// deadline_minutes, checking the int32 and [0, 100000] range (card HEADER
// MAPPING).
func parseDeadline(value []byte, line int) (int32, error) {
	s := string(value)
	const prefix = "finish within "
	const suffix = " minutes."
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, suffix) {
		return 0, fmt.Errorf("cards: DEADLINE value %q is not `finish within N minutes.` on line %d", s, line)
	}
	digits := s[len(prefix) : len(s)-len(suffix)]
	if digits == "" {
		return 0, fmt.Errorf("cards: DEADLINE value %q has no minutes on line %d", s, line)
	}
	n, err := strconv.ParseInt(digits, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("cards: DEADLINE minutes %q is not an integer on line %d", digits, line)
	}
	if n < 0 || n > 100000 {
		return 0, fmt.Errorf("cards: DEADLINE minutes %d is out of [0, 100000] on line %d", n, line)
	}
	return int32(n), nil
}

// build validates the body and every collection against the customer ceilings,
// fills the generated builder, checks, and publishes a Record (card API AND
// OWNERSHIP; docs/SPEC-TABLES.md §2.5, §2.9).
func (p *textParser) build(body []byte) (*Record, error) {
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("cards: body is %d bytes, past the %d-byte ceiling", len(body), MaxBodyBytes)
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return nil, errors.New("cards: body is not well-formed UTF-8 without NUL")
	}
	for _, n := range []struct {
		name  string
		count int
	}{
		{"paths", len(p.paths)},
		{"_new", len(p.new)},
		{"shared", len(p.shared)},
		{"needs", len(p.needs)},
		{"extra", len(p.extras)},
	} {
		if n.count > MaxListEntries {
			return nil, fmt.Errorf("cards: %s has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", n.name, n.count, MaxListEntries)
		}
	}

	var b CardBuilder
	if !b.Init() {
		return nil, errors.New("cards: arena initialization failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStringField(root.Id[:], &root.IdLength, p.id)
	setStringField(root.Sha[:], &root.ShaLength, p.sha)
	root.Tier = p.tier
	setStringField(root.Repo[:], &root.RepoLength, p.repo)
	setStringField(root.Base[:], &root.BaseLength, p.base)
	setStringField(root.Kind[:], &root.KindLength, p.kind)
	root.Priority = p.priority
	for _, s := range p.paths {
		if e := CardPathsAdd(&b.Main, &root.Paths); e != nil {
			setStringField(e.S[:], &e.SLength, s)
		}
	}
	for _, s := range p.new {
		if e := CardNewAdd(&b.Main, &root.New); e != nil {
			setStringField(e.S[:], &e.SLength, s)
		}
	}
	for _, s := range p.shared {
		if e := CardSharedAdd(&b.Main, &root.Shared); e != nil {
			setStringField(e.S[:], &e.SLength, s)
		}
	}
	for _, s := range p.needs {
		if e := CardNeedsAdd(&b.Main, &root.Needs); e != nil {
			setStringField(e.S[:], &e.SLength, s)
		}
	}
	setStringField(root.Test[:], &root.TestLength, p.test)
	if p.doneWhen != nil {
		TableStringEmplace(&b.Main, &root.DoneWhen, []byte(*p.doneWhen))
	}
	root.DeadlineMinutes = p.deadline
	if p.source != nil {
		TableStringEmplace(&b.Main, &root.Source, []byte(*p.source))
	}
	for _, kv := range p.extras {
		e := CardExtraAdd(&b.Main, &root.Extra)
		if e == nil {
			return nil, errors.New("cards: extra allocation failed")
		}
		setStringField(e.Key[:], &e.KeyLength, kv.key)
		TableStringEmplace(&b.Main, &e.Value, []byte(kv.value))
	}
	if len(body) > 0 {
		TableStringEmplace(&b.Main, &root.Body, body)
	}
	if err := checkCard(root, &b.Arena); err != nil {
		return nil, err
	}
	var report TableReport
	return publishRecord(&b, report)
}
