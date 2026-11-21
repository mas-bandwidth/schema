package rubytable

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRubyMessageBytesElementKind pins the message form's bytes(N) rule for
// the Ruby table leg (docs/SPEC-TABLES.md 3.3, "bytes(N) and [..N]uint8 are
// the same kind"; review-owed/cross-leg/message-form rule 5): a bytes(N) field
// reads only an announced array of element kind uint8. A writer's [..8]uint8
// lands; a writer's [..8]int8 is another kind and refuses kind_mismatch, where
// the read used to repack its elements as bytes.
func TestRubyMessageBytesElementKind(t *testing.T) {
	slowtest.Gate(t, "both pinned Ruby interpreters")

	const reader = `package mbytes

fixed table M
{
    b bytes(8)
}
`
	writers := []struct {
		name, src, want string
	}{
		{"uint8", "package mbytes\n\nfixed table M\n{\n    b [..8]uint8\n}\n", "loaded ff02"},
		{"int8", "package mbytes\n\nfixed table M\n{\n    b [..8]int8\n}\n", "refused kind_mismatch"},
	}
	rfiles, err := Generate(loadUnit(t, reader))
	if err != nil {
		t.Fatal(err)
	}
	rdir := t.TempDir()
	writeTree(t, rdir, rfiles)
	for _, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			wfiles, err := Generate(loadUnit(t, w.src))
			if err != nil {
				t.Fatal(err)
			}
			wdir := t.TempDir()
			writeTree(t, wdir, wfiles)
			elem := "255"
			if w.name == "int8" {
				elem = "-1"
			}
			writer := "# frozen_string_literal: true\n\nrequire_relative 'mbytes'\n\nm = Mbytes::M.new\nm.b = [" + elem + ", 2]\nputs Mbytes::ANNOUNCEMENT.unpack1('H*')\nputs Mbytes.save_messages(Mbytes::M, [m]).unpack1('H*')\n"
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
			if got != w.want {
				t.Errorf("a %s writer's batch read as bytes(8): %q, want %q", w.name, got, w.want)
			}
		})
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
