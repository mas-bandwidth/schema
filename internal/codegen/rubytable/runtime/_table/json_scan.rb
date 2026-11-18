# frozen_string_literal: true

module SchemaRuntimeUnit
  # Strict scanner for the schema table JSON dialect (SPEC-TABLES §16.2, §16.3).
  # Never uses Ruby's json stdlib (docs/PORT-RUBY.md D7).
  module SchemaTable
    JsonNumber = Struct.new(:token)

    JSON_ESCAPES = {
      '"' => '"',
      '\\' => '\\',
      '/' => '/',
      'b' => "\b",
      'f' => "\f",
      'n' => "\n",
      'r' => "\r",
      't' => "\t"
    }.freeze

    HEX_DIGITS = '0123456789abcdefABCDEF'

    # Scanner over a validated UTF-8 String.
    class JsonScanner
      attr_reader :text
      attr_accessor :pos

      def initialize(text, max_key)
        @text = text
        @max_key = max_key
        @pos = text.start_with?("\uFEFF") ? 1 : 0
      end

      def fail_scan
        raise Refused, 'malformed'
      end

      def skip_space
        len = @text.length
        while @pos < len
          ch = @text[@pos]
          if [' ', "\t", "\r", "\n"].include?(ch)
            @pos += 1
          elsif @text[@pos, 2] == '//'
            nl = @text.index("\n", @pos + 2) || len
            comment = @text[@pos...nl]
            fail_scan if comment.include?("\x00")
            @pos = nl
          elsif @text[@pos, 2] == '/*'
            close = @text.index('*/', @pos + 2)
            fail_scan if close.nil?
            comment = @text[@pos...close]
            fail_scan if comment.include?("\x00")
            @pos = close + 2
          else
            break
          end
        end
      end

      def peek
        skip_space
        @pos < @text.length ? @text[@pos] : ''
      end

      def scan_hex4
        digits = @text[@pos, 4]
        fail_scan if digits.nil? || digits.length != 4
        digits.each_char { |c| fail_scan unless HEX_DIGITS.include?(c) }
        @pos += 4
        digits.to_i(16)
      end

      def scan_string
        @pos += 1
        out = String.new(encoding: Encoding::UTF_8)
        len = @text.length
        while @pos < len
          c = @text[@pos]
          @pos += 1
          return out if c == '"'

          ord = c.ord
          fail_scan if ord < 32 || ord.between?(0xD800, 0xDFFF)
          if c != '\\'
            out << c
            next
          end
          fail_scan if @pos >= len
          esc = @text[@pos]
          @pos += 1
          mapped = JSON_ESCAPES[esc]
          if mapped
            out << mapped
            next
          end
          fail_scan if esc != 'u'
          code = scan_hex4
          if code.between?(0xD800, 0xDBFF)
            fail_scan if @text[@pos, 2] != '\\u'
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
        len = @text.length
        @pos += 1 while @pos < len && @text[@pos].between?('0', '9')
        @pos > start
      end

      def scan_number
        start = @pos
        @pos += 1 if @text[@pos] == '-'
        c = @text[@pos]
        if c == '0'
          @pos += 1
        elsif c&.between?('1', '9')
          scan_digits
        else
          fail_scan
        end
        if @text[@pos] == '.'
          @pos += 1
          fail_scan unless scan_digits
        end
        if %w[e E].include?(@text[@pos])
          @pos += 1
          @pos += 1 if ['+', '-'].include?(@text[@pos])
          fail_scan unless scan_digits
        end
        JsonNumber.new(@text[start...@pos])
      end

      def scan_value(depth)
        c = peek
        if ['{', '['].include?(c)
          fail_scan if depth > MAX_DEPTH
          @pos += 1
          return c == '{' ? scan_object(depth) : scan_array(depth)
        end
        return scan_string if c == '"'

        if @text[@pos, 4] == 'true'
          @pos += 4
          return true
        end
        if @text[@pos, 5] == 'false'
          @pos += 5
          return false
        end
        if @text[@pos, 4] == 'null'
          @pos += 4
          return nil
        end
        return scan_number if !c.empty? && (c == '-' || c.between?('0', '9'))

        fail_scan
      end

      def scan_object(depth)
        result = SchemaTable.map_h
        while peek != '}'
          fail_scan if peek != '"'
          key = scan_string
          fail_scan if (!@max_key.nil? && key.bytesize > @max_key) || result.key?(key) || peek != ':'
          @pos += 1
          result[key] = scan_value(depth + 1)
          break if peek == '}'

          fail_scan if peek != ','
          @pos += 1
        end
        @pos += 1
        result
      end

      def scan_array(depth)
        result = SchemaTable.arr
        while peek != ']'
          result << scan_value(depth + 1)
          break if peek == ']'

          fail_scan if peek != ','
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
      raise Refused, 'malformed' if scanner.pos != utf8.length

      val
    end
  end
end
