package elixirtable

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestOffsetsComposeFromHalvesElixir pins docs/PORTING.md M2 on the Elixir
// leg: the block's offset_of and the cook's self-relative delta are composed
// from two 32-bit halves, so no bignum is formed before the bound that refuses
// it runs. Two clauses:
//
//   - (a) THE ORDINARY PATH, block-ordinary and cook-ordinary: every corpus
//     block open, every row of every array of the opened block, and every
//     reference slot of every node of three corpus cooks is read with no more
//     heap words per call than a SAME-SHAPED IMMEDIATE-VALUE CONTROL, the same
//     generated code with only the composition replaced by an expression that
//     cannot form a bignum. A FORCED-BOXING POSITIVE CONTROL, the same code
//     with the composition made to pass through a bignum, must exceed the
//     control, so a zero or insensitive meter cannot pass.
//   - (b) THE FORGED WORD, block and cook: a FORGED offset_of word or cook
//     delta word (high half 0x7FFFFFFF, and the delta's negative twin) is
//     refused with no more minor collections than the same refusal of a word
//     whose high half is small: the refusal is decided from the high half
//     before any 64-bit integer is formed.
//
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestOffsetsComposeFromHalvesElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// HOW ALLOCATION IS MEASURED. The first version of this test bracketed the
	// open with garbage_collection_info heap_size inside a process with a huge
	// min_heap_size. Measured on OTP 29: heap_size and total_heap_size are the
	// heap BLOCK size and do not move as terms are allocated until a collection
	// runs, so a bignum formed by a whole-word read changed nothing and the
	// negative control could never go red. What does move is the number of
	// minor collections, so the probe runs in a spawned process with a small
	// heap and counts minor_gcs over many calls. A refusal of a forged offset
	// whose high half is 0x7FFFFFFF is compared with a refusal of a forged offset
	// whose high half is 1 (an immediate in either reader): the same code runs
	// and the same garbage is made, except that only a reader that forms the
	// 64-bit integer allocates a bignum for the first. Equal collection counts
	// mean the high half was refused before any 64-bit integer was formed.
	heaps := `
gcs = fn ->
  {:garbage_collection, p} = :erlang.process_info(self(), :garbage_collection)
  p[:minor_gcs]
end
`
	probeSetup := `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
`
	t.Run("block", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "block", "Render.schema"),
			filepath.Join(root, "tables", "block", "Padded.schema"),
		)
		files := generateElixir(t, unit)
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)
		golden := filepath.Join(root, "testdata", "wire", "tables", "block_render.bin")
		if _, err := os.ReadFile(golden); err != nil {
			t.Fatal(err)
		}
		probe := probeSetup + `image = File.read!(` + elixirString(golden) + `)
parent = self()
{_pid, ref} = :erlang.spawn_opt(fn ->
` + heaps + `
Probe.check(is_map(Blockdemo.RenderBlock.block_open_render_frame(image)) or
  is_tuple(Blockdemo.RenderBlock.block_open_render_frame(image)) or
  is_struct(Blockdemo.RenderBlock.block_open_render_frame(image)), "an ordinary open is accepted")
[array | _] = Blockdemo.RenderBlock.render_frame_block_arrays()
off = array.offset_of_offset
<<head::binary-size(off), _::binary-size(8), tail::binary>> = image
# a FORGED offset_of word: high half 0x7FFFFFFF (>= 0x08000000, past a BEAM
# immediate) must be refused from the high half alone, before any 64-bit
# integer is formed
forged = head <> <<0::little-unsigned-32, 0x7FFFFFFF::little-unsigned-32>> <> tail
# the control: an out-of-extent offset that is an immediate in either reader
small = head <> <<0::little-unsigned-32, 1::little-unsigned-32>> <> tail
Probe.check(Blockdemo.RenderBlock.block_open_render_frame(forged) == :error, "a forged offset_of high half is refused")
Probe.check(Blockdemo.RenderBlock.block_open_render_frame(small) == :error, "an out-of-extent offset_of is refused")
count = fn img ->
  :erlang.garbage_collect()
  g0 = gcs.()
  Enum.each(1..50_000, fn _ -> Blockdemo.RenderBlock.block_open_render_frame(img) end)
  gcs.() - g0
end
small_gcs = count.(small)
forged_gcs = count.(forged)
IO.puts("minor gcs over 50000 refusals: forged=#{forged_gcs} immediate=#{small_gcs}")
Probe.check(forged_gcs <= small_gcs, "forged offset_of refusal allocated: " <> Integer.to_string(forged_gcs) <> " collections against " <> Integer.to_string(small_gcs))
send(parent, :done)
end, [:monitor])
receive do
  :done -> IO.puts("ok")
  {:DOWN, ^ref, _, _, reason} -> IO.puts(:stderr, "probe process died: " <> inspect(reason)); System.halt(1)
after 120_000 -> IO.puts(:stderr, "probe timed out"); System.halt(1)
end
`
		t.Log(runProbe(t, elixirBin, dir, names, probe))
	})
	t.Run("cook", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "arms", "Gate.schema"),
		)
		files := generateElixir(t, unit)
		// the cook runtime module that holds deref/2
		var mod string
		for _, data := range files {
			if m := cookRuntimeModuleRE.FindStringSubmatch(string(data)); m != nil {
				mod = m[1]
			}
		}
		if mod == "" {
			t.Fatal("no CookRuntime module in the generated Elixir")
		}
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)
		// The same measurement as the block's: a forged delta word (high half
		// 0x7FFFFFFF, and its negative twin) is refused against a twin whose
		// high half is 1 (or -1), an immediate in either reader. Both resolve
		// outside the region and answer :error; only a reader that forms the
		// 64-bit integer allocates a bignum for the forged one, and that shows
		// as extra minor collections over many refusals.
		probe := probeSetup + `rt = ` + mod + `.CookRuntime
parent = self()
{_pid, ref} = :erlang.spawn_opt(fn ->
` + heaps + `
region = <<0::little-unsigned-32, 0::little-unsigned-32, 0::unit(8)-size(24)>>
fwd = <<0::little-unsigned-32, 0x7FFFFFFF::little-signed-32, 0::unit(8)-size(24)>>
fwd_neg = <<0::little-unsigned-32, -0x7FFFFFFF::little-signed-32, 0::unit(8)-size(24)>>
small = <<0::little-unsigned-32, 1::little-signed-32, 0::unit(8)-size(24)>>
small_neg = <<0::little-unsigned-32, -1::little-signed-32, 0::unit(8)-size(24)>>
Probe.check(rt.deref(region, 0) == :null, "a zero delta is null")
Probe.check(rt.deref(fwd, 0) == :error, "a forged cook delta high half is refused")
Probe.check(rt.deref(fwd_neg, 0) == :error, "a forged negative cook delta high half is refused")
Probe.check(rt.deref(small, 0) == :error, "an out-of-region cook delta is refused")
Probe.check(rt.deref(small_neg, 0) == :error, "a negative out-of-region cook delta is refused")
count = fn img ->
  :erlang.garbage_collect()
  g0 = gcs.()
  Enum.each(1..50_000, fn _ -> rt.deref(img, 0) end)
  gcs.() - g0
end
small_gcs = count.(small)
forged_gcs = count.(fwd)
Probe.check(forged_gcs <= small_gcs, "forged cook delta refusal allocated: " <> Integer.to_string(forged_gcs) <> " collections against " <> Integer.to_string(small_gcs))
small_neg_gcs = count.(small_neg)
forged_neg_gcs = count.(fwd_neg)
IO.puts("minor gcs over 50000 refusals: forged=#{forged_gcs} immediate=#{small_gcs} forged_neg=#{forged_neg_gcs} immediate_neg=#{small_neg_gcs}")
Probe.check(forged_neg_gcs <= small_neg_gcs, "forged negative cook delta refusal allocated: " <> Integer.to_string(forged_neg_gcs) <> " collections against " <> Integer.to_string(small_neg_gcs))
send(parent, :done)
end, [:monitor])
receive do
  :done -> IO.puts("ok")
  {:DOWN, ^ref, _, _, reason} -> IO.puts(:stderr, "probe process died: " <> inspect(reason)); System.halt(1)
after 120_000 -> IO.puts(:stderr, "probe timed out"); System.halt(1)
end
`
		t.Log(runProbe(t, elixirBin, dir, names, probe))
	})
	// (a) THE ORDINARY BLOCK: the corpus image opened, then every row of every
	// array read through its generated _at accessor and every array through its
	// bulk accessor. The composition is u64halves' last expression.
	t.Run("block-ordinary", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "block", "Render.schema"),
			filepath.Join(root, "tables", "block", "Padded.schema"),
		)
		golden := filepath.Join(root, "testdata", "wire", "tables", "block_render.bin")
		probe := `mod = Blockdemo.RenderBlock
image = File.read!(` + elixirString(golden) + `)
{:ok, block} = mod.block_open_render_frame(image)
fns = mod.__info__(:functions)
ats =
  for {f, 2} <- fns, s = Atom.to_string(f), String.starts_with?(s, "render_frame_"),
      String.ends_with?(s, "_at"), do: String.trim_trailing(s, "_at")
Probe.check(length(ats) > 0, "the Render frame block has row accessors")
rows =
  for a <- ats, i <- 0..(apply(mod, String.to_atom(a <> "_count"), [block]) - 1)//1,
      do: {String.to_atom(a <> "_at"), i}
bulk = for a <- ats, do: String.to_atom(a)
Probe.check(length(rows) > 0, "the corpus block has rows to read")
Probe.check(Enum.all?(rows, fn {f, i} -> is_binary(apply(mod, f, [block, i])) end),
  "every row of the corpus block is read")
IO.puts("ROWS #{length(rows)} in #{length(ats)} arrays")
Meter.report("open", fn -> {:ok, _} = mod.block_open_render_frame(image) end)
Meter.report("rows", fn ->
  Enum.each(rows, fn {f, i} -> apply(mod, f, [block, i]) end)
  Enum.each(bulk, fn f -> Stream.run(apply(mod, f, [block])) end)
end)
`
		measureOrdinary(t, elixirBin, unit, "BlockRuntime.ex",
			`if hi >= 0x08000000, do: nil, else: hi * 0x100000000 + lo`+"\n",
			`if hi >= 0x08000000, do: nil, else: lo`+"\n",
			`if hi >= 0x08000000, do: nil, else: hi * 0x100000000 + lo + 0x7FFFFFFF00000000 - 0x7FFFFFFF00000000`+"\n",
			probe, []string{"open", "rows"}, []callSite{
				{"offset_of = ", regexp.MustCompile(`^\s*offset_of = B\.u64halves\(data, a\.offset_of_offset\)$`)},
				{"B.row(", regexp.MustCompile(`^\s*B\.row\(data, B\.u64halves\(data, \d+\), B\.u32\(data, \d+\), B\.u32\(data, \d+\), index\)$`)},
				{"B.rows(", regexp.MustCompile(`^\s*B\.rows\(data, B\.u64halves\(data, \d+\), B\.u32\(data, \d+\), B\.u32\(data, \d+\)\)$`)},
			})
	})
	// (a) THE ORDINARY COOK: three corpus cooks (a tree, a shared graph, a deep
	// chain), every node reached from the root walked once to list its
	// reference slots, then every slot dereferenced through its generated
	// accessor. The composition is deref's last clause.
	t.Run("cook-ordinary", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "pointers", "Graph.schema"),
			filepath.Join(root, "tables", "pointers", "Marks.schema"),
			filepath.Join(root, "tables", "pointers", "Parts.schema"),
		)
		var probe strings.Builder
		probe.WriteString(`defmodule Walk do
  @mod Graphdemo.GraphCook
  # the reference slots of each node kind, and the kind each one reaches
  @refs %{
    scene: [scene_node_head: :list, scene_node_tree: :tree, scene_node_settings: :leaf, scene_node_alias: :list],
    list: [list_node_node_next: :list],
    tree: [tree_node_node_left: :tree, tree_node_node_right: :tree],
    leaf: []
  }
  def slots(region), do: walk(region, [{:scene, 0}], MapSet.new(), [])
  defp walk(_region, [], _seen, acc), do: Enum.reverse(acc)
  defp walk(region, [{kind, node} | rest], seen, acc) do
    if MapSet.member?(seen, node) do
      walk(region, rest, seen, acc)
    else
      {next, acc} =
        Enum.reduce(@refs[kind], {rest, acc}, fn {f, to}, {next, acc} ->
          Probe.check(function_exported?(@mod, f, 2), "the generated cook has #{f}/2")
          case apply(@mod, f, [region, node]) do
            :null -> {next, [{f, node} | acc]}
            {:ok, target} -> {[{to, target} | next], [{f, node} | acc]}
            other -> Probe.check(false, "#{f} at node #{node} answered #{inspect(other)}")
          end
        end)
      walk(region, next, MapSet.put(seen, node), acc)
    end
  end
end
mod = Graphdemo.GraphCook
`)
		var labels []string
		for _, name := range []string{"graph_tree", "graph_shared", "graph_deep"} {
			path := filepath.Join(root, "testdata", "conformance", "tables", "cook-write", name+".cook")
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			probe.WriteString(`{:ok, cook} = mod.cook_open_scene(File.read!(` + elixirString(path) + `))
slots = Walk.slots(cook.region)
Probe.check(Enum.any?(slots, fn {f, n} -> apply(mod, f, [cook.region, n]) != :null end), "` + name + ` has a non-null reference")
IO.puts("SLOTS ` + name + ` #{length(slots)}")
region = cook.region
Meter.report("` + name + `", fn -> Enum.each(slots, fn {f, n} -> apply(mod, f, [region, n]) end) end)
`)
			labels = append(labels, name)
		}
		measureOrdinary(t, elixirBin, unit, "CookRuntime.ex",
			`true -> inside(region, at + high * 0x100000000 + low)`+"\n",
			`true -> inside(region, at + if(high < 0, do: low - 0x100000000, else: low))`+"\n",
			`true -> inside(region, at + (high * 0x100000000 + low + 0x7FFFFFFF00000000 - 0x7FFFFFFF00000000))`+"\n",
			probe.String(), labels, []callSite{
				{"C.deref(", regexp.MustCompile(`^\s*def [a-z0-9_]+\(region, node\), do: C\.deref\(region, node \+ \d+\)$`)},
			})
	})
}

var cookRuntimeModuleRE = regexp.MustCompile(`defmodule ([A-Za-z0-9_.]+)\.CookRuntime do`)

// ordinaryMeter is the allocation meter of clause (a), the one
// test/elixir-alloc/main.exs uses (docs/PORTING.md I1): the work runs in a
// FRESH spawned process traced with :erlang.trace/3 on :garbage_collection, and
// the heap words its collections reclaimed, plus the growth of what is still
// live after a closing full collection, is what the loop allocated. Not
// heap_size alone: on OTP 29 that is the heap block's capacity and does not
// move as terms are allocated until a collection runs.
const ordinaryMeter = `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
defmodule Meter do
  @calls 2000
  # the MEDIAN of three fresh-process measurements: one in a few comes in
  # short by a young generation's worth when a collection boundary falls
  # badly (measured on OTP 29: 236.3 against a steady 238.055 words per open),
  # and a single short control reading must not red a reader that matches it
  def report(label, fun) do
    [_, w, _] = Enum.sort([words(fun), words(fun), words(fun)])
    IO.puts("WORDS #{label} #{:erlang.float_to_binary(w, decimals: 3)}")
  end
  def words(fun) do
    parent = self()
    {pid, mon} =
      spawn_monitor(fn ->
        fun.()
        :erlang.garbage_collect()
        live0 = used()
        reclaimed = traced(fn -> loop(fun, @calls) end)
        live1 = used()
        send(parent, {:words, self(), reclaimed + (live1 - live0)})
      end)
    receive do
      {:words, ^pid, w} ->
        Process.demonitor(mon, [:flush])
        w / @calls
      {:DOWN, ^mon, :process, ^pid, reason} ->
        Probe.check(false, "the measuring process died: #{inspect(reason)}")
    end
  end
  defp loop(_fun, 0), do: :ok
  defp loop(fun, k) do
    fun.()
    loop(fun, k - 1)
  end
  defp used do
    {:garbage_collection_info, info} = :erlang.process_info(self(), :garbage_collection_info)
    heap_of(info)
  end
  defp traced(work) do
    me = self()
    tracer = spawn(fn -> collect(0, nil) end)
    :erlang.trace(me, true, [:garbage_collection, {:tracer, tracer}])
    try do
      work.()
      :erlang.garbage_collect()
    after
      :erlang.trace(me, false, [:garbage_collection])
    end
    send(tracer, {:reclaimed, me})
    receive do
      {:reclaimed, words} -> words
    end
  end
  defp collect(sum, before) do
    receive do
      {:trace, _pid, kind, info} when kind in [:gc_minor_start, :gc_major_start] ->
        collect(sum, heap_of(info))
      {:trace, _pid, kind, info} when kind in [:gc_minor_end, :gc_major_end] ->
        collect(sum + max((before || heap_of(info)) - heap_of(info), 0), nil)
      {:reclaimed, from} ->
        send(from, {:reclaimed, sum})
    end
  end
  defp heap_of(info) do
    Keyword.get(info, :heap_size, 0) + Keyword.get(info, :old_heap_size, 0) +
      Keyword.get(info, :mbuf_size, 0)
  end
end
`

// callSite pins one kind of generated call that reaches the composition:
// every line containing marker must match shape.
type callSite struct {
	marker string
	shape  *regexp.Regexp
}

// ordinarySlack is the measurement noise allowed per call, in heap words: the
// meter's own boundary moves the total by tens of words over 2000 calls, and
// one bignum is two words (a header and a digit), so half a word per call is
// far below one bignum per call and far above the noise.
const ordinarySlack = 0.5

// measureOrdinary runs probe over the generated unit three ways — as
// generated (real), with the composition `real` in the runtime file replaced
// by the immediate-value `control`, and by the forced-boxing `boxed` — and
// holds every label: real <= control + slack, and boxed > control + slack.
func measureOrdinary(t *testing.T, elixirBin string, unit *ir.Unit, runtime, real, control, boxed, probe string, labels []string, sites []callSite) {
	t.Helper()
	files := generateElixir(t, unit)
	// THE CALL SITES. The three variants differ only in the runtime's
	// composition line, so the measurement proves that line forms no bignum;
	// what reaches it is pinned here by shape. Every generated line naming a
	// site's marker must be exactly the composed call, so a site that reads
	// the whole word (the negative controls' sabotage) or does arithmetic on
	// the composed value is refused by name, not averaged away. Measured: a
	// call-site edit that forced a bignum per read moved all three variants
	// alike (open 238 to 256 words) and the differential alone stayed green.
	sited := map[string]int{}
	for name, data := range files {
		if name == runtime || !strings.HasSuffix(name, ".ex") {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			for _, c := range sites {
				if !strings.Contains(line, c.marker) {
					continue
				}
				if !c.shape.MatchString(line) {
					t.Errorf("%s: %q is not the composed call %s: the ordinary path no longer reaches the runtime's half composition", name, strings.TrimSpace(line), c.shape)
				}
				sited[c.marker]++
			}
		}
	}
	for _, c := range sites {
		if sited[c.marker] == 0 {
			t.Errorf("no generated line names %q: the call-site pin holds nothing", c.marker)
		}
	}
	src, ok := files[runtime]
	if !ok {
		t.Fatalf("no %s in the generated Elixir", runtime)
	}
	// the composition is matched as a WHOLE LINE, so an edit anywhere in it
	// (a sabotage that boxes the result) is caught here and not compared as if
	// the line were intact
	if n := strings.Count(string(src), real); n != 1 {
		t.Fatalf("%s holds the half composition %q %d times, want once: the reader no longer composes from halves", runtime, real, n)
	}
	figures := map[string]map[string]float64{}
	for _, v := range []struct{ name, text string }{{"real", real}, {"control", control}, {"boxed", boxed}} {
		variant := map[string][]byte{}
		for k, d := range files {
			variant[k] = d
		}
		variant[runtime] = []byte(strings.Replace(string(src), real, v.text, 1))
		dir := t.TempDir()
		names := writeModuleDir(t, dir, variant)
		out := runProbe(t, elixirBin, dir, names, ordinaryMeter+probe)
		figures[v.name] = map[string]float64{}
		for line := range strings.SplitSeq(out, "\n") {
			f := strings.Fields(line)
			switch {
			case len(f) == 3 && f[0] == "WORDS":
				w, err := strconv.ParseFloat(f[2], 64)
				if err != nil {
					t.Fatalf("%s: unreadable figure %q", v.name, line)
				}
				figures[v.name][f[1]] = w
			case len(f) > 0 && (f[0] == "ROWS" || f[0] == "SLOTS") && v.name == "real":
				t.Log(line)
			}
		}
	}
	for _, l := range labels {
		r, okR := figures["real"][l]
		c, okC := figures["control"][l]
		b, okB := figures["boxed"][l]
		if !okR || !okC || !okB {
			t.Fatalf("%s: a variant reported no figure (real %v, control %v, boxed %v)", l, okR, okC, okB)
		}
		t.Logf("%s: heap words per call: real %.3f, immediate-value control %.3f, forced-boxing control %.3f", l, r, c, b)
		if b <= c+ordinarySlack {
			t.Errorf("%s: the forced-boxing positive control allocated %.3f words per call against the control's %.3f: the meter cannot see a bignum, so its green means nothing", l, b, c)
		}
		if r > c+ordinarySlack {
			t.Errorf("%s: the ordinary path allocated %.3f words per call against the same-shaped immediate-value control's %.3f: a bignum is formed reading a corpus offset or delta", l, r, c)
		}
	}
}
