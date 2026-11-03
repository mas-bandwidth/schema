# Integrity across file, fixed and message forms

The fixed-form pins are the existing generated-Go pins already checked by native
JavaScript in TestJavaScriptIntegrityConstruct. The three records are listed in
readings.json. File and message outputs are also checked against the neutral
engine, whose CRC implementation has independent native JavaScript regression
coverage. No wire pins were regenerated for the Rust implementation.
