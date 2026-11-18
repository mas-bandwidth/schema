# frozen_string_literal: true

module SchemaRuntimeUnit
  # Strict scanner for the schema table JSON dialect (SPEC-TABLES §16.2, §16.3).
  # Never uses Ruby's json stdlib (docs/PORT-RUBY.md D7).
  module SchemaTable
    JsonNumber = Struct.new(:token)

    JSON_ESCAPES = {
      34 => '"',
      92 => '\\',
      47 => '/',
      98 => "\b",
      102 => "\f",
      110 => "\n",
      114 => "\r",
      116 => "\t"
    }.freeze

    # Scanner over a validated UTF-8 String indexed by byte offset.
    class JsonScanner
      attr_reader :text
      attr_accessor :pos

      def initialize(text, max_key)
        @text = text
        @raw = text.b
        @max_key = max_key
        @pos = text.start_with?("\uFEFF") ? 3 : 0
      end

      def fail_scan
        raise Refused, 'malformed'
      end

      def skip_space
        len = @raw.bytesize
        while @pos < len
          b = @raw.getbyte(@pos)
          if b == 32 || b == 9 || b == 13 || b == 10
            @pos += 1
          elsif b == 47 && @raw.getbyte(@pos + 1) == 47
            nl = @raw.index("\n", @pos + 2) || len
            comment = @raw.byteslice(@pos, nl - @pos)
            fail_scan if comment.include?("\x00")
            @pos = nl
          elsif b == 47 && @raw.getbyte(@pos + 1) == 42
            close = @raw.index('*/', @pos + 2)
            fail_scan if close.nil?
            comment = @raw.byteslice(@pos, close - @pos)
            fail_scan if comment.include?("\x00")
            @pos = close + 2
          else
            break
          end
        end
      end

      def peek
        skip_space
        @pos < @raw.bytesize ? @raw.getbyte(@pos) : -1
      end

      def hex_val(byte)
        return byte - 48 if byte.between?(48, 57)
        return byte - 87 if byte.between?(97, 102)
        return byte - 55 if byte.between?(65, 70)

        fail_scan
      end

      def scan_hex4
        fail_scan if @pos + 4 > @raw.bytesize
        v = (hex_val(@raw.getbyte(@pos)) << 12) |
            (hex_val(@raw.getbyte(@pos + 1)) << 8) |
            (hex_val(@raw.getbyte(@pos + 2)) << 4) |
            hex_val(@raw.getbyte(@pos + 3))
        @pos += 4
        v
      end

      def scan_string
        @pos += 1
        out = String.new(encoding: Encoding::UTF_8)
        len = @raw.bytesize
        while @pos < len
          chunk_start = @pos
          while @pos < len
            b = @raw.getbyte(@pos)
            break if b == 34 || b == 92

            fail_scan if b < 32
            @pos += 1
          end
          if @pos > chunk_start
            chunk = @raw.byteslice(chunk_start, @pos - chunk_start)
            chunk.force_encoding(Encoding::UTF_8)
            out << chunk
          end
          fail_scan if @pos >= len
          b = @raw.getbyte(@pos)
          @pos += 1
          return out if b == 34

          fail_scan if @pos >= len
          esc = @raw.getbyte(@pos)
          @pos += 1
          mapped = JSON_ESCAPES[esc]
          if mapped
            out << mapped
            next
          end
          fail_scan if esc != 117
          code = scan_hex4
          if code.between?(0xD800, 0xDBFF)
            fail_scan if @raw.getbyte(@pos) != 92 || @raw.getbyte(@pos + 1) != 117
            @pos += 2
            low = scan_hex4
            fail_scan unless low.between?(0xDC00, 0xDFFF)
            code = 0x10000 + ((code - 0xD800) << 10) + (low - 0xDC00)
          elsif code.between?(0xDC00, 0xDFFF)
            fail_scan
          end
          out << code.chr(Encoding::UTF_8)
        end
        fail_scan
      end

      def scan_digits
        start = @pos
        len = @raw.bytesize
        while @pos < len
          b = @raw.getbyte(@pos)
          break unless b.between?(48, 57)

          @pos += 1
        end
        @pos > start
      end

      def scan_number
        start = @pos
        @pos += 1 if @raw.getbyte(@pos) == 45
        b = @raw.getbyte(@pos)
        if b == 48
          @pos += 1
        elsif b&.between?(49, 57)
          scan_digits
        else
          fail_scan
        end
        if @raw.getbyte(@pos) == 46
          @pos += 1
          fail_scan unless scan_digits
        end
        eb = @raw.getbyte(@pos)
        if eb == 101 || eb == 69
          @pos += 1
          sb = @raw.getbyte(@pos)
          @pos += 1 if sb == 43 || sb == 45
          fail_scan unless scan_digits
        end
        tok = @raw.byteslice(start, @pos - start)
        tok.force_encoding(Encoding::UTF_8)
        JsonNumber.new(tok)
      end

      def match_kw(kw)
        len = kw.bytesize
        return false if @pos + len > @raw.bytesize

        i = 0
        while i < len
          return false if @raw.getbyte(@pos + i) != kw.getbyte(i)

          i += 1
        end
        @pos += len
        true
      end

      def scan_value(depth)
        b = peek
        if b == 123 || b == 91
          fail_scan if depth > MAX_DEPTH
          @pos += 1
          return b == 123 ? scan_object(depth) : scan_array(depth)
        end
        return scan_string if b == 34
        return true if b == 116 && match_kw('true')
        return false if b == 102 && match_kw('false')
        return nil if b == 110 && match_kw('null')
        return scan_number if b == 45 || (b.between?(48, 57))

        fail_scan
      end

      def scan_object(depth)
        result = SchemaTable.map_h
        while peek != 125
          fail_scan if peek != 34
          key = scan_string
          fail_scan if (!@max_key.nil? && key.bytesize > @max_key) || result.key?(key) || peek != 58
          @pos += 1
          result[key] = scan_value(depth + 1)
          break if peek == 125

          fail_scan if peek != 44
          @pos += 1
        end
        @pos += 1
        result
      end

      def scan_array(depth)
        result = SchemaTable.arr
        while peek != 93
          result << scan_value(depth + 1)
          break if peek == 93

          fail_scan if peek != 44
          @pos += 1
        end
        @pos += 1
        result
      end
    end

    def self.parse_json_text(text, max_key = 255)
      raise Refused, 'kind_mismatch' unless text.instance_of?(String)
      raise Refused, 'invalid_utf8' unless TEXT_ENCODINGS.include?(text.encoding) && text.valid_encoding?

      utf8 = text.encoding == Encoding::UTF_8 ? text : text.encode(Encoding::UTF_8)
      scanner = JsonScanner.new(utf8, max_key)
      val = scanner.scan_value(0)
      scanner.skip_space
      raise Refused, 'malformed' if scanner.pos != utf8.bytesize

      val
    end
  end
end
