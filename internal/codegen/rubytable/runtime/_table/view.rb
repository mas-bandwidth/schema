# frozen_string_literal: true

module SchemaRuntimeUnit
  # Typed reflection records for UnitView (SPEC-TABLES §8.2).
  module SchemaTable
    # Type or table declaration row.
    class ViewType
      attr_reader :name, :file, :table, :type, :doc, :tags

      def initialize(name, file, table, type, doc, tags)
        @name = name
        @file = file
        @table = table
        @type = type
        @doc = doc
        @tags = tags
      end
    end

    # Enum, flags, or union variant row.
    class ViewVariant
      attr_reader :value, :name, :id, :payload_name, :payload, :field, :doc, :tags

      def initialize(value, name, id, payload_name, payload, field, doc, tags)
        @value = value
        @name = name
        @id = id
        @payload_name = payload_name
        @payload = payload
        @field = field
        @doc = doc
        @tags = tags
      end
    end

    # Enum, flags, or union declaration row.
    class ViewVocabulary
      attr_reader :name, :file, :max, :storage_bits, :payload_offset, :variants, :doc, :tags

      def initialize(name, file, max, storage_bits, payload_offset, variants, doc, tags)
        @name = name
        @file = file
        @max = max
        @storage_bits = storage_bits
        @payload_offset = payload_offset
        @variants = variants
        @doc = doc
        @tags = tags
      end
    end

    # Constant declaration row.
    class ViewConstant
      attr_reader :name, :file, :type_name, :is_float, :int_value, :float_value, :doc, :tags

      def initialize(name, file, type_name, is_float, int_value, float_value, doc, tags)
        @name = name
        @file = file
        @type_name = type_name
        @is_float = is_float
        @int_value = int_value
        @float_value = float_value
        @doc = doc
        @tags = tags
      end
    end

    # Top-level unit reflection registry.
    class UnitViewInfo
      attr_reader :package, :protocol_id, :types, :tables, :constants, :enums, :flags, :unions

      def initialize(package, protocol_id, types, tables, constants, enums, flags, unions)
        @package = package
        @protocol_id = protocol_id
        @types = types
        @tables = tables
        @constants = constants
        @enums = enums
        @flags = flags
        @unions = unions
      end
    end
  end
end
