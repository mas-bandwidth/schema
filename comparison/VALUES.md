# The values every format encodes

All four encoders in this directory encode exactly these values. If you change
one, change them all, or the comparison is meaningless.

| field | value | note |
|---|---:|---|
| `ship_type` | `Destroyer` | ordinal 4 of `None,Fighter,Corvette,Bomber,Destroyer,Carrier` |
| `position.x` | 1234567 | bound ±8388608 (`MaxWorldMeters * PositionUnits`) |
| `position.y` | -2345678 | |
| `position.z` | 3456789 | |
| `rotation.x` | 100 | bound ±1024 (`RotationUnits`) |
| `rotation.y` | -200 | |
| `rotation.z` | 300 | |
| `rotation.w` | 400 | |
| `linear_velocity.x` | 12345 | bound ±2097152 (`MaxSpeedMeters * VelocityUnits`) |
| `linear_velocity.y` | -23456 | |
| `linear_velocity.z` | 34567 | |
| `has_flags` | true | the branch is **taken** — the worst case for schema |
| `flags` | `Boosting \| Aiming` | bits 1 and 3, value 10 |
| `team` | `Blue` | ordinal 2 of `None,Red,Blue` |
| `health` | 750 | bound [0, 1000] |
| `thrust` | 42 | bound [0, 100] |

Two deliberate choices, both of which cost schema bytes rather than saving
them:

- **`has_flags` is true.** The untaken branch would cost schema zero bits for
  `flags` and make its number smaller. Taking the branch measures the longest
  wire path, which is also what the compiler's own `ShipCreateMaxBits = 219`
  describes.
- **Values are large and non-zero.** Protobuf varints and Cap'n Proto's packing
  both shrink on small or zero values. Encoding zeroes would have flattered
  schema considerably — a zeroed message packs to almost nothing in Cap'n
  Proto. These are realistic mid-range gameplay values.
