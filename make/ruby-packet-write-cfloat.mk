# PACKET WRITE COMPRESSED FLOAT, the Ruby leg (SPEC §5.1 row 10;
# PACKET-WIRE-ALGORITHM.md §2.5): test/packet-write-cfloat/ruby/main.rb runs
# every shared case of test/packet-write-cfloat/cases.txt through the generated
# writer, on the current and the floor interpreter. Ruby refuses in every
# build; a written case reads back and, where the case pins it, writes the
# exact wire.
.PHONY: packet-write-cfloat-ruby
packet-write-cfloat-ruby: build/schema-ruby ruby-toolchain-check build/mustrun
	@rm -rf build/packet-write-cfloat/ruby && mkdir -p build/packet-write-cfloat/ruby
	./build/schema-ruby generate --lang ruby --out build/packet-write-cfloat/ruby/generated test/packet-write-cfloat/PacketCFloat.schema
	$(MUSTRUN) -name packet-write-cfloat-ruby -count 12 -- $(RUBY) --disable-gems -w test/packet-write-cfloat/ruby/main.rb build/packet-write-cfloat/ruby/generated test/packet-write-cfloat/cases.txt
	$(MUSTRUN) -name packet-write-cfloat-ruby-floor -count 12 -- $(RUBY_FLOOR) --disable-gems -w test/packet-write-cfloat/ruby/main.rb build/packet-write-cfloat/ruby/generated test/packet-write-cfloat/cases.txt
