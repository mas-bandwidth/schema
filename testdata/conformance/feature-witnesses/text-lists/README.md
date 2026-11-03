# Bounded text-list witness

The four list spellings exercise UTF-8 byte bounds, count bounds, optional
presence versus present-empty text, wrong JSON element kinds, and embedded NUL.
Native language checks compare populated wire, retained wire, canonical JSON,
and both cook byte orders with the neutral implementation. Handframed malformed
wire and program-authored invalid storage are covered in native tests.
