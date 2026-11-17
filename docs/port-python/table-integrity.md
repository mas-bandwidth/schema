# Variable-file CRC-32C

A root declared with `integrity = "crc32c"` writes a CRC-32C trailer on its
variable file, counts four bytes in its measurement, and verifies the trailer
before decoding. A nested table does not introduce a second trailer. The
public construct remains refused until its other applicable forms are carried.

Fixed-form batches carry one trailer over the complete file. The dedicated
fixed load, save and measure functions and the one-record aliases share the
same checksum path; a mismatch is refused before layout or records are read.
