package javatable

// messageform_announce_shape_unsigned — every number an announcement spells
// for a shape is held to the unsigned 64-bit value the wire carries
// (docs/SPEC-TABLES.md §3.3 SECURITY), so the Java leg accepts exactly the
// shapes the engine accepts.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const shapeBoundsProbeSrc = `package shapeboundprobe

fixed table Probe
{
    v int32
}
`

type shapeBoundRow struct {
	name   string
	data   []byte
	accept bool
}

// shapeBoundFieldId is the announced id of the probe's one field, so the batch
// probe below meets the field it names rather than an unknown entry.
func shapeBoundFieldId(t *testing.T, u *ir.Unit) uint64 {
	t.Helper()
	for _, e := range tablewire.MessageVocabulary(u) {
		if e.Kind == ir.TableKindI32 {
			return e.Id
		}
	}
	t.Fatal("the probe unit has no int32 field entry")
	return 0
}

// shapeBoundAnnouncement spells one entry of the chosen kind and shape as an
// announcement, by hand: a form-1 file whose body carries the build version and
// the vocabulary under their reserved ids (docs/SPEC-TABLES.md §3.3).
func shapeBoundAnnouncement(id uint64, kind uint8, s ir.TableMessageShape) []byte {
	entry := ir.TableVocabularyEntry{Id: id, Kind: kind, Shape: s}.Encode(nil)
	fields := append(versionField(0x1122334455667788), vocabField(ir.TableKindU8, entry)...)
	return announcement(fields)
}

// shapeBoundBits52 packs bits low-bit-first, the message form's bit order.
func shapeBoundBits(bits []int) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b != 0 {
			out[i/8] |= 1 << (i % 8)
		}
	}
	return out
}

func TestJavaAnnounceShapeBoundsUnsigned(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)

	u := unitFrom(t, shapeBoundsProbeSrc)
	id := shapeBoundFieldId(t, u)

	// A wire value at or above 2^63 is a negative Java long; every bound must
	// hold it as the unsigned value the wire carries.
	var high uint64 = 1 << 63
	var highPlus40 = high + 40
	var highPlus5 = high + 5

	rows := []shapeBoundRow{
		// A ranged integer: bits above the kind's own width are hostile.
		{
			name:   "int32 bits 2^63+40",
			data:   shapeBoundAnnouncement(id, ir.TableKindI32, ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: int64(highPlus40)}),
			accept: false,
		},
		{
			name:   "int32 bits kind width",
			data:   shapeBoundAnnouncement(id, ir.TableKindI32, ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 32}),
			accept: true,
		},
		{
			name:   "int32 bits zero",
			data:   shapeBoundAnnouncement(id, ir.TableKindI32, ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 0}),
			accept: true,
		},
		{
			name:   "int32 bits 33",
			data:   shapeBoundAnnouncement(id, ir.TableKindI32, ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 33}),
			accept: false,
		},
		// A string: max above the int32 storage cap is hostile.
		{
			name:   "string max 2^63",
			data:   shapeBoundAnnouncement(id, ir.TableKindString, ir.TableMessageShape{Max: int64(high)}),
			accept: false,
		},
		{
			name:   "string max 2^31-1",
			data:   shapeBoundAnnouncement(id, ir.TableKindString, ir.TableMessageShape{Max: 1<<31 - 1}),
			accept: true,
		},
		{
			name:   "string max zero",
			data:   shapeBoundAnnouncement(id, ir.TableKindString, ir.TableMessageShape{Max: 0}),
			accept: true,
		},
		{
			name:   "string max 2^31",
			data:   shapeBoundAnnouncement(id, ir.TableKindString, ir.TableMessageShape{Max: 1 << 31}),
			accept: false,
		},
		// An array: min and max above the widest count this form spells are
		// hostile, min compared unsigned beside max.
		{
			name: "array min 2^63 max 2^63+5",
			data: shapeBoundAnnouncement(id, ir.TableKindArray, ir.TableMessageShape{
				Min: int64(high), Max: int64(highPlus5), Elem: ir.TableKindU8,
			}),
			accept: false,
		},
		{
			name: "array max 2^32-1",
			data: shapeBoundAnnouncement(id, ir.TableKindArray, ir.TableMessageShape{
				Min: 0, Max: 0xFFFFFFFF, Elem: ir.TableKindU8,
			}),
			accept: true,
		},
		{
			name: "array min max zero",
			data: shapeBoundAnnouncement(id, ir.TableKindArray, ir.TableMessageShape{
				Min: 0, Max: 0, Elem: ir.TableKindU8,
			}),
			accept: true,
		},
		{
			name: "array max 2^32",
			data: shapeBoundAnnouncement(id, ir.TableKindArray, ir.TableMessageShape{
				Min: 0, Max: 1 << 32, Elem: ir.TableKindU8,
			}),
			accept: false,
		},
	}

	// STEP ONE: the engine on the same bytes is the reference. It must give
	// the verdict the rule names before the Java leg is asked to match it.
	for _, row := range rows {
		var v tablewire.Vocabulary
		var report tabletext.Report
		err := v.AnnounceRead(row.data, &report)
		if got := err == nil; got != row.accept {
			t.Fatalf("reference %s: engine announceRead accepted=%v, want %v (report %+v, err %v)",
				row.name, got, row.accept, report, err)
		}
	}

	// A body for the hostile ranged int32: one field reference to the announced
	// entry, forty value bits, then the zero terminator. It is what a reader
	// that carried the hostile width would load.
	bodyBits := make([]int, 0, 42)
	bodyBits = append(bodyBits, 1)
	bodyBits = append(bodyBits, make([]int, 40)...)
	bodyBits = append(bodyBits, 0)
	body := append([]byte{2, 0}, shapeBoundBits(bodyBits)...)

	var annHex, probe []string
	for _, row := range rows {
		annHex = append(annHex, fmt.Sprintf("%q", hexOf(row.data)))
		isHostileInt32 := row.name == "int32 bits 2^63+40"
		if isHostileInt32 {
			probe = append(probe, "true")
		} else {
			probe = append(probe, "false")
		}
	}
	cls := ir.GoExportName("Probe")
	driver := fmt.Sprintf(`
        final String[] annHex = {
            %s
        };
        final boolean[] probeBatch = { %s };
        final String bodyHex = %q;
        for (int i = 0; i < annHex.length; i++) {
            final TableVocabulary v = new TableVocabulary();
            v.reset(4);
            final TableVocabulary.Report r = new TableVocabulary.Report();
            final boolean accepted = TableVocabulary.announceRead(v, fromHex(annHex[i]), r);
            int loaded = 0;
            if (probeBatch[i]) {
                final %sFixed.Value[] values = new %sFixed.Value[1];
                values[0] = new %sFixed.Value();
                final TableVocabulary.Report lr = new TableVocabulary.Report();
                loaded = %sMessage.loadMessages(values, 1, v, fromHex(bodyHex), lr);
            }
            System.out.println("ROW " + i + " " + (accepted ? "accept" : "reject") + " " + loaded);
        }
`, strings.Join(annHex, ",\n            "), strings.Join(probe, ", "), hexOf(body), cls, cls, cls, cls)

	out := runMessageBatchProbe(t, u, driver)
	got := map[int]string{}
	for line := range strings.SplitSeq(out, "\n") {
		var i int
		var verdict string
		var loaded int
		if n, err := fmt.Sscanf(strings.TrimSpace(line), "ROW %d %s %d", &i, &verdict, &loaded); n == 3 && err == nil {
			got[i] = fmt.Sprintf("%s %d", verdict, loaded)
		}
	}
	for i, row := range rows {
		want := "reject"
		if row.accept {
			want = "accept"
		}
		// Under a refused announcement no batch is loaded; the hostile ranged
		// int32 carries a body a reader that took the width would load.
		want += " 0"
		if got[i] != want {
			t.Errorf("%s: Java announceRead verdict/load = %q, want %q", row.name, got[i], want)
		}
	}
}
