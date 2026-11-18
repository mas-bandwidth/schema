# frozen_string_literal: true

module SchemaRuntimeUnit
  # Caller-owned bounded buffers for form-1 unknown-field retention (SPEC-TABLES §6.6).
  class TableRetain
    attr_reader :storage, :ids
    attr_accessor :used, :id_used, :root, :directory, :loaded_root, :id_root

    def initialize(storage = :_s_default, ids = :_s_default)
      if storage.equal?(:_s_default) && ids.equal?(:_s_default)
        storage = "\x00".b * 65_536
        ids = "\x00".b * (256 * 24)
      end
      unless storage.instance_of?(String) && storage.encoding == Encoding::ASCII_8BIT && !storage.frozen? &&
             ids.instance_of?(String) && ids.encoding == Encoding::ASCII_8BIT && !ids.frozen? &&
             (ids.bytesize % 24).zero?
        raise TypeError, 'retention needs mutable ASCII-8BIT Strings and 24-byte id slots'
      end

      @storage = storage
      @ids = ids
      reset
    end

    def reset
      @used = 0
      @id_used = 0
      @root = 0
      @directory = SchemaTable.arr
      @loaded_root = nil
      @id_root = 0
    end

    def word(at)
      SchemaTable.unpack_int_le(@storage, at, 4, false)
    end

    def set_word(at, val)
      @storage[at, 4] = [val].pack('V')
    end

    def key_end(record)
      record + 21 + (8 * word(record + 17))
    end

    def bit_at(record, bit)
      at = record + 13 + (bit >> 3)
      return 0 if at >= key_end(record)

      (@storage.getbyte(at) >> (bit & 7)) & 1
    end

    def first_difference(left, right)
      a = left + 13
      b = right + 13
      a_end = key_end(left)
      b_end = key_end(right)
      limit = [a_end - a, b_end - b].min
      limit.times do |offset|
        changed = @storage.getbyte(a + offset) ^ @storage.getbyte(b + offset)
        return (offset * 8) + (changed & -changed).bit_length - 1 unless changed.zero?
      end
      raise ArgumentError, 'invalid retained path key' if a_end - a != b_end - b

      nil
    end

    def leaf_for(record)
      pointer = @root
      while pointer.positive?
        at = pointer - 1
        tag = @storage.getbyte(at + 4)
        return at if tag == 2
        raise ArgumentError, 'corrupt retained path index' if tag != 3

        bit = word(at + 5)
        side = bit_at(record, bit)
        pointer = word(at + 9 + (side * 4))
      end
      -1
    end

    def id_word(node, offset)
      at = ((node - 1) * 24) + offset
      SchemaTable.unpack_int_le(@ids, at, 4, false)
    end

    def id_set(node, offset, val)
      at = ((node - 1) * 24) + offset
      @ids[at, 4] = [val].pack('V')
    end

    def id_key(node)
      at = (node - 1) * 24
      SchemaTable.unpack_int_le(@ids, at, 8, false)
    end

    def id_height(node)
      node.positive? ? id_word(node, 20) : 0
    end

    def id_fix(node)
      h = 1 + [id_height(id_word(node, 12)), id_height(id_word(node, 16))].max
      id_set(node, 20, h)
    end

    def id_rotate(node, left)
      a, b = left ? [16, 12] : [12, 16]
      child = id_word(node, a)
      id_set(node, a, id_word(child, b))
      id_set(child, b, node)
      id_fix(node)
      id_fix(child)
      child
    end

    def id_add(root_n, node)
      return node if root_n.zero?

      offset = id_key(node) < id_key(root_n) ? 12 : 16
      id_set(root_n, offset, id_add(id_word(root_n, offset), node))
      id_fix(root_n)
      left = id_word(root_n, 12)
      right = id_word(root_n, 16)
      bal = id_height(left) - id_height(right)
      if bal > 1
        id_set(root_n, 12, id_rotate(left, true)) if id_key(node) > id_key(left)
        return id_rotate(root_n, false)
      end
      if bal < -1
        id_set(root_n, 16, id_rotate(right, false)) if id_key(node) < id_key(right)
        return id_rotate(root_n, true)
      end
      root_n
    end

    def retained_id(identity)
      node = @id_root
      while node.positive?
        key = id_key(node)
        return id_word(node, 8) if key == identity

        node = id_word(node, identity < key ? 12 : 16)
      end
      0
    end

    def add_retained_id(identity, slot)
      raise SchemaTable::RetainCapacity if @id_used >= @ids.bytesize / 24

      node = @id_used + 1
      at = (node - 1) * 24
      @ids[at, 8] = [identity].pack('Q<')
      id_set(node, 8, slot)
      id_set(node, 12, 0)
      id_set(node, 16, 0)
      id_set(node, 20, 1)
      @id_root = id_add(@id_root, node)
      @id_used = node
    end

    def id_balance(root_n)
      id_fix(root_n)
      left = id_word(root_n, 12)
      right = id_word(root_n, 16)
      delta = id_height(left) - id_height(right)
      if delta > 1
        id_set(root_n, 12, id_rotate(left, true)) if id_height(id_word(left, 16)) > id_height(id_word(left, 12))
        return id_rotate(root_n, false)
      end
      if delta < -1
        id_set(root_n, 16, id_rotate(right, false)) if id_height(id_word(right, 12)) > id_height(id_word(right, 16))
        return id_rotate(root_n, true)
      end
      root_n
    end

    def id_pop_min(root_n)
      left = id_word(root_n, 12)
      return [id_word(root_n, 16), root_n] if left.zero?

      rem, min_n = id_pop_min(left)
      id_set(root_n, 12, rem)
      [id_balance(root_n), min_n]
    end

    def id_remove(root_n, identity)
      raise ArgumentError, 'retained id index is corrupt' if root_n.zero?

      key = id_key(root_n)
      if key != identity
        offset = identity < key ? 12 : 16
        id_set(root_n, offset, id_remove(id_word(root_n, offset), identity))
        return id_balance(root_n)
      end
      left = id_word(root_n, 12)
      right = id_word(root_n, 16)
      return right if left.zero?
      return left if right.zero?

      rem, repl = id_pop_min(right)
      id_set(repl, 12, left)
      id_set(repl, 16, rem)
      id_balance(repl)
    end

    def truncate_ids(count)
      while @id_used > count
        @id_root = id_remove(@id_root, id_key(@id_used))
        @id_used -= 1
      end
    end

    def capture(node, steps, identity, kind, payload, ids)
      if node < 1 || node > 0xFFFFFFFF || steps.length > SchemaTable::MAX_DEPTH ||
         SchemaTable::RETAIN_RESERVED[identity]
        raise ArgumentError, 'invalid retained field path or id'
      end

      start = @used
      sink = SchemaTable::RetainSink.new(@storage, start)
      begin
        sink.u32(0)
        sink.byte(1)
        sink.u32(0)
        sink.u32(node)
        sink.u32(node)
        sink.u32(steps.length)
        steps.each do |ordinal, idx|
          raise ArgumentError, 'negative retained path component' if ordinal.negative? || idx.negative?

          sink.u32(ordinal)
          sink.u32(idx)
        end
        sink.raw([identity].pack('Q<'))
        sink.byte(kind)
        length_at = sink.pos
        sink.u32(0)
        body_at = sink.pos
        end_pos = SchemaTable.retain_resolve_payload(kind, payload, ids, @storage, body_at)
        sink.pos = end_pos
        leaf = leaf_for(start)
        diff = leaf.negative? ? nil : first_difference(start, word(leaf + 5) - 1)
        new_leaf = -1
        new_branch = -1
        if leaf.negative? || !diff.nil?
          new_leaf = sink.pos
          sink.u32(13)
          sink.byte(2)
          sink.u32(start + 1)
          sink.u32(start + 1)
          sink.u32(0)
          unless diff.nil?
            new_branch = sink.pos
            sink.u32(13)
            sink.byte(3)
            sink.u32(diff)
            sink.u32(0)
            sink.u32(0)
          end
        end
      rescue SchemaTable::RetainCapacity, SchemaTable::RetainExcluded
        return false
      end
      set_word(length_at, end_pos - body_at)
      set_word(start, end_pos - start - 4)
      if leaf.negative?
        @root = new_leaf + 1
      elsif diff.nil?
        last = word(leaf + 9) - 1
        set_word(last + 5, start + 1)
        set_word(leaf + 9, start + 1)
      else
        pointer = @root
        parent = -1
        parent_side = 0
        while pointer.positive?
          at = pointer - 1
          break if @storage.getbyte(at + 4) != 3 || word(at + 5) >= diff

          parent = at
          parent_side = bit_at(start, word(at + 5))
          pointer = word(at + 9 + (parent_side * 4))
        end
        side = bit_at(start, diff)
        set_word(new_branch + 9 + (side * 4), new_leaf + 1)
        set_word(new_branch + 9 + ((1 - side) * 4), pointer)
        if parent.negative?
          @root = new_branch + 1
        else
          set_word(parent + 9 + (parent_side * 4), new_branch + 1)
        end
      end
      @used = sink.pos
      true
    end

    def query_bit(node, steps, bit)
      w = bit >> 5
      val = if w.zero?
              node
            elsif w == 1
              steps.length
            else
              pair = (w - 2) >> 1
              pair >= steps.length ? 0 : steps[pair][(w - 2) & 1]
            end
      (val >> (bit & 31)) & 1
    end

    def each_record_at(node, steps, &)
      pointer = @root
      while pointer.positive?
        at = pointer - 1
        tag = @storage.getbyte(at + 4)
        break if tag == 2
        raise ArgumentError, 'corrupt retained path index' if tag != 3

        bit = word(at + 5)
        side = query_bit(node, steps, bit)
        pointer = word(at + 9 + (side * 4))
      end
      return if pointer.zero?

      leaf = pointer - 1
      pointer = word(leaf + 5)
      record = pointer - 1
      return if word(record + 13) != node || word(record + 17) != steps.length

      cursor = record + 21
      matched = true
      steps.each do |ordinal, idx|
        if word(cursor) != ordinal || word(cursor + 4) != idx
          matched = false
          break
        end
        cursor += 8
      end
      return unless matched

      while pointer.positive?
        record = pointer - 1
        end_pos = record + 4 + word(record)
        cursor = key_end(record)
        identity = SchemaTable.unpack_int_le(@storage, cursor, 8, false)
        kind = @storage.getbyte(cursor + 8)
        payload_size = word(cursor + 9)
        cursor += 13
        raise ArgumentError, 'corrupt retained payload length' if end_pos > @used || cursor + payload_size != end_pos

        yield identity, kind, cursor, end_pos, record
        pointer = word(record + 5)
      end
    end

    def body?(node, steps)
      (steps.length..SchemaTable::MAX_DEPTH).each do |depth|
        pointer = @root
        while pointer.positive?
          at = pointer - 1
          tag = @storage.getbyte(at + 4)
          break if tag == 2
          raise ArgumentError, 'corrupt retained path index' if tag != 3

          bit = word(at + 5)
          w = bit >> 5
          val = if w.zero?
                  node
                elsif w == 1
                  depth
                else
                  pair = (w - 2) >> 1
                  pair < steps.length ? steps[pair][(w - 2) & 1] : 0
                end
          pointer = word(at + 9 + (((val >> (bit & 31)) & 1) * 4))
        end
        next if pointer.zero?

        record = word(pointer - 1 + 5) - 1
        next if word(record + 13) != node || word(record + 17) != depth

        cursor = record + 21
        matches = true
        steps.each do |ordinal, idx|
          if word(cursor) != ordinal || word(cursor + 4) != idx
            matches = false
            break
          end
          cursor += 8
        end
        return true if matches
      end
      false
    end

    def begin_save(ctx = nil, directory = nil)
      @id_used = 0
      @id_root = 0
      at = 0
      free = 0
      raise ArgumentError, 'node remapping needs the loaded directory' if !ctx.nil? && directory.nil?

      while at < @used
        len = word(at)
        end_pos = at + 4 + len
        raise ArgumentError, 'corrupt retained record' if end_pos > @used || len < 1

        tag = @storage.getbyte(at + 4) & 0x7F
        @storage.setbyte(at + 4, tag)
        unless ctx.nil?
          if [2, 3].include?(tag)
            @storage.setbyte(at + 4, 2)
            set_word(at + 5, free)
            free = at + 1
          elsif tag == 1
            set_word(at + 5, 0)
            original = word(at + 9)
            mapped = 1
            if original > 1
              idx = original - 2
              obj = idx < directory.length ? directory[idx] : nil
              mapped = obj.nil? ? 0 : (ctx.indices[SchemaTable.obj_id(obj)] || 0)
            end
            set_word(at + 13, mapped)
          else
            raise ArgumentError, 'corrupt retained index entry'
          end
        end
        at = SchemaTable.u(end_pos)
      end
      return if ctx.nil?

      @root = 0
      at = 0
      while at < @used
        end_pos = at + 4 + word(at)
        if @storage.getbyte(at + 4) == 1
          leaf = leaf_for(at)
          diff = leaf.negative? ? nil : first_difference(at, word(leaf + 5) - 1)
          if leaf >= 0 && diff.nil?
            last = word(leaf + 9) - 1
            set_word(last + 5, at + 1)
            set_word(leaf + 9, at + 1)
          else
            raise ArgumentError, 'retained index has too few nodes' if free.zero?

            new_leaf = free - 1
            free = word(new_leaf + 5)
            @storage.setbyte(new_leaf + 4, 2)
            set_word(new_leaf + 5, at + 1)
            set_word(new_leaf + 9, at + 1)
            set_word(new_leaf + 13, 0)
            if leaf.negative?
              @root = new_leaf + 1
            else
              raise ArgumentError, 'retained index has too few branches' if free.zero? || diff.nil?

              branch = free - 1
              free = word(branch + 5)
              @storage.setbyte(branch + 4, 3)
              set_word(branch + 5, diff)
              pointer = @root
              parent = -1
              parent_side = 0
              while pointer.positive?
                cur = pointer - 1
                break if @storage.getbyte(cur + 4) != 3 || word(cur + 5) >= diff

                parent = cur
                parent_side = bit_at(at, word(cur + 5))
                pointer = word(cur + 9 + (parent_side * 4))
              end
              side = bit_at(at, diff)
              set_word(branch + 9 + (side * 4), new_leaf + 1)
              set_word(branch + 9 + ((1 - side) * 4), pointer)
              if parent.negative?
                @root = branch + 1
              else
                set_word(parent + 9 + (parent_side * 4), branch + 1)
              end
            end
          end
        end
        at = SchemaTable.u(end_pos)
      end
    end

    def write_tail(node, steps, known, ctx, writer)
      lost = 0
      each_record_at(node, steps) do |identity, kind, cursor, end_pos, record|
        lost += 1 unless SchemaTable.retain_emit_record(self, identity, kind, cursor, end_pos, known, ctx, writer)
        @storage.setbyte(record + 4, @storage.getbyte(record + 4) | 0x80)
      end
      lost
    end

    def unplaced
      count = 0
      at = 0
      while at < @used
        len = word(at)
        end_pos = at + 4 + len
        raise ArgumentError, 'corrupt retained record' if end_pos > @used || len < 1

        count += 1 if @storage.getbyte(at + 4) == 1
        at = SchemaTable.u(end_pos)
      end
      count
    end
  end

  # Form-1 unknown-field capture and replay helpers.
  module SchemaTable
    RETAIN_NULL_ID = 0xFFFFFFFFFFFFFFFF
    RETAIN_RESERVED = {
      0xFFFFFFFFFFFFFFFF => true,
      0xFFFFFFFFFFFFFFFE => true,
      0xFFFFFFFFFFFFFFFD => true
    }.freeze

    # Internal signal for an unknown field containing a node index.
    class RetainExcluded < StandardError; end

    # Internal signal when caller-owned retention buffers are full.
    class RetainCapacity < StandardError; end

    # Bounded in-place byte writer into a pre-sized ASCII-8BIT String.
    class RetainSink
      attr_reader :data
      attr_accessor :pos

      def initialize(data, pos)
        @data = data
        @pos = pos
      end

      def raw(bytes)
        end_pos = @pos + bytes.bytesize
        raise RetainCapacity if end_pos > @data.bytesize

        @data[@pos, bytes.bytesize] = bytes if bytes.bytesize.positive?
        @pos = end_pos
      end

      def byte(val)
        raise RetainCapacity if @pos >= @data.bytesize

        @data.setbyte(@pos, val & 0xFF)
        @pos += 1
      end

      def u32(val)
        raise RetainCapacity if val > 0xFFFFFFFF

        raw([val].pack('V'))
      end

      def varint(val)
        while val >= 128
          byte((val & 0x7F) | 0x80)
          val >>= 7
        end
        byte(val)
      end
    end

    # Counting sink for post-order measurement of resolved retained payloads.
    class RetainCountSink
      attr_accessor :pos

      def initialize
        @pos = 0
      end

      def byte(_val)
        @pos += 1
      end

      def raw(bytes)
        @pos += bytes.bytesize
      end

      def varint(val)
        @pos += [1, (val.bit_length + 6) / 7].max
      end
    end

    # Discard-only edges list for skip_payload during retain capture.
    class DiscardEdges
      def <<(_val)
        self
      end
    end

    def self.retain_in_ref(reader, sink, allow_zero)
      ref = reader.varint
      if ref.zero?
        raise Refused.new('wire_damaged', 'a retained reference is zero') unless allow_zero

        sink.raw([RETAIN_NULL_ID].pack('Q<'))
        return false
      end
      identity = reader.id_at(ref)
      raise Refused.new('wire_damaged', 'a retained reference names a reserved id') if RETAIN_RESERVED[identity]

      sink.raw([identity].pack('Q<'))
      true
    end

    def self.retain_in_framed(reader, sink, kind, depth)
      len = reader.varint
      data = reader.take(len)
      sub = Reader.new(data, 0, data.bytesize, reader.ids)
      at = sink.pos
      sink.u32(0)
      sink.u32(0)
      start = sink.pos
      retain_in_content(sub, sink, kind, depth + 1)
      raise Refused.new('wire_damaged', 'bytes left inside a retained frame') if sub.pos != sub.end_pos

      sink.data[at, 4] = [sink.pos - start].pack('V')
    end

    def self.retain_in_content(reader, sink, kind, depth)
      raise Refused.new('wire_damaged', 'a retained field exceeds the nesting bound') if depth > MAX_DEPTH

      case kind
      when 13
        while retain_in_ref(reader, sink, true)
          ck = reader.byte
          sink.byte(ck)
          retain_in_payload(reader, sink, ck, depth)
        end
      when 14, 16
        elem = reader.byte
        count = reader.varint
        raise RetainExcluded if kind == 14 && elem == 17

        sink.byte(elem)
        sink.varint(count)
        count.times do
          if kind == 16
            retain_in_ref(reader, sink, false)
            retain_in_framed(reader, sink, elem, depth)
          else
            retain_in_payload(reader, sink, elem, depth)
          end
        end
      when 15, 30
        retain_in_payload(reader, sink, kind, depth)
      when 17
        raise RetainExcluded
      else
        sink.raw(reader.take(reader.end_pos - reader.pos))
      end
    end

    def self.retain_in_payload(reader, sink, kind, depth)
      width = FIXED_WIDTH[kind]
      unless width.nil?
        sink.raw(reader.take(width))
        return
      end
      case kind
      when 17
        raise RetainExcluded
      when 30
        retain_in_ref(reader, sink, true)
      when 15
        if retain_in_ref(reader, sink, true)
          arm_kind = reader.byte
          sink.byte(arm_kind)
          retain_in_framed(reader, sink, arm_kind, depth)
        end
      when 13, 14, 16
        retain_in_framed(reader, sink, kind, depth)
      when 12, 31, 32
        len = reader.varint
        sink.varint(len)
        sink.raw(reader.take(len))
      else
        raise Refused.new('wire_damaged', 'a retained payload has an unknown kind')
      end
    end

    def self.retain_resolve_payload(kind, payload, ids, storage, start)
      raise ArgumentError, 'start is outside the retention buffer' if start.negative? || start > storage.bytesize

      reader = Reader.new(payload, 0, payload.bytesize, ids)
      sink = RetainSink.new(storage, start)
      retain_in_payload(reader, sink, kind, 0)
      raise Refused.new('wire_damaged', 'bytes left after a retained payload') if reader.pos != reader.end_pos

      sink.pos
    end

    def self.retain_out_ref(reader, sink, intern)
      identity = unpack_int_le(reader.take(8), 0, 8, false)
      sink.varint(identity == RETAIN_NULL_ID ? 0 : intern.call(identity))
      identity != RETAIN_NULL_ID
    end

    def self.retain_out_frame(storage, reader, sink, kind, depth, intern, measure)
      size = unpack_int_le(reader.take(4), 0, 4, false)
      length_at = reader.pos
      measured = unpack_int_le(reader.take(4), 0, 4, false)
      sub = Reader.new(storage, reader.pos, reader.pos + size, SchemaTable.arr)
      reader.pos += size
      if measure
        cnt = RetainCountSink.new
        retain_out_content(storage, sub, cnt, kind, depth + 1, intern, true)
        measured = cnt.pos
        raise RetainCapacity if measured > 0xFFFFFFFF

        storage[length_at, 4] = [measured].pack('V')
        sink.varint(measured)
        sink.pos += measured
      else
        sink.varint(measured)
        retain_out_content(storage, sub, sink, kind, depth + 1, intern, false)
      end
      raise Refused.new('wire_damaged', 'bytes left inside a resolved frame') if sub.pos != sub.end_pos
    end

    def self.retain_out_content(storage, reader, sink, kind, depth, intern, measure)
      raise Refused.new('wire_damaged', 'a retained field exceeds the nesting bound') if depth > MAX_DEPTH

      case kind
      when 13
        while retain_out_ref(reader, sink, intern)
          ck = reader.byte
          sink.byte(ck)
          retain_out_payload(storage, reader, sink, ck, depth, intern, measure)
        end
      when 14, 16
        elem = reader.byte
        count = reader.varint
        sink.byte(elem)
        sink.varint(count)
        count.times do
          if kind == 16
            retain_out_ref(reader, sink, intern)
            retain_out_frame(storage, reader, sink, elem, depth, intern, measure)
          else
            retain_out_payload(storage, reader, sink, elem, depth, intern, measure)
          end
        end
      when 15, 30
        retain_out_payload(storage, reader, sink, kind, depth, intern, measure)
      else
        sink.raw(reader.take(reader.end_pos - reader.pos))
      end
    end

    def self.retain_out_payload(storage, reader, sink, kind, depth, intern, measure)
      width = FIXED_WIDTH[kind]
      unless width.nil?
        sink.raw(reader.take(width))
        return
      end
      case kind
      when 30
        retain_out_ref(reader, sink, intern)
      when 15
        if retain_out_ref(reader, sink, intern)
          arm_kind = reader.byte
          sink.byte(arm_kind)
          retain_out_frame(storage, reader, sink, arm_kind, depth, intern, measure)
        end
      when 13, 14, 16
        retain_out_frame(storage, reader, sink, kind, depth, intern, measure)
      when 12, 31, 32
        len = reader.varint
        sink.varint(len)
        sink.raw(reader.take(len))
      else
        raise Refused.new('wire_damaged', 'a retained record has an unknown kind')
      end
    end

    def self.retain_emit_record(retain, identity, kind, start_pos, end_pos, known, ctx, writer)
      prev_ids = ctx.ids.length
      prev_retained = retain.id_used
      intern = lambda do |val|
        if known[val]
          existing = ctx.refs[val]
          return existing unless existing.nil?
          raise RetainCapacity if ctx.ids.length + retain.id_used + ctx.remaining >= ID_BOUND && !ctx.reserved[val]

          return ctx.idref(val)
        end
        existing = retain.retained_id(val)
        return existing if existing.positive?
        raise RetainCapacity if ctx.ids.length + retain.id_used + ctx.remaining >= ID_BOUND

        slot = ctx.ids.length + retain.id_used + 1
        retain.add_retained_id(val, slot)
        slot
      end

      begin
        field_ref = intern.call(identity)
        m_reader = Reader.new(retain.storage, start_pos, end_pos, SchemaTable.arr)
        m_sink = RetainCountSink.new
        retain_out_payload(retain.storage, m_reader, m_sink, kind, 0, intern, true)
        raise Refused.new('wire_damaged', 'bytes left after a resolved record') if m_reader.pos != m_reader.end_pos
      rescue RetainCapacity
        ctx.forget_id(ctx.ids.pop) while ctx.ids.length > prev_ids
        retain.truncate_ids(prev_retained)
        return false
      end
      writer.varint(field_ref)
      writer.byte(kind)
      e_reader = Reader.new(retain.storage, start_pos, end_pos, SchemaTable.arr)
      retain_out_payload(retain.storage, e_reader, writer, kind, 0, intern, false)
      raise Refused.new('wire_damaged', 'bytes left after a resolved record') if e_reader.pos != e_reader.end_pos

      true
    end

    def self.capture_retain_file(cls, wire, retain, ids = nil)
      ids ||= parse_id_table(wire)
      skip = LoadCtx.new(ids)
      skip.edges = DiscardEdges.new
      placeable = placeable_for_root(cls)
      counts = [0, 0]
      retain.reset

      framed_fn = lambda do |raw|
        r = Reader.new(raw, 0, raw.bytesize, ids)
        r.take(r.varint)
      end

      body_fn = u(nil)
      nested_fn = lambda do |data, target, node, steps|
        tcls = by_name(target)
        body_fn.call(data, tcls.descriptor, node, steps) if tcls
      end

      union_fn = u(nil)
      array_fn = lambda do |raw, field, node, steps, ordinal = nil, unframed = false|
        data = unframed ? raw : framed_fn.call(raw)
        ord = ordinal.nil? ? field.ordinal : ordinal
        r = Reader.new(data, 0, data.bytesize, ids)
        k = r.byte
        cnt = r.varint
        codec = field.codec == 'optional' ? field.inner : field.codec
        if codec == 'map'
          target = field.map_entry.empty? ? field.target : field.map_entry
          cnt.times { |idx| nested_fn.call(r.take(r.varint), target, node, steps + [[ord, idx]]) }
          return
        end
        if codec == 'keyed'
          elem = field.elem
          cnt.times do
            key = r.id_at(r.varint)
            content = r.take(r.varint)
            slot_idx = field.keys.index(key)
            if slot_idx.nil?
              counts[1] += 1
              next
            end
            if elem && elem.codec == 'table'
              nested_fn.call(content, elem.target, node, steps + [[ord, slot_idx]])
            elsif elem && elem.codec == 'enum'
              item = Reader.new(content, 0, content.bytesize, ids)
              ref = item.varint
              counts[1] += 1 if ref.positive? && !elem.variant_ids.include?(item.id_at(ref))
            end
          end
          return
        end
        elem = field.codec == 'optional' ? field.elem : field.element
        cnt.times do |idx|
          if elem && elem.codec == 'table'
            nested_fn.call(r.take(r.varint), elem.target, node, steps + [[ord, idx]])
          elsif elem && elem.codec == 'enum'
            ref = r.varint
            counts[1] += 1 if ref.positive? && !elem.variant_ids.include?(r.id_at(ref))
          else
            skip_payload(r, k, skip, 0)
          end
        end
      end

      union_fn = lambda do |reader, target, ordinal, node, steps|
        ref = reader.varint
        return if ref.zero?

        arm_id = reader.id_at(ref)
        arm_kind = reader.byte
        payload = reader.take(reader.varint)
        ucls = union_by_name(target)
        arm = u(ucls ? ucls.descriptor.by_id[arm_id] : nil)
        if arm.nil?
          counts[1] += 1
          return
        end
        return if arm.field.nil?

        child_steps = steps + [[ordinal, arm.tag]]
        af = arm.field
        if af.codec == 'table' && arm_kind == 13
          nested_fn.call(payload, af.target, node, child_steps)
        elsif af.codec == 'union' && arm_kind == 15
          union_fn.call(Reader.new(payload, 0, payload.bytesize, ids), af.target, ordinal, node, child_steps)
        elsif %w[array map keyed].include?(af.codec) && [14, 16].include?(arm_kind)
          arr_ord = af.codec == 'array' ? ordinal : af.ordinal
          array_fn.call(payload, af, node, child_steps, arr_ord, true)
        elsif af.codec == 'union_array' && arm_kind == 14
          ar = Reader.new(payload, 0, payload.bytesize, ids)
          ar.byte
          cnt = ar.varint
          cnt.times { |idx| union_fn.call(ar, af.target, ordinal, node, child_steps + [[ordinal, idx]]) }
        elsif af.codec == 'enum' && arm_kind == 30
          er = Reader.new(payload, 0, payload.bytesize, ids)
          eref = er.varint
          counts[1] += 1 if eref.positive? && !af.variant_ids.include?(er.id_at(eref))
        end
      end

      value_fn = lambda do |raw, field, node, steps|
        codec = field.codec == 'optional' ? field.inner : field.codec
        case codec
        when 'table'
          nested_fn.call(framed_fn.call(raw), field.target, node, steps + [[field.ordinal, 0]])
        when 'array', 'map', 'keyed'
          array_fn.call(raw, field, node, steps)
        when 'enum'
          r = Reader.new(raw, 0, raw.bytesize, ids)
          ref = r.varint
          counts[1] += 1 if ref.positive? && !field.variant_ids.include?(r.id_at(ref))
        when 'union'
          union_fn.call(Reader.new(raw, 0, raw.bytesize, ids), field.target, field.ordinal, node, steps)
        when 'union_array'
          data = framed_fn.call(raw)
          r = Reader.new(data, 0, data.bytesize, ids)
          r.byte
          cnt = r.varint
          cnt.times { |idx| union_fn.call(r, field.target, field.ordinal, node, steps + [[field.ordinal, idx]]) }
        end
      end

      nodes_fn = lambda do |data|
        r = Reader.new(data, 0, data.bytesize, ids)
        cnt = r.varint
        cnt.times do |idx|
          identity = r.id_at(r.varint)
          payload = r.take(r.varint)
          next if [BYTES_WIRE_TYPE_ID, STRING_WIRE_TYPE_ID].include?(identity)

          target = u(placeable[identity] ? by_id(identity) : nil)
          if target.nil?
            counts[1] += 1
          else
            body_fn.call(payload, target.descriptor, idx + 2, SchemaTable::EMPTY_ARR)
          end
        end
      end

      body_fn = lambda do |data, desc, node, steps|
        r = Reader.new(data, 0, data.bytesize, ids)
        loop do
          ref = r.varint
          return if ref.zero?

          identity = r.id_at(ref)
          kind = r.byte
          if identity == NODE_WIRE_ID
            nodes_fn.call(r.take(r.varint))
            next
          end
          start = r.pos
          skip_payload(r, kind, skip, 0)
          raw = data.byteslice(start, r.pos - start)
          field = desc.by_id[identity]
          if field.nil?
            if retain.capture(node, steps, identity, kind, raw, ids)
              counts[0] += 1
            else
              counts[1] += 1
            end
            next
          end
          value_fn.call(raw, field, node, steps) if kind == field.kind
        end
      end

      root_slice = wire.byteslice(1, body_end_offset(wire) - 1)
      body_fn.call(root_slice, cls.descriptor, 1, SchemaTable::EMPTY_ARR)
      counts
    end
  end
end
