# The per-PR happy path for Elixir: one ShipCreate written with the generated
# packet writer, read back with the generated reader, every field compared.
# Run by make/happy-path.mk with the generated directory as argument.
[dir] = System.argv()
Enum.each(["PacketRuntime.ex", "Happy.ex"], &Code.require_file(&1, dir))

fail = fn why ->
  IO.puts(:stderr, "happy elixir: FAILED: #{why}")
  System.halt(1)
end

# struct!/2, not %Happy.ShipCreate{}: the modules load at run time, after
# this script compiles
sent =
  struct!(Happy.ShipCreate,
    ship_type: :corvette,
    position: struct!(Happy.Vector3, x: 1.5, y: -2.25, z: 3.0),
    health: 777,
    alive: true
  )

bytes =
  case Happy.Happy.write_ship_create(sent) do
    {:ok, bytes} -> bytes
    {:error, why} -> fail.("write_ship_create: #{why}")
  end

got =
  case Happy.Happy.read_ship_create(bytes, byte_size(bytes) * 8) do
    {:ok, got} -> got
    :error -> fail.("read_ship_create returned :error")
  end

[
  {"ship_type", got.ship_type, :corvette},
  {"position.x", got.position.x, 1.5},
  {"position.y", got.position.y, -2.25},
  {"position.z", got.position.z, 3.0},
  {"health", got.health, 777},
  {"alive", got.alive, true}
]
|> Enum.each(fn {name, have, want} ->
  if have !== want, do: fail.("#{name}: got #{inspect(have)}, want #{inspect(want)}")
end)

IO.puts("happy elixir: ok (#{byte_size(bytes)} bytes)")
