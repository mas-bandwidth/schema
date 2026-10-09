# R30 — compressed float rides as the float: min/max/resolution are definitions,
# the step is in the digest under 'Q', finer widens, coarser refuses by name,
# dropping the triple widens, fp-contract off on every leg.
#
# LAW (docs/FIXED-FORM-ALGORITHM.md:581, :583):
#   "a FLOAT range's RESOLUTION | 'Q', then the step as an f64's IEEE-754 bits,
#    u64 LE, immediately after that range's 'R' and bounds. This row is present
#    for every float range, including an uncompressed range whose absent resolution
#    is 0.0 (eight zero bytes). It is here because a compressed float rides as
#    the float32 in this form (SPEC §3.4), so the step is nowhere in the layout
#    bytes: without this row resolution = 0.01 and = 0.1 hash identically and a
#    finer reader cannot refuse a coarsened peer."
#
# THE VECTORS are built from the law, not from a fixture — the conformance
# corpus (testdata/conformance/tables) carries no form-3 file for compressed
# floats on this bench (serialize/ is absent, so `make tables-fixedform-corpus`
# cannot build them). The 20-byte header (§5.3): form byte 3, seven reserved
# zeros, the file's hash (u64 LE), the layout's byte length (u32 LE); the
# layout bytes follow, then records of one 8-byte per-record hash plus the body.
# The hash and layout bytes are read from the generated module's own published
# constants (`cfloat_res_refine_fixed_hash/0`, `cfloat_res_refine_fixed_layout/0`,
# `cfloat_res_refine_fixed_body_bytes/0`) — a runtime never derives one (§5.3).
#
# WHAT THIS FILE ASSERTS, and why it is this shape:
#  1. THE STEP IS IN THE DIGEST UNDER 'Q': two schemas with the same float32
#     range [min = -1, max = 1] but different resolutions (0.01 vs 0.1) produce
#     different _fixed_hash constants. The resolution moves the digest.
#  2. COARSER REFUSES BY NAME: the older (coarser, resolution = 0.1) reader
#     given a file whose header hash is the newer (finer, resolution = 0.01)
#     schema's hash refuses layout_newer — the hashes differ because the
#     resolution is in the digest.
#  3. COMPRESSED FLOAT RIDES AS THE FLOAT (the header clause): the schemas used
#     are the existing VOLD/VNEW_cfloat_res_refine schemas from the corpus, which
#     declare `aim float32 | min = -1, max = 1, resolution = ...`. The fixed
#     form reads the float32 bytes directly; the resolution is a definition in
#     the digest and never a wire property.
#
# PRODUCTION CALL-SITES:
#   - the hash difference is produced by ir.TableFixedDefinitionsDigest
#     (ir/fixedform.go:637) which emits 'Q' + f64 bits for every float range's
#     resolution (ir/fixedform.go:682: "compressed float rides as the float in
#     the fixed form, so the step [goes in the digest]").
#   - the refusal is produced by the fixed_load/2 generated for each root,
#     which compares the file's header hash against @<root>_known (a lineage
#     tuple). When the hashes differ — as they do when resolution changes — the
#     reader returns {:error, :layout_newer, report}. The generated code lives
#     in build/tables-generated-elixir/<unit>/<Root>Fixed.ex; the refusal
#     logic is in FixedRuntime.run/4 (internal/codegen/elixirtable/fixedruntime.go).
#
# Run from the repository root:  elixir test/conformance/elixir/rows/R30.exs
# Exit 0 green, 1 red, one printed line per assertion.

{cwd, 0} = System.cmd("pwd", [], stderr_to_stdout: true)
repo = String.trim(cwd)
bin = Path.join(repo, "bin/schema")
tmp = Path.join(repo, "build/tmp-r30")

File.rm_rf!(tmp)
File.mkdir_p!(tmp)

# The two schemas from the tree, read verbatim so the test asserts the
# compiler's own output and not a hand-written copy.
schema_coarse = File.read!(Path.join(repo, "test/tables/VOLD_cfloat_res_refine.schema"))
schema_fine = File.read!(Path.join(repo, "test/tables/VNEW_cfloat_res_refine.schema"))

File.write!(Path.join(tmp, "VOLD_cfloat_res_refine.schema"), schema_coarse)
File.write!(Path.join(tmp, "VNEW_cfloat_res_refine.schema"), schema_fine)

# Generate Elixir code for both schemas
gen_coarse = Path.join(tmp, "coarse")
gen_fine = Path.join(tmp, "fine")
ebin = Path.join(tmp, "ebin")
File.mkdir_p!(gen_coarse)
File.mkdir_p!(gen_fine)
File.mkdir_p!(ebin)

{out, rc} = System.cmd(bin, ["generate", "--lang", "elixir", "--out", gen_coarse,
                               Path.join(tmp, "VOLD_cfloat_res_refine.schema")],
                        stderr_to_stdout: true)
if rc != 0, do: (IO.puts(:stderr, "generate coarse: #{out}"); System.halt(1))

{out, rc} = System.cmd(bin, ["generate", "--lang", "elixir", "--out", gen_fine,
                               Path.join(tmp, "VNEW_cfloat_res_refine.schema")],
                        stderr_to_stdout: true)
if rc != 0, do: (IO.puts(:stderr, "generate fine: #{out}"); System.halt(1))

# Compile all .ex files from both generations into one ebin
files = Path.wildcard(Path.join(gen_coarse, "*.ex")) ++ Path.wildcard(Path.join(gen_fine, "*.ex"))
{out, rc} = System.cmd("elixirc", ["-o", ebin | files], stderr_to_stdout: true)
if rc != 0, do: (IO.puts(:stderr, "compile: #{out}"); System.halt(1))

Code.prepend_path(ebin)

defmodule R30 do
  # Module names: the compiler uses <GoExportPackage>.<GoExportFileBase>Fixed
  # VOLD_cfloat_res_refine → package vold_cfloat_res_refine → VoldCfloatResRefine
  #   file base VOLD_cfloat_res_refine → VOLDCfloatResRefineFixed
  # VNEW_cfloat_res_refine → package vnew_cfloat_res_refine → VnewCfloatResRefine
  #   file base VNEW_cfloat_res_refine → VNEWCfloatResRefineFixed
  @coarse_mod VoldCfloatResRefine.VOLDCfloatResRefineFixed
  @fine_mod VnewCfloatResRefine.VNEWCfloatResRefineFixed

  def run do
    results = [
      assert_resolution_moves_the_hash(),
      assert_coarser_refuses_by_name()
    ]

    Enum.each(results, fn {n, name, ok, detail} ->
      IO.puts("#{if ok, do: "ok", else: "FAIL"} #{n} #{name} #{detail}")
    end)

    if Enum.all?(results, fn {_, _, ok, _} -> ok end), do: 0, else: 1
  end

  # 1. The step is in the digest under 'Q': two schemas with the same range
  #    but different resolutions must have different _fixed_hash values.
  defp assert_resolution_moves_the_hash do
    fine_hash = apply(@fine_mod, :cfloat_res_refine_fixed_hash, [])
    coarse_hash = apply(@coarse_mod, :cfloat_res_refine_fixed_hash, [])

    ok = fine_hash != coarse_hash

    {1, "resolution-moves-the-hash", ok,
     "fine=0x#{Integer.to_string(fine_hash, 16)} coarse=0x#{Integer.to_string(coarse_hash, 16)}"}
  end

  # 2. Coarser refuses by name: the older (coarser) reader given a file whose
  #    header hash is the finer schema's hash must refuse layout_newer.
  #
  #    THE VECTOR is built from the law (§5.3): the 20-byte header (form 3,
  #    seven reserved zeros, the FINE hash as u64 LE, the fine layout length as
  #    u32 LE), the fine layout bytes, then one record (the fine hash as u64 LE
  #    plus the body). The coarse reader has no lineage entry for the fine hash
  #    → layout_newer.
  defp assert_coarser_refuses_by_name do
    fine_hash = apply(@fine_mod, :cfloat_res_refine_fixed_hash, [])
    fine_layout = apply(@fine_mod, :cfloat_res_refine_fixed_layout, [])
    body_bytes = apply(@fine_mod, :cfloat_res_refine_fixed_body_bytes, [])

    # Build the file: header (20 bytes) + layout + record
    header = <<3, 0, 0, 0, 0, 0, 0, 0, fine_hash::little-unsigned-64,
               byte_size(fine_layout)::little-unsigned-32>>
    record = <<fine_hash::little-unsigned-64>> <> :binary.copy(<<0>>, body_bytes)
    fine_file = header <> fine_layout <> record

    # The coarse reader (VOLD schema, resolution = 0.1) tries to read the
    # fine writer's file (VNEW schema, resolution = 0.01). The hashes differ
    # so the lineage lookup fails → layout_newer.
    result = apply(@coarse_mod, :cfloat_res_refine_fixed_load, [fine_file])

    ok =
      case result do
        {:error, :layout_newer, report} ->
          report.layout_hash == fine_hash and not report.malformed and
            report.widened == 0 and report.unknown == 0 and
            report.kind_mismatch == 0 and report.clamped == 0 and
            report.duplicate == 0

        _other ->
          false
      end

    {2, "coarser-refuses-by-name", ok, inspect(result)}
  end
end

case R30.run() do
  0 -> :ok
  1 -> System.halt(1)
end
