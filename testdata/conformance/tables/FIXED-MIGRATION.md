# Single-read corpus migration

Each new positive wire is encoded from the existing complete canonical JSON and read back with every value checked. Historical bytes remain unchanged and become `previous_form` refusal rows.

| Instance | Historical input | Positive input | Historical refusal case |
| --- | --- | --- | --- |
| root_full | `testdata/wire/tables/root_full.bin` | `testdata/conformance/tables/wire/root_full.bin` | previous_form_root_full |
| root_default | `testdata/wire/tables/root_default.bin` | `testdata/conformance/tables/wire/root_default.bin` | previous_form_root_default |
| profile_elide | `testdata/wire/tables/profile_elide.bin` | `testdata/conformance/tables/wire/profile_elide.bin` | previous_form_profile_elide |
| text_escapes | `testdata/wire/tables/text_escapes.bin` | `testdata/conformance/tables/wire/text_escapes.bin` | previous_form_text_escapes |
| text_escapes_raw | `testdata/wire/tables/text_escapes_raw.bin` | `testdata/conformance/tables/wire/text_escapes_raw.bin` | previous_form_text_escapes_raw |
| text_float_exp | `testdata/wire/tables/text_float_exp.bin` | `testdata/conformance/tables/wire/text_float_exp.bin` | previous_form_text_float_exp |
| text_float_small | `testdata/wire/tables/text_float_small.bin` | `testdata/conformance/tables/wire/text_float_small.bin` | previous_form_text_float_small |
| text_float_misc | `testdata/wire/tables/text_float_misc.bin` | `testdata/conformance/tables/wire/text_float_misc.bin` | previous_form_text_float_misc |
| text_float64_big | `testdata/wire/tables/text_float64_big.bin` | `testdata/conformance/tables/wire/text_float64_big.bin` | previous_form_text_float64_big |
| text_float64_ties | `testdata/wire/tables/text_float64_ties.bin` | `testdata/conformance/tables/wire/text_float64_ties.bin` | previous_form_text_float64_ties |
| text_negative_zero | `testdata/wire/tables/text_negative_zero.bin` | `testdata/conformance/tables/wire/text_negative_zero.bin` | previous_form_text_negative_zero |
| text_uint64_two_to_63 | `testdata/wire/tables/text_uint64_two_to_63.bin` | `testdata/conformance/tables/wire/text_uint64_two_to_63.bin` | previous_form_text_uint64_two_to_63 |
| text_icon_pad_one | `testdata/wire/tables/text_icon_pad_one.bin` | `testdata/conformance/tables/wire/text_icon_pad_one.bin` | previous_form_text_icon_pad_one |
| text_icon_pad_two | `testdata/wire/tables/text_icon_pad_two.bin` | `testdata/conformance/tables/wire/text_icon_pad_two.bin` | previous_form_text_icon_pad_two |
| text_icon_sixteen | `testdata/wire/tables/text_icon_sixteen.bin` | `testdata/conformance/tables/wire/text_icon_sixteen.bin` | previous_form_text_icon_sixteen |
| loadout_full | `testdata/wire/tables/loadout_full.bin` | `testdata/conformance/tables/wire/loadout_full.bin` | previous_form_loadout_full |
| wide_blob | `testdata/wire/tables/wide_blob.bin` | (none: no file form) | previous_form_wide_blob |
| archive | `testdata/wire/tables/archive.bin` | `testdata/conformance/tables/wire/archive.bin` | previous_form_archive |
| keyed_config | `testdata/wire/tables/keyed_config.bin` | `testdata/conformance/tables/wire/keyed_config.bin` | previous_form_keyed_config |
| keyed_default | `testdata/wire/tables/keyed_default.bin` | `testdata/conformance/tables/wire/keyed_default.bin` | previous_form_keyed_default |
| v1_cfg | `testdata/wire/tables/v1_cfg.bin` | `testdata/conformance/tables/wire/v1_cfg.bin` | previous_form_v1_cfg |
| v1_seams | `testdata/wire/tables/v1_seams.bin` | `testdata/conformance/tables/wire/v1_seams.bin` | previous_form_v1_seams |
| v2_cfg | `testdata/wire/tables/v2_cfg.bin` | `testdata/conformance/tables/wire/v2_cfg.bin` | previous_form_v2_cfg |
| v2_seams | `testdata/wire/tables/v2_seams.bin` | `testdata/conformance/tables/wire/v2_seams.bin` | previous_form_v2_seams |
| chain_value | `testdata/wire/tables/chain_value.bin` | `testdata/conformance/tables/wire/chain_value.bin` | previous_form_chain_value |
| chain_value_empty | `testdata/wire/tables/chain_value_empty.bin` | `testdata/conformance/tables/wire/chain_value_empty.bin` | previous_form_chain_value_empty |
| chain_optional | `testdata/wire/tables/chain_optional.bin` | `testdata/conformance/tables/wire/chain_optional.bin` | previous_form_chain_optional |
| chain_optional_empty | `testdata/wire/tables/chain_optional_empty.bin` | `testdata/conformance/tables/wire/chain_optional_empty.bin` | previous_form_chain_optional_empty |
| message_open | `testdata/wire/tables/message_open.bin` | `testdata/conformance/tables/wire/message_open.bin` | previous_form_message_open |
| message_transaction | `testdata/wire/tables/message_transaction.bin` | `testdata/conformance/tables/wire/message_transaction.bin` | previous_form_message_transaction |
| message_ping | `testdata/wire/tables/message_ping.bin` | `testdata/conformance/tables/wire/message_ping.bin` | previous_form_message_ping |
| message_save | `testdata/wire/tables/message_save.bin` | `testdata/conformance/tables/wire/message_save.bin` | previous_form_message_save |
| message_empty | `testdata/wire/tables/message_empty.bin` | `testdata/conformance/tables/wire/message_empty.bin` | previous_form_message_empty |
| message_batch | `testdata/wire/tables/message_batch.bin` | `testdata/conformance/tables/wire/message_batch.bin` | previous_form_message_batch |
| message_trace | `testdata/wire/tables/message_trace.bin` | `testdata/conformance/tables/wire/message_trace.bin` | previous_form_message_trace |
| message_arms | `testdata/wire/tables/message_arms.bin` | `testdata/conformance/tables/wire/message_arms.bin` | previous_form_message_arms |
| message_arm_edits | `testdata/wire/tables/message_arm_edits.bin` | `testdata/conformance/tables/wire/message_arm_edits.bin` | previous_form_message_arm_edits |
| message_ack | `testdata/wire/tables/message_ack.bin` | `testdata/conformance/tables/wire/message_ack.bin` | previous_form_message_ack |
| m1_open | `testdata/wire/tables/m1_open.bin` | `testdata/conformance/tables/wire/m1_open.bin` | previous_form_m1_open |
| m1_save | `testdata/wire/tables/m1_save.bin` | `testdata/conformance/tables/wire/m1_save.bin` | previous_form_m1_save |
| m2_close | `testdata/wire/tables/m2_close.bin` | `testdata/conformance/tables/wire/m2_close.bin` | previous_form_m2_close |
| m2_open | `testdata/wire/tables/m2_open.bin` | `testdata/conformance/tables/wire/m2_open.bin` | previous_form_m2_open |
| scalars_full | `testdata/wire/tables/scalars_full.bin` | `testdata/conformance/tables/wire/scalars_full.bin` | previous_form_scalars_full |
| scalars_default | `testdata/wire/tables/scalars_default.bin` | `testdata/conformance/tables/wire/scalars_default.bin` | previous_form_scalars_default |
| scalars_edges | `testdata/wire/tables/scalars_edges.bin` | `testdata/conformance/tables/wire/scalars_edges.bin` | previous_form_scalars_edges |
| login_full | `testdata/wire/tables/login_full.bin` | `testdata/conformance/tables/wire/login_full.bin` | previous_form_login_full |
| login_default | `testdata/wire/tables/login_default.bin` | `testdata/conformance/tables/wire/login_default.bin` | previous_form_login_default |
| match_full | `testdata/wire/tables/match_full.bin` | `testdata/conformance/tables/wire/match_full.bin` | previous_form_match_full |
| match_default | `testdata/wire/tables/match_default.bin` | `testdata/conformance/tables/wire/match_default.bin` | previous_form_match_default |
| store_full | `testdata/wire/tables/store_full.bin` | `testdata/conformance/tables/wire/store_full.bin` | previous_form_store_full |
| store_default | `testdata/wire/tables/store_default.bin` | `testdata/conformance/tables/wire/store_default.bin` | previous_form_store_default |
| vocab_wide | `testdata/wire/tables/vocab_wide.bin` | `testdata/conformance/tables/wire/vocab_wide.bin` | previous_form_vocab_wide |
| vocab_low | `testdata/wire/tables/vocab_low.bin` | `testdata/conformance/tables/wire/vocab_low.bin` | previous_form_vocab_low |
| peer_a | `testdata/wire/tables/peer_a.bin` | `testdata/conformance/tables/wire/peer_a.bin` | previous_form_peer_a |
| peer_b | `testdata/wire/tables/peer_b.bin` | `testdata/conformance/tables/wire/peer_b.bin` | previous_form_peer_b |
| vocab9_wide | `testdata/wire/tables/vocab9_wide.bin` | `testdata/conformance/tables/wire/vocab9_wide.bin` | previous_form_vocab9_wide |
| vocab9_low | `testdata/wire/tables/vocab9_low.bin` | `testdata/conformance/tables/wire/vocab9_low.bin` | previous_form_vocab9_low |
| r1_cfg | `testdata/wire/tables/r1_cfg.bin` | `testdata/conformance/tables/wire/r1_cfg.bin` | previous_form_r1_cfg |
| r1_ping | `testdata/wire/tables/r1_ping.bin` | `testdata/conformance/tables/wire/r1_ping.bin` | previous_form_r1_ping |
| r2_cfg | `testdata/wire/tables/r2_cfg.bin` | `testdata/conformance/tables/wire/r2_cfg.bin` | previous_form_r2_cfg |
| text_float32_double_round | `testdata/wire/tables/text_float32_double_round.bin` | `testdata/conformance/tables/wire/text_float32_double_round.bin` | previous_form_text_float32_double_round |
