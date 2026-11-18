# frozen_string_literal: true

module SchemaRuntimeUnit
  # Cooked form open, thorough region check, writer, and dump (SPEC-TABLES §7).
  module SchemaTable
    COOK_MAGIC = 0x4B4F4F434D484353
    COOK_HEADER_BYTES = 64
    COOK_MAX_ALIGN = 64
    COOK_DIRECTORY_ENTRY = 16
    COOK_NOT_MATERIALIZED = 0xFFFFFFFFFFFFFFFF
    COOK_ORDER_LITTLE = 1
    COOK_ORDER_BIG = 2

    SingleFieldDescHolder = Struct.new(:fields, :padding)
    SingleValueHolder = Struct.new(:field, :val)

    # Opened cooked image handle.
    class CookHandle
      attr_reader :root, :data, :big_endian, :data_offset, :data_length,
                  :attrib_offset, :attrib_length, :alignment
      attr_accessor :directory, :node_base, :node_end, :node_record_end, :ranges

      def initialize(root, data, big_endian, data_offset, data_length, attrib_length, alignment)
        @root = root
        @data = data
        @big_endian = big_endian
        @data_offset = data_offset
        @data_length = data_length
        @attrib_offset = data_offset + data_length
        @attrib_length = attrib_length
        @alignment = alignment
        @directory = SchemaTable.arr
        @node_base = 0
        @node_end = 0
        @node_record_end = 0
        @ranges = SchemaTable.arr
      end

      def dump
        SchemaTable.dump_cook(self)
      end
    end

    # Internal node entry during cook layout and serialization.
    class CookEntry
      attr_reader :cls, :value, :desc, :blobs, :placements
      attr_accessor :offset, :blob, :type_id

      def initialize(cls, value, desc)
        @cls = cls
        @value = value
        @desc = desc
        @offset = 0
        @blob = nil
        @type_id = desc ? desc.type_id : 0
        @blobs = SchemaTable.arr
        @placements = SchemaTable.map_h
      end
    end

    def self.cook_get_val(holder, fd)
      holder.is_a?(SingleValueHolder) ? holder.val : fd.get(holder)
    end

    def self.cook_optional_field(fd)
      field = fd.dup
      field.codec = fd.inner
      field.optional = false
      field.guards = SchemaTable::EMPTY_ARR
      field.at = 0
      field.pieces = fd.pieces[0...-1].map { |off, sz| [off - fd.at, sz].freeze }.freeze
      field.elem_codec = field.elem.codec if field.codec == 'array' && field.elem
      field
    end

    def self.cook_union_field(fd)
      FieldDesc.new(name: fd.name, codec: 'union', target: fd.target, at: 0)
    end

    def self.nonzero_slice?(raw, start_pos, end_pos)
      return false if end_pos <= start_pos

      idx = start_pos
      while idx < end_pos
        return true unless raw.getbyte(idx).zero?

        idx += 1
      end
      false
    end

    def self.open_cook(cls, data, base_offset: 0)
      desc = cls.descriptor
      raise Refused.new('cook_form_not_carried', "#{desc.name} does not carry cook") unless desc.forms.include?('cook')

      raw = binary_input(data)
      raise Refused, 'truncated' if raw.bytesize < COOK_HEADER_BYTES

      host_magic = unpack_int_order(raw, 0, 8, HOST_BIG_ENDIAN, false)
      other_magic = unpack_int_order(raw, 0, 8, !HOST_BIG_ENDIAN, false)
      if host_magic == COOK_MAGIC
        big = HOST_BIG_ENDIAN
        word = big ? COOK_ORDER_BIG : COOK_ORDER_LITTLE
      elsif other_magic == COOK_MAGIC
        raise Refused, 'foreign_order'
      else
        raise Refused, 'not_a_cook'
      end
      raise Refused, 'not_a_cook' if unpack_int_order(raw, 16, 8, big, false) != word
      raise Refused, 'wrong_build_version' if unpack_int_order(raw, 8, 8, big, false) != desc.build_version

      res0 = unpack_int_order(raw, 48, 8, big, false)
      res1 = unpack_int_order(raw, 56, 8, big, false)
      raise Refused, 'reserved_not_zero' if !res0.zero? || !res1.zero?

      data_len = unpack_int_order(raw, 24, 8, big, false)
      attrib_len = unpack_int_order(raw, 32, 8, big, false)
      alignment = unpack_int_order(raw, 40, 8, big, false)
      if alignment < 8 || alignment > COOK_MAX_ALIGN || alignment.anybits?(alignment - 1) ||
         desc.align < 1 || (alignment % desc.align) != 0
        raise Refused, 'bad_alignment'
      end

      data_off = align_up(COOK_HEADER_BYTES, alignment)
      raise Refused, 'truncated' if raw.bytesize < data_off
      raise Refused, 'truncated' if data_len > raw.bytesize - data_off
      raise Refused, 'truncated' if attrib_len != raw.bytesize - data_off - data_len
      raise Refused, 'truncated' if data_len < desc.size
      raise Refused, 'unaligned_base' if (base_offset % alignment) != 0

      CookHandle.new(cls, raw, big, data_off, data_len, attrib_len, alignment)
    end

    def self.cook_directory(handle)
      raise Refused.new('bad_layout', 'a cook with no attribution part cannot be checked') if handle.attrib_length.zero?
      if (handle.attrib_length % COOK_DIRECTORY_ENTRY) != 0
        raise Refused.new('bad_layout',
                          'the attribution part is not whole entries')
      end

      count = handle.attrib_length / COOK_DIRECTORY_ENTRY
      Array.new(count) do |i|
        at = handle.attrib_offset + (i * COOK_DIRECTORY_ENTRY)
        [
          unpack_int_order(handle.data, at, 8, handle.big_endian, false),
          unpack_int_order(handle.data, at + 8, 8, handle.big_endian, false)
        ]
      end
    end

    def self.cook_node_class(type_id)
      cls = by_id(type_id)
      raise Refused.new('bad_layout', format('type id 0x%x is no table this unit has', type_id)) if cls.nil?

      cls
    end

    def self.cook_check_directory(handle, dir)
      prev = 0
      rdesc = handle.root.descriptor
      dir.each_with_index do |(offset, type_id), idx|
        if offset == COOK_NOT_MATERIALIZED
          raise Refused.new('bad_layout',
                            'a directory entry is the not-materialized sentinel')
        end

        is_blob = [rdesc.bytes_blob_type_id, rdesc.string_blob_type_id].include?(type_id)
        if is_blob
          sz = 8
          al = 8
        else
          cls = cook_node_class(type_id)
          sz = cls.descriptor.size
          al = cls.descriptor.cook_align
        end
        if idx.zero? && (!offset.zero? || type_id != rdesc.type_id)
          raise Refused.new('bad_layout', 'the root is not at the region base or has the wrong type')
        end
        raise Refused.new('bad_layout', 'directory offsets do not ascend') if offset < prev
        raise Refused.new('bad_layout', 'a directory entry is not aligned for its type') if (offset % al) != 0
        raise Refused.new('bad_layout', 'a node leaves the region') if offset + sz > handle.data_length

        prev = offset + sz
      end
    end

    def self.cook_directory_find(handle, offsets, target)
      pos = offsets.bsearch_index { |x| x >= target }
      return nil if pos.nil? || offsets[pos] != target

      [pos, handle.directory[pos][1]]
    end

    def self.cook_check_range_overlap!(handle, start_pos, end_pos, msg)
      raise Refused.new('bad_layout', msg) if handle.ranges.any? { |os, oe| start_pos < oe && os < end_pos }

      handle.ranges << [start_pos, end_pos]
    end

    def self.cook_scan_record(handle, region, desc, base, offsets)
      big = handle.big_endian
      desc.padding.each do |s, e|
        raise Refused.new('bad_layout', 'a record padding byte is not zero') if nonzero_slice?(region, base + s,
                                                                                               base + e)
      end
      desc.fields.each do |fd|
        at = base + fd.at
        next if %w[bool int enum float32 float64].include?(fd.codec)

        if fd.codec == 'string' && fd.pieces.length == 1
          delta = unpack_int_order(region, at, 8, big, true)
          unless delta.zero?
            start_p = at + delta
            if start_p < handle.node_record_end || (start_p % 8) != 0 || start_p + 8 > handle.node_end
              raise Refused.new('bad_layout', 'a text blob header leaves its extent or is unaligned')
            end

            len = unpack_int_order(region, start_p, 4, big, false)
            end_p = start_p + 8 + len + 1
            if len.zero? || end_p > handle.node_end || !region.getbyte(end_p - 1).zero?
              raise Refused.new('bad_layout', 'a text blob has invalid length or terminator')
            end

            cook_check_range_overlap!(handle, start_p, end_p, 'text blobs overlap')
          end
          next
        end
        if %w[string bytes].include?(fd.codec)
          count_at = base + fd.pieces[1][0]
          used = unpack_int_order(region, count_at, 4, big, true)
          raise Refused.new('bad_layout', 'a text length leaves its buffer') unless used.between?(0, fd.size)

          storage_end = at + fd.pieces[0][1]
          if nonzero_slice?(region, at + used, storage_end) || nonzero_slice?(region, storage_end, count_at)
            raise Refused.new('bad_layout', 'a text tail or padding byte is not zero')
          end

          next
        end
        if fd.codec == 'table'
          target = by_name(fd.target)
          raise Refused.new('bad_layout', "the table #{fd.target} is not registered") if target.nil?

          cook_scan_record(handle, region, target.descriptor, at, offsets)
          next
        end
        if fd.codec == 'array' && [1,
                                   2].include?(fd.array_kind) && %w[bool int enum float32
                                                                    float64].include?(fd.elem_codec)
          stride = fd.pieces[0][1] / fd.bound
          if fd.array_kind == 2
            count_at = base + fd.pieces[1][0]
            cnt = unpack_int_order(region, count_at, 4, big, true)
            raise Refused.new('bad_layout', 'an array count exceeds its bound') unless cnt.between?(0, fd.bound)

            if nonzero_slice?(region, at + (cnt * stride), at + (fd.bound * stride)) ||
               nonzero_slice?(region, at + (fd.bound * stride), count_at)
              raise Refused.new('bad_layout', 'a dead array slot or padding is not zero')
            end
          end
          next
        end
        next if fd.codec == 'keyed' && fd.elem && %w[bool int enum float32 float64].include?(fd.elem.codec)

        if fd.codec == 'array' && [1,
                                   2].include?(fd.array_kind) && %w[table string text_optional].include?(fd.elem_codec)
          target = by_name(fd.target)
          raise Refused.new('bad_layout', "the table #{fd.target} is not registered") if target.nil?

          cnt = fd.bound
          stride = fd.pieces[0][1] / fd.bound
          if fd.array_kind == 2
            count_at = base + fd.pieces[1][0]
            cnt = unpack_int_order(region, count_at, 4, big, true)
            raise Refused.new('bad_layout', 'an array count exceeds its bound') unless cnt.between?(0, fd.bound)

            dead_end = at + (fd.bound * stride)
            if nonzero_slice?(region, at + (cnt * stride), dead_end)
              raise Refused.new('bad_layout', 'a dead array slot is not zero')
            end
            raise Refused.new('bad_layout', 'array padding is not zero') if nonzero_slice?(region, dead_end, count_at)
          end
          cnt.times { |pos| cook_scan_record(handle, region, target.descriptor, at + (pos * stride), offsets) }
          next
        end
        if fd.codec == 'keyed' && fd.elem && fd.elem.codec == 'table'
          target = by_name(fd.target)
          raise Refused.new('bad_layout', 'a keyed element is not registered') if target.nil?

          stride = fd.pieces[0][1] / fd.bound
          fd.bound.times { |pos| cook_scan_record(handle, region, target.descriptor, at + (pos * stride), offsets) }
          next
        end
        if fd.codec == 'optional'
          present_at = base + fd.pieces[-1][0]
          present = region.getbyte(present_at)
          raise Refused.new('bad_layout', 'an optional presence byte is not 0 or 1') unless [0, 1].include?(present)

          if present.zero?
            raise Refused.new('bad_layout', 'an absent optional has nonzero payload') if nonzero_slice?(region, at,
                                                                                                        present_at)
          else
            inner_f = cook_optional_field(fd)
            cook_scan_record(handle, region, SingleFieldDescHolder.new([inner_f], SchemaTable.arr), at, offsets)
            end_off = inner_f.pieces.map { |o, s| o + s }.max || 0
            raise Refused.new('bad_layout', 'optional padding is not zero') if nonzero_slice?(region, at + end_off,
                                                                                              present_at)
          end
          next
        end
        if fd.codec == 'union_array' && [1, 2].include?(fd.array_kind)
          cnt = fd.bound
          stride = fd.pieces[0][1] / fd.bound
          if fd.array_kind == 2
            count_at = base + fd.pieces[1][0]
            cnt = unpack_int_order(region, count_at, 4, big, true)
            raise Refused.new('bad_layout', 'a union array count exceeds its bound') unless cnt.between?(0, fd.bound)

            dead_end = at + (fd.bound * stride)
            if nonzero_slice?(region, at + (cnt * stride), dead_end)
              raise Refused.new('bad_layout', 'a dead union array slot is not zero')
            end
            if nonzero_slice?(region, dead_end, count_at)
              raise Refused.new('bad_layout', 'union array padding is not zero')
            end
          end
          uf = cook_union_field(fd)
          cnt.times do |pos|
            cook_scan_record(handle, region, SingleFieldDescHolder.new([uf], SchemaTable.arr), at + (pos * stride), offsets)
          end
          next
        end
        unbounded_arr = fd.codec == 'array' && fd.array_kind == 3 &&
                        %w[bool int enum float32 float64 table string text_optional].include?(fd.elem_codec)
        if fd.codec == 'map' || (fd.codec == 'union_array' && fd.array_kind == 3) || unbounded_arr
          delta = unpack_int_order(region, at, 8, big, true)
          cnt = unpack_int_order(region, at + 8, 4, big, true)
          if cnt.negative? || delta.zero? != cnt.zero?
            raise Refused.new('bad_layout',
                              'a list has an invalid count or reference')
          end
          raise Refused.new('bad_layout', 'a list slot has nonzero padding') if nonzero_slice?(region, at + 12, at + 16)

          if cnt.positive?
            start_p = at + delta
            end_p = start_p + (cnt * fd.elem_size)
            if start_p < handle.node_record_end || end_p > handle.node_end || (start_p % fd.elem_align) != 0
              raise Refused.new('bad_layout', 'a list leaves its extent or is unaligned')
            end

            cook_check_range_overlap!(handle, start_p, end_p, 'lists overlap')
            if fd.codec == 'union_array'
              uf = cook_union_field(fd)
              cnt.times do |pos|
                cook_scan_record(handle, region, SingleFieldDescHolder.new([uf], SchemaTable.arr), start_p + (pos * fd.elem_size),
                                 offsets)
              end
            elsif fd.codec == 'map' || %w[table string text_optional].include?(fd.elem_codec)
              target = by_name(fd.target)
              raise Refused.new('bad_layout', 'a list or map element is not registered') if target.nil?

              prev_k = nil
              cnt.times do |pos|
                elem_at = start_p + (pos * fd.elem_size)
                cook_scan_record(handle, region, target.descriptor, elem_at, offsets)
                next unless fd.codec == 'map'

                kf = target.descriptor.fields[0]
                key_at = elem_at + kf.at
                kv = u(if kf.codec == 'string'
                         if kf.pieces.length == 1
                           kd = unpack_int_order(region, key_at, 8, big, true)
                           if kd.zero?
                             ''.b
                           else
                             ts = key_at + kd
                             used = unpack_int_order(region, ts, 4, big, false)
                             region.byteslice(ts + 8, used)
                           end
                         else
                           used_at = elem_at + kf.pieces[1][0]
                           used = unpack_int_order(region, used_at, 4, big, true)
                           region.byteslice(key_at, used)
                         end
                       elsif %w[int enum bool].include?(kf.codec)
                         unpack_int_order(region, key_at, kf.pieces[0][1], big, kf.lo.negative?)
                       else
                         raise Refused.new('bad_layout', 'a map key kind is unsupported')
                       end)
                raise Refused.new('bad_layout', 'map keys do not ascend') if pos.positive? && u(prev_k <=> kv) >= 0

                prev_k = kv
              end
            end
          end
          next
        end
        if fd.codec == 'pointer_array'
          if fd.array_mode == 'list'
            delta = unpack_int_order(region, at, 8, big, true)
            cnt = unpack_int_order(region, at + 8, 4, big, true)
            if cnt.negative? || delta.zero? != cnt.zero? || nonzero_slice?(region, at + 12, at + 16)
              raise Refused.new('bad_layout', 'a pointer list has an invalid slot')
            end

            start_p = at + delta
            end_p = start_p + (cnt * 8)
            if cnt.positive?
              if start_p < handle.node_record_end || end_p > handle.node_end || (start_p % fd.elem_align) != 0
                raise Refused.new('bad_layout', 'a pointer list leaves its extent or is unaligned')
              end

              cook_check_range_overlap!(handle, start_p, end_p, 'lists overlap')
            end
          else
            start_p = at
            cnt = fd.bound
            if fd.array_mode == 'counted'
              count_at = base + fd.pieces[1][0]
              cnt = unpack_int_order(region, count_at, 4, big, true)
              raise Refused.new('bad_layout', 'a pointer array count exceeds its bound') unless cnt.between?(0,
                                                                                                             fd.bound)
              if nonzero_slice?(region, start_p + (cnt * 8), count_at)
                raise Refused.new('bad_layout', 'a dead pointer slot or padding is not zero')
              end
            end
          end
          pf = FieldDesc.new(codec: 'pointer', name: fd.name, target: fd.target, at: 0)
          cnt.times do |pos|
            cook_scan_record(handle, region, SingleFieldDescHolder.new([pf], SchemaTable.arr), start_p + (pos * 8), offsets)
          end
          next
        end
        if fd.codec == 'union'
          ucls = union_by_name(fd.target)
          raise Refused.new('bad_layout', 'a union is not registered') if ucls.nil?

          ud = ucls.descriptor
          tag = unpack_int_order(region, at, ud.tag_size, big, false)
          raise Refused.new('bad_layout', 'a union tag names no arm') if tag > ud.arms.length

          arm = u(tag.positive? ? ud.arms[tag - 1] : nil)
          arm_sz = arm ? arm.size : 0
          arm_at = at + ud.arm_offset
          if nonzero_slice?(region, at + ud.tag_size, arm_at) || nonzero_slice?(region, arm_at + arm_sz, at + ud.size)
            raise Refused.new('bad_layout', 'inactive union storage is not zero')
          end

          cook_scan_record(handle, region, SingleFieldDescHolder.new([arm.field], SchemaTable.arr), arm_at, offsets) if arm&.field
          next
        end
        if %w[string_blob bytes_blob].include?(fd.codec)
          delta = unpack_int_order(region, at, 8, big, true)
          unless delta.zero?
            found = cook_directory_find(handle, offsets, at + delta)
            rdesc = handle.root.descriptor
            expected = fd.codec == 'string_blob' ? rdesc.string_blob_type_id : rdesc.bytes_blob_type_id
            if found.nil? || found[1] != expected
              raise Refused.new('bad_layout',
                                'a blob reference names no matching node')
            end
          end
          next
        end
        if fd.codec == 'pointer'
          delta = unpack_int_order(region, at, 8, big, true)
          next if delta.zero?

          found = cook_directory_find(handle, offsets, at + delta)
          raise Refused.new('bad_layout', 'a reference names no directory entry') if found.nil?

          target = by_name(fd.target)
          if target.nil? || target.descriptor.type_id != found[1]
            raise Refused.new('bad_layout',
                              'a reference names another node type')
          end

          next
        end
        raise Refused.new('bad_layout', "unsupported cook codec #{fd.codec}")
      end
    end

    def self.check_cook(cls, data, base_offset: 0)
      handle = open_cook(cls, data, base_offset: base_offset)
      if (handle.data_length % handle.alignment) != 0
        raise Refused.new('bad_layout', 'the data part is not a whole number of alignments')
      end

      dir = cook_directory(handle)
      cook_check_directory(handle, dir)
      handle.directory = dir
      offsets = dir.map(&:first)
      region = handle.data.byteslice(handle.data_offset, handle.data_length)
      rdesc = handle.root.descriptor
      dir.each_with_index do |(offset, type_id), idx|
        handle.node_base = offset
        handle.node_end = idx + 1 < dir.length ? dir[idx + 1][0] : handle.data_length
        handle.ranges = SchemaTable.arr
        if [rdesc.bytes_blob_type_id, rdesc.string_blob_type_id].include?(type_id)
          end_p = handle.node_end
          len = unpack_int_order(region, offset, 4, handle.big_endian, false)
          needed = 8 + len + (type_id == rdesc.string_blob_type_id ? 1 : 0)
          raise Refused.new('bad_layout', 'a blob length leaves its extent') if needed > end_p - offset
          if nonzero_slice?(region, offset + 4, offset + 8) || nonzero_slice?(region, offset + needed, end_p)
            raise Refused.new('bad_layout', 'blob padding is not zero')
          end
          if type_id == rdesc.string_blob_type_id && !region.getbyte(offset + 8 + len).zero?
            raise Refused.new('bad_layout', 'a string blob is not terminated')
          end
        else
          cls2 = cook_node_class(type_id)
          handle.node_record_end = offset + cls2.descriptor.size
          cook_scan_record(handle, region, cls2.descriptor, offset, offsets)
          cursor = handle.node_record_end
          handle.ranges.sort_by(&:first).each do |s, e|
            raise Refused.new('bad_layout', 'extent padding is not zero') if nonzero_slice?(region, cursor, s)

            cursor = e
          end
          raise Refused.new('bad_layout', 'extent slack is not zero') if nonzero_slice?(region, cursor, handle.node_end)
        end
      end
      handle
    end

    # Cooked writer side (SPEC-TABLES §7.6)
    def self.cook_text_entry(fd, val)
      tcls = by_name(fd.target)
      raise InvalidValue.new(fd.name, 'a text-list element is not registered') if tcls.nil?

      entry = tcls.new
      tcls.descriptor.fields[0].set(entry, val)
      entry
    end

    def self.cook_map_entries(fd, mapping)
      tcls = by_name(fd.target)
      raise InvalidValue.new(fd.name, 'a map entry is not registered') if tcls.nil?

      kf, vf = tcls.descriptor.fields
      sorted_map_keys(fd, mapping).map do |k|
        entry = tcls.new
        kf.set(entry, k)
        vf.set(entry, mapping[k])
        entry
      end
    end

    def self.cook_collect_edges(desc, value, out, inactive_only: false, inherited_inactive: false)
      desc.fields.each do |fd|
        inactive = inherited_inactive || (!value.is_a?(SingleValueHolder) && !fd.guard_holds?(value))
        next if inactive && !inactive_only

        cur = cook_get_val(value, fd)
        case fd.codec
        when 'table'
          target = by_name(fd.target)
          raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if target.nil?

          cook_collect_edges(target.descriptor, cur, out, inactive_only: inactive_only, inherited_inactive: inactive)
        when 'array'
          if [1, 2].include?(fd.array_kind) && fd.elem_codec == 'table'
            target = by_name(fd.target)
            raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if target.nil?

            cur.each do |item|
              cook_collect_edges(target.descriptor, item, out, inactive_only: inactive_only,
                                                               inherited_inactive: inactive)
            end
          elsif fd.array_kind == 3
            unless %w[int bool enum float32 float64 table string text_optional].include?(fd.elem_codec)
              raise InvalidValue.new(fd.name, "unsupported cook list element #{fd.elem_codec}")
            end

            if fd.elem_codec == 'table'
              target = by_name(fd.target)
              raise InvalidValue.new(fd.name, 'a list table is not registered') if target.nil?

              cur.each do |item|
                cook_collect_edges(target.descriptor, item, out, inactive_only: inactive_only,
                                                                 inherited_inactive: inactive)
              end
            end
          end
        when 'keyed'
          if fd.elem && fd.elem.codec == 'table'
            target = by_name(fd.target)
            raise InvalidValue.new(fd.name, 'a keyed element is not registered') if target.nil?

            vals = fd.keyed_list ? cur : cur._s_values
            vals.each do |item|
              cook_collect_edges(target.descriptor, item, out, inactive_only: inactive_only,
                                                               inherited_inactive: inactive)
            end
          end
        when 'optional'
          unless cur.nil?
            of = cook_optional_field(fd)
            cook_collect_edges(SingleFieldDescHolder.new([of], SchemaTable.arr), SingleValueHolder.new(of, cur), out,
                               inactive_only: inactive_only, inherited_inactive: inactive)
          end
        when 'map'
          cook_map_entries(fd, cur).each do |entry|
            cook_collect_edges(obj_class(entry).descriptor, entry, out, inactive_only: inactive_only,
                                                                        inherited_inactive: inactive)
          end
        when 'union_array'
          uf = cook_union_field(fd)
          cur.each do |item|
            cook_collect_edges(SingleFieldDescHolder.new([uf], SchemaTable.arr), SingleValueHolder.new(uf, item), out,
                               inactive_only: inactive_only, inherited_inactive: inactive)
          end
        when 'union'
          if cur&.type&.positive?
            arm = obj_class(cur).descriptor.arms[cur.type - 1]
            if arm.field
              cook_collect_edges(SingleFieldDescHolder.new([arm.field], SchemaTable.arr), SingleValueHolder.new(arm.field,
                                                                                                                ivar_get(cur,
                                                                                                                         arm.ivar)),
                                 out, inactive_only: inactive_only, inherited_inactive: inactive)
            end
          end
        when 'string_blob', 'bytes_blob', 'pointer'
          out << [fd, cur] if !cur.nil? && (!inactive_only || inactive)
        when 'pointer_array'
          cur.each { |item| out << [fd, item] if !item.nil? && (!inactive_only || inactive) }
        end
      end
    end

    def self.cook_number_nodes(value)
      vcls = obj_class(value)
      vdesc = vcls.descriptor
      root = CookEntry.new(vcls, value, vdesc)
      entries = [root]
      vid = obj_id(value)
      index = { vid => root }
      open_ids = { vid => true }
      blob_index = SchemaTable.map_h
      first = SchemaTable.arr
      cook_collect_edges(vdesc, value, first)
      stack = [[root, first, 0]]
      until stack.empty?
        frame = stack[-1]
        cur_entry, edges, next_idx = frame
        if next_idx >= edges.length
          open_ids.delete(obj_id(cur_entry.value))
          stack.pop
          next
        end
        frame[2] = next_idx + 1
        fd, target_val = edges[next_idx]
        if %w[string_blob bytes_blob].include?(fd.codec)
          bid = target_val.is_a?(BlobRef) ? obj_id(target_val) : nil
          raise InvalidValue.new(fd.name, 'blob_shared') if bid && blob_index.key?(bid)

          body = encode_blob_body(fd, target_val, fd.name)
          bentry = CookEntry.new(Object, target_val, nil)
          bentry.blob = BlobNode.new(fd.codec == 'string_blob', body)
          bentry.type_id = fd.codec == 'string_blob' ? vdesc.string_blob_type_id : vdesc.bytes_blob_type_id
          entries << bentry
          cur_entry.blobs << bentry
          if bid
            blob_index[bid] = bentry
            index[bid] = bentry
          end
          next
        end
        tid = obj_id(target_val)
        raise InvalidValue.new(fd.name, 'a pointer cycle has no cooked form') if open_ids[tid]
        next if index.key?(tid)

        tcls = by_name(fd.target)
        raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if tcls.nil?

        nentry = CookEntry.new(tcls, target_val, tcls.descriptor)
        entries << nentry
        index[tid] = nentry
        open_ids[tid] = true
        more = SchemaTable.arr
        cook_collect_edges(tcls.descriptor, target_val, more)
        stack << [nentry, more, 0]
      end
      [entries, index]
    end

    def self.cook_field_has_extent?(fd, seen)
      if (fd.codec == 'string' && fd.pieces.length == 1) || (fd.codec == 'optional' && fd.inner == 'string' &&
        fd.pieces.length == 2)
        return true
      end
      return true if fd.codec == 'map' || (fd.codec == 'optional' && fd.inner == 'map')
      return true if %w[array union_array pointer_array].include?(fd.codec) && fd.array_kind == 3

      if %w[union union_array].include?(fd.codec)
        key = "union:#{fd.target}"
        return false if seen[key]

        seen[key] = true
        ucls = union_by_name(fd.target)
        return !ucls.nil? && ucls.descriptor.arms.any? { |a| a.field && cook_field_has_extent?(a.field, seen) }
      end
      if %w[table array keyed optional].include?(fd.codec) && !fd.target.empty?
        tcls = by_name(fd.target)
        return !tcls.nil? && cook_has_extent?(tcls.descriptor, seen)
      end
      false
    end

    def self.cook_has_extent?(desc, seen)
      key = "table:#{desc.name}"
      return false if seen[key]

      seen[key] = true
      desc.fields.any? { |fd| cook_field_has_extent?(fd, seen) }
    end

    def self.cook_plan_lists(desc, value, at, cursor, placements,
                             ignore_guard: false, skip_validation: false, inherited_inactive: false)
      desc.fields.each do |fd|
        cur = cook_get_val(value, fd)
        has_guard_check = !value.is_a?(SingleValueHolder)
        inactive = inherited_inactive || (has_guard_check && !fd.guard_holds?(value))
        if inactive && has_guard_check && !skip_validation
          ungated = fd.dup
          ungated.guards = SchemaTable::EMPTY_ARR
          validate_field_edges(ungated, cur, fd.name)
        end
        if !ignore_guard && has_guard_check && !fd.guard_holds?(value)
          trial = [0, cursor[1]]
          cook_plan_lists(SingleFieldDescHolder.new([fd], SchemaTable.arr), value, at, trial, SchemaTable.map_h,
                          ignore_guard: true, skip_validation: true, inherited_inactive: true)
          raise InvalidValue.new(fd.name, 'an inactive field holds an extent') if trial[0].positive?

          next
        end
        slot = at + fd.at
        if fd.codec == 'string' && fd.pieces.length == 1
          unless cur.empty?
            raw = cur.b
            cursor[0] = align_up(cursor[0], 8)
            placements[slot] = cursor[0]
            cursor[0] += align_up(8 + raw.bytesize + 1, cursor[1])
          end
        elsif fd.codec == 'map'
          entries = cook_map_entries(fd, cur)
          unless entries.empty?
            cursor[0] = align_up(cursor[0], fd.elem_align)
            start_p = cursor[0]
            placements[slot] = start_p
            cursor[0] += entries.length * fd.elem_size
            entries.each_with_index do |item, pos|
              cook_plan_lists(obj_class(item).descriptor, item, start_p + (pos * fd.elem_size), cursor, placements,
                              inherited_inactive: inactive)
            end
          end
        elsif fd.codec == 'pointer_array' && fd.array_mode == 'list'
          unless cur.empty?
            cursor[0] = align_up(cursor[0], fd.elem_align)
            placements[slot] = cursor[0]
            cursor[0] += cur.length * fd.elem_size
          end
        elsif fd.codec == 'union_array'
          if fd.array_kind == 3
            next if cur.empty?

            cursor[0] = align_up(cursor[0], fd.elem_align)
            start_p = cursor[0]
            placements[slot] = start_p
            cursor[0] += cur.length * fd.elem_size
            stride = fd.elem_size
          else
            start_p = slot
            stride = fd.pieces[0][1] / fd.bound
          end
          uf = cook_union_field(fd)
          cur.each_with_index do |item, pos|
            cook_plan_lists(SingleFieldDescHolder.new([uf], SchemaTable.arr), SingleValueHolder.new(uf, item),
                            start_p + (pos * stride), cursor, placements, inherited_inactive: inactive)
          end
        elsif fd.codec == 'array' && fd.array_kind == 3
          unless %w[int bool enum float32 float64 table string text_optional].include?(fd.elem_codec)
            raise InvalidValue.new(fd.name, "unsupported cook list element #{fd.elem_codec}")
          end

          unless cur.empty?
            cursor[0] = align_up(cursor[0], fd.elem_align)
            start_p = cursor[0]
            placements[slot] = start_p
            cursor[0] += cur.length * fd.elem_size
            if %w[table string text_optional].include?(fd.elem_codec)
              target = by_name(fd.target)
              raise InvalidValue.new(fd.name, 'a list table is not registered') if target.nil?

              cur.each_with_index do |item, pos|
                el = fd.elem_codec == 'table' ? item : cook_text_entry(fd, item)
                cook_plan_lists(target.descriptor, el, start_p + (pos * fd.elem_size), cursor, placements,
                                inherited_inactive: inactive)
              end
            end
          end
        elsif fd.codec == 'table'
          target = by_name(fd.target)
          cook_plan_lists(target.descriptor, cur, slot, cursor, placements, inherited_inactive: inactive) if target
        elsif fd.codec == 'array' && [1,
                                      2].include?(fd.array_kind) && %w[table string
                                                                       text_optional].include?(fd.elem_codec)
          target = by_name(fd.target)
          if target
            stride = fd.pieces[0][1] / fd.bound
            cur.each_with_index do |item, pos|
              el = fd.elem_codec == 'table' ? item : cook_text_entry(fd, item)
              cook_plan_lists(target.descriptor, el, slot + (pos * stride), cursor, placements,
                              inherited_inactive: inactive)
            end
          end
        elsif fd.codec == 'keyed' && fd.elem && fd.elem.codec == 'table'
          target = by_name(fd.target)
          if target
            vals = fd.keyed_list ? cur : cur._s_values
            stride = fd.pieces[0][1] / fd.bound
            vals.each_with_index do |item, pos|
              cook_plan_lists(target.descriptor, item, slot + (pos * stride), cursor, placements,
                              inherited_inactive: inactive)
            end
          end
        elsif fd.codec == 'optional' && !cur.nil?
          of = cook_optional_field(fd)
          cook_plan_lists(SingleFieldDescHolder.new([of], SchemaTable.arr), SingleValueHolder.new(of, cur), slot, cursor, placements,
                          ignore_guard: false, skip_validation: true, inherited_inactive: inactive)
        elsif fd.codec == 'union' && cur && cur.type.positive?
          ud = obj_class(cur).descriptor
          arm = ud.arms[cur.type - 1]
          if arm.field
            cook_plan_lists(SingleFieldDescHolder.new([arm.field], SchemaTable.arr), SingleValueHolder.new(arm.field,
                                                                                                           ivar_get(cur, arm.ivar)),
                            slot + ud.arm_offset, cursor, placements, inherited_inactive: inactive)
          end
        end
      end
    end

    def self.cook_build_layout(value)
      entries, index = cook_number_nodes(value)
      offset = 0
      align = 8
      entries.each do |entry|
        node_align = entry.blob ? 8 : [entry.desc.cook_align, 1].max
        align = node_align if node_align > align
        offset = align_up(offset, node_align)
        entry.offset = offset
        if entry.blob
          offset += 8 + entry.blob.body.bytesize + (entry.blob.string ? 1 : 0)
        elsif cook_has_extent?(entry.desc, SchemaTable.map_h)
          cursor = [offset + entry.desc.extent_start, entry.desc.region_align]
          cook_plan_lists(entry.desc, entry.value, offset, cursor, entry.placements)
          offset = cursor[0]
        else
          cook_plan_lists(entry.desc, entry.value, offset, [0, entry.desc.region_align], SchemaTable.map_h)
          offset += entry.desc.size
        end
        next if entry.blob

        inactive_edges = SchemaTable.arr
        cook_collect_edges(entry.desc, entry.value, inactive_edges, inactive_only: true)
        inactive_edges.each do |fd, target|
          if %w[string_blob bytes_blob].include?(fd.codec) && !target.is_a?(BlobRef)
            raise InvalidValue.new(fd.name, 'an inactive blob slot has no node index')
          end
          raise InvalidValue.new(fd.name, 'an inactive reference has no node index') unless index.key?(obj_id(target))
        end
      end
      [entries, index, align_up(offset, align), align]
    end

    def self.cook_write_scalar(buf, at, width, codec, val, signed, big)
      case codec
      when 'float32'
        r = float32_round(val)
        buf[at, 4] = [r].pack(big ? 'g' : 'e')
      when 'float64'
        buf[at, 8] = [val].pack(big ? 'G' : 'E')
      when 'bool'
        buf.setbyte(at, val ? 1 : 0)
      else
        buf[at, width] = pack_int_order(val, width, big, signed)
      end
    end

    def self.cook_write_record(buf, desc, value, base, index, big, blob_iter, placements, inherited_inactive: false)
      desc.fields.each do |fd|
        inactive = inherited_inactive || (!value.is_a?(SingleValueHolder) && !fd.guard_holds?(value))
        at = base + fd.at
        cur = cook_get_val(value, fd)
        case fd.codec
        when 'bool'
          buf.setbyte(at, cur ? 1 : 0)
        when 'int', 'enum', 'float32', 'float64'
          width = fd.pieces[0][1]
          cook_write_scalar(buf, at, width, fd.codec, cur, fd.lo.negative?, big)
        when 'string', 'bytes'
          raw = fd.codec == 'string' ? cur.b : cur
          if fd.codec == 'string' && fd.pieces.length == 1
            unless raw.empty?
              start_p = COOK_HEADER_BYTES + placements[at - COOK_HEADER_BYTES]
              buf[at, 8] = pack_int_order(start_p - at, 8, big, true)
              buf[start_p, 4] = pack_int_order(raw.bytesize, 4, big, false)
              buf[start_p + 8, raw.bytesize] = raw
            end
          else
            buf[at, raw.bytesize] = raw if raw.bytesize.positive?
            buf[base + fd.pieces[1][0], 4] = pack_int_order(raw.bytesize, 4, big, true)
          end
        when 'table'
          target = by_name(fd.target)
          raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if target.nil?

          cook_write_record(buf, target.descriptor, cur, at, index, big, blob_iter, placements,
                            inherited_inactive: inactive)
        when 'array'
          if [1, 2].include?(fd.array_kind) && %w[bool int enum float32 float64].include?(fd.elem_codec)
            stride = fd.pieces[0][1] / fd.bound
            cur.each_with_index do |item, pos|
              cook_write_scalar(buf, at + (pos * stride), stride, fd.elem_codec, item, fd.lo.negative?, big)
            end
            buf[base + fd.pieces[1][0], 4] = pack_int_order(cur.length, 4, big, true) if fd.array_kind == 2
          elsif [1, 2].include?(fd.array_kind) && %w[table string text_optional].include?(fd.elem_codec)
            target = by_name(fd.target)
            raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if target.nil?

            stride = fd.pieces[0][1] / fd.bound
            cur.each_with_index do |item, pos|
              el = fd.elem_codec == 'table' ? item : cook_text_entry(fd, item)
              cook_write_record(buf, target.descriptor, el, at + (pos * stride), index, big, blob_iter, placements,
                                inherited_inactive: inactive)
            end
            buf[base + fd.pieces[1][0], 4] = pack_int_order(cur.length, 4, big, true) if fd.array_kind == 2
          elsif fd.array_kind == 3
            unless cur.empty?
              start_p = COOK_HEADER_BYTES + placements[at - COOK_HEADER_BYTES]
              buf[at, 8] = pack_int_order(start_p - at, 8, big, true)
              buf[at + 8, 4] = pack_int_order(cur.length, 4, big, true)
              cur.each_with_index do |item, pos|
                elem_at = start_p + (pos * fd.elem_size)
                if %w[table string text_optional].include?(fd.elem_codec)
                  target = by_name(fd.target)
                  raise InvalidValue.new(fd.name, 'a list table is not registered') if target.nil?

                  el = fd.elem_codec == 'table' ? item : cook_text_entry(fd, item)
                  cook_write_record(buf, target.descriptor, el, elem_at, index, big, blob_iter, placements,
                                    inherited_inactive: inactive)
                else
                  signed = fd.elem ? fd.elem.lo.negative? : fd.lo.negative?
                  cook_write_scalar(buf, elem_at, fd.elem_size, fd.elem_codec, item, signed, big)
                end
              end
            end
          end
        when 'keyed'
          vals = fd.keyed_list ? cur : cur._s_values
          stride = fd.pieces[0][1] / fd.bound
          if %w[bool int enum float32 float64].include?(fd.elem.codec)
            vals.each_with_index do |item, pos|
              cook_write_scalar(buf, at + (pos * stride), stride, fd.elem.codec, item, fd.elem.lo.negative?, big)
            end
          elsif fd.elem.codec == 'table'
            target = by_name(fd.target)
            raise InvalidValue.new(fd.name, 'a keyed element is not registered') if target.nil?

            vals.each_with_index do |item, pos|
              cook_write_record(buf, target.descriptor, item, at + (pos * stride), index, big, blob_iter, placements,
                                inherited_inactive: inactive)
            end
          end
        when 'optional'
          unless cur.nil?
            of = cook_optional_field(fd)
            cook_write_record(buf, SingleFieldDescHolder.new([of], SchemaTable.arr), SingleValueHolder.new(of, cur), at, index,
                              big, blob_iter, placements,
                              inherited_inactive: inactive)
            buf.setbyte(base + fd.pieces[-1][0], 1)
          end
        when 'map'
          entries = cook_map_entries(fd, cur)
          unless entries.empty?
            start_p = COOK_HEADER_BYTES + placements[at - COOK_HEADER_BYTES]
            buf[at, 8] = pack_int_order(start_p - at, 8, big, true)
            buf[at + 8, 4] = pack_int_order(entries.length, 4, big, true)
            entries.each_with_index do |item, pos|
              cook_write_record(buf, obj_class(item).descriptor, item, start_p + (pos * fd.elem_size), index, big,
                                blob_iter, placements,
                                inherited_inactive: inactive)
            end
          end
        when 'union_array'
          uf = cook_union_field(fd)
          if fd.array_kind == 3
            next if cur.empty?

            start_p = COOK_HEADER_BYTES + placements[at - COOK_HEADER_BYTES]
            stride = fd.elem_size
            buf[at, 8] = pack_int_order(start_p - at, 8, big, true)
            buf[at + 8, 4] = pack_int_order(cur.length, 4, big, true)
          else
            start_p = at
            stride = fd.pieces[0][1] / fd.bound
            buf[base + fd.pieces[1][0], 4] = pack_int_order(cur.length, 4, big, true) if fd.array_kind == 2
          end
          cur.each_with_index do |item, pos|
            cook_write_record(buf, SingleFieldDescHolder.new([uf], SchemaTable.arr), SingleValueHolder.new(uf, item),
                              start_p + (pos * stride), index, big, blob_iter, placements, inherited_inactive: inactive)
          end
        when 'union'
          ucls = union_by_name(fd.target)
          raise InvalidValue.new(fd.name, 'a union is not registered') if ucls.nil?

          ud = ucls.descriptor
          tag = cur.nil? ? 0 : cur.type
          buf[at, ud.tag_size] = pack_int_order(tag, ud.tag_size, big, false)
          if tag.positive?
            arm = ud.arms[tag - 1]
            if arm.field
              f_holder = SingleFieldDescHolder.new([arm.field], SchemaTable.arr)
              v_holder = SingleValueHolder.new(arm.field, ivar_get(cur, arm.ivar))
              cook_write_record(buf, f_holder, v_holder, at + ud.arm_offset, index, big, blob_iter, placements,
                                inherited_inactive: inactive)
            end
          end
        when 'string_blob', 'bytes_blob'
          unless cur.nil?
            bentry = u(if inactive
                         cur.is_a?(BlobRef) ? index[obj_id(cur)] : nil
                       else
                         b = blob_iter[0][blob_iter[1]]
                         blob_iter[1] += 1
                         b
                       end)
            raise InvalidValue.new(fd.name, 'an inactive blob slot has no node index') if bentry.nil?

            buf[at, 8] = pack_int_order(COOK_HEADER_BYTES + bentry.offset - at, 8, big, true)
          end
        when 'pointer_array'
          if fd.array_mode == 'list'
            next if cur.empty?

            start_p = COOK_HEADER_BYTES + placements[at - COOK_HEADER_BYTES]
            buf[at, 8] = pack_int_order(start_p - at, 8, big, true)
            buf[at + 8, 4] = pack_int_order(cur.length, 4, big, true)
          else
            start_p = at
            buf[base + fd.pieces[1][0], 4] = pack_int_order(cur.length, 4, big, true) if fd.array_mode == 'counted'
          end
          cur.each_with_index do |item, pos|
            next if item.nil?

            nentry = index[obj_id(item)]
            raise InvalidValue.new(fd.name, 'the pointer target has no node index') if nentry.nil?

            slot = start_p + (pos * 8)
            buf[slot, 8] = pack_int_order(COOK_HEADER_BYTES + nentry.offset - slot, 8, big, true)
          end
        when 'pointer'
          if cur.nil?
            buf[at, 8] = pack_int_order(0, 8, big, true)
          else
            nentry = index[obj_id(cur)]
            raise InvalidValue.new(fd.name, 'the pointer target has no node index') if nentry.nil?

            buf[at, 8] = pack_int_order(COOK_HEADER_BYTES + nentry.offset - at, 8, big, true)
          end
        else
          raise InvalidValue.new(fd.name, "unsupported cook codec #{fd.codec}")
        end
      end
    end

    def self.require_cook_carried!(value)
      check_root_instance(value)
      desc = obj_class(value).descriptor
      raise InvalidValue.new(desc.name, 'the table does not carry cook') unless desc.forms.include?('cook')
    end

    def self.cook_measure(value)
      require_cook_carried!(value)
      validate_table_graph(value, obj_class(value).descriptor, '', SchemaTable.map_h)
      entries, _, data_len, = cook_build_layout(value)
      COOK_HEADER_BYTES + data_len + (entries.length * COOK_DIRECTORY_ENTRY)
    end

    def self.cook_table(value, order: COOK_ORDER_LITTLE, capacity: nil)
      require_cook_carried!(value)
      unless [COOK_ORDER_LITTLE, COOK_ORDER_BIG].include?(order)
        raise ArgumentError, 'the byte order is 1 (little-endian) or 2 (big-endian)'
      end

      big = (order == COOK_ORDER_BIG)
      desc = obj_class(value).descriptor
      validate_table_graph(value, desc, '', SchemaTable.map_h)
      entries, index, data_len, align = cook_build_layout(value)
      total = COOK_HEADER_BYTES + data_len + (entries.length * COOK_DIRECTORY_ENTRY)
      raise InvalidValue.new('', 'the capacity does not hold the cook') if !capacity.nil? && capacity < total

      buf = "\x00".b * total
      entries.each do |entry|
        at = COOK_HEADER_BYTES + entry.offset
        if entry.blob
          buf[at, 4] = pack_int_order(entry.blob.body.bytesize, 4, big, false)
          buf[at + 8, entry.blob.body.bytesize] = entry.blob.body if entry.blob.body.bytesize.positive?
        else
          cook_write_record(buf, entry.desc, entry.value, at, index, big, [entry.blobs, 0], entry.placements)
        end
      end
      buf[0, 8] = pack_int_order(COOK_MAGIC, 8, big, false)
      buf[8, 8] = pack_int_order(desc.build_version, 8, big, false)
      buf[16, 8] = pack_int_order(order, 8, big, false)
      buf[24, 8] = pack_int_order(data_len, 8, big, false)
      buf[32, 8] = pack_int_order(entries.length * COOK_DIRECTORY_ENTRY, 8, big, false)
      buf[40, 8] = pack_int_order(align, 8, big, false)
      at = COOK_HEADER_BYTES + data_len
      entries.each do |entry|
        buf[at, 8] = pack_int_order(entry.offset, 8, big, false)
        buf[at + 8, 8] = pack_int_order(entry.type_id, 8, big, false)
        at += COOK_DIRECTORY_ENTRY
      end
      buf
    end

    def self.dump_cook(handle)
      region = handle.data.byteslice(handle.data_offset, handle.data_length)
      big = handle.big_endian
      lines = SchemaTable.arr
      reached = SchemaTable.map_h

      int_at = ->(at, w, s) { unpack_int_order(region, at, w, big, s) }
      emit_ln = ->(path, val) { lines << "  #{path} = #{val}" }
      scalar_at = lambda do |at, w, codec, signed|
        raise Refused.new('bad_layout', 'the cook dump does not define a float spelling') if %w[float32
                                                                                                float64].include?(codec)

        v = int_at.call(at, w, signed)
        codec == 'bool' ? (!v.zero?).to_s : v.to_s
      end

      record_edges = u(nil)
      record_edges = lambda do |desc, at, prefix|
        edges = SchemaTable.arr
        desc.fields.each do |fd|
          nm = prefix.empty? ? fd.name : "#{prefix}.#{fd.name}"
          slot = at + fd.at
          if %w[bool int enum float32 float64].include?(fd.codec)
            emit_ln.call(nm, scalar_at.call(slot, fd.pieces[0][1], fd.codec, fd.codec == 'int' && fd.lo.negative?))
          elsif %w[string bytes].include?(fd.codec)
            if fd.codec == 'string' && fd.pieces.length == 1
              delta = int_at.call(slot, 8, true)
              start_p = delta.zero? ? 0 : slot + delta
              cnt = delta.zero? ? 0 : int_at.call(start_p, 4, false)
              emit_ln.call(nm, format_cook_text(region, start_p + 8, cnt))
            else
              cnt = int_at.call(at + fd.pieces[1][0], 4, true)
              raise Refused.new('bad_layout', 'a text length leaves its buffer') unless cnt.between?(0, fd.size)

              emit_ln.call(nm, format_cook_text(region, slot, cnt))
            end
          elsif fd.codec == 'table'
            child = by_name(fd.target)
            raise Refused.new('bad_layout', 'a nested table is not registered') if child.nil?

            edges.concat(record_edges.call(child.descriptor, slot, nm))
          elsif fd.codec == 'array' && fd.array_kind == 3
            delta = int_at.call(slot, 8, true)
            cnt = int_at.call(slot + 8, 4, true)
            cnt.times do |pos|
              elem_at = slot + delta + (pos * fd.elem_size)
              if %w[table string text_optional].include?(fd.elem_codec)
                child = by_name(fd.target)
                raise Refused.new('bad_layout', 'a list table is not registered') if child.nil?

                edges.concat(record_edges.call(child.descriptor, elem_at, "#{nm}[#{pos}]"))
              else
                emit_ln.call("#{nm}[#{pos}]",
                             scalar_at.call(elem_at, fd.elem_size, fd.elem_codec,
                                            fd.elem_codec == 'int' && fd.lo.negative?))
              end
            end
            emit_ln.call("#{nm}#count", cnt.to_s)
          elsif fd.codec == 'map'
            delta = int_at.call(slot, 8, true)
            cnt = int_at.call(slot + 8, 4, true)
            child = by_name(fd.target)
            raise Refused.new('bad_layout', 'a map entry is not registered') if child.nil?

            cnt.times do |pos|
              edges.concat(record_edges.call(child.descriptor, slot + delta + (pos * fd.elem_size), "#{nm}[#{pos}]"))
            end
            emit_ln.call("#{nm}#count", cnt.to_s)
          elsif fd.codec == 'union_array'
            ucls = union_by_name(fd.target)
            raise Refused.new('bad_layout', 'a union is not registered') if ucls.nil?

            ud = ucls.descriptor
            if fd.array_kind == 3
              delta = int_at.call(slot, 8, true)
              cnt = int_at.call(slot + 8, 4, true)
              start_p = slot + delta
              stride = fd.elem_size
            else
              cnt = fd.bound
              start_p = slot
              stride = fd.pieces[0][1] / fd.bound
            end
            cnt.times do |pos|
              uat = start_p + (pos * stride)
              tag = int_at.call(uat, ud.tag_size, false)
              enm = "#{nm}[#{pos}]"
              emit_ln.call("#{enm}#tag", tag.to_s)
              arm = u(tag.positive? ? ud.arms[tag - 1] : nil)
              if arm&.field
                edges.concat(record_edges.call(SingleFieldDescHolder.new([arm.field], SchemaTable.arr), uat + ud.arm_offset, enm))
              end
            end
            if fd.array_kind == 2
              emit_ln.call("#{nm}#count", int_at.call(at + fd.pieces[1][0], 4, true).to_s)
            elsif fd.array_kind == 3
              emit_ln.call("#{nm}#count", cnt.to_s)
            end
          elsif fd.codec == 'union'
            ucls = union_by_name(fd.target)
            raise Refused.new('bad_layout', 'a union is not registered') if ucls.nil?

            ud = ucls.descriptor
            tag = int_at.call(slot, ud.tag_size, false)
            emit_ln.call("#{nm}#tag", tag.to_s)
            arm = u(tag.positive? ? ud.arms[tag - 1] : nil)
            if arm&.field
              edges.concat(record_edges.call(SingleFieldDescHolder.new([arm.field], SchemaTable.arr), slot + ud.arm_offset,
                                             nm))
            end
          elsif %w[string_blob bytes_blob].include?(fd.codec)
            delta = int_at.call(slot, 8, true)
            emit_ln.call(nm, delta.zero? ? 'null' : "-> @#{slot + delta}")
          elsif fd.codec == 'pointer_array'
            child = by_name(fd.target)
            raise Refused.new('bad_layout', 'a pointer array target is not registered') if child.nil?

            if fd.array_mode == 'list'
              delta = int_at.call(slot, 8, true)
              cnt = int_at.call(slot + 8, 4, true)
              start_p = slot + delta
            else
              start_p = slot
              cnt = fd.bound
            end
            cnt.times do |pos|
              el = start_p + (pos * 8)
              d = int_at.call(el, 8, true)
              emit_ln.call("#{nm}[#{pos}]", d.zero? ? 'null' : "-> @#{el + d}")
              edges << [el + d, child.descriptor] unless d.zero?
            end
            if fd.array_mode == 'counted'
              emit_ln.call("#{nm}#count", int_at.call(at + fd.pieces[1][0], 4, true).to_s)
            elsif fd.array_mode == 'list'
              emit_ln.call("#{nm}#count", cnt.to_s)
            end
          elsif fd.codec == 'pointer'
            delta = int_at.call(slot, 8, true)
            if delta.zero?
              emit_ln.call(nm, 'null')
            else
              target_at = slot + delta
              child = by_name(fd.target)
              raise Refused.new('bad_layout', 'a pointer target is not registered') if child.nil?

              emit_ln.call(nm, "-> @#{target_at}")
              edges << [target_at, child.descriptor]
            end
          elsif fd.codec == 'array' && [1,
                                        2].include?(fd.array_kind) && %w[bool int enum float32
                                                                         float64].include?(fd.elem_codec)
            stride = fd.pieces[0][1] / fd.bound
            fd.bound.times do |pos|
              emit_ln.call("#{nm}[#{pos}]",
                           scalar_at.call(slot + (pos * stride), stride, fd.elem_codec,
                                          fd.elem_codec == 'int' && fd.lo.negative?))
            end
            emit_ln.call("#{nm}#count", int_at.call(at + fd.pieces[1][0], 4, true).to_s) if fd.array_kind == 2
          elsif fd.codec == 'array' && [1,
                                        2].include?(fd.array_kind) && %w[table string
                                                                         text_optional].include?(fd.elem_codec)
            child = by_name(fd.target)
            raise Refused.new('bad_layout', 'an array element is not registered') if child.nil?

            stride = fd.pieces[0][1] / fd.bound
            fd.bound.times do |pos|
              edges.concat(record_edges.call(child.descriptor, slot + (pos * stride), "#{nm}[#{pos}]"))
            end
            emit_ln.call("#{nm}#count", int_at.call(at + fd.pieces[1][0], 4, true).to_s) if fd.array_kind == 2
          elsif fd.codec == 'keyed' && fd.elem && %w[bool int enum float32 float64].include?(fd.elem.codec)
            stride = fd.pieces[0][1] / fd.bound
            fd.bound.times do |pos|
              emit_ln.call("#{nm}[#{pos}]",
                           scalar_at.call(slot + (pos * stride), stride, fd.elem.codec,
                                          fd.elem.codec == 'int' && fd.elem.lo.negative?))
            end
          elsif fd.codec == 'keyed' && fd.elem && fd.elem.codec == 'table'
            child = by_name(fd.target)
            raise Refused.new('bad_layout', 'a keyed element is not registered') if child.nil?

            stride = fd.pieces[0][1] / fd.bound
            fd.bound.times do |pos|
              edges.concat(record_edges.call(child.descriptor, slot + (pos * stride), "#{nm}[#{pos}]"))
            end
          elsif fd.codec == 'optional'
            of = cook_optional_field(fd)
            edges.concat(record_edges.call(SingleFieldDescHolder.new([of], SchemaTable.arr), slot, prefix))
            emit_ln.call("#{nm}#present", (!region.getbyte(at + fd.pieces[-1][0]).zero?).to_s)
          else
            raise Refused.new('bad_layout', "unsupported cook dump codec #{fd.codec}")
          end
        end
        edges
      end

      begin_node = lambda do |at, desc|
        if reached.key?(at)
          raise Refused.new('bad_layout', 'one offset has two table types') unless reached[at].equal?(desc)

          return nil
        end
        if at.negative? || at + desc.size > region.bytesize
          raise Refused.new('bad_layout',
                            'a record leaves the region')
        end

        reached[at] = desc
        lines << "node #{reached.length - 1} #{desc.name} @#{at}"
        record_edges.call(desc, at, '')
      end

      first = begin_node.call(0, handle.root.descriptor)
      stack = [[first, 0]]
      until stack.empty?
        frame = stack[-1]
        edges, idx = frame
        if idx >= edges.length
          stack.pop
          next
        end
        frame[1] = idx + 1
        target_at, cdesc = edges[idx]
        child_edges = begin_node.call(target_at, cdesc)
        stack << [child_edges, 0] if child_edges
      end
      "#{lines.join("\n")}\n"
    end
  end
end
