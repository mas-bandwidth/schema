package elixirtable

// THE MESSAGE FORM'S ANNOUNCEMENT ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3).
// The message form is a batch of bitpacked bodies under one announced
// vocabulary, and the announcement is an ordinary form-`1` file the compiler
// settles byte for byte. One subtest per clause of STEP 2; the bytes are the
// C++ reference's, pinned by testdata/conformance/tables/MANIFEST.txt's
// `connection` rows, and each probe runs the generated Elixir against the
// leg's own toolchain.
//
// RED FIRST. The elixirtable package carried none of this surface, so every
// subtest below was red at HEAD: the generated unit had no `TableMessage`
// module, no `announce`, `announce_measure` or `announce_read`, and no
// `TableVocabulary`. The implementation is internal/codegen/elixirtable/
// messageform.go.
//
// THE SURFACE NAMES IN ELIXIR'S NAMING CONVENTION (SPEC.md §6.1): the unit
// module <Package>.TableMessage carries `announce/0`, `announce_measure/0` and
// `announce_read/2`, and its `%TableMessage{}` struct is the `TableVocabulary`
// of §3.3's surface. A refusal is `{:error, reason, vocab}` against
// `{:ok, vocab}`, the packet emitter's reader verdict (§3.3's own note).
//
// THE TWO STRICT CHECKS ARE MALFORMED, NOT A REFUSAL (docs/SPEC-TABLES.md
// §3.3): the build version present exactly once at kind 9 eight bytes wide,
// and the vocabulary present exactly once at kind 14 element kind 6. The
// forged announcements those checks refuse ride the reference's own pinned
// files (testdata/wire/tables/announce_*.bin).

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMessageFormAnnounceElixir(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units, connections := messageManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))

	// 2.1 "Announce and AnnounceMeasure are a compile-time constant byte array
	// and its length, equal byte for byte to the manifest's announcement file."
	t.Run("announcement_constant", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				u := messageLoadUnit(t, root, units[name])
				ann := filepath.Join(root, connections[name].path)
				out, err := runMessageProbe(t, u, ann, `  data = File.read!(file())
  ann = M.announce()
  check(byte_size(ann) == M.announce_measure(), "the measure is not the constant's length")
  check(ann == data, "the announcement bytes differ from the reference's")
  check(M.announce_measure() == byte_size(data), "the measure differs from the reference's length")
`)
				if err != nil {
					t.Fatalf("%s: %v\n%s", name, err, out)
				}
			})
		}
	})

	// 2.2 "AnnounceRead accepts each of those files and fills TableVocabulary
	// with the entries the page names, the build version kept."
	t.Run("announce_read", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				u := messageLoadUnit(t, root, units[name])
				conn := connections[name]
				ann := filepath.Join(root, conn.path)
				out, err := runMessageProbe(t, u, ann, fmt.Sprintf(`  data = File.read!(file())
  {:ok, vocab} = M.announce_read(data)
  check(vocab.announced, "a clean read did not set announced")
  check(vocab.count == M.entries_here(), "the reader filled #{vocab.count} entries, the unit announces #{M.entries_here()}")
  check(vocab.build_version == 0x%016X, "the build version was not kept")
`, conn.buildVersion))
				if err != nil {
					t.Fatalf("%s: %v\n%s", name, err, out)
				}
			})
		}
	})

	// 2.3 "the refusals second_announcement (and refusal is terminal) and
	// vocabulary_too_large (both bounds), and the two strict checks answered
	// `malformed`."
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		u := messageLoadUnit(t, root, units["backenddemo"])
		ann := filepath.Join(root, connections["backenddemo"].path)
		out, err := runMessageProbe(t, u, ann, `  data = File.read!(file())

  # "NO RE-ANNOUNCEMENT, EVER. ... A second is refused by name and does not
  # replace or amend anything." (docs/SPEC-TABLES.md §3.3)
  {:ok, vocab} = M.announce_read(data)
  {:error, :second_announcement, again} = M.announce_read(data, vocab)
  check(again.count == vocab.count, "a second announcement replaced the vocabulary")

  # "REFUSAL IS TERMINAL ... every announcement after it is refused as
  # `+"`"+`second_announcement`+"`"+`" (docs/SPEC-TABLES.md §3.3)
  {:error, :newer_form, refused} = M.announce_read(<<9, 9, 9>>)
  {:error, :second_announcement, later} = M.announce_read(data, refused)
  check(not later.announced, "an announcement after a refused first was not second_announcement")

  # "`+"`"+`vocabulary_too_large`+"`"+` covers both bounds, the entry count and the
  # vocabulary's bytes." (docs/SPEC-TABLES.md §3.3)
  {:error, :vocabulary_too_large, by_entries} = M.announce_read(data, %M{} |> Map.put(:max_entries, 1))
  check(not by_entries.announced, "the entry bound refusal set a vocabulary")
  {:error, :vocabulary_too_large, by_bytes} = M.announce_read(data, %M{} |> Map.put(:max_bytes, 4))
  check(not by_bytes.announced, "the byte bound refusal set a vocabulary")

  # THE TWO STRICT CHECKS ANSWER MALFORMED (docs/SPEC-TABLES.md §3.3), each a
  # forged announcement the reference pins.
  for {label, bin} <- [
    {:build_version_absent, File.read!(golden("announce_no_build_version.bin"))},
    {:build_version_twice, File.read!(golden("announce_build_version_twice.bin"))},
    {:build_version_kind, File.read!(golden("announce_build_version_kind.bin"))},
    {:build_version_width, File.read!(golden("announce_build_version_width.bin"))},
    {:vocabulary_absent, File.read!(golden("announce_no_vocabulary.bin"))},
    {:vocabulary_twice, File.read!(golden("announce_vocabulary_twice.bin"))},
    {:vocabulary_element_kind, File.read!(golden("announce_vocabulary_element_kind.bin"))}
  ] do
    {:error, :malformed, bad} = M.announce_read(bin)
    check(not bad.announced, "#{label}: a failed strict check set a vocabulary")
  end
`)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	})

	// THE ANNOUNCEMENT IS A FILE (§3.3), and its id table is in FIRST-USE
	// ORDER like every other (§3): the reference's lawful announcement, then
	// broken one way each, every break malformed and setting no vocabulary,
	// and refused by the engine on the same bytes first.
	t.Run("first_use_order", func(t *testing.T) {
		t.Parallel()
		u := messageLoadUnit(t, root, units["backenddemo"])
		ann := filepath.Join(root, connections["backenddemo"].path)
		lawful, err := os.ReadFile(ann)
		if err != nil {
			t.Fatal(err)
		}
		var lawfulVocab tablewire.Vocabulary
		if err := lawfulVocab.AnnounceRead(lawful, &tabletext.Report{}); err != nil {
			t.Fatalf("the engine refuses the lawful announcement: %v", err)
		}
		var probe strings.Builder
		probe.WriteString("  {:ok, _} = M.announce_read(File.read!(file()))\n")
		for _, c := range announcementFirstUseBreaks(t, lawful) {
			var vocab tablewire.Vocabulary
			err := vocab.AnnounceRead(c.wire, &tabletext.Report{})
			if tablewire.RefusalReason(err) != "wire_damaged" || !strings.Contains(err.Error(), "first-use order") || vocab.Announced() {
				t.Fatalf("%s: the engine does not refuse it for its id table: %v", c.label, err)
			}
			fmt.Fprintf(&probe, "  check(\n    match?(\n      {:error, :malformed, %%{announced: false}},\n      M.announce_read(Base.decode16!(%q, case: :lower))\n    ),\n    %q\n  )\n",
				hex.EncodeToString(c.wire), c.label+": the announcement read did not answer malformed")
		}
		out, err := runMessageProbe(t, u, ann, probe.String())
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	})
}

type announcementBreak struct {
	label string
	wire  []byte
}

// announcementFirstUseBreaks breaks a lawful announcement's id table (the
// build version at reference 1, offset 0, and the vocabulary at reference 2,
// offset 10) three ways: an unused entry after the last, an unused entry
// between the two (the vocabulary's reference renumbered past it), and the
// two entries swapped with their references, so each field keeps its
// identity and payload and only the table's order differs.
func announcementFirstUseBreaks(t *testing.T, lawful []byte) []announcementBreak {
	t.Helper()
	n := int(binary.LittleEndian.Uint64(lawful[len(lawful)-8:]))
	start := len(lawful) - 8 - 8*n
	body := lawful[1:start]
	if n != 2 || len(body) < 12 || body[0] != 1 || body[1] != ir.TableKindU64 || body[10] != 2 || body[11] != ir.TableKindArray {
		t.Fatalf("the announcement is not its build version then its vocabulary: % x", lawful)
	}
	ids := []uint64{binary.LittleEndian.Uint64(lawful[start:]), binary.LittleEndian.Uint64(lawful[start+8:])}
	unused := ir.TableWireId("first_use_unused_announcement_entry")
	for unused == ids[0] || unused == ids[1] {
		unused++
	}
	wire := func(body []byte, ids ...uint64) []byte {
		w := append([]byte{ir.TableWireForm}, body...)
		for _, id := range ids {
			w = binary.LittleEndian.AppendUint64(w, id)
		}
		return binary.LittleEndian.AppendUint64(w, uint64(len(ids)))
	}
	shifted := append([]byte(nil), body...)
	shifted[10] = 3
	crossed := append([]byte(nil), body...)
	crossed[0], crossed[10] = 2, 1
	return []announcementBreak{
		{"unused_trailing", wire(body, ids[0], ids[1], unused)},
		{"unused_middle", wire(shifted, ids[0], unused, ids[1])},
		{"swapped", wire(crossed, ids[1], ids[0])},
	}
}

// ---- the manifest ------------------------------------------------------------

type messageConnection struct {
	unit         string
	buildVersion uint64
	path         string
}

// messageManifest reads the `unit` and `connection` rows the three units this
// card names ride, never a copied list.
func messageManifest(t *testing.T, path string) (map[string][]string, map[string]messageConnection) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	connections := map[string]messageConnection{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			if len(fields) >= 3 {
				units[fields[1]] = fields[2:]
			}
		case "connection":
			if len(fields) >= 5 {
				var version uint64
				if _, err := fmt.Sscanf(fields[3], "0x%x", &version); err != nil {
					t.Fatalf("manifest connection %q version %q: %v", fields[1], fields[3], err)
				}
				conn := messageConnection{unit: fields[2], buildVersion: version, path: fields[4]}
				// The `message` row names the connection by its KEY (fields[1]);
				// the announce test names it by its UNIT (fields[2]). One entry,
				// two names.
				connections[fields[1]] = conn
				connections[fields[2]] = conn
			}
		}
	}
	for _, unit := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
		if _, ok := units[unit]; !ok {
			t.Fatalf("the manifest carries no unit line for %s", unit)
		}
		if _, ok := connections[unit]; !ok {
			t.Fatalf("the manifest carries no connection line for unit %s", unit)
		}
	}
	return units, connections
}

// messageLoadUnit parses one unit's schema files off disk, exactly as the Go
// leg's goldens load them.
func messageLoadUnit(t *testing.T, root string, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		abs := filepath.Join(root, p)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(abs)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(abs, e.Name()))
				}
			}
		} else {
			names = append(names, abs)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path:  name,
				Name:  filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data,
				AST:   f,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// writeOtherBuildAnnouncement writes the unit's own vocabulary announced under
// another build version: a peer whose record this reader does not hold, so a
// bound read or write judges each value by its announced shape alone, never by
// the root's record (docs/SPEC-TABLES.md §3.3). Probes about a value's shape
// over a whole corpus unit read through it.
func writeOtherBuildAnnouncement(t *testing.T, u *ir.Unit, file string) {
	t.Helper()
	ann := depthBoundaryEncodeAnnouncement(ir.BuildVersion(u)^1, ir.TableVocabularyBytes(tablewire.MessageVocabulary(u)))
	if err := os.WriteFile(file, ann, 0o600); err != nil {
		t.Fatal(err)
	}
}

// runMessageProbe generates the table library and runs one generated probe
// against the announcement surface alone. The surface is SELF-CONTAINED: the
// `TableMessage` module names no packet module and no fixed-form module, so the
// probe loads that one file — the way a consumer that imports only the message
// surface would — and none of the unit's union-over-fixed-table struct cycle.
// `file` is the absolute path of the announcement under test; the forged
// goldens live beside it in testdata/wire/tables.
func runMessageProbe(t *testing.T, u *ir.Unit, file, body string) (string, error) {
	slowtest.Gate(t, "an external toolchain")

	t.Helper()
	elixirBin := elixirBinary(t)
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex; files are %v", TableMessageModule, keysOf(tables))
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TableMessageModule+".ex"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	ns := ir.GoExportName(u.Package)
	goldenDir := filepath.Dir(file)
	probe := fmt.Sprintf(`# GENERATED BY internal/codegen/elixirtable/messageform_announce_test.go.
defmodule Probe do
  import Bitwise
  alias %[1]s.TableMessage, as: M

  def file, do: %[2]s
  def golden(name), do: Path.join(%[3]s, name)

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def run do
%[4]s
    if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
  end
end

Probe.run()
`, ns, elixirString(file), elixirString(goldenDir), indentBody(body))
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(elixirBin, "-r", TableMessageModule+".ex", "probe.exs")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}
