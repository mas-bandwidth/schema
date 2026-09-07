package dart

// A private helper per library avoids growing nested field checks. It reads
// only used bytes and matches the reference's Unicode scalar ranges.
const utf8Helper = `// Malformed UTF-8 fails the read in every build mode (SPEC §4.7).
bool _schemaUtf8Valid(Uint8List bytes, int length) {
  var index = 0;
  while (index < length) {
    final lead = bytes[index++];
    if (lead < 0x80) continue;
    int remaining, point;
    if ((lead & 0xE0) == 0xC0) {
      remaining = 1;
      point = lead & 0x1F;
    } else if ((lead & 0xF0) == 0xE0) {
      remaining = 2;
      point = lead & 0x0F;
    } else if ((lead & 0xF8) == 0xF0) {
      remaining = 3;
      point = lead & 0x07;
    } else {
      return false;
    }
    if (remaining > length - index) return false;
    for (var part = 0; part < remaining; part++) {
      final byte = bytes[index++];
      if ((byte & 0xC0) != 0x80) return false;
      point = (point << 6) | (byte & 0x3F);
    }
    if (remaining == 1 && point < 0x80) return false;
    if (remaining == 2 &&
        (point < 0x800 || (point >= 0xD800 && point <= 0xDFFF))) {
      return false;
    }
    if (remaining == 3 && (point < 0x10000 || point > 0x10FFFF)) {
      return false;
    }
  }
  return true;
}

`
