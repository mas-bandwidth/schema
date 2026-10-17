package main

import "cards"

// The Card root's codec row, bound in an init so the shared codec table stays
// untouched. Backing region ownership is preserved across callbacks: the fromJson
// builder hands its packed region out before Shutdown (the default allocator's
// free is a no-op, so the region outlives the builder), and load/measure/save
// read a self-relative region the caller keeps alive (docs/SPEC-TABLES.md §6.2).
func init() {
	codecTable = append(codecTable,
		regionRow("cards", "Card",
			cards.CardLoadMeasure, cards.CardLoadTolerant, cards.CardMeasure, cards.CardSave,
			func(text []byte, r *cards.TableReport) (*cards.Card, []byte, bool) {
				var b cards.CardBuilder
				if !b.Init() {
					return nil, nil, false
				}
				defer b.Shutdown()
				ok := cards.CardFromJsonTolerant(&b, text, r)
				if !b.Lock() {
					return nil, nil, false
				}
				return b.AsConst(), b.Region(), ok
			},
			cards.CardToJsonMeasure, cards.CardToJson, cards.CardCookMeasure, cards.CardCookFrom,
			func(r *cards.TableReport) report {
				return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == cards.TableOpenRefused}
			}))
}
