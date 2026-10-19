package javatable

import "testing"

// TestCookWriteUnionsAndWideKindsJava pins the two halves of §7.6 this card
// adds: a root whose closure holds a union, and the wide-kind unit (§15). Every
// subtest loads the committed wire, measures, writes both byte orders, and
// compares the bytes with `schema cook`'s committed reference images.
func TestCookWriteUnionsAndWideKindsJava(t *testing.T) {
	t.Parallel()
	m := readCookManifest(t)
	units := []cookWriteUnit{
		{"../../../tables/examples", "tabledemo", "tabledemo", []cookWriteRoot{
			{"RootConfig", []string{"root_full", "root_default"}},
			{"ProfileConfig", []string{"profile_elide"}},
			{"LoadoutConfig", []string{"loadout_full"}},
			{"ArchiveConfig", []string{"archive"}},
		}},
		{"../../../test/tables/V1.schema", "tblv1", "tblv1", []cookWriteRoot{
			{"Cfg", []string{"v1_cfg", "v1_seams"}},
		}},
		{"../../../test/tables/V2.schema", "tblv2", "tblv2", []cookWriteRoot{
			{"Cfg", []string{"v2_cfg", "v2_seams"}},
		}},
		{"../../../tables/messages", "messagedemo", "messagedemo", []cookWriteRoot{
			{"ToolMessage", []string{"message_open", "message_transaction", "message_ping", "message_save",
				"message_empty", "message_batch", "message_arms", "message_arm_edits", "message_ack", "message_trace"}},
		}},
		{"../../../tables/stream", "streamdemo", "streamdemo", []cookWriteRoot{
			{"Feed", []string{"stream_chain", "stream_header", "stream_parts", "stream_arm_first", "stream_arm_pointer"}},
		}},
		{"../../../test/tables/M1.schema", "tblm1", "tblm1", []cookWriteRoot{
			{"Msg", []string{"m1_open"}},
		}},
		{"../../../test/tables/M2.schema", "tblm2", "tblm2", []cookWriteRoot{
			{"Msg", []string{"m2_open"}},
		}},
		{"../../../tables/scalars", "scalars", "scalardemo", []cookWriteRoot{
			{"SimState", []string{"scalars_full", "scalars_default", "scalars_edges"}},
		}},
	}
	for _, u := range units {
		for _, r := range u.roots {
			u, r := u, r
			t.Run(u.unit+"_"+r.root, func(t *testing.T) {
				t.Parallel()
				cookGoldenUnit(t, m, cookWriteUnit{
					schema: u.schema,
					unit:   u.unit,
					pkg:    u.pkg,
					roots:  []cookWriteRoot{r},
				})
			})
		}
	}
}
