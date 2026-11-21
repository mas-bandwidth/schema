package rubytable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// rubyMessageBytesDir is the committed corpus: the reader's and each writer's
// schema and cases.tsv, one row per writer and value.
const rubyMessageBytesDir = "testdata/message-bytes-element"

// TestRubyMessageBytesElementKind pins the message form's bytes(N) rule for
// the Ruby table leg (docs/SPEC-TABLES.md 3.3, "bytes(N) and [..N]uint8 are
// the same kind"; review-owed/cross-leg/message-form rule 5): a bytes(N) field
// reads only an announced array of element kind uint8. A writer's [..8]uint8
// lands; a writer's [..8]int8 is another kind and refuses kind_mismatch, where
// the read used to repack its elements as bytes, even when its elements hold
// the very bits a uint8 writer's do. The rows are the committed corpus.
func TestRubyMessageBytesElementKind(t *testing.T) {
	slowtest.Gate(t, "both pinned Ruby interpreters")

	schema := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(rubyMessageBytesDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	rows := rubyCorpusRows(t, filepath.Join(rubyMessageBytesDir, "cases.tsv"), 5)
	rfiles, err := Generate(loadUnit(t, schema("reader.schema")))
	if err != nil {
		t.Fatal(err)
	}
	rdir := t.TempDir()
	writeTree(t, rdir, rfiles)
	verdicts := map[string]int{}
	for _, r := range rows {
		id, writerSchema, elems, expect, reason := r[0], r[1], r[2], r[3], r[4]
		verdict := strings.Fields(expect)[0]
		verdicts[verdict]++
		want := expect
		switch verdict {
		case "loaded":
		case "refused":
			want = "refused " + reason
		default:
			t.Fatalf("%s: expect %q", id, expect)
		}
		t.Run(id, func(t *testing.T) {
			wfiles, err := Generate(loadUnit(t, schema(writerSchema)))
			if err != nil {
				t.Fatal(err)
			}
			wdir := t.TempDir()
			writeTree(t, wdir, wfiles)
			writer := "# frozen_string_literal: true\n\nrequire_relative 'mbytes'\n\nm = Mbytes::M.new\nm.b = " + elems + "\nputs Mbytes::ANNOUNCEMENT.unpack1('H*')\nputs Mbytes.save_messages(Mbytes::M, [m]).unpack1('H*')\n"
			out := strings.Fields(runBothRubyPins(t, wdir, "writer.rb", map[string][]byte{"writer.rb": []byte(writer)}))
			if len(out) != 2 {
				t.Fatalf("writer answered %q", out)
			}
			ann, _ := hex.DecodeString(out[0])
			batch, _ := hex.DecodeString(out[1])
			got := strings.TrimSpace(runBothRubyPins(t, rdir, "reader.rb", map[string][]byte{
				"reader.rb": []byte(rubyMessageBytesReader),
				"ann.bin":   ann,
				"batch.bin": batch,
			}))
			if got != want {
				t.Errorf("a %s writer's batch of %s read as bytes(8): %q, want %q", writerSchema, elems, got, want)
			}
		})
	}
	if verdicts["loaded"] == 0 || verdicts["refused"] == 0 {
		t.Fatalf("cases.tsv: verdicts %v; the corpus owes a load and a refusal", verdicts)
	}
}

const rubyMessageBytesReader = `# frozen_string_literal: true

require_relative 'mbytes'

vocab = Mbytes::MessageVocabulary.new
Mbytes.announce_read(vocab, File.binread('ann.bin'))
begin
  got = Mbytes.load_messages(Mbytes::M, vocab, File.binread('batch.bin'))
  puts "loaded #{got[0].b.unpack1('H*')}"
rescue Mbytes::Refused => e
  puts "refused #{e.reason}"
end
`
