# The per-PR happy path for Python: one ShipCreate written with the generated
# packet writer, read back with the generated reader, every field compared.
# Run by make/happy-path.mk with the generated package on PYTHONPATH.
import sys

from happy import ShipCreate, ShipType


def fail(why: str) -> None:
    print(f"happy python: FAILED: {why}", file=sys.stderr)
    sys.exit(1)


sent = ShipCreate()
sent.ship_type = ShipType.Corvette
sent.position.x = 1.5
sent.position.y = -2.25
sent.position.z = 3.0
sent.health = 777
sent.alive = True

try:
    data = sent.write()
except Exception as exc:
    fail(f"write: {exc!r}")
try:
    got = ShipCreate.read(data)
except Exception as exc:
    fail(f"read: {exc!r}")

for name, have, want in [
    ("ship_type", got.ship_type, ShipType.Corvette),
    ("position.x", got.position.x, 1.5),
    ("position.y", got.position.y, -2.25),
    ("position.z", got.position.z, 3.0),
    ("health", got.health, 777),
    ("alive", got.alive, True),
]:
    if type(have) is not type(want) or have != want:
        fail(f"{name}: got {have!r}, want {want!r}")

print(f"happy python: ok ({len(data)} bytes)")
