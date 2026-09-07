base = Path.expand("../../../build/table-base64/elixir", __DIR__)
Code.require_file("TableRuntime.ex", base)
Code.require_file("BytesTable.ex", base)

hex = fn
  <<>> -> "-"
  data -> Base.encode16(data, case: :lower)
end

for line <- IO.stream(:stdio, :line) do
  text = Base.decode16!(String.trim(line), case: :mixed)
  {value, report} = Base64test.BytesTable.from_json_blob(text)

  if report.malformed do
    IO.puts("1 0 0 - -")
  else
    {:ok, written} = Base64test.BytesTable.to_json_blob(value)
    IO.puts("0 #{report.clamped} #{report.kind_mismatch} #{hex.(value.payload)} #{hex.(written)}")
  end
end
