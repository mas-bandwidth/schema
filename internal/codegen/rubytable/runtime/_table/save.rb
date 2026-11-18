# frozen_string_literal: true

module SchemaRuntimeUnit
  # Form-1 binary writer, validator, and node collector.
  module SchemaTable
    NO_SUB = Object.new.freeze

    # Binary writer or counting sink for form-1 save and measure.
    class Writer
      attr_reader :buf, :count

      def initialize(buf)
        @buf = buf
        @count = 0
      end

      def byte(val)
        @count += 1
        @buf << (val & 0xFF) unless @buf.nil?
      end

      def raw(bytes)
        @count += bytes.bytesize
        @buf << bytes unless @buf.nil?
      end

      def varint(val)
        raise InvalidValue.new('', 'a reference is negative') if val.negative?

        loop do
          b = val & 0x7F
          val >>= 7
          if val.positive?
            byte(b | 0x80)
          else
            byte(b)
            return
          end
        end
      end

      def begin_frame
        mark = @count
        10.times { byte(0) }
        mark
      end

      def finish_frame(mark)
        finish_varint(mark, @count - mark - 10)
      end

      def finish_varint(mark, val)
        prefix = String.new(encoding: Encoding::ASCII_8BIT)
        while val >= 128
          prefix << ((val & 0x7F) | 0x80)
          val >>= 7
        end
        prefix << val
        shift = 10 - prefix.bytesize
        unless @buf.nil?
          tail = @buf.byteslice(mark + 10, @count - mark - 10)
          @buf.byteslice(0, mark)
          @buf[mark, @count - mark] = prefix + tail
        end
        @count -= shift
      end

      def truncate(mark)
        @buf[mark, @count - mark] = ''.b if !@buf.nil? && @count > mark
        @count = mark
      end
    end

    # Context for a normal form-1 save or measure.
    class SaveCtx
      attr_accessor :ids, :refs, :indices, :node_blobs, :blob_slots, :blob_next,
                    :order, :onstack, :current, :blob_refs

      def initialize
        @ids = SchemaTable.arr
        @refs = SchemaTable.map_h
        @indices = SchemaTable.map_h
        @node_blobs = SchemaTable.map_h
        @blob_slots = SchemaTable.arr
        @blob_next = 0
        @order = SchemaTable.arr
        @onstack = SchemaTable.map_h
        @current = nil
        @blob_refs = SchemaTable.map_h
      end

      def idref(val)
        ref = @refs[val]
        if ref.nil?
          @ids << val
          ref = @ids.length
          @refs[val] = ref
        end
        ref
      end

      def write_ids(writer)
        raise InvalidValue.new('', 'known values need more than 4096 wire IDs') if @ids.length > ID_BOUND

        @ids.each { |tid| writer.raw([tid].pack('Q<')) }
        writer.raw([@ids.length].pack('Q<'))
      end
    end

    # Context for a retaining form-1 save or measure.
    class RetainSaveCtx < SaveCtx
      attr_accessor :retain, :node, :steps, :lost, :known, :ordinal, :preview,
                    :reserved, :remaining, :emitted

      def initialize(retain, known)
        super()
        @retain = retain
        @node = 1
        @steps = SchemaTable::EMPTY_ARR
        @lost = 0
        @known = known
        @ordinal = 0
        @preview = false
        @reserved = SchemaTable.map_h
        @remaining = 0
        @emitted = 0
      end

      def can_id?(val)
        @refs.key?(val) || @reserved[val] || (@ids.length + @retain.id_used + @remaining < ID_BOUND)
      end

      def idref(val)
        ref = @refs[val]
        if ref.nil?
          ref = @ids.length + @retain.id_used + 1
          @ids << val
          @refs[val] = ref
          @remaining -= 1 if @reserved[val]
        end
        ref
      end

      def forget_id(val)
        @refs.delete(val)
        @remaining += 1 if @reserved[val]
      end

      def write_ids(writer)
        known_idx = 0
        retained_idx = 1
        while known_idx < @ids.length || retained_idx <= @retain.id_used
          known_slot = known_idx < @ids.length ? @refs[@ids[known_idx]] : 0
          retained_slot = retained_idx <= @retain.id_used ? @retain.id_word(retained_idx, 8) : 0
          if known_slot.positive? && (retained_slot.zero? || known_slot < retained_slot)
            writer.raw([@ids[known_idx]].pack('Q<'))
            known_idx += 1
          else
            writer.raw([@retain.id_key(retained_idx)].pack('Q<'))
            retained_idx += 1
          end
        end
        writer.raw([@ids.length + @retain.id_used].pack('Q<'))
      end
    end

    def self.save_table(value)
      sink = String.new(encoding: Encoding::ASCII_8BIT)
      encode_table(value, Writer.new(sink))
      sink
    end

    def self.measure_table(value)
      encode_table(value, Writer.new(nil))
    end

    def self.check_root_instance(value)
      cls_obj = obj_class(value)
      raise InvalidValue.new('', 'not a generated table') unless cls_obj.respond_to?(:descriptor)

      desc = cls_obj.descriptor
      registered = by_name(desc.name)
      raise InvalidValue.new('', "not a #{desc.name}") if registered.nil? || !cls_obj.equal?(registered)
    end

    def self.encode_table(value, writer)
      check_root_instance(value)
      validate_table_graph(value, obj_class(value).descriptor, '', SchemaTable.map_h)
      ctx = SaveCtx.new
      vid = obj_id(value)
      ctx.indices[vid] = 1
      ctx.onstack[vid] = true
      collect_root_graph(value, ctx)
      writer.byte(FORM)
      write_table_body(writer, value, ctx, true)
      ctx.write_ids(writer)
      writer.count
    end

    def self.encode_retain_table(value, retain, writer, known)
      check_root_instance(value)
      raise InvalidValue.new('', 'retention belongs to a different loaded root') unless retain.loaded_root.equal?(value)

      validate_table_graph(value, obj_class(value).descriptor, '', SchemaTable.map_h)
      ctx = RetainSaveCtx.new(retain, known)
      vid = obj_id(value)
      ctx.indices[vid] = 1
      ctx.onstack[vid] = true
      collect_root_graph(value, ctx)
      retain.begin_save(ctx, retain.directory)
      ctx.preview = true
      counter = Writer.new(nil)
      counter.byte(FORM)
      write_table_body(counter, value, ctx, true)
      reserved = SchemaTable.map_h
      ctx.ids.each { |id| reserved[id] = true }
      raise InvalidValue.new('', 'known values need more than 4096 wire IDs') if reserved.length > ID_BOUND

      ctx.ids.clear
      ctx.refs.clear
      ctx.reserved = reserved.freeze
      ctx.remaining = reserved.length
      ctx.preview = false
      ctx.blob_next = 0
      ctx.lost = 0
      ctx.emitted = 0
      retain.begin_save(ctx, retain.directory)
      writer.byte(FORM)
      write_table_body(writer, value, ctx, true)
      ctx.write_ids(writer)
      [writer.count, ctx.lost + retain.unplaced]
    end

    def self.join_path(parent, name)
      parent.empty? ? name : "#{parent}.#{name}"
    end

    def self.render_edge_path(path)
      segments = SchemaTable.arr
      cur = path
      until cur.nil?
        parent, name, sub = cur
        segments << (sub.equal?(NO_SUB) ? name : "#{name}[#{sub.inspect}]")
        cur = parent
      end
      out = +''
      segments.reverse_each do |seg|
        out << '.' if !out.empty? && !seg.start_with?('[')
        out << seg
      end
      out
    end

    # Iterative whole-value validation before writing a single byte.
    def self.validate_table_graph(value, desc, path, seen)
      vid = obj_id(value)
      return if seen[vid]

      seen[vid] = true
      stack = [[value, desc, path, 0]]
      until stack.empty?
        frame = stack[-1]
        cur_val, cur_desc, cur_path, idx = frame
        if idx >= cur_desc.fields.length
          stack.pop
          next
        end
        field = cur_desc.fields[idx]
        frame[3] = idx + 1
        next unless field.guard_holds?(cur_val)

        fval = field.get(cur_val)
        where = join_path(cur_path, field.name)
        children = validate_field_edges(field, fval, where)
        children.reverse_each do |child, child_where|
          cid = obj_id(child)
          next if seen[cid]

          seen[cid] = true
          stack << [child, obj_class(child).descriptor, child_where, 0]
        end
      end
    end

    def self.validate_field_edges(field, current, where)
      edges = SchemaTable.arr
      case field.codec
      when 'optional'
        return edges if current.nil?

        case field.inner
        when 'map'
          validate_map_edges(field, current, where, edges)
        when 'array'
          validate_optional_array(field, current, where, edges)
        when 'table'
          target = by_name(field.target)
          raise InvalidValue.new(where, "not a #{field.target}") if target.nil? || !obj_class(current).equal?(target)

          edges << [current, where]
        when 'union'
          inner_fd = FieldDesc.new(name: field.name, field_id: field.field_id, kind: field.kind, codec: 'union',
                                   target: field.target, ordinal: field.ordinal)
          validate_union_edges(inner_fd, current, where, edges)
        else
          inner_fd = FieldDesc.new(
            name: field.name, field_id: field.field_id, kind: field.kind, codec: field.inner,
            lo: field.lo, hi: field.hi, f32: field.f32, float_range: field.float_range,
            fmin: field.fmin, fmax: field.fmax, target: field.target, size: field.size,
            unbounded: field.unbounded, map_entry: field.map_entry, bound: field.bound, ordinal: field.ordinal,
            variant_ids: field.variant_ids, deprecated: field.deprecated, default: field.default_value
          )
          validate_scalar_or_leaf(inner_fd, current, where)
        end
      when 'pointer'
        return edges if current.nil?

        target = by_name(field.target)
        if target.nil? || !obj_class(current).equal?(target)
          raise InvalidValue.new(where,
                                 "not a #{field.target} or nil")
        end

        edges << [current, where]
      when 'pointer_array'
        validate_pointer_array(field, current, where, edges)
      when 'table'
        target = by_name(field.target)
        raise InvalidValue.new(where, "not a #{field.target}") if target.nil? || !obj_class(current).equal?(target)

        edges << [current, where]
      when 'union', 'union_array'
        validate_union_edges(field, current, where, edges)
      when 'map'
        validate_map_edges(field, current, where, edges)
      when 'array'
        validate_plain_array(field, current, where, edges)
      when 'keyed'
        validate_keyed_edges(field, current, where, edges)
      else
        validate_scalar_or_leaf(field, current, where)
      end
      edges
    end

    def self.validate_scalar_or_leaf(fd, value, path)
      case fd.codec
      when 'bool'
        validate_bool(value, path)
      when 'int'
        validate_int(value, path, fd.lo, fd.hi)
      when 'float32'
        if fd.float_range
          validate_compressed_float(value, path, fd.fmin, fd.fmax)
        else
          validate_float32(value, path)
        end
      when 'float64'
        validate_float64(value, path)
        if fd.float_range && (value < fd.fmin || value > fd.fmax)
          raise InvalidValue.new(path, "#{value} is outside [#{fd.fmin}, #{fd.fmax}]")
        end
      when 'string'
        validate_text(value, path, fd.size)
      when 'bytes'
        validate_bytes(value, path, fd.size)
      when 'enum'
        raise InvalidValue.new(path, 'not an Integer') unless value.instance_of?(Integer)
        raise InvalidValue.new(path, "#{value} names no variant") if !value.zero? && !fd.wire_of.key?(value)
        if fd.deprecated[value]
          raise InvalidValue.new(path,
                                 "#{value} names a deprecated variant; a strict write refuses it")
        end
      when 'text_optional'
        validate_text(value, path, fd.size) unless value.nil?
      when 'string_blob', 'bytes_blob'
        encode_blob_body(fd, value, path) unless value.nil?
      else
        raise InvalidValue.new(path, "unsupported codec #{fd.codec}")
      end
    end

    def self.encode_blob_body(fd, value, path)
      raw_val = value.is_a?(BlobRef) ? value.value : value
      if fd.codec == 'string_blob'
        utf8 = validate_text(raw_val, path, 0)
        utf8.b
      else
        validate_bytes(raw_val, path, 0)
      end
    end

    def self.validate_pointer_array(fd, value, path, edges)
      raise InvalidValue.new(path, 'not an Array') unless value.instance_of?(Array)

      n = value.length
      if fd.array_mode == 'fixed'
        raise InvalidValue.new(path, "length #{n} is not #{fd.bound}") if n != fd.bound
      elsif fd.array_mode == 'counted'
        if n < fd.min_count || n > fd.bound
          raise InvalidValue.new(path,
                                 "length #{n} is outside [#{fd.min_count}, #{fd.bound}]")
        end
      elsif n > COUNT_CAP
        raise InvalidValue.new(path, 'length exceeds int32 storage cap')
      end
      target = by_name(fd.target)
      raise InvalidValue.new(path, "the table #{fd.target} is not registered") if target.nil?

      value.each_with_index do |elem, idx|
        next if elem.nil?

        cell = "#{path}[#{idx}]"
        raise InvalidValue.new(cell, "not a #{fd.target} or nil") unless obj_class(elem).equal?(target)

        edges << [elem, cell]
      end
    end

    def self.validate_optional_array(fd, value, path, edges)
      raise InvalidValue.new(path, 'not an Array') unless value.instance_of?(Array)

      if fd.array_kind == 1
        if value.length != fd.bound
          raise InvalidValue.new(path,
                                 "a fixed array holds exactly #{fd.bound} elements, not #{value.length}")
        end
      elsif value.length < fd.array_min || value.length > fd.bound
        raise InvalidValue.new(path, "#{value.length} elements is outside [#{fd.array_min}, #{fd.bound}]")
      end
      elem = fd.elem
      raise InvalidValue.new(path, 'an optional array carries no element descriptor') if elem.nil?

      if elem.codec == 'table'
        target = by_name(elem.target)
        value.each_with_index do |item, idx|
          cell = "#{path}[#{idx}]"
          raise InvalidValue.new(cell, "not a #{elem.target}") if target.nil? || !obj_class(item).equal?(target)

          edges << [item, cell]
        end
      else
        value.each_with_index do |item, idx|
          validate_scalar_or_leaf(elem, item, "#{path}[#{idx}]")
        end
      end
    end

    def self.validate_plain_array(fd, value, path, edges)
      raise InvalidValue.new(path, 'not an Array') unless value.instance_of?(Array)

      count = value.length
      raise InvalidValue.new(path, "a list of #{count} elements is past the int32 storage cap") if count > COUNT_CAP
      if fd.array_kind == 1 && count != fd.bound
        raise InvalidValue.new(path,
                               "#{count} elements where #{fd.bound} are declared")
      end
      if fd.array_kind == 2 && (count < fd.array_min || (fd.bound.positive? && count > fd.bound))
        raise InvalidValue.new(path, "#{count} elements outside [#{fd.array_min}, #{fd.bound}]")
      end

      element = fd.element
      raise InvalidValue.new(path, 'an array carries no element descriptor') if element.nil?

      total_text = 0
      value.each_with_index do |item, idx|
        cell = "#{path}[#{idx}]"
        if element.codec == 'table'
          target = by_name(element.target)
          raise InvalidValue.new(cell, "not a #{element.target}") if target.nil? || !obj_class(item).equal?(target)

          edges << [item, cell]
        else
          validate_scalar_or_leaf(element, item, cell)
          total_text += item.bytesize if fd.text_total.positive? && !item.nil?
        end
      end
      raise InvalidValue.new(path, 'text total exceeded') if fd.text_total.positive? && total_text > fd.text_total
    end

    def self.validate_keyed_edges(fd, value, path, edges)
      elem = fd.elem
      raise InvalidValue.new(path, 'a keyed field without an element descriptor') if elem.nil?

      slots = if fd.keyed_list
                value.instance_of?(Array) ? value : nil
              else
                value.is_a?(KeyedArray) ? value._s_values : nil
              end
      if slots.nil? || slots.length != fd.keys.length
        raise InvalidValue.new(path, "not the keyed array of #{fd.keys.length} slots by #{fd.key_enum}")
      end

      if elem.codec == 'table'
        target = by_name(elem.target)
        slots.each_with_index do |cur, slot|
          cell = "#{path}[#{slot + 1}]"
          raise InvalidValue.new(cell, "not a #{elem.target}") if target.nil? || !obj_class(cur).equal?(target)

          edges << [cur, cell]
        end
      else
        slots.each_with_index do |cur, slot|
          validate_scalar_or_leaf(elem, cur, "#{path}[#{slot + 1}]")
        end
      end
      fd.keys.each_with_index do |key, slot|
        if key.zero?
          raise InvalidValue.new("#{path}[#{slot + 1}]",
                                 "the key #{slot + 1} names no variant of #{fd.key_enum}")
        end
      end
    end

    def self.validate_map_edges(fd, value, path, edges)
      raise InvalidValue.new(path, 'not a Hash') unless value.instance_of?(Hash)

      entry_cls = by_name(fd.target)
      raise InvalidValue.new(path, "the table #{fd.target} is not registered") if entry_cls.nil?

      key_field = entry_cls.descriptor.fields[0]
      value_field = entry_cls.descriptor.fields[1]
      value.each do |k, cur|
        where = "#{path}[#{k.inspect}]"
        validate_scalar_or_leaf(key_field, k, where)
        case value_field.codec
        when 'pointer'
          unless cur.nil?
            target = by_name(value_field.target)
            if target.nil? || !obj_class(cur).equal?(target)
              raise InvalidValue.new(where,
                                     "not a #{value_field.target} or nil")
            end

            edges << [cur, where]
          end
        when 'table'
          target = by_name(value_field.target)
          raise InvalidValue.new(where, "not a #{value_field.target}") if target.nil? || !obj_class(cur).equal?(target)

          edges << [cur, where]
        when 'array'
          validate_plain_array(value_field, cur, where, edges)
        when 'pointer_array'
          validate_pointer_array(value_field, cur, where, edges)
        when 'map'
          validate_map_edges(value_field, cur, where, edges)
        when 'keyed'
          validate_keyed_edges(value_field, cur, where, edges)
        when 'union', 'union_array'
          validate_union_edges(value_field, cur, where, edges)
        else
          validate_scalar_or_leaf(value_field, cur, where)
        end
      end
    end

    def self.validate_union_edges(fd, value, path, edges)
      if fd.codec == 'union_array'
        raise InvalidValue.new(path, 'not an Array') unless value.instance_of?(Array)

        count = value.length
        if fd.array_kind == 1
          raise InvalidValue.new(path, "a fixed array of #{fd.bound} holds #{count}") if count != fd.bound
        elsif fd.array_kind == 2
          if count < fd.array_min || count > fd.bound
            raise InvalidValue.new(path,
                                   "a counted array holds #{count}, outside [#{fd.array_min}, #{fd.bound}]")
          end
        elsif count > COUNT_CAP
          raise InvalidValue.new(path, "a list of #{count} elements is past the int32 storage cap")
        end
        value.each_with_index do |item, idx|
          validate_single_union(item, fd.target, "#{path}[#{idx}]", edges)
        end
      else
        validate_single_union(value, fd.target, path, edges)
      end
    end

    def self.validate_single_union(value, name, path, edges)
      un_cls = union_by_name(name)
      raise InvalidValue.new(name, "the union #{name} is not registered") if un_cls.nil?
      raise InvalidValue.new(path, "not a #{name}") unless obj_class(value).equal?(un_cls)

      tag = value.type
      arms = un_cls.descriptor.arms
      unless tag.instance_of?(Integer) && tag >= 0 && tag <= arms.length
        raise InvalidValue.new(path,
                               "type #{tag.inspect} names no arm")
      end
      return if tag.zero?

      arm = arms[tag - 1]
      raise InvalidValue.new(path, "arm #{arm.name} is deprecated; a strict write refuses it") if arm.deprecated
      return if arm.field.nil?

      validate_arm_value(arm.field, ivar_get(value, arm.ivar), "#{path}.#{arm.name}", edges)
    end

    def self.validate_arm_value(fd, value, path, edges)
      case fd.codec
      when 'pointer', 'table'
        return if fd.codec == 'pointer' && value.nil?

        target = by_name(fd.target)
        suffix = fd.codec == 'pointer' ? ' or nil' : ''
        raise InvalidValue.new(path, "not a #{fd.target}#{suffix}") if target.nil? || !obj_class(value).equal?(target)

        edges << [value, path]
      when 'pointer_array'
        validate_pointer_array(fd, value, path, edges)
      when 'union', 'union_array'
        validate_union_edges(fd, value, path, edges)
      when 'map'
        validate_map_edges(fd, value, path, edges)
      when 'array'
        raise InvalidValue.new(path, 'not an Array') unless value.instance_of?(Array)

        count = value.length
        raise InvalidValue.new(path, "a list of #{count} elements is past the int32 storage cap") if count > COUNT_CAP
        if fd.array_kind == 1 && count != fd.bound
          raise InvalidValue.new(path,
                                 "#{count} elements where #{fd.bound} are declared")
        end
        if fd.array_kind == 2 && (count < fd.array_min || (fd.bound.positive? && count > fd.bound))
          raise InvalidValue.new(path, "#{count} elements outside [#{fd.array_min}, #{fd.bound}]")
        end

        value.each_with_index do |item, idx|
          validate_arm_value(fd.element, item, "#{path}[#{idx}]", edges)
        end
      when 'keyed'
        validate_keyed_edges(fd, value, path, edges)
      else
        validate_scalar_or_leaf(fd, value, path)
      end
    end

    # Pre-order node and blob numbering walk (iterative, cycle-safe).
    def self.collect_root_graph(value, ctx)
      root_blobs = SchemaTable.arr
      ctx.node_blobs[1] = root_blobs
      collect_walk(collect_table_edges(value, ctx, root_blobs, nil), ctx)
      ctx.blob_slots = root_blobs.dup
      ctx.order.each_with_index do |node, pos|
        ctx.blob_slots.concat(ctx.node_blobs[pos + 2]) unless node.is_a?(BlobNode)
      end
    end

    def self.collect_walk(start_edges, ctx)
      stack = [[start_edges, 0, -1]]
      until stack.empty?
        frame = stack[-1]
        edges, idx, open_key = frame
        if idx >= edges.length
          stack.pop
          ctx.onstack.delete(open_key) if open_key >= 0
          next
        end
        frame[1] = idx + 1
        entry = edges[idx]
        if entry[0] == :blob
          _, fd, val, path_str, blobs = entry
          if val.is_a?(BlobRef)
            bid = obj_id(val)
            raise InvalidValue.new(path_str, 'blob_shared') if ctx.blob_refs[bid]

            ctx.blob_refs[bid] = true
          end
          body = encode_blob_body(fd, val, path_str)
          blob_idx = ctx.order.length + 2
          ctx.order << BlobNode.new(fd.codec == 'string_blob', body)
          blobs << blob_idx
          next
        end
        child, where, is_node, child_blobs = entry
        unless is_node
          stack << [collect_table_edges(child, ctx, child_blobs, where), 0, -1]
          next
        end
        key = obj_id(child)
        if ctx.indices.key?(key)
          raise InvalidValue.new(render_edge_path(where), 'a pointer cycle has no wire form') if ctx.onstack[key]

          next
        end
        node_index = ctx.order.length + 2
        ctx.indices[key] = node_index
        ctx.order << child
        ctx.onstack[key] = true
        inner = SchemaTable.arr
        ctx.node_blobs[node_index] = inner
        stack << [collect_table_edges(child, ctx, inner, where), 0, key]
      end
    end

    def self.collect_table_edges(value, ctx, blobs, path)
      out = SchemaTable.arr
      desc = obj_class(value).descriptor
      desc.fields.each do |field|
        next unless field.guard_holds?(value)

        cur = field.get(value)
        case field.codec
        when 'optional'
          next if cur.nil?

          if field.inner == 'map'
            collect_map_edges_into(field, cur, [path, field.name, NO_SUB], ctx, blobs, out)
          elsif field.inner == 'table'
            out << [cur, [path, field.name, NO_SUB], false, blobs]
          elsif field.inner == 'array' && field.elem && field.elem.codec == 'table'
            cur.each_with_index do |item, idx|
              out << [item, [path, field.name, idx], false, blobs]
            end
          end
        when 'string_blob', 'bytes_blob'
          next if cur.nil?

          out << [:blob, field, cur, join_path(desc.name, field.name), blobs]
        when 'pointer'
          out << [cur, [path, field.name, NO_SUB], true, blobs] unless cur.nil?
        when 'pointer_array'
          cur.each_with_index do |elem, idx|
            out << [elem, [path, field.name, idx], true, blobs] unless elem.nil?
          end
        when 'table'
          out << [cur, [path, field.name, NO_SUB], false, blobs] unless field.default?(cur)
        when 'array'
          if field.element && field.element.codec == 'table'
            cur.each_with_index do |item, idx|
              out << [item, [path, field.name, idx], false, blobs]
            end
          end
        when 'keyed'
          if field.elem && field.elem.codec == 'table'
            slots = field.keyed_list ? cur : cur._s_values
            slots.each_with_index do |item, idx|
              out << [item, [path, field.name, idx + 1], false, blobs]
            end
          end
        when 'union', 'union_array'
          collect_union_edges_into(field, cur, ctx, blobs, [path, field.name, NO_SUB], out)
        when 'map'
          collect_map_edges_into(field, cur, [path, field.name, NO_SUB], ctx, blobs, out)
        end
      end
      out
    end

    def self.collect_map_edges_into(field, value, path, ctx, blobs, out)
      entry_cls = by_name(field.target)
      value_field = entry_cls.descriptor.fields[1]
      sorted_map_keys(field, value).each do |k|
        cur = value[k]
        where = [path, '', k]
        case value_field.codec
        when 'string_blob', 'bytes_blob'
          out << [:blob, value_field, cur, render_edge_path(where), blobs] unless cur.nil?
        when 'pointer'
          out << [cur, where, true, blobs] unless cur.nil?
        when 'table'
          out << [cur, where, false, blobs] unless value_field.default?(cur)
        when 'array'
          if value_field.element && value_field.element.codec == 'table'
            cur.each_with_index { |elem, idx| out << [elem, [where, '', idx], false, blobs] }
          end
        when 'pointer_array'
          cur.each_with_index { |elem, idx| out << [elem, [where, '', idx], true, blobs] unless elem.nil? }
        when 'map'
          collect_map_edges_into(value_field, cur, where, ctx, blobs, out)
        when 'union', 'union_array'
          collect_union_edges_into(value_field, cur, ctx, blobs, where, out)
        end
      end
    end

    def self.collect_union_edges_into(field, value, ctx, blobs, path, out)
      if field.codec == 'union_array'
        value.each_with_index do |item, idx|
          collect_single_union_into(item, ctx, blobs, [path, '', idx], out)
        end
      else
        collect_single_union_into(value, ctx, blobs, path, out)
      end
    end

    def self.collect_single_union_into(value, ctx, blobs, path, out)
      return if value.type.zero?

      arm = obj_class(value).descriptor.arms[value.type - 1]
      return if arm.field.nil?

      collect_arm_into(arm.field, ivar_get(value, arm.ivar), ctx, blobs, [path, arm.name, NO_SUB], out)
    end

    def self.collect_arm_into(fd, value, ctx, blobs, path, out)
      case fd.codec
      when 'string_blob', 'bytes_blob'
        out << [:blob, fd, value, render_edge_path(path), blobs] unless value.nil?
      when 'pointer'
        out << [value, path, true, blobs] unless value.nil?
      when 'pointer_array'
        value.each_with_index { |elem, idx| out << [elem, [path, '', idx], true, blobs] unless elem.nil? }
      when 'table'
        out << [value, path, false, blobs]
      when 'union', 'union_array'
        collect_union_edges_into(fd, value, ctx, blobs, path, out)
      when 'map'
        collect_map_edges_into(fd, value, path, ctx, blobs, out)
      when 'array'
        if fd.element
          value.each_with_index { |elem, idx| collect_arm_into(fd.element, elem, ctx, blobs, [path, '', idx], out) }
        end
      when 'keyed'
        if fd.elem && fd.elem.codec == 'table'
          slots = fd.keyed_list ? value : value._s_values
          slots.each_with_index { |elem, idx| out << [elem, [path, '', idx + 1], false, blobs] }
        end
      end
    end

    # Body and field writers
    def self.write_table_body(writer, value, ctx, root)
      if ctx.is_a?(RetainSaveCtx)
        write_retain_table_body(writer, value, ctx, root)
        return
      end
      desc = obj_class(value).descriptor
      previous = ctx.current
      ctx.current = value
      desc.fields.each do |field|
        next unless field.guard_holds?(value)

        cur = field.get(value)
        next if !field.required && field.default?(cur)

        write_field_codec(writer, field, cur, ctx)
      end
      write_node_table(writer, ctx) if root && !ctx.order.empty?
      writer.byte(0)
      ctx.current = previous
    end

    def self.write_node_table(writer, ctx)
      writer.varint(ctx.idref(NODE_WIRE_ID))
      writer.byte(12)
      payload = String.new(encoding: Encoding::ASCII_8BIT)
      nodes = Writer.new(payload)
      nodes.varint(ctx.order.length)
      ctx.order.each do |node|
        if node.is_a?(BlobNode)
          type_id = node.string ? STRING_WIRE_TYPE_ID : BYTES_WIRE_TYPE_ID
          nodes.varint(ctx.idref(type_id))
          nodes.varint(node.body.bytesize)
          nodes.raw(node.body)
        else
          nodes.varint(ctx.idref(obj_class(node).descriptor.type_id))
          body = String.new(encoding: Encoding::ASCII_8BIT)
          write_table_body(Writer.new(body), node, ctx, false)
          nodes.varint(body.bytesize)
          nodes.raw(body)
        end
      end
      writer.varint(payload.bytesize)
      writer.raw(payload)
    end

    def self.write_retain_table_body(writer, value, ctx, root)
      desc = obj_class(value).descriptor
      previous = ctx.current
      parent_steps = ctx.steps
      parent_ordinal = ctx.ordinal
      ctx.current = value
      desc.fields.each do |field|
        next unless field.guard_holds?(value)

        cur = field.get(value)
        codec = field.codec == 'optional' ? field.inner : field.codec
        child = parent_steps + [[field.ordinal, 0]]
        has_child = codec == 'table' && !cur.nil? && ctx.retain.body?(ctx.node, child)
        element = field.codec == 'optional' ? field.elem : field.element
        if codec == 'array' && !cur.nil? && element && element.codec == 'table'
          has_child = (0...cur.length).any? do |idx|
            ctx.retain.body?(ctx.node, parent_steps + [[field.ordinal, idx]])
          end
        end
        if codec == 'keyed' && field.elem && field.elem.codec == 'table'
          slots = field.keyed_list ? cur : cur._s_values
          has_child = (0...slots.length).any? do |idx|
            ctx.retain.body?(ctx.node, parent_steps + [[field.ordinal, idx]])
          end
        end
        is_def = !field.required && field.default?(cur)
        has_child = false if ctx.preview
        next if is_def && !has_child

        forced = is_def && has_child
        next if forced && !ctx.can_id?(field.field_id)

        start = writer.count
        id_mark = ctx.ids.length
        retained_mark = ctx.retain.id_used
        emitted_mark = ctx.emitted
        ctx.ordinal = field.ordinal
        ctx.steps = child if codec == 'table'
        write_field_codec(writer, field, cur, ctx)
        ctx.steps = parent_steps
        ctx.ordinal = parent_ordinal
        next unless forced && ctx.emitted == emitted_mark

        ctx.forget_id(ctx.ids.pop) while ctx.ids.length > id_mark
        ctx.retain.truncate_ids(retained_mark)
        writer.truncate(start)
      end
      unless ctx.preview
        tail_start = writer.count
        ctx.lost += ctx.retain.write_tail(ctx.node, parent_steps, ctx.known, ctx, writer)
        ctx.emitted += 1 if writer.count != tail_start
      end
      if root && !ctx.order.empty?
        writer.varint(ctx.idref(NODE_WIRE_ID))
        writer.byte(12)
        nodes_frame = writer.begin_frame
        writer.varint(ctx.order.length)
        ctx.order.each_with_index do |node, idx|
          if node.is_a?(BlobNode)
            type_id = node.string ? STRING_WIRE_TYPE_ID : BYTES_WIRE_TYPE_ID
            writer.varint(ctx.idref(type_id))
            writer.varint(node.body.bytesize)
            writer.raw(node.body)
          else
            writer.varint(ctx.idref(obj_class(node).descriptor.type_id))
            node_frame = writer.begin_frame
            ctx.node = idx + 2
            ctx.steps = SchemaTable::EMPTY_ARR
            write_retain_table_body(writer, node, ctx, false)
            writer.finish_frame(node_frame)
          end
        end
        ctx.node = 1
        ctx.steps = parent_steps
        writer.finish_frame(nodes_frame)
      end
      writer.byte(0)
      ctx.current = previous
    end

    def self.write_field_codec(writer, fd, value, ctx)
      case fd.codec
      when 'bool'
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        writer.byte(value ? 1 : 0)
      when 'int'
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        write_int_element(writer, fd, value)
      when 'float32', 'float64'
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        write_float_element(writer, fd, value)
      when 'string'
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        write_string_element(writer, value)
      when 'bytes'
        write_bytes_field(writer, fd, value, ctx)
      when 'enum'
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        write_enum_element(writer, fd, value, ctx)
      when 'table'
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        write_table_element(writer, value, ctx)
      when 'pointer'
        idx = value.nil? ? 0 : ctx.indices[obj_id(value)]
        raise InvalidValue.new(fd.name, 'the pointer target has no node index') if idx.nil?

        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        writer.varint(idx)
      when 'pointer_array'
        write_pointer_array_field(writer, fd, value, ctx)
      when 'string_blob', 'bytes_blob'
        if value.nil?
          writer.varint(ctx.idref(fd.field_id))
          writer.byte(fd.kind)
          writer.varint(0)
        else
          if ctx.blob_next >= ctx.blob_slots.length
            raise InvalidValue.new(fd.name,
                                   'the byte buffer slot has no node index')
          end

          idx = ctx.blob_slots[ctx.blob_next]
          ctx.blob_next += 1
          writer.varint(ctx.idref(fd.field_id))
          writer.byte(fd.kind)
          writer.varint(idx)
        end
      when 'optional'
        write_optional_field(writer, fd, value, ctx)
      when 'array'
        write_array_field(writer, fd, value, ctx)
      when 'keyed'
        write_keyed_field(writer, fd, value, ctx)
      when 'map'
        write_map_field(writer, fd, value, ctx)
      when 'union'
        write_union_field(writer, fd, value, ctx)
      when 'union_array'
        write_union_array_field(writer, fd, value, ctx)
      else
        raise InvalidValue.new(fd.name, "unsupported codec #{fd.codec}")
      end
    end

    def self.write_element_codec(writer, fd, value, ctx)
      case fd.codec
      when 'bool'
        writer.byte(value ? 1 : 0)
      when 'int'
        write_int_element(writer, fd, value)
      when 'float32', 'float64'
        write_float_element(writer, fd, value)
      when 'string'
        write_string_element(writer, value)
      when 'enum'
        write_enum_element(writer, fd, value, ctx)
      when 'table'
        write_table_element(writer, value, ctx)
      when 'text_optional'
        rec_cls = by_name(fd.target)
        rec = rec_cls.new
        rec_cls.descriptor.fields[0].set(rec, value)
        write_table_element(writer, rec, ctx)
      else
        raise InvalidValue.new(fd.name, "unsupported element codec #{fd.codec}")
      end
    end

    def self.write_int_element(writer, fd, value)
      width = FIXED_WIDTH[fd.kind]
      writer.raw(pack_int_le(value, width, fd.lo.negative?))
    end

    def self.write_float_element(writer, fd, value)
      v = fd.float_range && value.zero? ? 0.0 : value
      if fd.kind == 10
        writer.raw([float32_round(v)].pack('e'))
      else
        writer.raw([v].pack('E'))
      end
    end

    def self.write_string_element(writer, value)
      raw = value.encoding == Encoding::UTF_8 ? value.b : value.encode(Encoding::UTF_8).b
      writer.varint(raw.bytesize)
      writer.raw(raw)
    end

    def self.write_bytes_field(writer, fd, value, ctx)
      inner = String.new(encoding: Encoding::ASCII_8BIT)
      bw = Writer.new(inner)
      bw.byte(6)
      bw.varint(value.bytesize)
      bw.raw(value)
      writer.varint(ctx.idref(fd.field_id))
      writer.byte(fd.kind)
      writer.varint(inner.bytesize)
      writer.raw(inner)
    end

    def self.write_enum_element(writer, fd, value, ctx)
      if value.zero?
        writer.varint(0)
      else
        writer.varint(ctx.idref(fd.wire_of[value]))
      end
    end

    def self.write_table_element(writer, value, ctx)
      if ctx.is_a?(RetainSaveCtx)
        frame = writer.begin_frame
        write_table_body(writer, value, ctx, false)
        writer.finish_frame(frame)
      else
        body = String.new(encoding: Encoding::ASCII_8BIT)
        write_table_body(Writer.new(body), value, ctx, false)
        writer.varint(body.bytesize)
        writer.raw(body)
      end
    end

    def self.write_pointer_array_field(writer, fd, value, ctx)
      payload = String.new(encoding: Encoding::ASCII_8BIT)
      pw = Writer.new(payload)
      pw.byte(17)
      pw.varint(value.length)
      value.each_with_index do |elem, idx|
        if elem.nil?
          pw.byte(0)
        else
          node_idx = ctx.indices[obj_id(elem)]
          raise InvalidValue.new("#{fd.name}[#{idx}]", 'the pointer target has no node index') if node_idx.nil?

          pw.varint(node_idx)
        end
      end
      writer.varint(ctx.idref(fd.field_id))
      writer.byte(fd.kind)
      writer.varint(payload.bytesize)
      writer.raw(payload)
    end

    def self.write_optional_field(writer, fd, value, ctx)
      if fd.inner == 'array'
        elem = fd.elem
        writer.varint(ctx.idref(fd.field_id))
        writer.byte(fd.kind)
        if ctx.is_a?(RetainSaveCtx)
          frame = writer.begin_frame
          writer.byte(elem.kind)
          writer.varint(value.length)
          if elem.codec == 'table'
            parent = ctx.steps
            ordinal = ctx.ordinal
            value.each_with_index do |item, idx|
              ctx.steps = parent + [[ordinal, idx]]
              write_element_codec(writer, elem, item, ctx)
            end
            ctx.steps = parent
          else
            value.each { |item| write_element_codec(writer, elem, item, ctx) }
          end
          writer.finish_frame(frame)
        else
          body = String.new(encoding: Encoding::ASCII_8BIT)
          bw = Writer.new(body)
          bw.byte(elem.kind)
          bw.varint(value.length)
          value.each { |item| write_element_codec(bw, elem, item, ctx) }
          writer.varint(body.bytesize)
          writer.raw(body)
        end
        return
      end

      inner_fd = FieldDesc.new(
        name: fd.name, field_id: fd.field_id, kind: fd.kind, codec: fd.inner,
        lo: fd.lo, hi: fd.hi, f32: fd.f32, float_range: fd.float_range,
        fmin: fd.fmin, fmax: fd.fmax, target: fd.target, size: fd.size,
        unbounded: fd.unbounded, map_entry: fd.map_entry, bound: fd.bound, ordinal: fd.ordinal,
        variant_ids: fd.variant_ids, deprecated: fd.deprecated, default: fd.default_value
      )
      write_field_codec(writer, inner_fd, value, ctx)
    end

    def self.write_array_bulk(writer, element, value)
      if element.codec == 'bool'
        out = String.new(capacity: value.length, encoding: Encoding::ASCII_8BIT)
        value.each { |v| out << (v ? 1 : 0) }
        writer.raw(out)
        return true
      end
      return false unless element.codec == 'int'

      width = FIXED_WIDTH[element.kind]
      return false if width.nil? || width > 8

      directive = case width
                  when 1 then element.lo.negative? ? 'c*' : 'C*'
                  when 2 then element.lo.negative? ? 's<*' : 'S<*'
                  when 4 then element.lo.negative? ? 'l<*' : 'L<*'
                  when 8 then element.lo.negative? ? 'q<*' : 'Q<*'
                  end
      writer.raw(value.pack(directive))
      true
    end

    def self.write_array_field(writer, fd, value, ctx)
      element = fd.element
      writer.varint(ctx.idref(fd.field_id))
      writer.byte(fd.kind)
      if ctx.is_a?(RetainSaveCtx)
        frame = writer.begin_frame
        writer.byte(fd.elem_kind)
        writer.varint(value.length)
        unless write_array_bulk(writer, element, value)
          if element.codec == 'table'
            parent = ctx.steps
            ordinal = ctx.ordinal
            value.each_with_index do |item, idx|
              ctx.steps = parent + [[ordinal, idx]]
              write_element_codec(writer, element, item, ctx)
            end
            ctx.steps = parent
          else
            value.each { |item| write_element_codec(writer, element, item, ctx) }
          end
        end
        writer.finish_frame(frame)
      else
        body = String.new(encoding: Encoding::ASCII_8BIT)
        bw = Writer.new(body)
        bw.byte(fd.elem_kind)
        bw.varint(value.length)
        value.each { |item| write_element_codec(bw, element, item, ctx) } unless write_array_bulk(bw, element, value)
        writer.varint(body.bytesize)
        writer.raw(body)
      end
    end

    def self.rollback_ids(ctx, mark)
      (mark...ctx.ids.length).each do |idx|
        tid = ctx.ids[idx]
        if ctx.is_a?(RetainSaveCtx)
          ctx.forget_id(tid)
        else
          ctx.refs.delete(tid)
        end
      end
      ctx.ids.slice!(mark, ctx.ids.length - mark)
    end

    def self.write_keyed_field(writer, fd, value, ctx)
      elem = fd.elem
      values = fd.keyed_list ? value : value._s_values
      if ctx.is_a?(RetainSaveCtx)
        write_retain_keyed_field(writer, fd, elem, values, ctx)
        return
      end
      mark = ctx.ids.length
      ref = ctx.idref(fd.field_id)
      slots = String.new(encoding: Encoding::ASCII_8BIT)
      sw = Writer.new(slots)
      present = 0
      fd.keys.each_with_index do |key_wid, slot|
        cur = values[slot]
        slot_mark = ctx.ids.length
        key_ref = ctx.idref(key_wid)
        elem_buf = String.new(encoding: Encoding::ASCII_8BIT)
        if elem.codec == 'table'
          write_table_body(Writer.new(elem_buf), cur, ctx, false)
          if elem_buf.bytesize <= 1
            rollback_ids(ctx, slot_mark)
            next
          end
        else
          if elem.default?(cur)
            rollback_ids(ctx, slot_mark)
            next
          end
          write_element_codec(Writer.new(elem_buf), elem, cur, ctx)
        end
        sw.varint(key_ref)
        sw.varint(elem_buf.bytesize)
        sw.raw(elem_buf)
        present += 1
      end
      if present.zero? && !fd.required
        rollback_ids(ctx, mark)
        return
      end
      inner = String.new(encoding: Encoding::ASCII_8BIT)
      iw = Writer.new(inner)
      iw.byte(elem.kind)
      iw.varint(present)
      iw.raw(slots)
      writer.varint(ref)
      writer.byte(fd.kind)
      writer.varint(inner.bytesize)
      writer.raw(inner)
    end

    def self.write_retain_keyed_field(writer, fd, elem, values, ctx)
      start = writer.count
      ids_start = ctx.ids.length
      writer.varint(ctx.idref(fd.field_id))
      writer.byte(fd.kind)
      frame = writer.begin_frame
      writer.byte(elem.kind)
      count_mark = writer.begin_frame
      present = 0
      fd.keys.each_with_index do |key_wid, slot|
        cur = values[slot]
        if elem.default?(cur)
          next if elem.codec != 'table' || ctx.preview

          child = ctx.steps + [[fd.ordinal, slot]]
          next if !ctx.retain.body?(ctx.node, child) || !ctx.can_id?(key_wid)
        end
        start_slot = writer.count
        ids_slot = ctx.ids.length
        writer.varint(ctx.idref(key_wid))
        elem_frame = writer.begin_frame
        if elem.codec == 'table'
          parent = ctx.steps
          ctx.steps = parent + [[fd.ordinal, slot]]
          write_table_body(writer, cur, ctx, false)
          ctx.steps = parent
          if writer.count == elem_frame + 11
            rollback_ids(ctx, ids_slot)
            writer.truncate(start_slot)
            next
          end
        else
          write_element_codec(writer, elem, cur, ctx)
        end
        writer.finish_frame(elem_frame)
        present += 1
      end
      if present.zero? && !fd.required
        rollback_ids(ctx, ids_start)
        writer.truncate(start)
        return
      end
      writer.finish_varint(count_mark, present)
      writer.finish_frame(frame)
    end

    def self.write_map_field(writer, fd, value, ctx)
      entry_cls = by_name(fd.target)
      key_field = entry_cls.descriptor.fields[0]
      value_field = entry_cls.descriptor.fields[1]
      field_ref = ctx.idref(fd.field_id)
      keys = sorted_map_keys(fd, value)
      if ctx.is_a?(RetainSaveCtx)
        writer.varint(field_ref)
        writer.byte(fd.kind)
        frame = writer.begin_frame
        writer.byte(13)
        writer.varint(keys.length)
        keys.each_with_index do |k, idx|
          item = value[k]
          entry_frame = writer.begin_frame
          write_field_codec(writer, key_field, k, ctx) unless key_field.default?(k)
          parent = ctx.steps
          prev_ordinal = ctx.ordinal
          entry_path = parent + [[fd.ordinal, idx]]
          value_path = entry_path + [[value_field.ordinal, 0]]
          has_child = !ctx.preview && value_field.codec == 'table' && ctx.retain.body?(ctx.node, value_path)
          is_def = value_field.default?(item)
          forced = is_def && has_child
          if (!is_def || has_child) && (!forced || ctx.can_id?(value_field.field_id))
            start = writer.count
            id_mark = ctx.ids.length
            retained_mark = ctx.retain.id_used
            emitted_mark = ctx.emitted
            ctx.ordinal = value_field.ordinal
            ctx.steps = value_field.codec == 'table' ? value_path : entry_path
            write_field_codec(writer, value_field, item, ctx)
            if forced && ctx.emitted == emitted_mark
              ctx.forget_id(ctx.ids.pop) while ctx.ids.length > id_mark
              ctx.retain.truncate_ids(retained_mark)
              writer.truncate(start)
            end
          end
          ctx.steps = parent
          ctx.ordinal = prev_ordinal
          ctx.lost += ctx.retain.write_tail(ctx.node, entry_path, ctx.known, ctx, writer) unless ctx.preview
          writer.byte(0)
          writer.finish_frame(entry_frame)
        end
        writer.finish_frame(frame)
      else
        body = String.new(encoding: Encoding::ASCII_8BIT)
        bw = Writer.new(body)
        bw.byte(13)
        bw.varint(keys.length)
        keys.each do |k|
          item = value[k]
          entry_body = String.new(encoding: Encoding::ASCII_8BIT)
          ew = Writer.new(entry_body)
          write_field_codec(ew, key_field, k, ctx) unless key_field.default?(k)
          write_field_codec(ew, value_field, item, ctx) unless value_field.default?(item)
          ew.byte(0)
          bw.varint(entry_body.bytesize)
          bw.raw(entry_body)
        end
        writer.varint(field_ref)
        writer.byte(fd.kind)
        writer.varint(body.bytesize)
        writer.raw(body)
      end
    end

    def self.header_end_offset(raw)
      idx = 0
      loop do
        b = raw.getbyte(idx)
        idx += 1
        return idx + 1 if b.nobits?(0x80)
      end
    end

    def self.write_arm_header(writer, arm, value, ctx)
      if arm.field.nil?
        writer.varint(ctx.idref(arm.arm_id))
        writer.byte(arm.kind)
        writer.varint(0)
        return
      end
      f = arm.field
      if ctx.is_a?(RetainSaveCtx) && f.codec == 'union'
        writer.varint(ctx.idref(arm.arm_id))
        writer.byte(f.kind)
        frame = writer.begin_frame
        write_union_element(writer, union_by_name(f.target), value, ctx)
        writer.finish_frame(frame)
        return
      end
      if f.kind == 17 && value.nil?
        writer.varint(ctx.idref(arm.arm_id))
        writer.byte(arm.kind)
        writer.varint(1)
        writer.byte(0)
        return
      end
      if LENGTH_KINDS[f.kind]
        write_field_codec(writer, f, value, ctx)
        return
      end
      ctx.idref(arm.arm_id)
      body = String.new(encoding: Encoding::ASCII_8BIT)
      write_field_codec(Writer.new(body), f, value, ctx)
      hend = header_end_offset(body)
      payload = u(body.byteslice(hend, body.bytesize - hend))
      writer.raw(body.byteslice(0, hend))
      writer.varint(payload.bytesize)
      writer.raw(payload)
    end

    def self.write_union_element(writer, un_cls, value, ctx)
      if value.type.zero?
        writer.varint(0)
        return
      end
      arm = un_cls.descriptor.arms[value.type - 1]
      payload = arm.field.nil? ? nil : ivar_get(value, arm.ivar)
      if ctx.is_a?(RetainSaveCtx)
        parent = ctx.steps
        ctx.steps = parent + [[ctx.ordinal, arm.tag]]
        write_arm_header(writer, arm, payload, ctx)
        ctx.steps = parent
      else
        write_arm_header(writer, arm, payload, ctx)
      end
    end

    def self.write_union_field(writer, fd, value, ctx)
      writer.varint(ctx.idref(fd.field_id))
      writer.byte(fd.kind)
      un_cls = union_by_name(fd.target)
      write_union_element(writer, un_cls, value, ctx)
    end

    def self.write_union_array_field(writer, fd, value, ctx)
      ref = ctx.idref(fd.field_id)
      un_cls = union_by_name(fd.target)
      if ctx.is_a?(RetainSaveCtx)
        writer.varint(ref)
        writer.byte(fd.kind)
        frame = writer.begin_frame
        writer.byte(15)
        writer.varint(value.length)
        parent = ctx.steps
        ordinal = ctx.ordinal
        value.each_with_index do |elem, idx|
          ctx.steps = parent + [[ordinal, idx]]
          write_union_element(writer, un_cls, elem, ctx)
        end
        ctx.steps = parent
        writer.finish_frame(frame)
      else
        body = String.new(encoding: Encoding::ASCII_8BIT)
        bw = Writer.new(body)
        bw.byte(15)
        bw.varint(value.length)
        value.each { |elem| write_union_element(bw, un_cls, elem, ctx) }
        writer.varint(ref)
        writer.byte(fd.kind)
        writer.varint(body.bytesize)
        writer.raw(body)
      end
    end
  end
end
