# R22 — the closure rule: every table or type reached by value is itself fixed;
# a pointer, map or unbounded array in the closure is a compile refusal; T is
# never in its own closure.
#
# docs/FIXED-FORM-ALGORITHM.md:961:
#   "closure(T) is T, every table or type it reaches by value, every enum, union,
#    flags and constant any of them names, recursively. Every table or type in it
#    is declared fixed; a pointer, map or unbounded array in it is a compile
#    refusal; T is never in its own closure."
#
# The production path is: the compiler's ir.TableFixedSupported (ir/fixedform.go)
# is the single function that walks the closure and returns false when a pointer,
# map or unbounded array is found. fixedRoots (internal/codegen/elixirtable/fixedform.go)
# calls it and excludes any table it rejects, so no Fixed module is emitted for
# that table. The "compile refusal" is the absence of the Fixed surface — the
# table keeps form 1 and is not offered as fixed.
#
# Run:  elixir test/conformance/elixir/rows/R22.exs
# Exit: 0 = green (all assertions pass), 1 = red

{cwd, 0} = System.cmd("pwd", [], stderr_to_stdout: true)
repo = String.trim(cwd)
bin = Path.join(repo, "bin/schema")
tmp = Path.join(repo, "build/tmp-r22")

File.rm_rf!(tmp)
File.mkdir_p!(tmp)

defmodule R22 do
  def run(bin, tmp) do
    results = [
      assert_valid_closure(bin, tmp),
      assert_pointer_in_closure_refused(bin, tmp),
      assert_map_in_closure_refused(bin, tmp),
      assert_unbounded_array_in_closure_refused(bin, tmp),
      assert_self_referencing_pointer_not_in_own_closure(bin, tmp)
    ]

    failures = Enum.count(results, &(!&1))

    if failures == 0 do
      IO.puts("R22 elixir closure: 5 assertions passed")
      System.halt(0)
    else
      IO.puts("R22 elixir closure: #{failures} assertion(s) FAILED")
      System.halt(1)
    end
  end

  def assert_valid_closure(bin, tmp) do
    name = "a1_valid_closure"
    schema = """
    package r22a1

    type Inner {
        x int32
        y int32
    }

    fixed table Outer {
        inner Inner
        flag bool
    }
    """

    out = compile(name, schema, bin, tmp)
    has_fixed = File.exists?(Path.join(out, "testFixed.ex"))
    has_outer = file_has(out, "testFixed.ex", "defmodule R22a1.Outer")
    has_inner = file_has(out, "testFixed.ex", "%R22a1.Inner{")
    ok = has_fixed and has_outer and has_inner
    say("A1: valid closure gets Fixed modules", ok,
      "testFixed.ex=#{has_fixed}, Outer=#{has_outer}, Inner=#{has_inner}")
    ok
  end

  def assert_pointer_in_closure_refused(bin, tmp) do
    name = "a2_pointer_refused"
    schema = """
    package r22a2

    table Node {
        value int32
        next  *Node
    }

    fixed table Container {
        node Node
    }
    """

    out = compile(name, schema, bin, tmp)
    ok = not has_fixed(out)
    say("A2: pointer in closure refused (no Fixed modules)", ok,
      "any Fixed file=#{has_fixed(out)}")
    ok
  end

  def assert_unbounded_array_in_closure_refused(bin, tmp) do
    name = "a3_unbounded_refused"
    schema = """
    package r22a3

    table Bag {
        items []int32
    }

    fixed table Holder {
        bag Bag
    }
    """

    out = compile(name, schema, bin, tmp)
    ok = not has_fixed(out)
    say("A3: unbounded array in closure refused (no Fixed modules)", ok,
      "any Fixed file=#{has_fixed(out)}")
    ok
  end

  def assert_map_in_closure_refused(bin, tmp) do
    name = "a4_map_refused"
    schema = """
    package r22a4

    table Mapping {
        entries map[string(8)]int32
    }

    fixed table Owner {
        mapping Mapping
    }
    """

    out = compile(name, schema, bin, tmp)
    ok = not has_fixed(out)
    say("A4: map in closure refused (no Fixed modules)", ok,
      "any Fixed file=#{has_fixed(out)}")
    ok
  end

  def assert_self_referencing_pointer_not_in_own_closure(bin, tmp) do
    name = "a5_self_ref_not_own_closure"
    schema = """
    package r22a5

    fixed table Link {
        payload int32
        next    *Link
    }
    """

    out = compile(name, schema, bin, tmp)
    ok = not has_fixed(out)
    say("A5: self-referencing pointer refuses fixed (T not in own closure)", ok,
      "any Fixed file=#{has_fixed(out)}")
    ok
  end

  defp file_has(out, file, pat) do
    case File.read(Path.join(out, file)) do
      {:ok, c} -> String.contains?(c, pat)
      _ -> false
    end
  end

  defp has_fixed(out) do
    case File.ls(out) do
      {:ok, files} -> Enum.any?(files, &String.contains?(&1, "Fixed"))
      _ -> false
    end
  end

  defp compile(name, schema, bin, tmp) do
    dir = Path.join(tmp, name)
    File.rm_rf!(dir)
    File.mkdir_p!(dir)
    schema_file = Path.join(dir, "test.schema")
    File.write!(schema_file, schema)
    out = Path.join(dir, "gen")
    {output, rc} = System.cmd(bin, ["generate", "--lang", "elixir", "--out", out, schema_file],
      stderr_to_stdout: true)
    if rc != 0 do
      IO.puts("  note: compiler refused #{name} (exit #{rc}): #{String.trim(output)}")
    end
    out
  end

  defp say(label, ok, detail) do
    s = if ok, do: "PASS", else: "FAIL"
    IO.puts("  #{s} #{label} — #{detail}")
    ok
  end
end

R22.run(bin, tmp)
