package tablenames

import "sort"

// LOBSTER (internal/codegen/lobstertable). Its table runtime is one Lobster
// MODULE per file (runtime/*.lobster), every one of them inside `namespace
// schema_rt`, and the generated table modules reach it only as
// `schema_rt.<name>`. A schema declaration is emitted inside the unit's own
// namespace (its package), so no declaration can collide with a runtime name:
// every name here claims nothing, and none is in the shared registry, whose
// names are looked for in every backend's view files. The list is the scan's
// ground truth and nothing else: compiler's
// TestLobsterTableRuntimeNamesAreClaimed holds it to the emitted runtime both
// ways, and TestLobsterRuntimeNameScanGoesRed is its control.
//
// The claimable Lobster spellings are the two the emitter refuses by name
// instead: the namespace itself (a package named schema_rt) and a runtime
// module's FILE name, because a Lobster import names a file beside the module
// (a schema file named for a runtime module is refused beside a table and
// kept in a table-free unit, TestLobsterRuntimeFileNamesAreClaimed).

// lobsterRuntimeNames is every name the Lobster table runtime defines at
// namespace scope (a def, a class, a let or a var at column zero), by the
// file that defines it.
var lobsterRuntimeNames = map[string][]string{
	"block_form.lobster": {
		"BlockField", "BlockLayout", "BlockRecord", "balign", "block_build", "block_build_all", "block_byte_order",
		"block_dump", "block_dump_record", "block_float_at", "block_float_put", "block_magic",
		"block_magic_swapped", "block_non_finite", "block_open", "block_open_reason", "block_read",
		"block_read_all", "block_read_elem", "block_read_field", "block_read_record", "block_record", "block_used_length",
		"block_registry", "block_write_elem", "block_write_field", "block_write_record", "bq", "bread_int",
		"bscalar", "bscalar_elem", "bwrite_int", "bx", "lookup_block", "register_block",
	},
	"collections.lobster": {
		"_collections_arr", "_collections_map", "_collections_ptr", "collection_construct",
	},
	"cook.lobster": {
		"CookCarve", "CookDump", "CookField", "CookGraph", "CookNode", "CookOut", "CookRead", "CookTable",
		"CookUnion", "CookWrite", "cc_elem", "cc_extent", "cc_field", "cc_has_field", "cc_list", "cc_record",
		"cd_emit", "cd_field", "cd_find", "cd_node", "cd_scalar", "cd_storage", "cd_text", "cg_add_blob",
		"cg_add_rec", "cg_blob_index", "cg_place", "cg_rec_index", "cg_visit_edges", "cg_visit_union",
		"cg_visit_value", "co_bytes", "co_u16", "co_u32", "co_u64", "co_u8", "cook_align_up", "cook_bswap16",
		"cook_bswap32", "cook_bswap64", "cook_byte_order_big", "cook_byte_order_little", "cook_directory_entry",
		"cook_dump", "cook_f32frombits", "cook_header_bytes", "cook_magic", "cook_magic_swapped", "cook_measure",
		"cook_open", "cook_open_named", "cook_open_reason", "cook_plan", "cook_read", "cook_table",
		"cook_table_of", "cook_tables", "cook_union_named", "cook_union_of", "cook_unions", "cook_wide",
		"cook_word", "cook_word32", "cook_write", "cr_array", "cr_count", "cr_elem", "cr_entry", "cr_field",
		"cr_int", "cr_list", "cr_record", "cr_ref", "cr_text", "cr_union", "cr_units", "cr_wide", "cr_wide_at",
		"cr_word", "cr_word32", "cw_array", "cw_elem", "cw_field", "cw_int", "cw_record", "cw_ref", "cw_ref_blob",
		"cw_str", "cw_union", "cw_units", "cw_wide",
	},
	"defaults_renames.lobster": {
		"DefaultsEnumShape", "RetiredVariant", "_defaults_enum", "dep_enum_cell", "deprecate_variant",
		"refused_array", "refused_elem", "refused_record", "refused_slot", "refused_union", "retired_value",
		"retired_variants", "save_strict", "variant_retired",
	},
	"descriptors.lobster": {
		"DeprecatedEnumShape", "DeprecatedName", "FieldAnnotation", "KeyEnumName", "MapEntryName", "UnitView",
		"ViewConstant", "ViewType", "ViewVariant", "ViewVocabulary", "_descriptors_enum", "deprecated_names",
		"deprecated_variant", "enum_default", "field_annotation", "field_annotations", "field_doc", "field_tags",
		"is_deprecated_variant", "key_enums", "keyed_enum", "keyed_enum_of", "keyed_slot", "map_entries", "map_entry",
		"map_entry_of", "set_unit", "unit_view", "variant_name", "view_constant", "view_constants", "view_enums",
		"view_flags", "view_package", "view_protocol", "view_table", "view_tables", "view_type", "view_types",
		"view_unions", "view_vocabulary",
	},
	"fixed.lobster": {
		"fixed_file", "fixed_record",
	},
	"fixedevolve.lobster": {
		"TableFixedDst", "TableFixedEntry", "TableFixedForm", "TableFixedGuardLink", "TableFixedHashAt",
		"TableFixedHeaderBytes", "TableFixedHole", "TableFixedKnownLayout", "TableFixedLayoutEntry",
		"TableFixedLayoutView", "TableFixedLineagePlan", "TableFixedPlan", "TableOpenCompatible",
		"TableOpenDamaged", "TableOpenExact", "TableOpenIncompatible", "TableOpenMalformed", "TableOpenRefused",
		"TableRefuseBatchTooLarge", "TableRefuseClamped", "TableRefuseCountOverBound",
		"TableRefuseIntegrityMismatch", "TableRefuseInvalidValue", "TableRefuseKindMismatch",
		"TableRefuseLayoutCountMismatch", "TableRefuseLayoutKindInvalid", "TableRefuseLayoutKindUnknown",
		"TableRefuseLayoutMalformed", "TableRefuseLayoutNewer", "TableRefuseLayoutRecordTooLarge",
		"TableRefuseLayoutSizeMismatch", "TableRefuseLayoutTooDeep", "TableRefuseLayoutTreeUnclosed",
		"TableRefuseLayoutUnknown", "TableRefuseLayoutUnsupported", "TableRefuseLengthOverBound",
		"TableRefuseMessageFormAsFile", "TableRefuseNewerForm", "TableRefuseNoLayout", "TableRefuseNonFiniteFloat",
		"TableRefusePlanTooLarge", "TableRefusePreviousForm", "TableRefuseReservedNotZero",
		"TableRefuseTextIllFormed", "TableRefuseTruncated", "TableRefuseUnknownForm", "TableReport",
		"fixed_arg_width", "fixed_build_plan", "fixed_chain_holds", "fixed_chains_equal", "fixed_check_entry",
		"fixed_check_fail", "fixed_clone_entry", "fixed_compile", "fixed_compile_entry", "fixed_copy_run",
		"fixed_crc32c", "fixed_entry_at", "fixed_forged_bounds", "fixed_holes", "fixed_integrity_holds",
		"fixed_integrity_seal", "fixed_kind_known", "fixed_land", "fixed_lay_guards", "fixed_lay_table",
		"fixed_leaf_size", "fixed_lineage_holes", "fixed_lineage_plans", "fixed_match_children",
		"fixed_ordinal_width", "fixed_parse_layout", "fixed_partition", "fixed_plan_max", "fixed_prefill_holes",
		"fixed_push", "fixed_refuse", "fixed_refuse_hash", "fixed_refuse_non_finite", "fixed_report_equal",
		"fixed_report_err", "fixed_reset_report", "fixed_run", "fixed_runs", "fixed_select", "fixed_signed_kind",
		"fixed_src_end", "fixed_subtree", "fixed_tag_at", "fixed_tag_bytes", "fixed_text_ill_formed",
		"fixed_text_refusal", "fixed_union_arm_bytes", "fixed_utf8_valid_no_nul", "fixed_wide_ill_formed",
		"fixed_widens", "i128_lt", "tableFixedBool", "tableFixedCheck", "tableFixedCompiler", "tableFixedConst",
		"tableFixedCopy", "tableFixedCount", "tableFixedDstDeprecated", "tableFixedEntryBytes",
		"tableFixedLayoutHeaderBytes", "tableFixedMaxDepth", "tableFixedNoGuard", "tableFixedOrdinal",
		"tableFixedRecordMaxBytes", "tableFixedRemapMax", "tableFixedTag", "tableFixedText", "tableFixedTextBytes",
		"tableFixedTextUtf8", "tableFixedTextWide", "tableFixedWiden", "tableFixedWidenF", "u128_lt",
	},
	"json.lobster": {
		"JsonBlobCount", "JsonFlags", "JsonGuard", "JsonInt", "JsonLabel", "JsonNodeCount", "JsonReader",
		"JsonWriter", "j_array_count", "j_b64", "j_b64_alphabet", "j_b64_table", "j_bn_add", "j_bn_add_small",
		"j_bn_cmp", "j_bn_copy", "j_bn_divmod_small", "j_bn_from_dec", "j_bn_lanes", "j_bn_low", "j_bn_mask",
		"j_bn_mul_pow10", "j_bn_mul_pow5", "j_bn_mul_small", "j_bn_norm", "j_bn_of_lanes", "j_bn_pow2", "j_bn_shl",
		"j_bn_shr", "j_bn_sub", "j_bn_to_dec", "j_bn_u64", "j_byte_strings", "j_bytes", "j_cmp_token_float",
		"j_count_cell", "j_count_field", "j_count_node", "j_count_record", "j_days", "j_decimal", "j_decode_utf8",
		"j_digit", "j_element_shape", "j_entry_order", "j_enum_value", "j_escape", "j_exp_digits", "j_f32frombits",
		"j_fail", "j_field_index", "j_find_label", "j_float_text", "j_floor_div", "j_flt_max", "j_format_g",
		"j_format_wide", "j_hex", "j_hex4", "j_i128_float", "j_i128_neg", "j_in_domain", "j_indent", "j_indents",
		"j_integer_of", "j_interpret_exact", "j_interpret_integer", "j_is_digit", "j_is_fixed", "j_is_inf",
		"j_is_text_list", "j_kind", "j_leap", "j_line", "j_literal", "j_month_days", "j_mul10_add", "j_name_zero",
		"j_node_entry", "j_pad", "j_parse_f32", "j_parse_wide", "j_peek", "j_place_float", "j_place_integer",
		"j_place_map_key", "j_place_wide", "j_raw", "j_read_array", "j_read_base64", "j_read_blob", "j_read_field",
		"j_read_flags", "j_read_keyed", "j_read_map", "j_read_map_value", "j_read_pointer_cell", "j_read_scalar",
		"j_read_table", "j_read_table_keys", "j_read_text_list", "j_read_union", "j_rfc3339_format",
		"j_rfc3339_parse", "j_round", "j_scale", "j_scan_label", "j_scan_number", "j_scan_stored", "j_scan_string",
		"j_scan_text", "j_scan_unit", "j_scan_wstring", "j_shape", "j_skip_container", "j_skip_string",
		"j_skip_value", "j_skipped_ampersand", "j_sorted_entries", "j_space", "j_storage_bytes", "j_strip_zeros",
		"j_takes_null", "j_token_decimal", "j_two", "j_two128", "j_u64_float", "j_utf8", "j_utf8_sequence",
		"j_value_shape", "j_walk_number", "j_wide_signed", "j_write_base64", "j_write_field", "j_write_fields",
		"j_write_keyed", "j_write_map", "j_write_pointer", "j_write_scalar", "j_write_string", "j_write_value",
		"j_write_wstring", "j_zeros_text", "json_array_registry", "json_array_table", "json_fd_of", "json_flags",
		"json_flags_of", "json_flags_registry", "json_guard", "json_guard_holds", "json_guard_registry",
		"json_is_array_table", "json_max_depth", "json_max_key", "json_read", "json_read_into", "json_text_list",
		"json_text_list_registry", "json_write",
	},
	"load.lobster": {
		"ArrayShape", "EnumShape", "KeyedKeys", "KeyedShape", "LoadReport", "LoadState", "MapShape", "Node",
		"NodeRecord", "PointerShape", "Reader", "Record", "RetainCapture", "RetainedField", "Shape", "Slot",
		"StringShape", "TableShape", "TextListShape", "UnionShape", "Value", "WideShape", "active_retain_capture",
		"arm_fixed_width", "arm_key", "arm_read", "arm_wire_kind", "array_body", "array_shape", "body_at",
		"body_extent", "build_version_id", "bytes_blob_id", "bytes_shape", "collection_cap", "decode_variable",
		"elem_key", "elem_of", "element_zero", "enum_cell", "enum_shape", "enum_value", "field_default",
		"finite_f32", "finite_f32_bits", "finite_f64", "finite_f64_bits", "float_out_of_range", "float_owner",
		"forget", "forget_cell", "forget_slot", "forget_value", "has", "hex64", "i128_lt", "i128_shl",
		"key_is_string", "keyed_keys", "keyed_registry", "keyed_shape", "keyed_variants", "kind_signed",
		"kind_wide", "kind_width", "leb", "load", "load_tolerant", "load_walk", "map_key", "map_key_fits",
		"map_key_id", "map_key_order", "map_shape", "mark_seen", "message_vocabulary_id", "nested_body",
		"new_record", "new_slot", "node_table_bytes", "node_table_id", "parse_float64", "pointer_shape",
		"read_leb", "read_raw", "reader", "rec_edges", "refill", "refuse_non_finite", "register_retain_capture",
		"register_shape", "release", "reset_slot", "reset_union", "resolve_cell", "retain_captures",
		"retained_payload_finite", "rid", "scalar_at", "scalar_shape", "scan_node_records", "shape", "shape_key",
		"shape_registry", "skip", "slot_by_id", "slot_edges", "slot_named", "strict_reason", "string_blob_id",
		"string_shape", "sub", "sub_to", "table_of", "table_shape", "text_boundary", "text_list_shape",
		"text_valid", "trailer", "u8", "union_cell", "union_shape", "unlink_unreached", "value_edges",
		"wide_boundary", "wide_shape", "wide_units", "wide_valid", "widens",
	},
	"message.lobster": {
		"MessageBits", "MessageEntry", "MessageRead", "MessageShape", "MessageSlots", "MessageUnitFacts",
		"MessageVocabulary", "MessageWriter", "announce", "announce_inner", "announce_measure", "announce_read",
		"is_message_root", "load_messages", "load_messages_into", "load_messages_tolerant", "mb_align", "mb_get",
		"mb_skip", "measure_messages", "message_arm_slots", "message_array", "message_batch_max",
		"message_batch_ok", "message_bits", "message_body", "message_build_version", "message_bytes",
		"message_cell", "message_compatible", "message_count", "message_entries_here", "message_entry",
		"message_entry_read", "message_enum", "message_facts", "message_keyed", "message_kind_bits",
		"message_known_kind", "message_name", "message_name_slot", "message_open", "message_own_entry",
		"message_quantization", "message_refuse", "message_reset_report", "message_root", "message_scalar",
		"message_shape_read", "message_skip", "message_skip_body", "message_skip_depth", "message_slots",
		"message_slots_of", "message_text", "message_union", "message_unit", "message_value",
		"message_vocabulary_max_bytes", "message_wide", "mw_align", "mw_body", "mw_cell", "mw_cell_rides",
		"mw_keyed_pairs", "mw_put", "mw_raw", "mw_rides", "mw_scalar", "mw_union", "mw_value", "new_vocabulary",
		"own_vocabulary", "save_messages",
	},
	"optionals.lobster": {
		"present_optionals",
	},
	"region_measure.lobster": {
		"ExtentScan", "extent_body", "extent_field", "extent_field_of", "extent_record", "extent_union",
		"load_measure", "region_round", "region_size",
	},
	"retain_unknown.lobster": {
		"Cursor", "Emitter", "Resolver", "Retain", "RetainCaptureImpl", "RetainTailImpl", "cur_has", "cur_leb",
		"cur_u8", "em_body", "em_content", "em_framed", "em_framed_in", "em_known", "em_payload", "em_record",
		"em_ref", "em_resolved", "known_ids", "load_retain", "map_value_id", "note_known", "res_body",
		"res_content", "res_framed", "res_id", "res_keyed_slot", "res_mark", "res_patch", "res_payload", "res_raw",
		"res_skip_payload", "res_u64", "res_u8", "retain_depth_cap", "save_retain",
	},
	"schema_save.lobster": {
		"RetainTail", "SaveEncoder", "SaveGraph", "SaveIds", "SaveNode", "SaveOut", "active_retain_tail",
		"encode_body", "encode_body_fields", "encode_field", "encode_node_table", "guard_holds",
		"register_retain_tail", "retain_tails", "save", "save_arm_header", "save_arm_payload", "save_array",
		"save_elem_is_default", "save_element", "save_enum_ref", "save_is_default", "save_keyed",
		"save_live_count", "save_raw_width", "save_reasoned", "save_wide_bytes", "save_wide_raw", "sg_add_blob",
		"sg_add_rec", "sg_blob_index", "sg_map_order", "sg_rec_index", "sg_visit_edges", "sg_visit_union",
		"sg_visit_value", "sid_mark", "sid_ref", "sid_rollback", "so_bytes", "so_grow", "so_leb", "so_take",
		"so_u32", "so_u64", "so_u8",
	},
	"schema_tables.lobster": {
		"EnumVariant", "FieldDescriptor", "TableDescriptor", "UnionDescriptor", "lookup_table", "lookup_union",
		"registered_tables", "registered_unions", "table", "table_registry", "union_of", "union_registry",
	},
	"strict_and_refusals.lobster": {
		"load_strict",
	},
	"text_form.lobster": {
		"TNode", "TextDec", "TextDoc", "TextFlags", "TextParser", "TextSpan", "i128_add", "i128_bit", "i128_cmp",
		"i128_div_small", "i128_mul_small", "i128_neg", "i128_setbit", "t_b64_val", "t_base64", "t_dec",
		"t_dec_value", "t_field_of", "t_fill", "t_fixed_token", "t_gap", "t_int", "t_int_store", "t_int_token",
		"t_key_text", "t_name_char", "t_parse_value", "t_set_fail", "t_set_flags", "t_set_keyed", "t_set_scalar",
		"t_set_union", "t_string", "t_value_into", "text_edit", "text_flags", "text_flags_of",
		"text_flags_registry", "text_load", "text_save",
	},
	"text_lists.lobster": {
		"TextListElementShape", "TextListFact", "TextMapShape", "TextStringShape", "TextTotalShape", "_text_list",
		"_text_map", "_text_string", "_text_total", "text_list", "text_list_facts", "text_list_of",
		"text_value_len", "total_keep",
	},
	"variable_class.lobster": {
		"_variable_class_array", "_variable_class_pointer", "_variable_class_table", "_variable_class_union",
		"variable_class_carries", "variable_class_keys",
	},
	"wide_kinds.lobster": {
		"WideScalarShape", "_wide_scalar", "wide_timestamp_at",
	},
}

// LobsterRuntimeNames is every name the Lobster table runtime defines at
// namespace scope, sorted and without repeats (a class declared forward and
// then defined is one name).
func LobsterRuntimeNames() []string {
	seen := map[string]bool{}
	for _, names := range lobsterRuntimeNames {
		for _, n := range names {
			seen[n] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
