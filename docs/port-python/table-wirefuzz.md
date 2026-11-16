# Python table wire-fuzz notes

The form-1 array reader decodes an older scalar element kind at the wire
width before writing the current kind. One widening is counted per field.

For the widening read, a bulk `struct.unpack` followed by the range check was
compared with a per-element `int.from_bytes` walk over identical signed
16-bit payloads, on Linux with the pinned interpreters. The minimum of five
runs, in microseconds per read, was:

| elements | Python 3.15.2 walk | Python 3.15.2 bulk | Python 3.11.18 walk | Python 3.11.18 bulk |
| ---: | ---: | ---: | ---: | ---: |
| 16 | 2.620 | 0.691 | 3.034 | 0.771 |
| 1024 | 155.958 | 25.505 | 168.501 | 27.734 |

Both shapes included the range check and result list. The bulk form was kept.
