# Exact timestamp boundaries

60 explicit cases extracted from the JavaScript timestamp native vectors, with absent, numeric, null, object and array JSON input and a single nanosecond added. Values are signed decimal nanoseconds; canonical strings are UTC. These expectations are literal and checked before consulting the neutral wire/cook encoder. Calendar counts were independently calculated using integer day/second arithmetic; the two signed int64 endpoints are explicit.

The schema supplies variable Clock and fixed Tick roots so native implementations can check JSON, wire, fixed, message and cook forms. The adjacent parent timestamp corpus remains the Dart owner's 52-case fixture; this subdirectory does not replace it.
