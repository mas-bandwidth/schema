# Run once per variant beside that variant's runtime; every ANSWER line is held
# to cases.tsv. The word is lo = 2, hi = 1.
word = <<2::little-unsigned-32, 1::little-unsigned-32>>
IO.puts("ANSWER offset_of #{Fixture.Reader.offset_of(word)}")
IO.puts("ANSWER build #{Fixture.Shared.build()}")
