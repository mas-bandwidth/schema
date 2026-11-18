# frozen_string_literal: true

module SchemaRuntimeUnit
  # Hand-text refusal carrying a zero-based byte offset.
  class TextError < Refused
    attr_reader :offset

    def initialize(reason, offset)
      super(reason)
      @offset = offset
    end
  end

  # Lossless bounded tokenizer for the S-expression table text form.
  module SchemaTable
    TextNode = Struct.new(:kind, :start, :end_pos, :value, :children)
    class TextLimits
      attr_accessor :bytes, :tokens, :nodes, :allocation, :depth

      def initialize(bytes: 0, tokens: 0, nodes: 0, allocation: 0, depth: 0)
        @bytes = bytes
        @tokens = tokens
        @nodes = nodes
        @allocation = allocation
        @depth = depth
      end
    end
    TextComment = Struct.new(:start, :end_pos, :path)

    TEXTFORM_NUMBER_RE = /\A-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\z/
    TEXTFORM_KEYWORD_RE = %r{\A:[A-Za-z_][A-Za-z_0-9./-]*\z}

    # Bounded parser over raw UTF-8 bytes.
    class TextFormParser
      attr_reader :comments
      attr_accessor :pos

      def initialize(data, limits)
        @data = data
        @pos = 0
        @tokens = 0
        @nodes = 0
        @allocation = 0
        @comments = SchemaTable.arr
        @max_tokens = limits.tokens.positive? ? limits.tokens : 8192
        @max_nodes = limits.nodes.positive? ? limits.nodes : 4096
        @max_allocation = limits.allocation.positive? ? limits.allocation : 65_536
        @max_depth = limits.depth.positive? ? [limits.depth, MAX_DEPTH].min : MAX_DEPTH
        max_b = limits.bytes.positive? ? limits.bytes : 65_536
        raise TextError.new('text_limit', 0) if data.bytesize > max_b

        validate_utf8_bytes(data)
      end

      def validate_utf8_bytes(data)
        str = data.dup.force_encoding(Encoding::UTF_8)
        return if str.valid_encoding?

        # Find exact first invalid byte offset.
        idx = 0
        len = data.bytesize
        while idx < len
          ch = str.byteslice(idx, 4).force_encoding(Encoding::UTF_8)
          first_char = ch[0]
          raise TextError.new('text_encoding', idx) if first_char.nil? || !first_char.valid_encoding?

          idx += first_char.bytesize
        end
        raise TextError.new('text_encoding', 0)
      end

      def skip_gap
        len = @data.bytesize
        while @pos < len
          c = @data.getbyte(@pos)
          if [32, 9, 10].include?(c)
            @pos += 1
          elsif c == 13
            raise TextError.new('text_syntax', @pos) if @pos + 1 == len || @data.getbyte(@pos + 1) != 10

            @pos += 2
          elsif c == 59
            start = @pos
            @pos += 1
            while @pos < len && ![10, 13].include?(@data.getbyte(@pos))
              raise TextError.new('text_syntax', @pos) if @data.getbyte(@pos).zero?

              @pos += 1
            end
            @comments << [start, @pos]
          else
            break
          end
        end
      end

      def parse_node(depth)
        skip_gap
        start = @pos
        len = @data.bytesize
        raise TextError.new('text_unterminated', start) if start >= len

        @tokens += 1
        raise TextError.new('text_limit', start) if @tokens > @max_tokens

        c = @data.getbyte(start)
        if c == 40
          raise TextError.new('text_limit', start) if depth >= @max_depth

          @nodes += 1
          raise TextError.new('text_limit', start) if @nodes > @max_nodes

          @pos += 1
          children = SchemaTable.arr
          loop do
            skip_gap
            raise TextError.new('text_unterminated', @pos) if @pos == len

            if @data.getbyte(@pos) == 41
              @tokens += 1
              raise TextError.new('text_limit', @pos) if @tokens > @max_tokens

              @pos += 1
              return TextNode.new('list', start, @pos, '', children.freeze)
            end
            children << parse_node(depth + 1)
          end
        end
        if c == 34
          @pos += 1
          escaped = false
          while @pos < len
            ch = @data.getbyte(@pos)
            @pos += 1
            if escaped
              escaped = false
            elsif ch == 92
              escaped = true
            elsif ch == 34
              raw_utf8 = @data.byteslice(start, @pos - start).force_encoding(Encoding::UTF_8)
              begin
                scanner = JsonScanner.new(raw_utf8, nil)
                decoded = scanner.scan_string
                raise Refused, 'malformed' if scanner.pos != raw_utf8.bytesize
              rescue Refused
                raise TextError.new('text_encoding', start)
              end
              @allocation += decoded.bytesize
              raise TextError.new('text_limit', start) if @allocation > @max_allocation

              return TextNode.new('string', start, @pos, decoded, SchemaTable::EMPTY_ARR)
            end
          end
          raise TextError.new('text_unterminated', start)
        end
        raise TextError.new('text_syntax', start) if c == 41

        @pos += 1 while @pos < len && ![32, 9, 13, 10, 40, 41, 59, 34].include?(@data.getbyte(@pos))
        atom = @data.byteslice(start, @pos - start).force_encoding(Encoding::US_ASCII)
        kind = if %w[true false null].include?(atom)
                 atom
               elsif TEXTFORM_KEYWORD_RE.match?(atom)
                 'keyword'
               elsif TEXTFORM_NUMBER_RE.match?(atom)
                 'number'
               else
                 raise TextError.new('text_syntax', start)
               end
        TextNode.new(kind, start, @pos, atom.encode(Encoding::UTF_8), SchemaTable::EMPTY_ARR)
      end
    end

    def self.parse_textform_with_comments(data, limits)
      raw = data.is_a?(String) ? data.b : raise(TextError.new('text_syntax', 0))
      parser = TextFormParser.new(raw, limits)
      parser.skip_gap
      raise TextError.new('text_syntax', parser.pos) if parser.pos == raw.bytesize || raw.getbyte(parser.pos) != 40

      root = parser.parse_node(0)
      parser.skip_gap
      raise TextError.new('text_syntax', parser.pos) if parser.pos != raw.bytesize

      [root, parser.comments.freeze]
    end
  end
end
