;;;; wire.lisp -- the variable form, form byte 1 (§3, §3.1, §3.2, §4, §22).
;;;;
;;;; The read is two passes. FRAME walks the bytes with no declaration and
;;;; settles everything "decidable from the wire alone" (§4.1, §6.5): framing,
;;;; canonical LEB128, the closed kind and element-kind sets, first-use id
;;;; order, repeated ids, text content, bool bytes, finite floats, the node
;;;; table, the nesting bound and the pointer graph. BIND then walks that tree
;;;; against the reader's declaration: kinds, widening, ranges, bounds,
;;;; vocabularies, required fields and pointer targets. Either pass refuses
;;;; the whole load; nothing partial is ever returned (§4, "ONE read").

(in-package :spec-port)

(defconstant +node-table-id+ #xFFFFFFFFFFFFFFFF)
(defconstant +build-version-id+ #xFFFFFFFFFFFFFFFE)
(defconstant +vocabulary-id+ #xFFFFFFFFFFFFFFFD)
(defparameter +blob-bytes-id+ (fnv1a64 "bytes"))
(defparameter +blob-string-id+ (fnv1a64 "string"))
(defparameter +key-id+ (fnv1a64 "key"))
(defparameter +value-id+ (fnv1a64 "value"))
(defconstant +max-depth+ 128)
(defconstant +max-id-entries+ 4096)

(defun damage (fmt &rest args) (apply #'refuse (concatenate 'string "wire_damaged: " fmt) args))

(defun kind-width (kind)
  "The fixed payload width of a scalar kind, or NIL."
  (case kind
    ((1 2 6 20 25) 1) ((3 7 21 26) 2) ((4 8 10 22 27) 4) ((5 9 11 23 28 36) 8)
    ((18 19 24 29) 16) (t nil)))

(defun signed-kind-p (kind) (member kind '(2 3 4 5 18 20 21 22 23 24 36)))

(defparameter +array-elem-kinds+ (append (loop for k from 1 to 13 collect k) '(15 17) (loop for k from 18 to 30 collect k)))
(defparameter +keyed-elem-kinds+ (append (loop for k from 1 to 11 collect k) '(13) (loop for k from 18 to 30 collect k)))

;;; ------------------------------------------------------------------
;;; FRAME: the declaration-free walk

(defstruct wf id kind payload)   ; one field of a body

(defvar *buf*)
(defvar *ids*)        ; vector of u64 ids, entry k at index k-1
(defvar *used*)       ; number of distinct ids referenced so far (first-use order)
(defvar *records*)    ; vector of (type-id . body-or-octets), record k+2 at index k
(defvar *edges*)      ; list of (from . to) node indices
(defvar *cur-node*)

(defun rd-u8 (pos end)
  (when (>= pos end) (damage "byte past body end"))
  (values (aref *buf* pos) (1+ pos)))

(defun rd-le (pos end n)
  (when (> (+ pos n) end) (damage "scalar past body end"))
  (let ((v 0)) (dotimes (i n) (setf v (logior v (ash (aref *buf* (+ pos i)) (* 8 i)))))
    (values v (+ pos n))))

(defun rd-leb (pos end)
  "Canonical unsigned LEB128 (§3): non-minimal or past 64 bits is damage."
  (let ((v 0) (shift 0) (i pos))
    (loop
      (when (>= i end) (damage "leb128 past body end"))
      (let ((b (aref *buf* i)))
        (incf i)
        (when (and (= shift 63) (> b 1)) (damage "leb128 past 64 bits"))
        (setf v (logior v (ash (logand b #x7F) shift)))
        (when (< b #x80)
          (when (and (= b 0) (> (- i pos) 1)) (damage "non-canonical leb128"))
          (return (values v i)))
        (incf shift 7)))))

(defun rd-ref (pos end)
  "A reference, resolved to its id, with §3's first-use rule. 0 -> NIL."
  (multiple-value-bind (r p) (rd-leb pos end)
    (cond ((zerop r) (values nil p))
          ((> r (length *ids*)) (damage "reference above the entry count"))
          (t
           (cond ((<= r *used*) nil)
                 ((= r (1+ *used*)) (incf *used*))
                 (t (refuse "non_canonical: id table out of first-use order")))
           (values (aref *ids* (1- r)) p)))))

(defun check-depth (depth) (when (> depth +max-depth+) (refuse "nesting past ~a" +max-depth+)))

(defun frame-scalar (kind pos end)
  "A fixed-width scalar payload: the raw unsigned integer."
  (let ((w (kind-width kind)))
    (multiple-value-bind (v p) (rd-le pos end w)
      (case kind
        (1 (unless (<= v 1) (damage "bool byte ~a" v)))
        (10 (unless (f32-finite-p v) (refuse "non_finite_float")))
        (11 (unless (f64-finite-p v) (refuse "non_finite_float"))))
      (values v p))))

(defun frame-text (pos end)
  (unless (utf8-well-formed-p *buf* :start pos :end end) (damage "ill-formed text"))
  (subseq *buf* pos end))

(defun frame-len (pos end)
  "L, then the end of the L bytes, checked against END."
  (multiple-value-bind (l p) (rd-leb pos end)
    (when (> (+ p l) end) (damage "length past body end"))
    (values p (+ p l))))

(defun frame-body (pos end depth &optional root)
  "A table body in [POS, END): fields, then the zero reference at END-1."
  (check-depth depth)
  (let ((fields '()) (seen (make-hash-table)) (p pos))
    (loop
      (multiple-value-bind (id q) (rd-ref p end)
        (setf p q)
        (when (null id)
          (unless (= p end) (damage "terminator before the body's end"))
          (return (nreverse fields)))
        (when (gethash id seen) (damage "field id twice in one body"))
        (setf (gethash id seen) t)
        (multiple-value-bind (kind q2) (rd-u8 p end)
          (setf p q2)
          (cond
            ((= id +node-table-id+)
             (unless root (damage "node-table id in a nested body"))
             (unless (= kind 12) (damage "node table under kind ~a" kind))
             (multiple-value-bind (s e) (frame-len p end)
               (frame-node-table s e)
               (setf p e))
             ;; §3.1: the node-table field is the root body's LAST field
             (unless (and (< p end) (= (aref *buf* p) 0) (= (1+ p) end))
               (refuse "non_canonical: node table not the root body's last field"))
             (push (make-wf :id id :kind kind :payload :node-table) fields))
            ((or (= id +build-version-id+) (= id +vocabulary-id+))
             (damage "reserved announcement id in a file body"))
            (t
             (multiple-value-bind (payload q3) (frame-payload kind p end depth)
               (setf p q3)
               (push (make-wf :id id :kind kind :payload payload) fields)))))))))

(defun frame-payload (kind pos end depth)
  "A field's payload of KIND starting at POS, bounded by the parent's END."
  (cond
    ((kind-width kind) (frame-scalar kind pos end))
    (t
     (case kind
       (12 (multiple-value-bind (s e) (frame-len pos end) (values (frame-text s e) e)))
       (13 (multiple-value-bind (s e) (frame-len pos end) (values (frame-body s e (1+ depth)) e)))
       (14 (multiple-value-bind (s e) (frame-len pos end) (values (frame-array s e (1+ depth)) e)))
       (16 (multiple-value-bind (s e) (frame-len pos end) (values (frame-keyed s e (1+ depth)) e)))
       (15 (frame-union pos end depth))
       (17 (multiple-value-bind (ix p) (rd-leb pos end) (push (cons *cur-node* ix) *edges*) (values ix p)))
       (30 (rd-ref pos end))
       (31 (multiple-value-bind (s e) (frame-len pos end) (declare (ignore s)) (values :escape e)))
       (32 (multiple-value-bind (s e) (frame-len pos end)
             (unless (= s e) (damage "kind 32 with a nonzero length"))
             (values :none e)))
       (t (damage "kind ~a no reader can skip" kind))))))

(defun frame-union (pos end depth)
  "§3 kind 15: arm id reference; when not 0, kind, L, then the arm payload."
  (multiple-value-bind (arm p) (rd-ref pos end)
    (if (null arm)
        (values nil p)
        (multiple-value-bind (kind p2) (rd-u8 p end)
          (multiple-value-bind (s e) (frame-len p2 end)
            ;; SPEC-GAP 4 (AMBIGUOUS): §4 counts "a union whose arm holds" a
            ;; structural payload as a level; whether the arm's own body is a
            ;; second level is unsaid. This port counts the union, then the arm.
            (let ((d (if (member kind '(13 14 15 16)) (1+ depth) depth)))
              (values (list arm kind (frame-arm kind s e d)) e)))))))

(defun frame-arm (kind s e depth)
  "§3's arm payload table: the arm's L stands in for the field's own length."
  (cond
    ((kind-width kind)
     (unless (= (- e s) (kind-width kind)) (damage "arm length does not frame kind ~a" kind))
     (values (frame-scalar kind s e)))
    (t
     (case kind
       (12 (frame-text s e))
       (13 (frame-body s e (1+ depth)))
       (14 (frame-array s e (1+ depth)))
       (16 (frame-keyed s e (1+ depth)))
       (15 (multiple-value-bind (v p) (frame-union s e depth)
             (unless (= p e) (damage "bytes after a nested union inside its arm"))
             v))
       (17 (multiple-value-bind (ix p) (rd-leb s e)
             (unless (= p e) (damage "arm length does not frame its index"))
             (push (cons *cur-node* ix) *edges*) ix))
       (30 (multiple-value-bind (id p) (rd-ref s e)
             (unless (= p e) (damage "arm length does not frame its reference"))
             id))
       (31 :escape)
       (32 (unless (= s e) (damage "kind 32 with a nonzero length")) :none)
       (t (damage "arm kind ~a no reader can skip" kind))))))

(defun frame-elements (ek n pos end depth)
  "N elements of element kind EK from POS; returns (list) and the end."
  (let ((w (kind-width ek)) (out '()) (p pos))
    (when (and w (> (* n w) (- end p))) (damage "count the body cannot cover"))
    (dotimes (i n)
      (cond
        (w (multiple-value-bind (v q) (frame-scalar ek p end) (push v out) (setf p q)))
        (t (case ek
             (12 (multiple-value-bind (s e) (frame-len p end) (push (frame-text s e) out) (setf p e)))
             (13 (multiple-value-bind (s e) (frame-len p end) (push (frame-body s e (1+ depth)) out) (setf p e)))
             (15 (multiple-value-bind (v q) (frame-union p end depth) (push v out) (setf p q)))
             (17 (multiple-value-bind (ix q) (rd-leb p end) (push (cons *cur-node* ix) *edges*) (push ix out) (setf p q)))
             (30 (multiple-value-bind (id q) (rd-ref p end) (push id out) (setf p q)))))))
    (values (nreverse out) p)))

(defun frame-array (s e depth)
  "§3 kind 14's body: element kind, N, the elements; the header inside L."
  (check-depth depth)
  (when (< (- e s) 2) (damage "array body too short for its header"))
  (let ((ek (aref *buf* s)))
    (unless (member ek +array-elem-kinds+) (damage "array element kind ~a off the closed list" ek))
    (multiple-value-bind (n p) (rd-leb (1+ s) e)
      (multiple-value-bind (elems q) (frame-elements ek n p e depth)
        (unless (= q e) (damage "bytes left inside an array's length"))
        (list :arr ek elems)))))

(defun frame-keyed (s e depth)
  "§3.2 kind 16. SPEC-GAP 5 (CONTRADICTION): §3 'THE HEADER INSIDE THE BODY'
puts kind 16's header inside L; §3.2 says its count is read 'against the
ENCLOSING body and never against the field's own L'. This port reads it
inside L, as §3 states for all three body kinds."
  (check-depth depth)
  (when (< (- e s) 2) (damage "keyed body too short for its header"))
  (let ((ek (aref *buf* s)) (seen (make-hash-table)) (out '()))
    (unless (member ek +keyed-elem-kinds+) (damage "keyed element kind ~a off the closed list" ek))
    (multiple-value-bind (n p) (rd-leb (1+ s) e)
      (dotimes (i n)
        (multiple-value-bind (key q) (rd-ref p e)
          (unless key (damage "keyed slot key reference 0"))
          (when (gethash key seen) (damage "keyed slot twice"))
          (setf (gethash key seen) t)
          (multiple-value-bind (s2 e2) (frame-len q e)
            (let ((v (cond ((kind-width ek)
                            (unless (= (- e2 s2) (kind-width ek)) (damage "keyed element length does not frame its kind"))
                            (frame-scalar ek s2 e2))
                           ((= ek 13) (frame-body s2 e2 (1+ depth)))
                           ((= ek 30) (multiple-value-bind (id r) (rd-ref s2 e2)
                                        (unless (= r e2) (damage "keyed element length does not frame its reference"))
                                        id)))))
              (push (cons key v) out)
              (setf p e2)))))
      (unless (= p e) (damage "bytes left inside a keyed body's length"))
      (list :keyed ek (nreverse out)))))

(defun frame-node-table (s e)
  "§3.1: node_count, then records of type id reference, length, body."
  (multiple-value-bind (count p) (rd-leb s e)
    (let ((recs '()) (k 0))
      (loop while (< p e)
            do (multiple-value-bind (tid q) (rd-ref p e)
                 (unless tid (damage "node record type id reference 0"))
                 (multiple-value-bind (bs be) (frame-len q e)
                   (incf k)
                   (let ((*cur-node* (1+ k)))
                     (push (cons tid
                                 (cond ((= tid +blob-bytes-id+)
                                        (when (> (- be bs) #xFFFFFFFF) (refuse "blob_over_size_cap"))
                                        (subseq *buf* bs be))
                                       ((= tid +blob-string-id+)
                                        (when (> (- be bs) #xFFFFFFFF) (refuse "blob_over_size_cap"))
                                        (frame-text bs be))
                                       (t (frame-body bs be 0))))
                           recs))
                   (setf p be))))
      (unless (= k count) (damage "node_count disagrees with the record scan"))
      (when (zerop count) (refuse "non_canonical: empty node table"))
      (setf *records* (coerce (nreverse recs) 'vector)))))

(defun check-graph ()
  "§3.1/§6.5: indices in range, every record reached, no cycle, no blob
named twice."
  (let* ((n (length *records*)) (top (1+ n))
         (adj (make-array (1+ top) :initial-element nil))
         (indeg (make-array (1+ top) :initial-element 0)))
    (dolist (e *edges*)
      (let ((to (cdr e)))
        (when (> to top) (damage "node index above node_count + 1"))
        (unless (zerop to)
          (push to (aref adj (car e)))
          (incf (aref indeg to)))))
    (loop for k from 2 to top
          for rec = (aref *records* (- k 2))
          when (and (member (car rec) (list +blob-bytes-id+ +blob-string-id+))
                    (> (aref indeg k) 1))
            do (refuse "blob_shared"))
    ;; iterative three-colour DFS from the root: linear, no recursion
    (let ((mark (make-array (1+ top) :initial-element 0))  ; 0 unvisited 1 open 2 closed
          (stack (list (cons 1 nil))))
      (setf (aref mark 1) 1)
      (setf (car stack) (cons 1 (reverse (aref adj 1))))
      (loop while stack
            do (let ((top-frame (car stack)))
                 (if (null (cdr top-frame))
                     (progn (setf (aref mark (car top-frame)) 2) (pop stack))
                     (let ((to (pop (cdr top-frame))))
                       (case (aref mark to)
                         (1 (damage "cycle in the pointer graph"))
                         (2 nil)
                         (0 (setf (aref mark to) 1)
                          (push (cons to (reverse (aref adj to))) stack)))))))
      (loop for k from 2 to top
            unless (= (aref mark k) 2) do (damage "node record no pointer reaches")))))

(defun frame-file (buf)
  "Form 1 framing: form byte, root body, id table (§3, §22.1). Returns the
root body tree; the records land in *RECORDS*."
  (let* ((len (length buf)))
    (when (< len 9) (damage "file too short for an id table"))
    (let* ((count (let ((v 0)) (dotimes (i 8 v) (setf v (logior v (ash (aref buf (+ (- len 8) i)) (* 8 i)))))))
           (table-start (- len 8 (* 8 count))))
      (when (> count +max-id-entries+) (damage "id table past ~a entries" +max-id-entries+))
      (when (< table-start 1) (damage "id table runs past the front of the file"))
      (let ((ids (make-array count)) (seen (make-hash-table)))
        (dotimes (k count)
          (let ((v 0) (base (+ table-start (* 8 k))))
            (dotimes (i 8) (setf v (logior v (ash (aref buf (+ base i)) (* 8 i)))))
            (when (gethash v seen) (damage "id table carries one id twice"))
            (setf (gethash v seen) t)
            (setf (aref ids k) v)))
        (let ((*ids* ids))
          (setf *used* 0)
          (let ((root (frame-body 1 table-start 0 t)))
            (unless (= *used* count) (refuse "non_canonical: id-table entry no reference uses"))
            root))))))

;;; ------------------------------------------------------------------
;;; BIND: the tree against the reader's declaration

(defvar *fixups*)     ; list of (setter target index)
(defvar *unknown*)

(defun kind-mismatch (fmt &rest args) (apply #'refuse (concatenate 'string "kind_mismatch: " fmt) args))

(defun ladder-accepts-p (want got)
  "§4 WIDENED: each kind accepts every kind below it on its own ladder."
  (flet ((on (ladder)
           (let ((a (position want ladder)) (b (position got ladder)))
             (and a b (<= b a)))))
    (or (= want got) (on '(2 3 4 5 18)) (on '(6 7 8 9 19)) (and (= want 11) (= got 10)))))

(defun signed-value (raw kind)
  (let ((bits (* 8 (kind-width kind))))
    (if (and (signed-kind-p kind) (logbitp (1- bits) raw)) (- raw (ash 1 bits)) raw)))

(defun bind-scalar (td kind raw where)
  "A scalar payload RAW of wire KIND into type TD, with widening and range."
  (let ((want (type-kind td)))
    (unless (ladder-accepts-p want kind) (kind-mismatch "~a: kind ~a for ~a" where kind want))
    (ecase (first td)
      (:int (let ((v (signed-value raw kind)))
              (unless (<= (fourth td) v (fifth td)) (refuse "out_of_range: ~a" where))
              v))
      (:bits (unless (< raw (expt 2 (second td))) (refuse "out_of_range: ~a" where)) raw)
      (:flags raw)
      (:bool (= raw 1))
      (:i128 (let ((v (signed-value raw kind)))
               (unless (<= (second td) v (third td)) (refuse "out_of_range: ~a" where))
               v))
      (:u128 raw)
      (:timestamp (signed-value raw kind))
      (:fixed (let ((v (signed-value raw kind)))
                (unless (<= (fifth td) v (sixth td)) (refuse "out_of_range: ~a" where))
                v))
      (:float
       (let* ((w (second td))
              (bits (if (and (= w 64) (= kind 10)) (f32-to-f64-bits raw) raw)))
         (if (compressed-p td)
             (destructuring-bind (mn mx res) (third td)
               (declare (ignore res))
               ;; SPEC-GAP 6 (SILENT): the bounds a compressed float is held to
               ;; on read are stated in decimal; this port compares the exact
               ;; stored value with the bounds rounded to the field's width.
               (let ((v (float-bits-rational bits w))
                     (lo (float-bits-rational (rational-to-float-bits (rational mn) w) w))
                     (hi (float-bits-rational (rational-to-float-bits (rational mx) w) w)))
                 (unless (<= lo v hi) (refuse "out_of_range: ~a" where))
                 (if (zerop v) 0 bits)))     ; §3.3: a compressed -0 lands as +0
             bits))))))

(defun scalar-type-p (td)
  (member (first td) '(:int :bits :flags :bool :i128 :u128 :timestamp :fixed :float)))

(defun bind-value (td kind payload where)
  "One payload of wire KIND into the reader's type TD."
  (when (member kind '(31 32)) (kind-mismatch "~a: kind ~a at a named position" where kind))
  (cond
    ((eq (first td) :optional) (bind-value (second td) kind payload where))
    ((scalar-type-p td) (bind-scalar td kind payload where))
    (t
     (let ((want (type-kind td)))
       (unless (= want kind) (kind-mismatch "~a: kind ~a for ~a" where kind want))
       (ecase (first td)
         (:enum (bind-enum (second td) payload))
         (:string (let ((n (second td)))
                    (when (and n (> (length payload) n)) (refuse "length_over_bound: ~a" where))
                    payload))
         (:bytes (destructuring-bind (tag ek elems) payload
                   (declare (ignore tag))
                   (unless (= ek 6) (kind-mismatch "~a: bytes element kind ~a" where ek))
                   (let ((n (second td)))
                     (when (and n (> (length elems) n)) (refuse "length_over_bound: ~a" where))
                     (to-octets elems))))
         (:struct (bind-body (second td) payload))
         (:union (bind-union (second td) payload where))
         (:array (bind-array td payload where))
         (:map (bind-map td payload where))
         (:ptr (let ((cell (list nil)))
                 (push (list cell (second td) payload) *fixups*)
                 cell)))))))

(defun bind-enum (e id)
  (if (null id)
      0
      (let ((v (find id (edecl-variants e) :key #'variant-wire-id)))
        (if v (variant-ordinal v) (progn (incf *unknown*) 0)))))

(defun bind-union (u payload where)
  (if (null payload)
      nil
      (destructuring-bind (arm-id kind apayload) payload
        (let ((arm (find arm-id (union-arms u) :key #'arm-wire-id)))
          (cond
            ((null arm) (incf *unknown*) nil)
            ((null (arm-type arm))
             (unless (= kind 32) (kind-mismatch "~a: payload-free arm under kind ~a" where kind))
             (cons (arm-tag arm) nil))
            (t (cons (arm-tag arm) (bind-value (arm-type arm) kind apayload where))))))))

(defun bind-elements (elem ek elems where)
  "Elements of wire element kind EK into element type ELEM (§3, §4)."
  (let ((want (elem-kind elem)))
    (cond
      ((eq (first elem) :optional)       ; a text list's ?string(M) element record
       (unless (= ek 13) (kind-mismatch "~a: element kind ~a" where ek))
       (mapcar (lambda (body)
                 (let ((v :absent))
                   (dolist (f body)
                     (if (= (wf-id f) +value-id+)
                         (setf v (bind-value (second elem) (wf-kind f) (wf-payload f) where))
                         (incf *unknown*)))
                     (if (eq v :absent) nil v)))
               elems))
      ((scalar-type-p elem)
       (unless (ladder-accepts-p want ek) (kind-mismatch "~a: element kind ~a for ~a" where ek want))
       (mapcar (lambda (raw) (bind-scalar elem ek raw where)) elems))
      (t
       (unless (= want ek) (kind-mismatch "~a: element kind ~a for ~a" where ek want))
       (mapcar (lambda (p) (bind-value elem ek p where)) elems)))))

(defun bind-array (td payload where)
  (destructuring-bind (mode lo hi elem) (rest td)
    (if (eq mode :keyed)
        (destructuring-bind (tag ek triples) payload
          (declare (ignore tag))
          (let ((out (type-default td)))
            (dolist (tr triples out)
              (let ((v (find (car tr) (edecl-variants hi) :key #'variant-wire-id)))
                (if (null v)
                    (incf *unknown*)
                    (setf (aref out (1- (variant-ordinal v)))
                          (first (bind-elements elem ek (list (cdr tr)) where))))))))
        (destructuring-bind (tag ek elems) payload
          (declare (ignore tag))
          (let* ((vals (bind-elements elem ek elems where)) (n (length vals)))
            (ecase mode
              (:fixed (when (> n hi) (refuse "count_over_bound: ~a" where))
               (let ((out (type-default td)))
                 (loop for v in vals for i from 0 do (setf (aref out i) v))
                 out))
              (:counted (when (> n hi) (refuse "count_over_bound: ~a" where))
               ;; SPEC-GAP 7 (SILENT): a wire count below a [A..B] array's A is
               ;; named nowhere in §4's refusals; this port refuses it.
               (when (< n lo) (refuse "count_under_bound: ~a" where))
               (coerce vals 'vector))
              (:unbounded (coerce vals 'vector))))))))

(defun key-less-p (td a b)
  "§22.2.1's key order: integers by value, strings by UTF-8 bytes."
  (if (member (first td) '(:string))
      (let ((n (min (length a) (length b))))
        (loop for i below n
              do (cond ((< (aref a i) (aref b i)) (return-from key-less-p t))
                       ((> (aref a i) (aref b i)) (return-from key-less-p nil))))
        (< (length a) (length b)))
      (< a b)))

(defun bind-map (td payload where)
  "§2.8: kind 14 over element kind 13, one { key, value } body per entry,
ascending with no key twice."
  (destructuring-bind (ktd vtd) (rest td)
    (destructuring-bind (tag ek elems) payload
      (declare (ignore tag))
      (unless (= ek 13) (kind-mismatch "~a: map element kind ~a" where ek))
      (let ((out '()) (prev nil) (first t))
        (dolist (body elems)
          (let ((k (type-default ktd)) (v (if (eq (first vtd) :optional) :absent (type-default vtd))))
            (dolist (f body)
              (cond ((= (wf-id f) +key-id+) (setf k (bind-value ktd (wf-kind f) (wf-payload f) where)))
                    ((= (wf-id f) +value-id+) (setf v (bind-value vtd (wf-kind f) (wf-payload f) where)))
                    (t (incf *unknown*))))
            (unless first
              (unless (key-less-p ktd prev k) (damage "map key not ascending")))
            (setf first nil prev k)
            (push (cons k v) out)))
        (nreverse out)))))

(defun bind-body (decl body)
  "A body's fields into a fresh value of DECL, absent fields at defaults."
  (let* ((tv (make-default-tval decl)) (present (make-hash-table)))
    (dolist (f body)
      (let ((fd (gethash (wf-id f) (sdecl-by-id decl))))
        (if (null fd)
            (incf *unknown*)
            (let ((where (format nil "~a.~a" (sdecl-name decl) (field-name fd))))
              (setf (gethash (field-index fd) present) t)
              (setf (aref (tval-slots tv) (field-index fd))
                    (bind-value (field-type fd) (wf-kind f) (wf-payload f) where))))))
    (loop for fd across (struct-fields decl)
          do (when (and (field-required fd) (not (gethash (field-index fd) present)) (guard-true-p tv fd))
               (refuse "missing_required: ~a.~a" (sdecl-name decl) (field-name fd)))
             (when (field-total fd)
               (let ((sum (loop for e across (aref (tval-slots tv) (field-index fd)) sum (if e (length e) 0))))
                 (when (> sum (field-total fd)) (refuse "length_over_bound: total ~a" (field-name fd))))))
    tv))

(defun resolve-cells (v)
  "Replace the pointer CELLS bind left with their targets, in place."
  (cond ((tval-p v) (let ((s (tval-slots v))) (dotimes (i (length s)) (setf (aref s i) (resolve-cells (aref s i))))) v)
        ((typep v 'octets) v)
        ((and (consp v) (eq (car v) :cell)) (cdr v))
        ((and (vectorp v) (not (stringp v))) (dotimes (i (length v) v) (setf (aref v i) (resolve-cells (aref v i)))))
        ((and (consp v) (integerp (car v))) (setf (cdr v) (resolve-cells (cdr v))) v)    ; union
        ((consp v) (dolist (e v v) (when (consp e) (setf (car e) (resolve-cells (car e)) (cdr e) (resolve-cells (cdr e))))))
        (t v)))

(defun wire-load (decl buf)
  "Load a form-1 file as DECL. Returns the root value; refuses otherwise."
  (when (zerop (length buf)) (damage "empty file"))
  (unless (= (aref buf 0) 1) (refuse "form ~a is not this reader's" (aref buf 0)))
  (when (sdecl-integrity decl)
    (when (< (length buf) 5) (refuse "integrity_mismatch"))
    (let* ((n (- (length buf) 4))
           (want (logior (aref buf n) (ash (aref buf (+ n 1)) 8) (ash (aref buf (+ n 2)) 16) (ash (aref buf (+ n 3)) 24))))
      (unless (= want (crc32c buf :end n)) (refuse "integrity_mismatch"))
      (setf buf (subseq buf 0 n))))
  (let ((*buf* buf) (*records* (vector)) (*edges* '()) (*cur-node* 1) (*used* 0)
        (*fixups* '()) (*unknown* 0))
    (let ((root-body (frame-file buf)))
      (check-graph)
      (let* ((root (bind-body decl (remove :node-table root-body :key #'wf-payload)))
             (nodes (map 'vector
                         (lambda (rec)
                           (let ((tid (car rec)))
                             (cond ((= tid +blob-bytes-id+) (make-blob :kind :bytes :data (cdr rec)))
                                   ((= tid +blob-string-id+) (make-blob :kind :string :data (cdr rec)))
                                   (t (let ((d (gethash tid (unit-tables-by-id *unit*))))
                                        (if d (bind-body d (cdr rec)) (progn (incf *unknown*) :unknown)))))))
                         *records*)))
        ;; pointer targets (§3.1, §6.5)
        (dolist (fx *fixups*)
          (destructuring-bind (cell target ix) fx
            (setf (car cell) :cell)
            (setf (cdr cell)
                  (cond ((zerop ix) nil)
                        ((= ix 1) (damage "pointer to the root"))
                        (t (let ((node (aref nodes (- ix 2))))
                             (cond ((eq node :unknown)
                                    (if (keywordp target) (damage "wrong-type pointer target") nil))
                                   ((blob-p node)
                                    (unless (eq target (blob-kind node)) (damage "wrong-type pointer target"))
                                    node)
                                   (t (unless (eq target (tval-decl node)) (damage "wrong-type pointer target"))
                                      node))))))))
        (resolve-cells root)
        (loop for n across nodes when (tval-p n) do (resolve-cells n))
        root))))

;;; ------------------------------------------------------------------
;;; WRITE (§22)

(defvar *intern*)       ; adjustable vector of ids in first-use order
(defvar *intern-ix*)    ; id -> reference
(defvar *numbering*)    ; node -> index
(defvar *nodes*)        ; nodes in index order (from 2)

(defun invalid (fmt &rest args) (apply #'refuse (concatenate 'string "invalid_value: " fmt) args))

(defun intern-id (id)
  (or (gethash id *intern-ix*)
      (progn (vector-push-extend id *intern*)
             (setf (gethash id *intern-ix*) (length *intern*)))))

(defun intern-mark () (length *intern*))
(defun intern-rollback (mark)
  (loop while (> (length *intern*) mark)
        do (remhash (vector-pop *intern*) *intern-ix*)))

;;; Numbering (§22.3): one depth-first pre-order walk over pointer edges.

(defun number-walk (tv open)
  (let ((decl (tval-decl tv)))
    (loop for f across (struct-fields decl)
          for v = (aref (tval-slots tv) (field-index f))
          when (guard-true-p tv f)
            do (number-value (field-type f) v open))))

(defun number-value (td v open)
  (ecase (first td)
    ((:int :bits :flags :bool :i128 :u128 :timestamp :fixed :float :enum :string :bytes) nil)
    (:optional (unless (eq v :absent) (number-value (second td) v open)))
    (:struct (number-walk v open))
    (:union (when (and v (cdr v))
              (let ((arm (aref (union-arms (second td)) (1- (car v)))))
                (when (arm-type arm) (number-value (arm-type arm) (cdr v) open)))))
    (:array (let ((elem (fifth td))) (loop for e across v do (number-value elem e open))))
    (:map (dolist (e v) (number-value (second td) (car e) open) (number-value (third td) (cdr e) open)))
    (:ptr
     (when v
       (let ((ix (gethash v *numbering*)))
         (cond
           ((blob-p v) (if ix (invalid "blob_shared") (progn (vector-push-extend v *nodes*)
                                                             (setf (gethash v *numbering*) (1+ (length *nodes*))))))
           ((gethash v open) (invalid "data_cycle"))
           (ix nil)
           (t (vector-push-extend v *nodes*)
              (setf (gethash v *numbering*) (1+ (length *nodes*)))
              (setf (gethash v open) t)
              (number-walk v open)
              (remhash v open))))))))

;;; Bodies and payloads

(defun write-body (tv &optional root)
  "The body's fields in declaration order (§22.2), then the terminator."
  (let ((buf (make-buf)) (decl (tval-decl tv)))
    (loop for f across (struct-fields decl)
          do (when (guard-true-p tv f)
               (write-field buf f (aref (tval-slots tv) (field-index f)))))
    (when (and root (plusp (length *nodes*)))
      (buf-leb buf (intern-id +node-table-id+))
      (buf-byte buf 12)
      (let ((nt (make-buf)))
        (buf-leb nt (length *nodes*))
        (loop for node across *nodes*
              do (if (blob-p node)
                     (progn (buf-leb nt (intern-id (if (eq (blob-kind node) :bytes) +blob-bytes-id+ +blob-string-id+)))
                            (check-blob node)
                            (buf-leb nt (length (blob-data node)))
                            (buf-bytes nt (blob-data node)))
                     (progn (buf-leb nt (intern-id (sdecl-wire-id (tval-decl node))))
                            (let ((b (write-body node)))
                              (buf-leb nt (length b))
                              (buf-bytes nt b)))))
        (buf-leb buf (length nt))
        (buf-bytes buf (buf-octets nt))))
    (buf-byte buf 0)
    (buf-octets buf)))

(defun check-blob (b)
  (when (and (eq (blob-kind b) :string) (not (utf8-well-formed-p (blob-data b))))
    (invalid "blob text is not text")))

(defun write-field (buf f v)
  (let ((td (field-type f)) (req (field-required f)))
    (cond
      ((eq (first td) :optional)
       (unless (eq v :absent)
         (write-header-and-payload buf f (second td) v :always t)))
      (t (write-header-and-payload buf f td v :always req)))))

(defun write-header-and-payload (buf f td v &key always)
  "§22.2's table: elide, or the header (reference, kind) then the payload."
  (let ((mark (intern-mark)))
    (unless (and (not always) (scalar-type-p td) (value-equal td v (field-default f)))
      (let* ((ref (intern-id (field-wire-id f)))
             (payload (encode-payload td v (field-default f) (format nil "~a" (field-name f)) always)))
        (if (eq payload :elide)
            (intern-rollback mark)
            (progn
              (buf-leb buf ref)
              (buf-byte buf (type-kind td))
              (buf-bytes buf payload)))))))

(defun with-len (bytes)
  (let ((b (make-buf))) (buf-leb b (length bytes)) (buf-bytes b bytes) (buf-octets b)))

(defun encode-scalar (td v where)
  "A scalar payload at its kind's width, after the domain check (§22.2)."
  (let ((b (make-buf)) (k (type-kind td)))
    (ecase (first td)
      (:int (unless (<= (fourth td) v (fifth td)) (invalid "~a" where)))
      (:bits (unless (<= 0 v (1- (expt 2 (second td)))) (invalid "~a" where)))
      (:i128 (unless (<= (second td) v (third td)) (invalid "~a" where)))
      (:fixed (unless (<= (fifth td) v (sixth td)) (invalid "~a" where)))
      (:float (unless (if (= (second td) 32) (f32-finite-p v) (f64-finite-p v)) (invalid "non-finite float ~a" where))
       (when (compressed-p td)
         (when (zerop (float-bits-rational v (second td))) (setf v 0))))
      ((:flags :u128 :timestamp :bool) nil))
    (if (eq (first td) :bool) (buf-byte b (if v 1 0)) (buf-le b v (kind-width k)))
    (buf-octets b)))

(defun encode-payload (td v default where &optional always)
  "The payload bytes after a field header, or :ELIDE when §22.2 elides it.
ALWAYS (a required field, a present optional) writes the empty shapes §22.2
spells out instead of eliding."
  (flet ((elide-if (test thunk) (if (and test (not always)) :elide (funcall thunk))))
    (ecase (first td)
      ((:int :bits :flags :bool :i128 :u128 :timestamp :fixed :float) (encode-scalar td v where))
      (:enum (elide-if (eql v default) (lambda () (encode-enum-ref (second td) v where))))
      (:string (check-text v (second td) where)
       (elide-if (equalp v default) (lambda () (with-len v))))
      (:bytes (when (and (second td) (> (length v) (second td))) (invalid "~a" where))
       (elide-if (equalp v default)
                 (lambda () (with-len (encode-array-body 6 (coerce v 'list) (lambda (x) (octets x)))))))
      (:struct (let ((body (write-body v)))
                 (elide-if (= (length body) 1) (lambda () (with-len body)))))
      (:union (elide-if (null v) (lambda () (if v (encode-union (second td) v where) (octets 0)))))
      (:ptr (elide-if (null v) (lambda () (let ((b (make-buf))) (buf-leb b (if v (gethash v *numbering*) 0)) (buf-octets b)))))
      (:map (elide-if (null v) (lambda () (with-len (encode-map td v where)))))
      (:array (encode-array td v where always)))))

(defun check-text (v bound where)
  (when (and bound (> (length v) bound)) (invalid "~a past its bound" where))
  (unless (utf8-well-formed-p v) (invalid "~a is not text" where)))

(defun encode-enum-ref (e v where)
  (when (or (< v 0) (> v (edecl-max e))) (invalid "~a: enum value no variant names" where))
  (let ((b (make-buf)))
    (buf-leb b (if (zerop v) 0 (intern-id (variant-wire-id (aref (edecl-variants e) (1- v))))))
    (buf-octets b)))

(defun encode-union (u v where)
  (let* ((arms (union-arms u)))
    (when (or (< (car v) 1) (> (car v) (length arms))) (invalid "~a: union tag" where))
    (let* ((arm (aref arms (1- (car v)))) (b (make-buf)))
      (buf-leb b (intern-id (arm-wire-id arm)))
      (if (null (arm-type arm))
          (progn (buf-byte b 32) (buf-byte b 0))
          (let* ((td (arm-type arm)) (p (encode-arm td (cdr v) where)))
            (buf-byte b (type-kind td))
            (buf-leb b (length p))
            (buf-bytes b p)))
      (buf-octets b))))

(defun encode-arm (td v where)
  "§3's arm payload table: the field payload without its own length prefix;
an arm's payload never elides."
  (ecase (first td)
    ((:int :bits :flags :bool :i128 :u128 :timestamp :fixed :float) (encode-scalar td v where))
    (:enum (encode-enum-ref (second td) v where))
    (:string (check-text v (second td) where) (copy-seq v))
    (:bytes (encode-array-body 6 (coerce v 'list) (lambda (x) (octets x))))
    (:struct (write-body v))
    (:union (if (null v) (octets 0) (encode-union (second td) v where)))
    (:ptr (let ((b (make-buf))) (buf-leb b (if v (gethash v *numbering*) 0)) (buf-octets b)))
    ;; strip the field-level length: an arm's L stands in for it
    (:array (strip-len (encode-array td v where t)))
    (:map (encode-map td v where))))

(defun strip-len (bytes)
  (let ((*buf* bytes)) (multiple-value-bind (l p) (rd-leb 0 (length bytes)) (declare (ignore l)) (subseq bytes p))))

(defun encode-array-body (ek elems encoder)
  (let ((b (make-buf)))
    (buf-byte b ek)
    (buf-leb b (length elems))
    (dolist (e elems) (buf-bytes b (funcall encoder e)))
    (buf-octets b)))

(defun encode-element (elem e where)
  "One array element (§22.2.1): never elided."
  (ecase (first elem)
    ((:int :bits :flags :bool :i128 :u128 :timestamp :fixed :float) (encode-scalar elem e where))
    (:enum (encode-enum-ref (second elem) e where))
    (:string (check-text e (second elem) where) (with-len e))
    (:struct (with-len (write-body e)))
    (:union (if (null e) (octets 0) (encode-union (second elem) e where)))
    (:ptr (let ((b (make-buf))) (buf-leb b (if e (gethash e *numbering*) 0)) (buf-octets b)))
    (:optional                                   ; a text list's ?string(M) element
     (if (null e)
         (with-len (octets 0))
         (progn (check-text e (second (second elem)) where)
                (let ((b (make-buf)))
                  (buf-leb b (intern-id +value-id+)) (buf-byte b 12)
                  (buf-bytes b (with-len e)) (buf-byte b 0)
                  (with-len (buf-octets b))))))))

(defun encode-array (td v where &optional always)
  (destructuring-bind (mode lo hi elem) (rest td)
    (ecase mode
      (:keyed
       (let ((present (loop for i from 0 below (length v)
                            for e = (aref v i)
                            unless (value-equal elem e (type-default elem)) collect i)))
         (if (and (null present) (not always))
             :elide
             (let ((b (make-buf)))
               (buf-byte b (elem-kind elem))
               (buf-leb b (length present))
               (dolist (i present)
                 (buf-leb b (intern-id (variant-wire-id (aref (edecl-variants hi) i))))
                 (let ((p (encode-keyed-element elem (aref v i) where)))
                   (buf-leb b (length p)) (buf-bytes b p)))
               (with-len (buf-octets b))))))
      (:fixed
       (when (/= (length v) hi) (invalid "~a: fixed array length" where))
       (if (and (/= (elem-kind elem) 13) (not always)
                (every (lambda (e) (value-equal elem e (type-default elem))) v))
           :elide
           (with-len (encode-array-body (elem-kind elem) (coerce v 'list) (lambda (e) (encode-element elem e where))))))
      ((:counted :unbounded)
       (when (and hi (> (length v) hi)) (invalid "~a: count past its bound" where))
       (when (< (length v) lo) (invalid "~a: count below its bound" where))
       (if (and (zerop (length v)) (not always))
           :elide
           (with-len (encode-array-body (elem-kind elem) (coerce v 'list) (lambda (e) (encode-element elem e where)))))))))

(defun encode-keyed-element (elem e where)
  (ecase (first elem)
    ((:int :bits :flags :bool :i128 :u128 :timestamp :fixed :float) (encode-scalar elem e where))
    (:enum (encode-enum-ref (second elem) e where))
    (:struct (write-body e))))

(defun encode-map (td v where)
  "§22.2.1: kind 14 over element kind 13, entries ascending, each an ordinary
body of `key` then `value` under the elision table."
  (destructuring-bind (ktd vtd) (rest td)
    (let ((entries (stable-sort (copy-list v) (lambda (a b) (key-less-p ktd (car a) (car b))))))
      (loop for (a b) on entries while b
            unless (key-less-p ktd (car a) (car b)) do (invalid "~a: map key twice" where))
      (encode-array-body 13 entries
                         (lambda (e)
                           (let ((b (make-buf)))
                             (write-entry-field b +key-id+ ktd (car e) where)
                             (write-entry-field b +value-id+ vtd (cdr e) where)
                             (buf-byte b 0)
                             (with-len (buf-octets b))))))))

(defun write-entry-field (buf id td v where)
  (let ((pseudo (make-field :name where :wire-id id :type td
                            :default (if (eq (first td) :optional) :absent (type-default td)))))
    (if (eq (first td) :optional)
        (unless (eq v :absent) (write-header-and-payload buf pseudo (second td) v :always t))
        (write-header-and-payload buf pseudo td v))))

(defun wire-save (tv)
  "Save a root value as a form-1 file (§22.1)."
  (let ((*intern* (make-array 16 :adjustable t :fill-pointer 0))
        (*intern-ix* (make-hash-table))
        (*numbering* (make-hash-table :test 'eq))
        (*nodes* (make-array 8 :adjustable t :fill-pointer 0)))
    (let ((open (make-hash-table :test 'eq)))
      (setf (gethash tv open) t)
      (number-walk tv open))
    (let ((out (make-buf)))
      (buf-byte out 1)
      (buf-bytes out (write-body tv t))
      (loop for id across *intern* do (buf-le out id 8))
      (buf-le out (length *intern*) 8)
      (when (sdecl-integrity (tval-decl tv))
        (buf-le out (crc32c out) 4))
      (buf-octets out))))
