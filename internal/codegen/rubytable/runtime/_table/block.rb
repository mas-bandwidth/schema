# frozen_string_literal: true

module SchemaRuntimeUnit
  # Block form reader, canonical row dumper, and builder (SPEC-TABLES §19).
  module SchemaTable
    BLOCK_MAGIC = 0x4B4C42414D484353
    BLOCK_PROLOGUE = 24
    BLOCK_ALIGN = 64
    HOST_BIG_ENDIAN = u([1].pack('S').getbyte(0)).zero?
    BLOCK_BYTE_ORDER = HOST_BIG_ENDIAN ? 2 : 1
    BLOCK_MAGIC_BYTES = pack_int_order(BLOCK_MAGIC, 8, HOST_BIG_ENDIAN, false).freeze
    BLOCK_FOREIGN_MAGIC_BYTES = BLOCK_MAGIC_BYTES.reverse.freeze

    # One opened out-of-line array slice.
    class BlockArrayView
      attr_reader :desc, :offset, :count, :stride, :record

      def initialize(desc, offset, count, stride, record)
        @desc = desc
        @offset = offset
        @count = count
        @stride = stride
        @record = record
      end
    end

    # Opened or freshly built fixed-table block projection.
    class Block
      attr_reader :name, :bytes, :build_version, :projection, :arrays, :used, :base_offset

      def initialize(name, bytes, build_version, projection, arrays, used, base_offset)
        @name = name
        @bytes = bytes
        @build_version = build_version
        @projection = projection
        @arrays = arrays.freeze
        @used = used
        @base_offset = base_offset
      end

      def dump
        SchemaTable.block_dump(self)
      end
    end

    def self.align_up(val, alignment)
      return val if alignment <= 1

      ((val + alignment - 1) / alignment) * alignment
    end

    def self.block_stride_align(stride)
      align = 1
      align *= 2 while align < 16 && (stride % (align * 2)).zero?
      align
    end

    def self.block_rows_of(name)
      cls = by_name(name)
      raise Refused.new('bad_layout', "the array element #{name} is not registered") if cls.nil?
      unless cls.respond_to?(:block_row) && cls.block_row.is_a?(BlockRecord)
        raise Refused.new('bad_layout',
                          "the array element #{name} carries no row layout")
      end

      cls.block_row
    end

    def self.block_buffer_bytes(data)
      raise Refused.new('truncated', 'the block is not a binary String') unless data.is_a?(String)

      data.encoding == Encoding::ASCII_8BIT ? data : data.b
    end

    def self.block_open(cls, data, build_version: nil, base_offset: 0)
      raw = block_buffer_bytes(data)
      layout = u(cls.respond_to?(:block) ? cls.block : nil)
      name = cls.respond_to?(:descriptor) ? cls.descriptor.name : cls.to_s
      raise Refused.new('not_a_cook', "the table #{name} has no block form") unless layout.is_a?(BlockTable)

      bver = build_version.nil? ? cls.descriptor.build_version : build_version
      if raw.bytesize < layout.projection.size
        raise Refused.new('truncated', "the block is #{raw.bytesize} bytes, shorter than its projection")
      end

      head8 = raw.byteslice(0, 8)
      if head8 != BLOCK_MAGIC_BYTES
        if head8 == BLOCK_FOREIGN_MAGIC_BYTES
          raise Refused.new('foreign_order',
                            'the block was written in the other byte order')
        end

        raise Refused.new('not_a_cook', 'the block magic is not a schema block')
      end
      if unpack_int_order(raw, 16, 8, HOST_BIG_ENDIAN, false) != BLOCK_BYTE_ORDER
        raise Refused.new('not_a_cook', 'the block byte order word disagrees with this build')
      end
      if bver.nil? || unpack_int_order(raw, 8, 8, HOST_BIG_ENDIAN, false) != bver
        raise Refused.new('wrong_build_version', 'the block was written by another build')
      end

      used = layout.projection.size
      arrays = layout.arrays.map do |a|
        offset_of = unpack_int_order(raw, a.offset, 8, HOST_BIG_ENDIAN, false)
        count = unpack_int_order(raw, a.offset + 8, 4, HOST_BIG_ENDIAN, false)
        stride = unpack_int_order(raw, a.offset + 12, 4, HOST_BIG_ENDIAN, false)
        if stride != a.stride
          raise Refused.new('bad_layout',
                            "#{a.name} has pitch #{stride}, this build is #{a.stride}")
        end
        if count > a.maximum
          raise Refused.new('bad_layout',
                            "#{a.name} has #{count} rows, past its maximum #{a.maximum}")
        end

        start_align = [BLOCK_ALIGN, block_stride_align(stride)].max
        if offset_of < layout.projection.size || (offset_of % start_align) != 0
          raise Refused.new('bad_layout', "#{a.name} starts at #{offset_of}, outside or unaligned")
        end
        raise Refused.new('bad_layout', "#{a.name} starts at #{offset_of}, past the block") if offset_of > raw.bytesize

        rows_bytes = count * stride
        raise Refused.new('bad_layout', "#{a.name} rows leave the block") if rows_bytes > raw.bytesize - offset_of

        ending = offset_of + rows_bytes
        used = ending if ending > used
        BlockArrayView.new(a, offset_of, count, stride, block_rows_of(a.element))
      end

      padding = (BLOCK_ALIGN - (used % BLOCK_ALIGN)) % BLOCK_ALIGN
      raise Refused.new('truncated', 'the bytes stop inside the used extent padding') if padding > raw.bytesize - used

      used += padding
      if (base_offset % BLOCK_ALIGN) != 0
        raise Refused.new('unaligned_base',
                          "the base is #{base_offset} bytes past an aligned one")
      end

      Block.new(name, raw, bver, layout.projection, arrays, used, base_offset)
    end

    def self.block_max_bytes(cls)
      layout = u(cls.respond_to?(:block) ? cls.block : nil)
      name = cls.respond_to?(:descriptor) ? cls.descriptor.name : cls.to_s
      raise InvalidValue.new(name, 'the table has no block form') unless layout.is_a?(BlockTable)

      layout.max_bytes
    end

    def self.block_begin(cls, counts = nil)
      layout = u(cls.respond_to?(:block) ? cls.block : nil)
      name = cls.respond_to?(:descriptor) ? cls.descriptor.name : cls.to_s
      raise InvalidValue.new(name, 'the table has no block form') unless layout.is_a?(BlockTable)

      version = cls.descriptor.build_version
      chosen = counts || SchemaTable.map_h
      layout.arrays.each do |a|
        cnt = chosen.fetch(a.name, chosen.fetch(a.name.to_sym, 0))
        unless cnt.instance_of?(Integer) && cnt.between?(0, a.maximum)
          raise InvalidValue.new(a.name, "count #{cnt} is outside [0, #{a.maximum}]")
        end
      end

      offset = layout.projection.size
      starts = layout.arrays.map do |a|
        cnt = chosen.fetch(a.name, chosen.fetch(a.name.to_sym, 0))
        align = [BLOCK_ALIGN, block_stride_align(a.stride)].max
        offset = align_up(offset, align)
        st = offset
        offset += cnt * a.stride
        [a, st, cnt]
      end
      used = align_up(offset, BLOCK_ALIGN)
      buf = "\x00".b * used
      buf[0, 8] = pack_int_order(BLOCK_MAGIC, 8, HOST_BIG_ENDIAN, false)
      buf[8, 8] = pack_int_order(version, 8, HOST_BIG_ENDIAN, false)
      buf[16, 8] = pack_int_order(BLOCK_BYTE_ORDER, 8, HOST_BIG_ENDIAN, false)
      arrays = starts.map do |a, st, cnt|
        buf[a.offset, 8] = pack_int_order(st, 8, HOST_BIG_ENDIAN, false)
        buf[a.offset + 8, 4] = pack_int_order(cnt, 4, HOST_BIG_ENDIAN, false)
        buf[a.offset + 12, 4] = pack_int_order(a.stride, 4, HOST_BIG_ENDIAN, false)
        elem_cls = by_name(a.element)
        raise InvalidValue.new(a.name, "the array element #{a.element} is not registered") if elem_cls.nil?
        unless elem_cls.respond_to?(:block_row) && elem_cls.block_row.is_a?(BlockRecord)
          raise InvalidValue.new(a.name, "the array element #{a.element} carries no row layout")
        end

        BlockArrayView.new(a, st, cnt, a.stride, elem_cls.block_row)
      end
      Block.new(name, buf, version, layout.projection, arrays, used, 0)
    end

    def self.format_cook_text(raw, at, used)
      out = String.new('"', encoding: Encoding::UTF_8)
      used.times do |i|
        c = raw.getbyte(at + i)
        out << if c >= 0x20 && c < 0x7F && c != 0x22 && c != 0x5C
                 c.chr
               else
                 format('\\x%02x', c)
               end
      end
      out << format('" len=%d', used)
      out
    end

    def self.block_dump_scalar(raw, at, kind, width)
      return raw.getbyte(at).zero? ? 'false' : 'true' if kind == 1
      return format('0x%08x', unpack_int_order(raw, at, 4, HOST_BIG_ENDIAN, false)) if kind == 10
      return format('0x%016x', unpack_int_order(raw, at, 8, HOST_BIG_ENDIAN, false)) if kind == 11
      return unpack_int_order(raw, at, width, HOST_BIG_ENDIAN, true).to_s if kind.between?(2, 5)

      unpack_int_order(raw, at, width, HOST_BIG_ENDIAN, false).to_s
    end

    def self.block_dump_record(out, raw, base, record, prefix)
      record.fields.each do |f|
        next if f.out_of_line

        nm = prefix.empty? ? f.name : "#{prefix}.#{f.name}"
        if f.counted
          used = unpack_int_order(raw, base + f.count_offset, 4, HOST_BIG_ENDIAN, true)
          unless used.between?(0, f.bound)
            raise Refused.new('bad_layout',
                              "#{record.name}.#{f.name} carries a used length of #{used}, outside [ 0, #{f.bound} ]")
          end

          out << "  #{nm} = #{format_cook_text(raw, base + f.offset, used)}"
        else
          slots = f.array ? f.bound : 1
          slots.times do |s|
            at = base + f.offset + (s * f.elem_size)
            slot = f.array ? "#{nm}[#{s}]" : nm
            if f.target.empty?
              out << "  #{slot} = #{block_dump_scalar(raw, at, f.kind, f.elem_size)}"
            else
              block_dump_record(out, raw, at, block_rows_of(f.target), slot)
            end
          end
        end
        if f.optional
          present = !raw.getbyte(base + f.present_offset).zero?
          out << "  #{nm}#present = #{present}"
        end
      end
    end

    def self.block_dump(handle)
      raw = block_buffer_bytes(handle.bytes)
      out = ["projection #{handle.name} @0"]
      block_dump_record(out, raw, 0, handle.projection, '')
      handle.arrays.each do |a|
        out << "array #{a.desc.name} #{a.record.name} @#{a.offset} count=#{a.count} stride=#{a.stride}"
        a.count.times do |r|
          at = a.offset + (r * a.stride)
          out << "row #{r} @#{at}"
          block_dump_record(out, raw, at, a.record, '')
        end
      end
      "#{out.join("\n")}\n"
    end
  end
end
