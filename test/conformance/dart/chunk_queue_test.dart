// Unit test for ChunkQueue (test/conformance/dart/chunk_queue.dart): no clock,
// no sockets, no stdin. It feeds many large frames through the queue in odd
// chunk sizes, checks every byte comes back in order, and holds the bytes the
// queue copied to a small multiple of the bytes read. The old reader (one
// growing list, each frame removed from the front) shifts the whole backlog on
// every read, which is quadratic and fails the bound by orders of magnitude.
import 'dart:typed_data';

import 'chunk_queue.dart';

int _fails = 0;

void _check(bool ok, String what) {
  if (!ok) {
    _fails++;
    print('FAIL: $what');
  }
}

void main() {
  const frames = 256;
  const frameLen = 64 * 1024 + 7;
  final total = frames * frameLen;

  // The whole stream, arriving in 4093-byte chunks (not a divisor of the
  // frame), is queued before any frame is read: the worst case for a queue that
  // shifts, which is the harness writing ahead of a slow reader.
  final stream = Uint8List(total);
  for (var i = 0; i < total; i++) {
    stream[i] = (i * 31 + (i >> 8)) & 0xFF;
  }
  final q = ChunkQueue();
  for (var at = 0; at < total; at += 4093) {
    final end = at + 4093 < total ? at + 4093 : total;
    q.add(Uint8List.sublistView(stream, at, end));
  }
  _check(q.length == total, 'queued length ${q.length} != $total');

  var pos = 0;
  var wrong = 0;
  for (var f = 0; f < frames; f++) {
    final got = q.take(frameLen);
    if (got.length != frameLen) wrong++;
    for (var i = 0; i < got.length; i++) {
      if (got[i] != stream[pos + i]) {
        wrong++;
        break;
      }
    }
    pos += got.length;
  }
  _check(wrong == 0, '$wrong frames came back wrong');
  _check(q.length == 0, 'queue not drained: ${q.length}');
  _check(q.take(10).isEmpty, 'take past the end returned bytes');
  print('bytes read $total, bytes copied ${q.copied}');
  _check(
    q.copied <= 2 * total,
    'copied ${q.copied} bytes for $total read (limit ${2 * total})',
  );

  // A short queue answers short, and a generic List<int> chunk is copied once.
  final s = ChunkQueue()..add(<int>[1, 2, 3]);
  final r = s.take(8);
  _check(r.length == 3 && r[2] == 3, 'short take');
  _check(s.copied <= 6, 'generic chunk copied ${s.copied}');

  if (_fails != 0) {
    print('chunk_queue_test: $_fails failure(s)');
    throw StateError('chunk_queue_test failed');
  }
  print('chunk_queue_test: ok');
}
