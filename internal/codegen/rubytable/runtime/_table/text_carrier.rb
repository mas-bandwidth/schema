# frozen_string_literal: true

module SchemaRuntimeUnit
  # Canonical RFC 4648 Base64 wire carrier (SPEC-TABLES §16.8), pure Ruby core.
  module SchemaTable
    BASE64_RE = %r{\A(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?\z}

    def self.encode_text(wire)
      [binary_input(wire)].pack('m0').encode(Encoding::UTF_8)
    end

    def self.decode_text(text)
      raise Refused, 'base64_damaged' unless text.instance_of?(String)
      raise Refused, 'base64_damaged' unless TEXT_ENCODINGS.include?(text.encoding) && text.valid_encoding?
      raise Refused, 'base64_damaged' unless text.ascii_only?

      stripped = text.delete("\r\n")
      raise Refused, 'base64_damaged' unless BASE64_RE.match?(stripped)

      begin
        wire = stripped.unpack1('m0')
      rescue ArgumentError
        raise Refused, 'base64_damaged'
      end
      raise Refused, 'base64_not_canonical' if [wire].pack('m0') != text

      wire
    end

    def self.decode_strict_base64(text, field_name)
      raise Refused.new('kind_mismatch', field_name) unless text.instance_of?(String)

      valid_str = TEXT_ENCODINGS.include?(text.encoding) && text.valid_encoding? && text.ascii_only?
      raise Refused.new('base64_damaged', field_name) unless valid_str && BASE64_RE.match?(text)

      begin
        wire = text.unpack1('m0')
      rescue ArgumentError
        raise Refused.new('base64_damaged', field_name)
      end
      raise Refused.new('base64_not_canonical', field_name) if [wire].pack('m0') != text

      wire
    end
  end
end
