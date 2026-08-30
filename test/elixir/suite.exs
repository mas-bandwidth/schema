# The test battery main.exs loads after the generated modules — see the note
# there on why the two files are split.
defmodule SchemaTestElixir do
  import Bitwise

  def check(ok, what) do
    unless ok do
      IO.puts("FAILED: #{what}")
      Process.put(:schema_test_failed, true)
    end
  end

  # expect_raise runs a writer-contract violation and demands ArgumentError —
  # contracts are always on (no compile-out assert exists on the BEAM).
  def expect_raise(fun, what) do
    fun.()
    check(false, what)
  rescue
    ArgumentError -> :ok
  end

  def golden(name), do: File.read!("../../testdata/wire/#{name}.bin")

  # float32 rounding for expected-value computation (the generated codecs
  # carry their own private twin)
  def fround(v) do
    <<b::little-32>> = <<v::float-32-little>>
    <<r::float-32-little>> = <<b::little-32>>
    r
  end

  # pin writes value, byte-compares against the C++-pinned golden, holds
  # measure to the written wire, reads it back, and re-writes to prove
  # byte-identical round-trip — the leg's core instrument. Returns the
  # decoded value for further checks.
  def pin(name, value, write, read, measure) do
    wire = write.(value)
    g = golden(name)

    check(
      byte_size(wire) == byte_size(g),
      "#{name}: wrote #{byte_size(wire)} bytes, golden has #{byte_size(g)}"
    )

    check(wire == g, "#{name}: Elixir bytes == the C++-pinned bytes")
    bits = measure.(value)

    check(
      div(bits + 7, 8) == byte_size(wire),
      "#{name}: measure #{bits} bits vs #{byte_size(wire)} bytes written"
    )

    case read.(g, byte_size(g) * 8) do
      {:ok, out} ->
        wire2 = write.(out)
        check(wire2 == g, "#{name}: round-trips to identical bytes")
        out

      :error ->
        check(false, "#{name}: read")
        nil
    end
  end

  def make_ship_create do
    %Example.ShipCreate{
      ship_type: Example.ShipType.bomber(),
      position: %Example.QuantizedPosition{x: 1000, y: -2000, z: 3000},
      has_flags: true,
      flags: Example.Enums.ship_flags_boosting() ||| Example.Enums.ship_flags_aiming(),
      team: Example.Team.blue(),
      health: 750,
      thrust: 55
    }
  end

  def make_rigid_body do
    %Example.RigidBody{
      position: %Example.Vec3{x: 1.5, y: -2.5, z: 3.25},
      orientation: %Example.Quat{x: 0.1, y: 0.2, z: 0.3, w: 0.9},
      at_rest: false,
      linear_velocity: %Example.Vec3{x: 10.0, y: 20.0, z: -3.0},
      angular_velocity: %Example.Vec3{x: 0.25, y: 0.5, z: 0.75}
    }
  end

  def make_input_packet do
    %Example.InputPacket{
      synchronize_sequence: 7,
      current_frame: 123_456_789,
      start_frame: 123_456_780,
      inputs: [
        %Example.Input{throttle: 0.5, fire: true},
        %Example.Input{stick_x: -0.25, boost: true}
      ]
    }
  end

  def test_data_instance do
    %Example.TestData{
      a: -100,
      b: 100,
      c: 149,
      d: 0x11,
      e: 0x22,
      f: 0x33,
      g: true,
      items: [0, 128, 255],
      float_value: fround(3.1415926),
      compressed_float_value: 2.5,
      double_value: 1.0 / 3.0,
      int8_value: -128,
      int16_value: -32768,
      uint8_value: 255,
      uint16_value: 65535,
      uint32_value: 4_294_967_295,
      uint64_value: 0xFFFFFFFFFFFFFFFF,
      # int64 min — a plain BEAM integer holds it directly
      int64_full: -9_223_372_036_854_775_808,
      int64_range: -999_999_999_999,
      fixed_bytes: Enum.map(0..16, fn i -> i * 3 &&& 0xFF end),
      text: "the quick brown fox"
    }
  end

  def run do
    # ---- ShipCreate: the bool-gated flags branch, both ways ----
    inp = make_ship_create()

    out =
      pin(
        "shipcreate_flags",
        inp,
        &Example.Types.write_ship_create/1,
        &Example.Types.read_ship_create/2,
        &Example.Types.measure_ship_create/1
      )

    check(
      out.has_flags and
        out.flags == (Example.Enums.ship_flags_boosting() ||| Example.Enums.ship_flags_aiming()),
      "ShipCreate flags round-trip"
    )

    # untaken branch: flags must read back ZERO (SPEC §5)
    inp2 = %{inp | has_flags: false}
    wire = Example.Types.write_ship_create(inp2)
    {:ok, out2} = Example.Types.read_ship_create(wire, byte_size(wire) * 8)
    check(not out2.has_flags and out2.flags == 0, "untaken branch reads as zero (SPEC §5)")

    check(
      div(Example.Types.measure_ship_create(inp2) + 7, 8) == byte_size(wire),
      "measure tracks the untaken branch"
    )

    # ---- RigidBody: the back-reference example, both branch sides ----
    rb = make_rigid_body()

    pin(
      "rigidbody_moving",
      rb,
      &Example.Types.write_rigid_body/1,
      &Example.Types.read_rigid_body/2,
      &Example.Types.measure_rigid_body/1
    )

    rb_rest = %{rb | at_rest: true}

    out_rest =
      pin(
        "rigidbody_at_rest",
        rb_rest,
        &Example.Types.write_rigid_body/1,
        &Example.Types.read_rigid_body/2,
        &Example.Types.measure_rigid_body/1
      )

    check(out_rest.at_rest, "at_rest reads true")

    check(
      out_rest.linear_velocity == %Example.Vec3{x: 0.0, y: 0.0, z: 0.0} and
        out_rest.angular_velocity == %Example.Vec3{x: 0.0, y: 0.0, z: 0.0},
      "velocities read as zero under the taken at-rest branch (SPEC §5)"
    )

    # ---- Chat: the string framing == classic serialize_string over N + 1 ----
    chat = %Example.Chat{text: "wire parity"}

    out_chat =
      pin(
        "chat",
        chat,
        &Example.Wire.write_chat/1,
        &Example.Wire.read_chat/2,
        &Example.Wire.measure_chat/1
      )

    check(out_chat.text == "wire parity", "Chat round-trips")

    # ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
    ph = %Example.ProbeHeader{version: 5, probe_id: 0x1122334455667788}
    ph_wire = Example.Wire.write_probe_header(ph)
    check(binary_part(ph_wire, 0, 1) == <<0xAB>>, "const(0xAB, 8) leads the wire")

    out_ph =
      pin(
        "probe_header",
        ph,
        &Example.Wire.write_probe_header/1,
        &Example.Wire.read_probe_header/2,
        &Example.Wire.measure_probe_header/1
      )

    check(
      out_ph.version == 5 and out_ph.probe_id == 0x1122334455667788,
      "ProbeHeader round-trips"
    )

    corrupt = <<0xAC>> <> binary_part(ph_wire, 1, byte_size(ph_wire) - 1)

    check(
      Example.Wire.read_probe_header(corrupt, byte_size(corrupt) * 8) == :error,
      "a corrupted wire constant is REJECTED (SPEC §4.3)"
    )

    # ---- InputPacket + TestData against their C++ pins ----
    pin(
      "inputpacket",
      make_input_packet(),
      &Example.Types.write_input_packet/1,
      &Example.Types.read_input_packet/2,
      &Example.Types.measure_input_packet/1
    )

    out_td =
      pin(
        "testdata",
        test_data_instance(),
        &Example.Wire.write_test_data/1,
        &Example.Wire.read_test_data/2,
        &Example.Wire.measure_test_data/1
      )

    check(
      out_td.int64_full == -9_223_372_036_854_775_808 and
        out_td.uint64_value == 0xFFFFFFFFFFFFFFFF and out_td.int8_value == -128,
      "TestData extremes round-trip — signed narrows and full-range ints"
    )

    # ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
    # 0.005 quantizes to 1 under the float32 two-rounding law; -4.8585 over
    # the non-zero-min range quantizes to 142. Same pinned instance as the
    # C++ leg, against the same golden.
    cp = %Example.CompressedProbe{boundary: 0.005, offset: -4.8585}

    out_cp =
      pin(
        "compressed_probe",
        cp,
        &Example.Wire.write_compressed_probe/1,
        &Example.Wire.read_compressed_probe/2,
        &Example.Wire.measure_compressed_probe/1
      )

    check(out_cp.boundary == fround(fround(1 / 1000) * 10), "boundary reconstructs integer 1")

    check(
      out_cp.offset == fround(fround(fround(142 / 10000) * 10) - 5),
      "offset reconstructs integer 142"
    )

    # ---- {:nonfinite, bits}: the bit-transparent float convention ----
    # BEAM floats cannot hold NaN or the infinities, so non-finite IEEE-754
    # patterns travel as {:nonfinite, bits} — write accepts the form and the
    # read reproduces the exact transmitted pattern.
    nf = %{
      test_data_instance()
      | float_value: {:nonfinite, 0x7FC00001},
        double_value: {:nonfinite, 0xFFF0000000000000}
    }

    nf_wire = Example.Wire.write_test_data(nf)
    {:ok, nf_out} = Example.Wire.read_test_data(nf_wire, byte_size(nf_wire) * 8)

    check(
      nf_out.float_value == {:nonfinite, 0x7FC00001} and
        nf_out.double_value == {:nonfinite, 0xFFF0000000000000},
      "non-finite patterns round-trip as {:nonfinite, bits}"
    )

    # ---- specified defaults: construction carries them; zero_* is the zero form ----
    check(%Example.ProbeSample{}.active, "ProbeSample.active defaults true")

    check(
      not Example.Wire.zero_probe_sample().active,
      "the §5 zero form stays zero — zero_* does not reapply defaults"
    )

    check(%Example.ProbeConfig{}.retries == -1, "ProbeConfig.retries defaults -1")

    check(
      %Example.ProbeConfig{}.preferred == Example.Weapon.railgun(),
      "ProbeConfig.preferred defaults Railgun"
    )

    # ---- ProbeBits: the full-range uint32/uint64 paths, C++-pinned ----
    pb = %Example.ProbeBits{
      small: 0x1FF,
      boundary: 0x1FFFFFFFF,
      wide: 0xFEDCBA9876543210,
      sensor: 4_294_967_295,
      nonce: 0xFFFFFFFFFFFFFFFF
    }

    out_pb =
      pin(
        "probebits",
        pb,
        &Example.Wire.write_probe_bits/1,
        &Example.Wire.read_probe_bits/2,
        &Example.Wire.measure_probe_bits/1
      )

    check(
      out_pb.wide == 0xFEDCBA9876543210 and out_pb.nonce == 0xFFFFFFFFFFFFFFFF,
      "ProbeBits round-trips — 9/33/64-bit and full-range paths"
    )

    # ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
    # round trip, the None arm, an array of unions, and the refusal negative
    # controls ----
    check(
      %Example.ProbeCollider{}.shape.type == Example.ProbeShapeType.none(),
      "construction is the empty union"
    )

    check(Example.Wire.probe_shape_max_bits() == 2 + 16, "MaxBits is tag + the largest arm")

    pc = %Example.ProbeCollider{
      armor: 7,
      shape: %Example.ProbeShape{
        type: Example.ProbeShapeType.slab(),
        slab: %Example.ProbeSlab{width: 42, height: 9}
      },
      # backup stays None — the empty arm costs the tag bits only
      extras: [
        %Example.ProbeShape{
          type: Example.ProbeShapeType.ring(),
          ring: %Example.ProbeRing{radius: 777}
        }
      ]
    }

    out_pc =
      pin(
        "probecollider",
        pc,
        &Example.Wire.write_probe_collider/1,
        &Example.Wire.read_probe_collider/2,
        &Example.Wire.measure_probe_collider/1
      )

    check(
      out_pc.armor == 7 and out_pc.shape.type == Example.ProbeShapeType.slab() and
        out_pc.shape.slab.width == 42 and out_pc.shape.slab.height == 9,
      "the selected arm round-trips"
    )

    check(out_pc.backup.type == Example.ProbeShapeType.none(), "the None arm reads back empty")

    check(
      match?(
        [%Example.ProbeShape{type: 1, ring: %Example.ProbeRing{radius: 777}}],
        out_pc.extras
      ),
      "the union array round-trips"
    )

    # the all-None shape — the wire is far shorter than MaxBits; a reader
    # whose fused bounds counted MaxBitsUnion would refuse this valid wire
    none = %Example.ProbeCollider{armor: 7}
    none_wire = Example.Wire.write_probe_collider(none)

    check(
      match?({:ok, _}, Example.Wire.read_probe_collider(none_wire, byte_size(none_wire) * 8)),
      "the all-None union wire reads (no MaxBits over-bounding)"
    )

    check(
      div(Example.Wire.measure_probe_collider(none) + 7, 8) == byte_size(none_wire),
      "measure prices the selected arm, not MaxBits"
    )

    # NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
    # [0, 2]; forcing both bits makes 3 and the read must refuse
    g_pc = golden("probecollider")
    <<b0, b1, rest::binary>> = g_pc
    corrupt_tag = <<b0, b1 ||| 0x03, rest::binary>>

    check(
      Example.Wire.read_probe_collider(corrupt_tag, byte_size(g_pc) * 8) == :error,
      "an out-of-range union tag is refused (SPEC §4.8)"
    )

    # NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at bit
    # offset 10 with range [0, 100]; all seven bits decode 127
    <<c0, c1, c2, crest::binary>> = g_pc
    corrupt_arm = <<c0, c1 ||| 0xFC, c2 ||| 0x01, crest::binary>>

    check(
      Example.Wire.read_probe_collider(corrupt_arm, byte_size(g_pc) * 8) == :error,
      "a corrupt union arm payload is refused (SPEC §4.8)"
    )

    # the write side validates the tag BEFORE it rides
    expect_raise(
      fn -> Example.Wire.write_probe_shape(%Example.ProbeShape{type: 3}) end,
      "an out-of-set union tag must trip the writer contract"
    )

    # ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
    ps = %Example.ProbeSample{
      orientation: 90.0,
      raw_delta: -5,
      big_delta: -1_234_567_890_123,
      weapon: Example.Weapon.laser(),
      has_target: true,
      target_id: 777,
      # untaken side on the wire — must read back ZERO
      idle_ticks: 12345,
      samples: [42]
    }

    ps_wire = Example.Wire.write_probe_sample(ps)
    {:ok, out_ps} = Example.Wire.read_probe_sample(ps_wire, byte_size(ps_wire) * 8)

    check(
      out_ps.active and out_ps.weapon == Example.Weapon.laser() and out_ps.has_target and
        out_ps.target_id == 777,
      "the taken branch round-trips, nested branch included"
    )

    check(out_ps.idle_ticks == 0, "the untaken else side reads as zero (SPEC §5)")
    check(out_ps.orientation == 90.0, "compressed float round-trips exactly at its resolution")

    check(
      div(Example.Wire.measure_probe_sample(ps) + 7, 8) == byte_size(ps_wire),
      "ProbeSample measure vs written bytes"
    )

    ps_idle = %{ps | active: false, has_target: false}
    idle_wire = Example.Wire.write_probe_sample(ps_idle)
    {:ok, out_idle} = Example.Wire.read_probe_sample(idle_wire, byte_size(idle_wire) * 8)
    check(not out_idle.active and out_idle.idle_ticks == 12345, "the else branch round-trips")

    check(
      out_idle.weapon == Example.Weapon.none() and not out_idle.has_target and
        out_idle.target_id == 0,
      "the whole untaken then side reads as zero, nested branch included"
    )

    # ---- ProbeArray: transitive defaults and its C++ pin ----
    fresh = %Example.ProbeArray{}
    check(Enum.all?(fresh.samples, & &1.active), "defaults reach through a fixed array")

    check(
      fresh.config.retries == -1 and fresh.config.preferred == Example.Weapon.railgun(),
      "defaults reach through a plain member"
    )

    pa = %Example.ProbeArray{
      samples: [
        %Example.ProbeSample{
          orientation: 90.0,
          raw_delta: -5,
          big_delta: -1_234_567_890_123,
          weapon: Example.Weapon.laser(),
          has_target: true,
          target_id: 777,
          samples: [42]
        },
        %Example.ProbeSample{
          active: false,
          orientation: -45.5,
          raw_delta: 7,
          big_delta: 99,
          idle_ticks: 1000,
          samples: [7, 8]
        }
      ],
      config: %Example.ProbeConfig{retries: 3, preferred: Example.Weapon.missile()}
    }

    out_pa =
      pin(
        "probearray",
        pa,
        &Example.Wire.write_probe_array/1,
        &Example.Wire.read_probe_array/2,
        &Example.Wire.measure_probe_array/1
      )

    [_, s1] = out_pa.samples
    check(not s1.active and s1.idle_ticks == 1000, "nested else branch round-trips")

    check(
      s1.weapon == Example.Weapon.none() and not s1.has_target,
      "nested untaken side reads as zero (SPEC §5)"
    )

    check(
      out_pa.config.retries == 3 and out_pa.config.preferred == Example.Weapon.missile(),
      "config round-trips"
    )

    # ---- ProbeReport: nested composition, and the widened flags wire ----
    pr = %Example.ProbeReport{
      header: %Example.ProbeHeader{version: 3, probe_id: 0xCAFEBABE},
      flags: Example.Wire.probe_flags_armed() ||| Example.Wire.probe_flags_damaged(),
      echo: %Example.Test{test_a: 555, test_b: 1000}
    }

    pr_wire = Example.Wire.write_probe_report(pr)
    {:ok, out_pr} = Example.Wire.read_probe_report(pr_wire, byte_size(pr_wire) * 8)

    check(
      out_pr.header.probe_id == 0xCAFEBABE and
        out_pr.flags == (Example.Wire.probe_flags_armed() ||| Example.Wire.probe_flags_damaged()) and
        out_pr.echo.test_a == 555 and out_pr.echo.test_b == 1000,
      "ProbeReport round-trips — a named type as an ordinary field"
    )

    # a mask bit above the widened 8-bit wire is refused, not truncated
    expect_raise(
      fn -> Example.Wire.write_probe_report(%{pr | flags: 1 <<< 9}) end,
      "a mask bit above the flags wire width must trip the writer contract"
    )

    # ---- Block: the bytes(N) framing; Heartbeat: the empty body ----
    block = %Example.Block{data: :binary.list_to_bin(Enum.to_list(0..99))}
    block_wire = Example.Wire.write_block(block)
    {:ok, out_block} = Example.Wire.read_block(block_wire, byte_size(block_wire) * 8)
    check(out_block.data == block.data, "Block round-trips — bytes(N) framing")
    check(rem(Example.Wire.measure_block(block), 8) == 0, "Block wire ends byte-aligned")

    hb = %Example.Heartbeat{}

    check(
      Example.Wire.write_heartbeat(hb) == <<>> and
        Example.Wire.read_heartbeat(<<>>, 0) == {:ok, hb} and
        Example.Wire.measure_heartbeat(hb) == 0,
      "Heartbeat — presence is the payload (SPEC §4.6)"
    )

    # ---- the readers agree on what they REJECT, and never raise ----
    chat_golden = golden("chat")
    <<h0, h1, h2, h3, _skip, hrest::binary>> = chat_golden
    corrupt_null = <<h0, h1, h2, h3, 0, hrest::binary>>

    check(
      Example.Wire.read_chat(corrupt_null, byte_size(corrupt_null) * 8) == :error,
      "an interior null is rejected (SPEC §4.7)"
    )

    corrupt_utf8 = <<h0, h1, h2, h3, 0xFF, hrest::binary>>

    check(
      Example.Wire.read_chat(corrupt_utf8, byte_size(corrupt_utf8) * 8) == :error,
      "invalid UTF-8 is rejected (SPEC §4.7 — strings are UTF-8 by contract)"
    )

    truncated = binary_part(chat_golden, 0, 3)

    check(
      Example.Wire.read_chat(truncated, byte_size(truncated) * 8) == :error,
      "truncation is refused, not raised"
    )

    check(
      Example.Wire.read_chat(truncated, 4096) == :error,
      "an oversized num_bits is refused up front"
    )

    probe_golden = golden("probe_header")
    <<p0, p1, prest::binary>> = probe_golden
    corrupt_reserved = <<p0, p1 ||| 0x08, prest::binary>>

    check(
      Example.Wire.read_probe_header(corrupt_reserved, byte_size(probe_golden) * 8) == :error,
      "a nonzero reserved bit is rejected (SPEC §4.3)"
    )

    # an out-of-range array count is refused before any element rides —
    # corrupt the count bits INSIDE a complete valid wire (the preamble is
    # 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4)
    packet_golden = golden("inputpacket")
    <<head::binary-size(18), b18, tail::binary>> = packet_golden
    corrupt_count = <<head::binary, (b18 &&& bnot(0x1F)) ||| 17, tail::binary>>

    check(
      Example.Types.read_input_packet(corrupt_count, byte_size(corrupt_count) * 8) == :error,
      "an out-of-range count is refused before the loop"
    )

    # ---- writer contracts (always on — no release twin exists) ----
    expect_raise(
      fn -> Example.Types.write_ship_create(%{make_ship_create() | health: 5000}) end,
      "an out-of-range ranged write must trip the writer contract"
    )

    expect_raise(
      fn -> Example.Wire.write_chat(%Example.Chat{text: String.duplicate("x", 999)}) end,
      "an out-of-range string length must trip the writer contract"
    )

    expect_raise(
      fn ->
        Example.Types.write_input_packet(%Example.InputPacket{
          inputs: List.duplicate(%Example.Input{}, 17)
        })
      end,
      "an out-of-range array count must trip the writer contract"
    )

    expect_raise(
      fn -> Example.Types.write_ship_create(%{make_ship_create() | ship_type: 99}) end,
      "enum headroom above the wire range must trip the writer contract"
    )

    # ---- flag_name / flag_names: per-bit names and the set renderer ----
    check(Example.Enums.flag_name_ship_flags(0) == "FiringLaser", "flag_name names bit 0")
    check(Example.Enums.flag_name_ship_flags(9) == "???", "flag_name is out-of-range safe")
    check(Example.Enums.flag_names_ship_flags(0) == "0", "flag_names renders the empty set as 0")

    check(
      Example.Enums.flag_names_ship_flags(
        Example.Enums.ship_flags_firing_laser() ||| Example.Enums.ship_flags_braking()
      ) == "FiringLaser|Braking",
      "flag_names renders the set bits"
    )

    check(
      Example.Enums.flag_names_ship_flags(Example.Enums.ship_flags_aiming() ||| 1 <<< 63) ==
        "Aiming|0x8000000000000000",
      "flag_names renders unknown high bits as hex"
    )

    check(
      Example.Wire.enum_name_weapon(Example.Weapon.railgun()) == "Railgun",
      "enum_name names a variant"
    )

    check(Example.Wire.enum_name_weapon(15) == "???", "enum_name is headroom-safe")

    # ============== THE BENCH CORPUS (BENCH-STANDARD.md §1.5) ==============
    # The same pinned instances test/bench/main.cpp authored into
    # testdata/wire/{bench_*,real_packet}.bin — the oracle gate the Elixir
    # bench runner is held to; this leg carries it because the runner imports
    # these exact modules.
    packet = %Bench.BenchPacket{
      a: -37,
      b: 12345,
      c: 987_654,
      bits7: 97,
      bits13: 5000,
      bits23: 1_234_567,
      flag: true,
      x: 1.5,
      y: -3.25,
      z: 100.125,
      big: 0x123456789ABCDEF0,
      blob: Enum.map(0..16, fn i -> i * 31 &&& 0xFF end)
    }

    pin(
      "bench_packet",
      packet,
      &Bench.Bench.write_bench_packet/1,
      &Bench.Bench.read_bench_packet/2,
      &Bench.Bench.measure_bench_packet/1
    )

    check(Bench.Bench.measure_bench_packet(packet) == 392, "BenchPacket is 392 bits")

    ints = %Bench.BenchInts{
      f0: -37,
      f1: 12345,
      f2: 987_654,
      f3: 2,
      f4: -15,
      f5: 777,
      f6: -2048,
      f7: 200,
      f8: -543_210,
      f9: 99
    }

    pin(
      "bench_ints",
      ints,
      &Bench.Bench.write_bench_ints/1,
      &Bench.Bench.read_bench_ints/2,
      &Bench.Bench.measure_bench_ints/1
    )

    bits = %Bench.BenchBits{
      b7: 97,
      b13: 5000,
      b23: 1_234_567,
      b3: 5,
      b32: 0xDEADBEEF,
      b11: 1024,
      b19: 333_333,
      b48: 0xFEDCBA987654
    }

    pin(
      "bench_bits",
      bits,
      &Bench.Bench.write_bench_bits/1,
      &Bench.Bench.read_bench_bits/2,
      &Bench.Bench.measure_bench_bits/1
    )

    mixed = %Bench.BenchMixed{
      sequence: 52428,
      ack_bits: 0xA5A5A5A5,
      entity_id: 2049,
      pos_x: -16384,
      pos_y: 16383,
      pos_z: -1,
      yaw: 511,
      moving: true,
      firing: false,
      timestamp: 0x123456789ABC,
      weapon: 15
    }

    pin(
      "bench_mixed",
      mixed,
      &Bench.Bench.write_bench_mixed/1,
      &Bench.Bench.read_bench_mixed/2,
      &Bench.Bench.measure_bench_mixed/1
    )

    # RealPacket pins the ALL-DEFAULTS instance: constructed and serialized
    # unmodified — 1629 bits = 204 bytes
    real = %Realworld.RealPacket{}

    pin(
      "real_packet",
      real,
      &Realworld.RealWorld.write_real_packet/1,
      &Realworld.RealWorld.read_real_packet/2,
      &Realworld.RealWorld.measure_real_packet/1
    )

    check(Realworld.RealWorld.measure_real_packet(real) == 1629, "RealPacket is 1629 bits")

    if Process.get(:schema_test_failed, false) do
      System.halt(1)
    end

    IO.puts("OK")
  end
end

SchemaTestElixir.run()
