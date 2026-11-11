# Packet end conformance vectors

`End.schema` declares one type, `End`, whose only field is
`value uint8 | max = 5`: three wire bits. Each port reads these packets
with its generated public `Read<T>` and holds the verdict.

| file | bytes | bits | verdict |
|---|---|---|---|
| `exact.bin` | `03` | 3 | accepted: the value 3 and no other bits |
| `trailing.bin` | `03 ff` | 3 | refused: one whole byte is left after the object |
| `padding.bin` | `fb` | 3 | refused: the final byte's five unused bits are set |

The matching `.bits` file holds each packet's bit count. `exact.bin` is the
single byte `03`; `trailing.bin` is the same packet followed by one extra
whole byte; `padding.bin` is the same value with the final byte's five unused
bits set, so its unused bits are non-zero.
