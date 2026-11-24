// THE PLANTED DEFECT for dart-chunk-queue-negative-control: the reader the
// wire-fuzz driver used before ChunkQueue, behind the same interface. One
// growing list, each frame removed from the front, so every read shifts the
// whole backlog. `copied` counts the elements each step moves. The unit test
// must go red against this file.
import 'dart:typed_data';

class ChunkQueue {
  final List<int> _buf = <int>[];

  int copied = 0;

  int get length => _buf.length;

  void add(List<int> chunk) {
    _buf.addAll(chunk);
    copied += chunk.length;
  }

  Uint8List take(int n) {
    final take = _buf.length < n ? _buf.length : n;
    final out = Uint8List.fromList(_buf.sublist(0, take));
    copied += 2 * take; // sublist, then fromList
    copied += _buf.length - take; // removeRange shifts the tail down
    _buf.removeRange(0, take);
    return out;
  }
}
