// `schema help ai` is pasted into other projects' AGENTS.md and CLAUDE.md, so
// its text is held here: the same bytes every run, the one source file with
// its --lang list filled from internal/languages, short enough to paste,
// carrying the guide's URL, naming exactly the targets this compiler
// generates, and listed where `schema` lists its commands. Every test runs in process; nothing is built or spawned.
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/languages"
)

const aiGuideURL = "https://github.com/mas-bandwidth/schema/blob/main/docs/AI.md"

func aiHelpOutput(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := helpTopic(&buf, "ai"); err != nil {
		t.Fatalf("schema help ai: %v", err)
	}
	return buf.Bytes()
}

func TestHelpAIIsByteStableAndIsTheSourceFile(t *testing.T) {
	t.Parallel()
	first, second := aiHelpOutput(t), aiHelpOutput(t)
	if !bytes.Equal(first, second) {
		t.Fatal("schema help ai printed different bytes on two runs")
	}
	src, err := os.ReadFile("help/ai.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), targetsPlaceholder); n != 1 {
		t.Fatalf("help/ai.md holds %s %d times; the --lang list is filled in exactly once", targetsPlaceholder, n)
	}
	if want := fillTargets(string(src)); string(first) != want {
		t.Fatal("schema help ai does not print help/ai.md with its --lang list filled; the embedded copy is stale")
	}
	if strings.Contains(string(first), "{{") {
		t.Error("schema help ai printed an unfilled placeholder")
	}
}

func TestHelpAIIsShortAndCarriesTheGuide(t *testing.T) {
	t.Parallel()
	out := string(aiHelpOutput(t))
	if strings.TrimSpace(out) == "" {
		t.Fatal("schema help ai printed nothing")
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("schema help ai does not end with a newline; a pasted block would run into the next line")
	}
	for i, line := range strings.Split(out, "\n") {
		if len(line) > 80 {
			t.Errorf("schema help ai line %d is %d columns: %q", i+1, len(line), line)
		}
	}
	if n := strings.Count(out, "\n"); n >= 60 {
		t.Errorf("schema help ai is %d lines; it is pasted into other projects and stays under 60", n)
	}
	if !strings.Contains(out, aiGuideURL) {
		t.Errorf("schema help ai does not carry the guide's URL %s", aiGuideURL)
	}
	for _, cmd := range []string{"schema check", "schema fmt", "schema generate --lang"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("schema help ai does not name %q", cmd)
		}
	}
	// the URL names a page this repository carries
	if _, err := os.Stat("../../docs/AI.md"); err != nil {
		t.Errorf("the guide the block points at is missing: %v", err)
	}
}

// TestHelpAINamesEveryTarget holds the block's --lang list, filled from
// internal/languages, to the compiler's own registry: a target added or removed without the block following is a red test,
// not a paste that teaches a language the binary refuses.
func TestHelpAINamesEveryTarget(t *testing.T) {
	t.Parallel()
	out := string(aiHelpOutput(t))
	_, rest, ok := strings.Cut(out, "--lang takes:")
	if !ok {
		t.Fatal("schema help ai has no `--lang takes:` list")
	}
	list, _, _ := strings.Cut(rest, "\n\n")
	var got []string
	for name := range strings.SplitSeq(list, ",") {
		got = append(got, strings.TrimSpace(name))
	}
	want := compiler.New().Targets()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("schema help ai lists %v; this compiler's targets are %v", got, want)
	}
}

func TestHelpUnknownTopicIsRefusedByName(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := helpTopic(&buf, "nope")
	if err == nil {
		t.Fatal("schema help nope was accepted")
	}
	if !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "ai") {
		t.Errorf("the refusal %q does not name the topic asked for and the topics that exist", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a refused topic still printed %q", buf.String())
	}
}

// TestHelpIsListedWithTheCommands reads the usage text from source, because
// usage writes straight to stderr: the command list is where a user finds the
// verb, and the package comment is where a maintainer does.
func TestHelpIsListedWithTheCommands(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	_, body, ok := strings.Cut(text, "func usage() {")
	if !ok {
		t.Fatal("usage not found in main.go")
	}
	if !strings.Contains(body, "  schema help       [ai]\n") {
		t.Error("the usage listing does not carry `schema help [ai]`")
	}
	header, _, _ := strings.Cut(text, "package main")
	if !strings.Contains(header, "schema help") {
		t.Error("the package comment's command list does not carry `schema help`")
	}
}

// TestSchemaSkillIsWellFormed holds skills/schema/SKILL.md, the Agent Skill:
// frontmatter that names it `schema` and says when to use it, a body under 100
// lines that points at the guide, and a --lang list this compiler generates.
func TestSchemaSkillIsWellFormed(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../skills/schema/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		t.Fatal("SKILL.md does not open with YAML frontmatter")
	}
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatal("SKILL.md's frontmatter is never closed")
	}
	fields := map[string]string{}
	for line := range strings.SplitSeq(front, "\n") {
		if k, v, ok := strings.Cut(line, ": "); ok {
			fields[k] = v
		}
	}
	if fields["name"] != "schema" {
		t.Errorf("SKILL.md name is %q, want schema", fields["name"])
	}
	if d := fields["description"]; !strings.Contains(d, ".schema") || !strings.HasPrefix(d, "Use when") {
		t.Errorf("SKILL.md description %q does not say when to use it", d)
	}
	if n := strings.Count(body, "\n"); n >= 100 {
		t.Errorf("SKILL.md body is %d lines; it stays under 100", n)
	}
	if !strings.Contains(body, aiGuideURL) {
		t.Errorf("SKILL.md does not point at %s", aiGuideURL)
	}
	for _, target := range helpTargets() {
		if !strings.Contains(body, "`"+target+"`") {
			t.Errorf("SKILL.md's --lang list leaves out %s", target)
		}
	}
}

// TestLLMsTxtStatesTheLanguageList holds llms.txt's summary to the derived
// list (internal/languages), as languages_test.go holds the other pages.
func TestLLMsTxtStatesTheLanguageList(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../llms.txt")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "# schema\n\n> ") {
		t.Error("llms.txt does not open with the H1 name and a blockquote summary")
	}
	if want := languages.Summary(); !strings.Contains(text, want) {
		t.Errorf("llms.txt does not state the language list %q", want)
	}
	if _, after, ok := strings.Cut(text, "## Docs\n\n- [AI.md]("+aiGuideURL+")"); !ok || after == "" {
		t.Error("llms.txt does not link docs/AI.md first under ## Docs")
	}
}

// TestAIGuideLinksTheHelpSource keeps the one copy one: docs/AI.md points at
// the embedded file instead of repeating it.
func TestAIGuideLinksTheHelpSource(t *testing.T) {
	t.Parallel()
	page, err := os.ReadFile("../../docs/AI.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "(../cmd/schema/help/ai.md)") {
		t.Error("docs/AI.md does not link cmd/schema/help/ai.md, the source of `schema help ai`")
	}
	if !strings.Contains(string(page), "`schema help ai`") {
		t.Error("docs/AI.md does not name `schema help ai`")
	}
}
