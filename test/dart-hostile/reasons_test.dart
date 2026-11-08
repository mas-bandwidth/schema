import 'dart:typed_data';

import 'runner.dart' as runner;

void main() {
  final cases = <(String, List<int>, String)>[
    ('non_finite_float: ShipEntry.health', [1], 'non_finite_float'),
    ('non_finite_float', [1], 'non_finite_float'),
    ('count_over_extent_cap', [1], 'count_refusal'),
    ('unknown_form', [2], 'message_form_as_file'),
    ('unknown_form', [255], 'newer_form'),
    ('unknown_form', [0], 'unknown_form'),
    ('unknown_form', [], 'unknown_form'),
    ('wire_damaged', [255], 'wire_damaged'),
    ('different_reason', [2], 'different_reason'),
  ];
  for (final (reason, data, want) in cases) {
    final got = runner.wireRefusalReason(reason, Uint8List.fromList(data));
    if (got != want) throw StateError('$reason $data: $got != $want');
  }
  // the corpus names only its own refusals; every other reason is damaged
  final corpus = <(String, List<int>, String)>[
    ('non_finite_float: ShipEntry.health', [1], 'non_finite_float'),
    ('count_over_extent_cap', [1], 'count_refusal'),
    ('unknown_form', [2], 'message_form_as_file'),
    ('unknown_form', [255], 'newer_form'),
    ('no_vocabulary', [2], 'no_vocabulary'),
    ('batch_too_large', [2], 'batch_too_large'),
    ('unknown_form', [0], 'damaged'),
    ('wire_damaged', [1], 'damaged'),
    ('out_of_range', [1], 'damaged'),
    ('text_damaged', [0x7b], 'damaged'),
  ];
  for (final (reason, data, want) in corpus) {
    final got = runner.corpusReason(reason, Uint8List.fromList(data));
    if (got != want) throw StateError('corpus $reason $data: $got != $want');
  }
  print('wire refusal vocabulary controls pass');
}
