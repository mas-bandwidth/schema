package cpptable

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type cppNonCanonicalRow struct {
	file             string
	form             string
	spelling         string
	verdict          string
	unitPath         string
	announcementPath string
	root             string
}

func loadCppNonCanonicalManifest(t *testing.T, repo, vectorDir string) []cppNonCanonicalRow {
	t.Helper()
	f, err := os.Open(filepath.Join(vectorDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("MANIFEST.tsv: %v", err)
	}
	defer f.Close()

	var rows []cppNonCanonicalRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) != 4 {
			t.Fatalf("MANIFEST.tsv: %d fields, want 4: %q", len(cells), line)
		}
		if cells[0] == "file" && cells[1] == "form" && cells[2] == "case" && cells[3] == "verdict" {
			continue
		}
		row := cppNonCanonicalRow{
			file:     cells[0],
			form:     cells[1],
			spelling: cells[2],
			verdict:  cells[3],
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv: %s: verdict %q, want read or refused", row.file, row.verdict)
		}
		switch row.form {
		case "variable", "message", "announcement":
		default:
			t.Fatalf("MANIFEST.tsv: %s: unknown form %q", row.file, row.form)
		}
		if strings.HasPrefix(row.file, "../message-one-encoding/") {
			row.unitPath = filepath.Join(repo, "test", "message-one-encoding", "One.schema")
			row.announcementPath = filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin")
			row.root = "One"
		} else {
			row.unitPath = filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema")
			row.announcementPath = filepath.Join(vectorDir, "announcement.bin")
			if row.form == "variable" {
				row.root = "Root"
			} else {
				row.root = "Message"
			}
		}
		if _, err := os.Stat(filepath.Join(vectorDir, row.file)); err != nil {
			t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("MANIFEST.tsv: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vector rows")
	}
	return rows
}

func cppMeasureAnswersRefusal(spelling string) bool {
	switch spelling {
	case "unused-id-entry", "id-table-out-of-order", "empty-node-table", "node-table-out-of-place":
		return true
	default:
		return false
	}
}

// TestCppReadsRefuseNonCanonical holds the C++ variable-form and message-form
// readers to docs/SPEC-TABLES.md §3, §3.1, §3.3, §4 and docs/rulings.json
// reads-refuse-non-canonical over every row in testdata/wire/non-canonical/MANIFEST.tsv.
func TestCppReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	vectorDir := filepath.Join(repo, "testdata", "wire", "non-canonical")
	rows := loadCppNonCanonicalManifest(t, repo, vectorDir)

	byUnit := map[string][]cppNonCanonicalRow{}
	var unitOrder []string
	for _, row := range rows {
		if _, seen := byUnit[row.unitPath]; !seen {
			unitOrder = append(unitOrder, row.unitPath)
		}
		byUnit[row.unitPath] = append(byUnit[row.unitPath], row)
	}

	reached := make(map[string]bool, len(rows))

	for _, unitPath := range unitOrder {
		unitRows := byUnit[unitPath]
		u := loadUnit(t, unitPath)

		var probe strings.Builder
		for i, row := range unitRows {
			wirePath := filepath.Join(vectorDir, row.file)
			switch row.form {
			case "variable":
				fmt.Fprintf(&probe, `
    {
        std::vector<uint8_t> wire;
        if ( !slurp( %q, wire ) ) { printf( "ERR %d slurp\n" ); return 2; }
        const uint8_t * buf = wire.data();
        const int64_t n = (int64_t) wire.size();

        int64_t attr = 0;
        const int64_t need = %sLoadMeasure( buf, n, &attr, NULL );
        const int64_t alloc = ( need > 0 ? need : 4096 );

        // 1. Plain region load (%sLoad)
        std::vector<uint8_t> reg1( (size_t) alloc + 64, 0 );
        uint8_t * aligned1 = reg1.data();
        while ( ( (uintptr_t) aligned1 & 63 ) != 0 ) { aligned1++; }
        TableReport rep1;
        const %s * r1 = %sLoad( aligned1, alloc, buf, n, &rep1 );
        bool plain_ok = ( r1 != NULL && !rep1.damaged && !rep1.refused && !rep1.missing_required );
        bool plain_same = false;
        if ( plain_ok )
        {
            std::vector<uint8_t> saved( (size_t) n + 256, 0 );
            const int64_t sn = %sSave( r1, saved.data(), (int64_t) saved.size() );
            plain_same = ( sn == n && memcmp( saved.data(), buf, (size_t) n ) == 0 );
        }

        // 2. Builder load (%sLoadBuilder)
        %sBuilder builder;
        TableReport rep2;
        bool builder_ok = %sLoadBuilder( builder, buf, n, &rep2 ) && !rep2.damaged && !rep2.refused && !rep2.missing_required;
        bool builder_same = false;
        if ( builder_ok )
        {
            std::vector<uint8_t> saved( (size_t) n + 256, 0 );
            const int64_t sn = %sSave( builder, saved.data(), (int64_t) saved.size() );
            builder_same = ( sn == n && memcmp( saved.data(), buf, (size_t) n ) == 0 );
        }

        // 3. Strict allocating read (%sReadStrict)
        const %s * rs = %sReadStrict( buf, n, NULL );
        bool strict_ok = ( rs != NULL );
        bool strict_same = false;
        if ( strict_ok )
        {
            std::vector<uint8_t> saved( (size_t) n + 256, 0 );
            const int64_t sn = %sSave( rs, saved.data(), (int64_t) saved.size() );
            strict_same = ( sn == n && memcmp( saved.data(), buf, (size_t) n ) == 0 );
            table_release( TableDefaultAllocator(), (void *) rs );
        }

        // 4. Retaining region load (%sLoadRetain)
        std::vector<uint8_t> reg4( (size_t) alloc + 64, 0 );
        uint8_t * aligned4 = reg4.data();
        while ( ( (uintptr_t) aligned4 & 63 ) != 0 ) { aligned4++; }
        uint8_t ret_bytes[4096];
        TableRetain::Id ret_ids[128];
        TableRetain retain;
        retain.bytes = ret_bytes;
        retain.capacity = (int64_t) sizeof( ret_bytes );
        retain.ids = ret_ids;
        retain.id_capacity = 128;
        TableReport rep4;
        const %s * r4 = %sLoadRetain( aligned4, alloc, buf, n, &retain, &rep4 );
        bool retain_ok = ( r4 != NULL && !rep4.damaged && !rep4.refused && !rep4.missing_required );
        bool retain_same = false;
        if ( retain_ok )
        {
            std::vector<uint8_t> saved( (size_t) n + 256, 0 );
            TableReport srep;
            const int64_t sn = %sSaveRetain( r4, &retain, saved.data(), (int64_t) saved.size(), &srep );
            retain_same = ( sn == n && memcmp( saved.data(), buf, (size_t) n ) == 0 );
        }

        printf( "ROW %d measure=%%lld plain=%%d plain_same=%%d builder=%%d builder_same=%%d strict=%%d strict_same=%%d retain=%%d retain_same=%%d\n",
            (long long) need,
            plain_ok ? 1 : 0, plain_same ? 1 : 0,
            builder_ok ? 1 : 0, builder_same ? 1 : 0,
            strict_ok ? 1 : 0, strict_same ? 1 : 0,
            retain_ok ? 1 : 0, retain_same ? 1 : 0 );
    }
`, wirePath, i,
					row.root,
					row.root, row.root, row.root, row.root,
					row.root, row.root, row.root, row.root,
					row.root, row.root, row.root, row.root,
					row.root, row.root, row.root, row.root,
					i)

			case "announcement":
				fmt.Fprintf(&probe, `
    {
        std::vector<uint8_t> wire;
        if ( !slurp( %q, wire ) ) { printf( "ERR %d slurp\n" ); return 2; }
        TableMessageEntry entries[kTableMessageEntriesHere + 16];
        TableVocabulary vocab( entries, kTableMessageEntriesHere + 16 );
        TableReport rep;
        const bool ok_read = AnnounceRead( vocab, wire.data(), (int64_t) wire.size(), &rep ) && !rep.damaged && !rep.refused;
        std::vector<uint8_t> own( (size_t) AnnounceMeasure(), 0 );
        const int64_t wrote = Announce( own.data(), (int64_t) own.size() );
        const bool same = ( wrote == (int64_t) wire.size() && memcmp( own.data(), wire.data(), wire.size() ) == 0 );
        printf( "ROW %d announce=%%d same=%%d\n", ok_read ? 1 : 0, same ? 1 : 0 );
    }
`, wirePath, i, i)

			case "message":
				fmt.Fprintf(&probe, `
    {
        std::vector<uint8_t> ann;
        std::vector<uint8_t> wire;
        if ( !slurp( %q, ann ) || !slurp( %q, wire ) ) { printf( "ERR %d slurp\n" ); return 2; }
        TableMessageEntry entries[kTableMessageEntriesHere + 16];
        TableVocabulary vocab( entries, kTableMessageEntriesHere + 16 );
        TableReport arep;
        if ( !AnnounceRead( vocab, ann.data(), (int64_t) ann.size(), &arep ) || arep.damaged || arep.refused )
        {
            printf( "ERR %d announce_failed\n" );
            return 2;
        }
        %s val;
        %sReset( val );
        int64_t count = 1;
        TableReport rep;
        const bool ok_read = %sLoadMessages( &val, &count, vocab, wire.data(), (int64_t) wire.size(), &rep ) && !rep.damaged && !rep.refused && count == 1;
        bool same = false;
        if ( ok_read )
        {
            std::vector<uint8_t> saved( wire.size() + 64, 0 );
            TableReport srep;
            const int64_t sn = %sSaveMessages( &val, 1, saved.data(), (int64_t) saved.size(), &srep );
            same = ( sn == (int64_t) wire.size() && memcmp( saved.data(), wire.data(), wire.size() ) == 0 );
        }
        printf( "ROW %d message=%%d count=%%lld same=%%d\n", ok_read ? 1 : 0, (long long) count, same ? 1 : 0 );
    }
`, row.announcementPath, wirePath, i, i, row.root, row.root, row.root, row.root, i)
			}
		}

		out, err := cppHeldRun(t, u, probe.String())
		if err != nil {
			t.Fatalf("%s: probe failed: %v\n%s", unitPath, err, out)
		}

		lines := map[int]map[string]string{}
		for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "ROW" {
				t.Fatalf("%s: unexpected probe line: %q", unitPath, line)
			}
			var idx int
			if _, err := fmt.Sscanf(fields[1], "%d", &idx); err != nil {
				t.Fatalf("%s: bad row index in %q: %v", unitPath, line, err)
			}
			kv := map[string]string{}
			for _, tok := range fields[2:] {
				k, v, ok := strings.Cut(tok, "=")
				if ok {
					kv[k] = v
				}
			}
			lines[idx] = kv
		}

		for i, row := range unitRows {
			kv, ok := lines[i]
			if !ok {
				t.Errorf("%s: probe produced no result", row.file)
				continue
			}
			reached[row.file] = true
			switch row.form {
			case "variable":
				var measure int64
				_, _ = fmt.Sscanf(kv["measure"], "%d", &measure)
				if row.verdict == "read" {
					if measure <= 0 {
						t.Errorf("%s (%s): LoadMeasure = %d, want > 0", row.file, row.spelling, measure)
					}
					for _, entry := range []string{"plain", "builder", "strict", "retain"} {
						if kv[entry] != "1" {
							t.Errorf("%s (%s): %s refused canonical input", row.file, row.spelling, entry)
						} else if kv[entry+"_same"] != "1" {
							t.Errorf("%s (%s): %s did not re-save byte-identically", row.file, row.spelling, entry)
						}
					}
				} else {
					if cppMeasureAnswersRefusal(row.spelling) && measure >= 0 {
						t.Errorf("%s (%s): LoadMeasure = %d, want < 0", row.file, row.spelling, measure)
					}
					for _, entry := range []string{"plain", "builder", "strict", "retain"} {
						if kv[entry] != "0" {
							t.Errorf("%s (%s): %s accepted non-canonical input (want refused)", row.file, row.spelling, entry)
						}
					}
				}
			case "announcement":
				if row.verdict == "read" {
					if kv["announce"] != "1" {
						t.Errorf("%s: AnnounceRead refused canonical announcement", row.file)
					}
					if kv["same"] != "1" {
						t.Errorf("%s: Announce bytes do not match announcement.bin", row.file)
					}
				} else if kv["announce"] != "0" {
					t.Errorf("%s: AnnounceRead accepted non-canonical announcement", row.file)
				}
			case "message":
				if row.verdict == "read" {
					if kv["message"] != "1" {
						t.Errorf("%s (%s): LoadMessages refused canonical batch", row.file, row.spelling)
					} else if kv["same"] != "1" {
						t.Errorf("%s (%s): SaveMessages did not reproduce canonical batch", row.file, row.spelling)
					}
				} else {
					if kv["message"] != "0" || kv["count"] != "0" {
						t.Errorf("%s (%s): LoadMessages accepted non-canonical batch (ok=%s count=%s, want refused)", row.file, row.spelling, kv["message"], kv["count"])
					}
				}
			}
		}
	}

	for _, row := range rows {
		if !reached[row.file] {
			t.Errorf("MANIFEST.tsv row %s was not reached", row.file)
		}
	}

	t.Run("clauses", testCppNonCanonicalClauses)
}

func appendCppTestIDTable(body []byte, ids ...uint64) []byte {
	out := make([]byte, 0, 1+len(body)+8*len(ids)+8)
	out = append(out, ir.TableWireForm)
	out = append(out, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
	return out
}

func testCppNonCanonicalClauses(t *testing.T) {
	t.Parallel()

	const clausesSchema = `package clauses

enum Slot { A, B }
enum Mode { Alpha, Beta }
union Pick {
    Num int32
    Flag bool
}

table Leaf {
    v int32
}

fixed table Sub {
    v int32
}

table PtrRoot {
    a *Leaf
    b *Leaf
    items [..2]*Leaf
    picks [..2]Pick
}

table ReqRoot {
    x int32 | required
    m Mode = Beta | required
    p *Leaf | required
    u Pick | required
}

fixed table OptMsg {
    x ?int32
    m ?Mode
    sub ?Sub
    f float32
    fixed_ints [2]int32
    fixed_subs [2]Sub
    keyed [Slot]int32
}

table VarContainers {
    fixed_ints [2]int32
    keyed [Slot]int32
}

table EvolReader {
    a int32
    b int64
}

fixed table EvolMsgReader {
    a int32
    b int64
}
`
	u := loadUnitFromSource(t, clausesSchema)

	mClauses := tabletext.NewModel(u)
	instArrNull := mClauses.New(mClauses.Lookup("PtrRoot"))
	var repArr tabletext.Report
	if !mClauses.Read(instArrNull, []byte(`{"items": [null, {"v": 9}], "picks": [{}, {"Num": 5}]}`), &repArr) {
		t.Fatalf("clauses array null read: %+v", repArr)
	}
	arrayNullElemWire, err := tablewire.Encode(mClauses, instArrNull)
	if err != nil {
		t.Fatalf("clauses array null encode: %v", err)
	}

	// Sender unit with int16 widened into int64 and an extra unknown field appended at the end.
	const senderSchema = `package clauses

table EvolReader {
    a int32
    b int16
    extra int32
}

fixed table EvolMsgReader {
    a int32
    b int16
    extra int32
}
`
	uSender := loadUnitFromSource(t, senderSchema)
	mSender := tabletext.NewModel(uSender)

	instVar := mSender.New(mSender.Lookup("EvolReader"))
	var rep tabletext.Report
	if !mSender.Read(instVar, []byte(`{"a": 5, "b": 12, "extra": 99}`), &rep) {
		t.Fatalf("sender variable read: %+v", rep)
	}
	v2VarWire, err := tablewire.Encode(mSender, instVar)
	if err != nil {
		t.Fatalf("sender variable encode: %v", err)
	}

	const senderWideSchema = `package clauses

table EvolReader {
    a int32
    b int64
    extra int32
}
`
	uSenderWide := loadUnitFromSource(t, senderWideSchema)
	mSenderWide := tabletext.NewModel(uSenderWide)
	instVarWide := mSenderWide.New(mSenderWide.Lookup("EvolReader"))
	var repWide tabletext.Report
	if !mSenderWide.Read(instVarWide, []byte(`{"a": 5, "b": 12, "extra": 99}`), &repWide) {
		t.Fatalf("sender wide read: %+v", repWide)
	}
	v2VarRetainSavedWire, err := tablewire.Encode(mSenderWide, instVarWide)
	if err != nil {
		t.Fatalf("sender wide encode: %v", err)
	}

	// Form 1 widened scalar explicitly stored at the reader's declared default (0).
	widenedDefVarWire := appendCppTestIDTable(
		[]byte{
			0x01, byte(ir.TableKindI32), 0x05, 0x00, 0x00, 0x00,
			0x02, byte(ir.TableKindI16), 0x00, 0x00,
			0x00,
		},
		ir.TableWireId("a"),
		ir.TableWireId("b"),
	)

	// Form 1 explicit null pointer (b = node_index 0) alongside a valid non-empty node table (a -> Leaf{v: 7}).
	explicitNullPtrWire := appendCppTestIDTable(
		[]byte{
			0x01, byte(ir.TableKindPointer), 0x02, // a -> node 2
			0x02, byte(ir.TableKindPointer), 0x00, // b -> explicit null pointer
			0x03, byte(ir.TableKindString), 0x0a, // node table, len=10
			0x01,       // node_count = 1
			0x04, 0x07, // type_ref = 4 (Leaf), rec_len = 7
			0x05, byte(ir.TableKindI32), 0x07, 0x00, 0x00, 0x00, 0x00, // v = 7, end
			0x00, // root end
		},
		ir.TableWireId("a"),
		ir.TableWireId("b"),
		ir.TableNodeWireId,
		ir.TableWireId("Leaf"),
		ir.TableWireId("v"),
	)

	// Form 1 fixed [2]int32 all-default on wire.
	allDefFixedIntsVar := appendCppTestIDTable(
		[]byte{
			0x01, byte(ir.TableKindArray), 0x0a, // fixed_ints, len = 10
			byte(ir.TableKindI32), 0x02,
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
			0x00,
		},
		ir.TableWireId("fixed_ints"),
	)

	// Form 1 keyed [Slot]int32 out of enum declaration order (B before A) and slot at default (A = 0).
	keyedOutOfOrderVar := appendCppTestIDTable(
		[]byte{
			0x01, byte(ir.TableKindKeyed), 0x0e, // keyed, len = 14
			byte(ir.TableKindI32), 0x02,
			0x02, 0x04, 0x04, 0x00, 0x00, 0x00, // B = 4
			0x03, 0x04, 0x03, 0x00, 0x00, 0x00, // A = 3
			0x00,
		},
		ir.TableWireId("keyed"),
		ir.TableWireId("B"),
		ir.TableWireId("A"),
	)
	keyedSlotDefaultVar := appendCppTestIDTable(
		[]byte{
			0x01, byte(ir.TableKindKeyed), 0x08, // keyed, len = 8
			byte(ir.TableKindI32), 0x01,
			0x02, 0x04, 0x00, 0x00, 0x00, 0x00, // A = 0 (declared default)
			0x00,
		},
		ir.TableWireId("keyed"),
		ir.TableWireId("A"),
	)

	// Form 2 unknown + widened good batch, and widened-at-default non-canonical batch.
	v2MsgAnn := tablewire.Announce(uSender)
	instMsgGood := mSender.New(mSender.Lookup("EvolMsgReader"))
	rep = tabletext.Report{}
	if !mSender.Read(instMsgGood, []byte(`{"a": 5, "extra": 99, "b": 12}`), &rep) {
		t.Fatalf("sender message read: %+v", rep)
	}
	v2MsgGood, err := tablewire.EncodeMessage(mSender, instMsgGood)
	if err != nil {
		t.Fatalf("sender message encode: %v", err)
	}
	instMsgOne := mSender.New(mSender.Lookup("EvolMsgReader"))
	rep = tabletext.Report{}
	if !mSender.Read(instMsgOne, []byte(`{"a": 5, "b": 257}`), &rep) {
		t.Fatalf("sender message one read: %+v", rep)
	}
	v2MsgOne, err := tablewire.EncodeMessage(mSender, instMsgOne)
	if err != nil {
		t.Fatalf("sender message one encode: %v", err)
	}

	probe := fmt.Sprintf(`
    auto check_var = []( const char * label, const uint8_t * buf, int64_t n, auto measure_fn, auto load_fn, auto builder_fn, auto strict_fn, auto retain_fn, auto save_fn, auto save_retain_fn, bool want_read, bool check_resave ) -> bool {
        int64_t attr = 0;
        const int64_t need = measure_fn( buf, n, &attr );
        const int64_t alloc = ( need > 0 ? need : 4096 );

        std::vector<uint8_t> reg1( (size_t) alloc + 64, 0 );
        uint8_t * aligned1 = reg1.data();
        while ( ( (uintptr_t) aligned1 & 63 ) != 0 ) { aligned1++; }
        TableReport rep1;
        const auto * r1 = load_fn( aligned1, alloc, buf, n, &rep1 );
        const bool plain_ok = ( r1 != NULL && !rep1.damaged && !rep1.refused && !rep1.missing_required );

        decltype( builder_fn( buf, n ) ) b_unused;
        (void) b_unused;
        const bool strict_ok = [&]() {
            const auto * rs = strict_fn( buf, n );
            if ( rs == NULL ) { return false; }
            bool ok = true;
            if ( check_resave )
            {
                std::vector<uint8_t> saved( (size_t) n + 256, 0 );
                const int64_t sn = save_fn( rs, saved.data(), (int64_t) saved.size() );
                ok = ( sn == n && memcmp( saved.data(), buf, (size_t) n ) == 0 );
            }
            table_release( TableDefaultAllocator(), (void *) rs );
            return ok;
        }();

        const bool builder_ok = builder_fn( buf, n );

        std::vector<uint8_t> reg4( (size_t) alloc + 64, 0 );
        uint8_t * aligned4 = reg4.data();
        while ( ( (uintptr_t) aligned4 & 63 ) != 0 ) { aligned4++; }
        uint8_t ret_bytes[4096];
        TableRetain::Id ret_ids[128];
        TableRetain retain;
        retain.bytes = ret_bytes;
        retain.capacity = (int64_t) sizeof( ret_bytes );
        retain.ids = ret_ids;
        retain.id_capacity = 128;
        TableReport rep4;
        const auto * r4 = retain_fn( aligned4, alloc, buf, n, &retain, &rep4 );
        bool retain_ok = ( r4 != NULL && !rep4.damaged && !rep4.refused && !rep4.missing_required );
        if ( retain_ok && check_resave )
        {
            std::vector<uint8_t> saved( (size_t) n + 256, 0 );
            TableReport srep;
            const int64_t sn = save_retain_fn( r4, &retain, saved.data(), (int64_t) saved.size(), &srep );
            retain_ok = ( sn == n && memcmp( saved.data(), buf, (size_t) n ) == 0 );
        }

        if ( plain_ok != want_read || builder_ok != want_read || strict_ok != want_read || retain_ok != want_read )
        {
            printf( "FAIL %%s: want=%%d plain=%%d builder=%%d strict=%%d retain=%%d need=%%lld\n",
                label, want_read ? 1 : 0, plain_ok ? 1 : 0, builder_ok ? 1 : 0, strict_ok ? 1 : 0, retain_ok ? 1 : 0, (long long) need );
            return false;
        }
        return true;
    };

    // 1. Explicit null pointer alongside valid non-empty node table: measure > 0, all 4 loads refuse.
    {
        static const uint8_t wire[] = { %s };
        const int64_t n = (int64_t) sizeof( wire );
        if ( PtrRootLoadMeasure( wire, n ) <= 0 ) { printf( "FAIL explicit_null_ptr: measure <= 0\n" ); return 1; }
        auto bfn = []( const uint8_t * b, int64_t len ) {
            PtrRootBuilder bld; TableReport r;
            return PtrRootLoadBuilder( bld, b, len, &r ) && !r.damaged && !r.refused && !r.missing_required;
        };
        if ( !check_var( "explicit_null_ptr", wire, n,
            []( const uint8_t * b, int64_t l, int64_t * a ) { return PtrRootLoadMeasure( b, l, a, NULL ); },
            PtrRootLoad, bfn,
            []( const uint8_t * b, int64_t l ) { return PtrRootReadStrict( b, l, NULL ); },
            PtrRootLoadRetain, []( const PtrRoot * r, uint8_t * b, int64_t l ) { return PtrRootSave( r, b, l ); }, PtrRootSaveRetain, false, false ) ) { return 1; }
    }

    // 2. [..2]*Leaf with a null element [null, Leaf{v: 9}] is canonical and reads + re-saves byte-identically across all 4 entries.
    {
        static const uint8_t buf[] = { %s };
        const int64_t n = (int64_t) sizeof( buf );
        auto bfn = []( const uint8_t * b, int64_t len ) {
            PtrRootBuilder lb; TableReport r;
            return PtrRootLoadBuilder( lb, b, len, &r ) && !r.damaged && !r.refused && !r.missing_required;
        };
        if ( !check_var( "array_null_elem", buf, n,
            []( const uint8_t * b, int64_t l, int64_t * a ) { return PtrRootLoadMeasure( b, l, a, NULL ); },
            PtrRootLoad, bfn,
            []( const uint8_t * b, int64_t l ) { return PtrRootReadStrict( b, l, NULL ); },
            PtrRootLoadRetain, []( const PtrRoot * r, uint8_t * b, int64_t l ) { return PtrRootSave( r, b, l ); }, PtrRootSaveRetain, true, true ) ) { return 1; }
    }

    // 3. | required scalar at default, null pointer, and None union are canonical and read across all 4 entries.
    {
        ReqRootBuilder bld;
        uint8_t buf[512];
        const int64_t n = ReqRootSave( bld, buf, (int64_t) sizeof( buf ) );
        if ( n <= 0 ) { printf( "FAIL required_defaults: save failed\n" ); return 1; }
        auto bfn = []( const uint8_t * b, int64_t len ) {
            ReqRootBuilder lb; TableReport r;
            return ReqRootLoadBuilder( lb, b, len, &r ) && !r.damaged && !r.refused && !r.missing_required;
        };
        if ( !check_var( "required_defaults", buf, n,
            []( const uint8_t * b, int64_t l, int64_t * a ) { return ReqRootLoadMeasure( b, l, a, NULL ); },
            ReqRootLoad, bfn,
            []( const uint8_t * b, int64_t l ) { return ReqRootReadStrict( b, l, NULL ); },
            ReqRootLoadRetain, []( const ReqRoot * r, uint8_t * b, int64_t l ) { return ReqRootSave( r, b, l ); }, ReqRootSaveRetain, true, true ) ) { return 1; }
    }

    // 4. Unknown + widened fields in Form 1 read and SaveRetain preserves unknown field; widened-at-default refuses.
    {
        static const uint8_t good_wire[] = { %s };
        auto bfn = []( const uint8_t * b, int64_t len ) {
            EvolReaderBuilder lb; TableReport r;
            if ( !EvolReaderLoadBuilder( lb, b, len, &r ) || r.damaged || r.refused || r.missing_required ) { return false; }
            return lb.GetRoot()->a == 5 && lb.GetRoot()->b == 12;
        };
        if ( !check_var( "evol_var_good", good_wire, (int64_t) sizeof( good_wire ),
            []( const uint8_t * b, int64_t l, int64_t * a ) { return EvolReaderLoadMeasure( b, l, a, NULL ); },
            EvolReaderLoad, bfn,
            []( const uint8_t * b, int64_t l ) { return EvolReaderReadStrict( b, l, NULL ); },
            EvolReaderLoadRetain, []( const EvolReader * r, uint8_t * b, int64_t l ) { return EvolReaderSave( r, b, l ); }, EvolReaderSaveRetain, true, false ) ) { return 1; }

        // Also check SaveRetain byte-identity against canonical widened+retained wire!
        static const uint8_t want_retain_wire[] = { %s };
        alignas( 64 ) uint8_t reg[4096];
        uint8_t ret_bytes[4096];
        TableRetain::Id ret_ids[64];
        TableRetain retain;
        retain.bytes = ret_bytes; retain.capacity = (int64_t) sizeof( ret_bytes );
        retain.ids = ret_ids; retain.id_capacity = 64;
        TableReport rr;
        const EvolReader * kept = EvolReaderLoadRetain( reg, (int64_t) sizeof( reg ), good_wire, (int64_t) sizeof( good_wire ), &retain, &rr );
        uint8_t saved[512];
        TableReport sr;
        const int64_t sn = EvolReaderSaveRetain( kept, &retain, saved, (int64_t) sizeof( saved ), &sr );
        if ( sn != (int64_t) sizeof( want_retain_wire ) || memcmp( saved, want_retain_wire, sizeof( want_retain_wire ) ) != 0 )
        {
            printf( "FAIL evol_var_good: SaveRetain mismatch\n" );
            return 1;
        }

        static const uint8_t bad_wire[] = { %s };
        auto bfn_bad = []( const uint8_t * b, int64_t len ) {
            EvolReaderBuilder lb; TableReport r;
            return EvolReaderLoadBuilder( lb, b, len, &r ) && !r.damaged && !r.refused && !r.missing_required;
        };
        if ( !check_var( "evol_var_widened_default", bad_wire, (int64_t) sizeof( bad_wire ),
            []( const uint8_t * b, int64_t l, int64_t * a ) { return EvolReaderLoadMeasure( b, l, a, NULL ); },
            EvolReaderLoad, bfn_bad,
            []( const uint8_t * b, int64_t l ) { return EvolReaderReadStrict( b, l, NULL ); },
            EvolReaderLoadRetain, []( const EvolReader * r, uint8_t * b, int64_t l ) { return EvolReaderSave( r, b, l ); }, EvolReaderSaveRetain, false, false ) ) { return 1; }
    }

    // 5. ?T present at default, -0.0f plain float, [2]Sub all-default, [2]int32 all-default, and [Slot]int32 in Form 1 & Form 2.
    {
        uint8_t ann[4096];
        const int64_t ann_n = Announce( ann, (int64_t) sizeof( ann ) );
        TableMessageEntry entries[kTableMessageEntriesHere + 16];
        TableVocabulary vocab( entries, kTableMessageEntriesHere + 16 );
        TableReport arep;
        if ( ann_n <= 0 || !AnnounceRead( vocab, ann, ann_n, &arep ) ) { printf( "FAIL clauses announce\n" ); return 1; }

        // 5a. ?int32 present at 0, ?Mode present at Alpha, ?Sub present at {0}, [2]Sub all-default, f = -0.0f
        OptMsg m;
        OptMsgReset( m );
        m.x_present = true;
        m.x = 0;
        m.m_present = true;
        m.m = Mode::Alpha;
        m.sub_present = true;
        m.sub.v = 0;
        m.f = table_bits_to_float( 0x80000000u );

        uint8_t var_buf[512];
        const int64_t vn = OptMsgSave( m, var_buf, (int64_t) sizeof( var_buf ) );
        OptMsg vout; OptMsgReset( vout );
        TableReport vrep;
        if ( vn <= 0 || !OptMsgLoad( vout, var_buf, vn, &vrep ) || vrep.damaged || vrep.refused ||
             !vout.x_present || vout.x != 0 || !vout.m_present || vout.m != Mode::Alpha ||
             !vout.sub_present || vout.sub.v != 0 ||
             table_float_to_bits( vout.f ) != 0x80000000u )
        {
            printf( "FAIL opt_minuszero_var\n" );
            return 1;
        }

        uint8_t msg_buf[512];
        TableReport msrep;
        const int64_t mn = OptMsgSaveMessages( &m, 1, msg_buf, (int64_t) sizeof( msg_buf ), &msrep );
        OptMsg mout; OptMsgReset( mout );
        int64_t mcnt = 1;
        TableReport mlrep;
        if ( mn <= 0 || !OptMsgLoadMessages( &mout, &mcnt, vocab, msg_buf, mn, &mlrep ) || mcnt != 1 ||
             !mout.x_present || mout.x != 0 || !mout.m_present || mout.m != Mode::Alpha ||
             !mout.sub_present || mout.sub.v != 0 ||
             table_float_to_bits( mout.f ) != 0x80000000u )
        {
            printf( "FAIL opt_minuszero_msg\n" );
            return 1;
        }

        // 5b. Form 1 [2]int32 all-default, [Slot]int32 out-of-order, and [Slot]int32 slot-at-default all refuse.
        static const uint8_t bad_fixed_ints[] = { %s };
        static const uint8_t bad_keyed_order[] = { %s };
        static const uint8_t bad_keyed_def[] = { %s };
        for ( auto pair : { std::pair<const uint8_t *, size_t>{ bad_fixed_ints, sizeof( bad_fixed_ints ) },
                            std::pair<const uint8_t *, size_t>{ bad_keyed_order, sizeof( bad_keyed_order ) },
                            std::pair<const uint8_t *, size_t>{ bad_keyed_def, sizeof( bad_keyed_def ) } } )
        {
            OptMsg tmp; OptMsgReset( tmp );
            TableReport tr;
            if ( OptMsgLoad( tmp, pair.first, (int64_t) pair.second, &tr ) && !tr.damaged && !tr.refused )
            {
                printf( "FAIL form1_container_noncanon accepted\n" );
                return 1;
            }
            auto vbfn = []( const uint8_t * b, int64_t len ) {
                VarContainersBuilder lb; TableReport r;
                return VarContainersLoadBuilder( lb, b, len, &r ) && !r.damaged && !r.refused && !r.missing_required;
            };
            if ( !check_var( "form1_container_noncanon", pair.first, (int64_t) pair.second,
                []( const uint8_t * b, int64_t l, int64_t * a ) { return VarContainersLoadMeasure( b, l, a, NULL ); },
                VarContainersLoad, vbfn,
                []( const uint8_t * b, int64_t l ) { return VarContainersReadStrict( b, l, NULL ); },
                VarContainersLoadRetain, []( const VarContainers * r, uint8_t * b, int64_t l ) { return VarContainersSave( r, b, l ); }, VarContainersSaveRetain, false, false ) )
            {
                return 1;
            }
        }
    }

    // 6. Form 2 unknown + widened batch reads; widened-at-default refuses.
    {
        static const uint8_t sann[] = { %s };
        static const uint8_t sgood[] = { %s };
        static const uint8_t sone[] = { %s };
        TableMessageEntry sentries[64];
        TableVocabulary svocab( sentries, 64 );
        TableReport sarep;
        if ( !AnnounceRead( svocab, sann, (int64_t) sizeof( sann ), &sarep ) )
        {
            printf( "FAIL sender_announce_read\n" );
            return 1;
        }
        EvolMsgReader val; EvolMsgReaderReset( val );
        int64_t cnt = 1;
        TableReport rgood;
        if ( !EvolMsgReaderLoadMessages( &val, &cnt, svocab, sgood, (int64_t) sizeof( sgood ), &rgood ) ||
             cnt != 1 || val.a != 5 || val.b != 12 )
        {
            printf( "FAIL evol_msg_good\n" );
            return 1;
        }

        // Mutate b's non-zero int16 payload (0x0101) in sone to 0x0000 (widened field at declared default).
        // Find the bit offset by replacing the two 0x0101 payload bytes or scanning 16-bit windows:
        // sone encodes count=1 (8 bits), slot(a)=1 (2 bits), a=5 (32 bits), slot(b)=3 (2 bits), b=0x0101 (16 bits), term=0 (2 bits).
        uint8_t sbad[sizeof( sone )];
        memcpy( sbad, sone, sizeof( sone ) );
        // Clear bits [44..59] of payload (after byte 0 = 0x02, bit 0 is byte 1):
        // bit 0..7: count-1=0
        // bit 8..9: slot(a)
        // bit 10..41: a=5
        // bit 42..43: slot(b)
        // bit 44..59: b (16 bits) -> zero all 16 bits!
        for ( int bit = 44; bit < 60; bit++ )
        {
            sbad[1 + ( bit >> 3 )] &= uint8_t( ~( 1u << ( bit & 7 ) ) );
        }
        EvolMsgReader bad_val; EvolMsgReaderReset( bad_val );
        int64_t bad_cnt = 1;
        TableReport rbad;
        if ( EvolMsgReaderLoadMessages( &bad_val, &bad_cnt, svocab, sbad, (int64_t) sizeof( sbad ), &rbad ) &&
             !rbad.damaged && !rbad.refused && bad_cnt == 1 )
        {
            printf( "FAIL evol_msg_widened_default accepted\n" );
            return 1;
        }
    }
    printf( "CLAUSES_OK\n" );
`,
		cppBytes(explicitNullPtrWire),
		cppBytes(arrayNullElemWire),
		cppBytes(v2VarWire),
		cppBytes(v2VarRetainSavedWire),
		cppBytes(widenedDefVarWire),
		cppBytes(allDefFixedIntsVar),
		cppBytes(keyedOutOfOrderVar),
		cppBytes(keyedSlotDefaultVar),
		cppBytes(v2MsgAnn),
		cppBytes(v2MsgGood),
		cppBytes(v2MsgOne),
	)

	out, err := cppHeldRun(t, u, probe)
	if err != nil {
		t.Fatalf("clauses probe failed: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("CLAUSES_OK")) {
		t.Fatalf("clauses probe missing CLAUSES_OK:\n%s", out)
	}
}
