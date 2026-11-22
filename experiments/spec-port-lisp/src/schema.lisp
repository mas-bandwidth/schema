;;;; schema.lisp -- schema intake: lexer, parser and resolver for .schema text.
;;;;
;;;; SPEC-GAP 2 (DEFERS-TO-CODE): SPEC.md §7.1 says backends consume the Go
;;;; `ir` package's fully-resolved IR and nothing else; no serialized IR is
;;;; specified, so a non-Go port has no way to "receive" it. This port parses
;;;; the .schema text itself from SPEC.md §4.1-§4.2's grammar.

(in-package :spec-port)

;;; ------------------------------------------------------------------
;;; Lexer (SPEC.md §4.1)

(defstruct (tok (:constructor make-tok (type val line)))
  type val line)

(defun lex-schema (text)
  (let ((toks '()) (i 0) (n (length text)) (line 1))
    (labels ((peekc (&optional (k 0)) (if (< (+ i k) n) (char text (+ i k)) nil))
             (emit (type val) (push (make-tok type val line) toks))
             (ident-start-p (c) (and c (or (alpha-char-p c) (char= c #\_))))
             (ident-char-p (c) (and c (or (alphanumericp c) (char= c #\_)))))
      (loop
        (let ((c (peekc)))
          (cond
            ((null c) (emit :nl nil) (emit :eof nil) (return))
            ((char= c #\Newline) (emit :nl nil) (incf line) (incf i))
            ((member c '(#\Space #\Tab #\Return)) (incf i))
            ((and (char= c #\/) (eql (peekc 1) #\/))
             (loop while (and (peekc) (char/= (peekc) #\Newline)) do (incf i)))
            ((and (char= c #\/) (eql (peekc 1) #\*))
             (incf i 2)
             (loop until (or (null (peekc)) (and (eql (peekc) #\*) (eql (peekc 1) #\/)))
                   do (when (char= (peekc) #\Newline) (incf line)) (incf i))
             (incf i 2))
            ((ident-start-p c)
             (let ((s i)) (loop while (ident-char-p (peekc)) do (incf i))
               (emit :ident (subseq text s i))))
            ((digit-char-p c)
             (let ((s i))
               (cond ((and (char= c #\0) (member (peekc 1) '(#\x #\X)))
                      (incf i 2) (loop while (and (peekc) (digit-char-p (peekc) 16)) do (incf i))
                      (emit :int (parse-integer text :start (+ s 2) :end i :radix 16)))
                     ((and (char= c #\0) (member (peekc 1) '(#\b #\B)))
                      (incf i 2) (loop while (and (peekc) (digit-char-p (peekc) 2)) do (incf i))
                      (emit :int (parse-integer text :start (+ s 2) :end i :radix 2)))
                     (t
                      (loop while (and (peekc) (digit-char-p (peekc))) do (incf i))
                      (let ((floatp nil))
                        (when (and (eql (peekc) #\.) (peekc 1) (digit-char-p (peekc 1)))
                          (setf floatp t) (incf i)
                          (loop while (and (peekc) (digit-char-p (peekc))) do (incf i)))
                        (when (and (member (peekc) '(#\e #\E))
                                   (or (and (peekc 1) (digit-char-p (peekc 1)))
                                       (and (member (peekc 1) '(#\+ #\-)) (peekc 2) (digit-char-p (peekc 2)))))
                          (setf floatp t) (incf i)
                          (when (member (peekc) '(#\+ #\-)) (incf i))
                          (loop while (and (peekc) (digit-char-p (peekc))) do (incf i)))
                        (if floatp
                            (emit :float (parse-decimal-rational (subseq text s i)))
                            (emit :int (parse-integer text :start s :end i))))))))
            ((char= c #\")
             (let ((s (1+ i)))
               (incf i)
               (loop while (and (peekc) (char/= (peekc) #\")) do (incf i))
               (emit :string (subseq text s i))
               (incf i)))
            ((and (char= c #\.) (eql (peekc 1) #\.)) (emit :punct "..") (incf i 2))
            ((and (char= c #\:) (eql (peekc 1) #\:)) (emit :punct "::") (incf i 2))
            (t (emit :punct (string c)) (incf i))))))
    (coerce (nreverse toks) 'vector)))

(defun parse-decimal-rational (s)
  "Exact value of a decimal literal: digits [. digits] [e [+-] digits]."
  (let* ((epos (position-if (lambda (c) (member c '(#\e #\E))) s))
         (mant (subseq s 0 (or epos (length s))))
         (ex (if epos (parse-integer s :start (1+ epos)) 0))
         (dot (position #\. mant))
         (int-part (if dot (subseq mant 0 dot) mant))
         (frac-part (if dot (subseq mant (1+ dot)) ""))
         (digits (concatenate 'string int-part frac-part))
         (v (if (string= digits "") 0 (parse-integer digits))))
    (* v (expt 10 (- ex (length frac-part))))))

;;; ------------------------------------------------------------------
;;; Parser (SPEC.md §4.2) -> a list of declaration forms

(defvar *toks*)
(defvar *pos*)

(defun pk (&optional (k 0)) (aref *toks* (min (+ *pos* k) (1- (length *toks*)))))
(defun pk-is (type &optional val (k 0))
  (let ((tk (pk k))) (and (eq (tok-type tk) type) (or (null val) (equal (tok-val tk) val)))))
(defun nxt () (prog1 (pk) (incf *pos*)))
(defun expect (type &optional val)
  (unless (pk-is type val)
    (error "schema parse: expected ~a ~a at line ~a, got ~a ~s"
           type (or val "") (tok-line (pk)) (tok-type (pk)) (tok-val (pk))))
  (nxt))
(defun skip-nl () (loop while (pk-is :nl) do (nxt)))
(defun punct-p (v &optional (k 0)) (pk-is :punct v k))

(defun parse-schema-text (text)
  (let ((*toks* (lex-schema text)) (*pos* 0) (decls '()))
    (loop
      (skip-nl)
      (when (pk-is :eof) (return))
      (push (parse-declaration) decls))
    (nreverse decls)))

(defun parse-declaration ()
  (let ((tk (pk)))
    (unless (eq (tok-type tk) :ident) (error "schema parse: declaration expected at line ~a" (tok-line tk)))
    (let ((w (tok-val tk)))
      (cond
        ((string= w "package") (nxt) (list :package (tok-val (expect :ident))))
        ((string= w "const") (nxt)
         (let ((name (tok-val (expect :ident))) (ctype nil))
           (unless (punct-p "=") (setf ctype (tok-val (expect :ident))))
           (expect :punct "=")
           (let ((e (parse-expr)) (attrs (parse-attr-section)))
             (list :const name e ctype attrs))))
        ((or (string= w "enum") (string= w "flags")) (nxt)
         (let* ((name (tok-val (expect :ident))) (attrs (parse-attr-section)))
           (skip-nl)
           (list (if (string= w "enum") :enum :flags) name attrs (parse-variant-list))))
        ((string= w "union") (nxt)
         (let* ((name (tok-val (expect :ident))) (attrs (parse-attr-section)))
           (skip-nl)
           (list :union name attrs (parse-union-block))))
        ((or (string= w "type") (string= w "table")
             (and (string= w "fixed") (pk-is :ident "table" 1)))
         (let ((class (cond ((string= w "type") :type) ((string= w "table") :table) (t :fixed))))
           (nxt) (when (eq class :fixed) (nxt))
           (let* ((name (tok-val (expect :ident))) (attrs (parse-attr-section)))
             (skip-nl)
             (list :struct name class attrs (parse-block)))))
        (t (error "schema parse: unknown declaration ~s at line ~a" w (tok-line tk)))))))

(defun parse-attr-section ()
  "| attr, attr ... to the end of the line. SPEC-GAP 3 (CONTRADICTION):
SPEC.md §4.1 says the pipe claims the rest of the line, yet the corpus's
Features.schema writes `table Root | forms=..., integrity=\"crc32c\" {`; this
parser ends a section at `{` as well as at a newline."
  (let ((attrs '()))
    (when (punct-p "|")
      (nxt)
      (loop
        (let ((key (tok-val (expect :ident))))
          (if (punct-p "=")
              (progn (nxt)
                     (cond ((pk-is :string) (push (cons key (list :string (tok-val (nxt)))) attrs))
                           ((and (pk-is :ident) (punct-p "::" 1))
                            (let ((parts (list (tok-val (nxt)))))
                              (loop while (punct-p "::") do (nxt) (push (tok-val (expect :ident)) parts))
                              (push (cons key (list :qual (reverse parts))) attrs)))
                           (t (push (cons key (list :expr (parse-expr))) attrs))))
              (push (cons key nil) attrs)))
        (if (punct-p ",") (nxt) (return))))
    (nreverse attrs)))

(defun parse-variant-list ()
  (expect :punct "{")
  (let ((vs '()))
    (loop
      (loop while (or (pk-is :nl) (punct-p ",")) do (nxt))
      (when (punct-p "}") (nxt) (return))
      (let ((name (tok-val (expect :ident))))
        (push (list name (parse-attr-section)) vs)))
    (nreverse vs)))

(defun arm-type-start-p ()
  (not (or (pk-is :nl) (punct-p "|") (punct-p "}"))))

(defun parse-union-block ()
  (expect :punct "{")
  (let ((arms '()))
    (loop
      (skip-nl)
      (when (punct-p "}") (nxt) (return))
      (let* ((name (tok-val (expect :ident)))
             (type (when (arm-type-start-p) (parse-type)))
             (attrs (parse-attr-section)))
        (push (list name type attrs) arms)))
    (nreverse arms)))

(defun parse-block ()
  (expect :punct "{")
  (let ((items '()))
    (loop
      (skip-nl)
      (when (punct-p "}") (nxt) (return))
      (progn
        (cond
          ((and (pk-is :ident "if"))
           (nxt)
           (let ((neg nil))
             (when (punct-p "!") (nxt) (setf neg t))
             (let ((g (tok-val (expect :ident))))
               (skip-nl)
               (let ((then (parse-block)) (else nil))
                 (let ((save *pos*))
                   (skip-nl)
                   (if (pk-is :ident "else")
                       (progn (nxt) (skip-nl) (setf else (parse-block)))
                       (setf *pos* save)))
                 (push (list :if g neg then else) items)))))
          ((and (pk-is :ident "const") (punct-p "(" 1))
           (nxt) (nxt) (let ((v (parse-expr))) (expect :punct ",") (let ((b (parse-expr))) (expect :punct ")")
                                                                     (push (list :constfield v b) items))))
          ((and (pk-is :ident "reserved") (punct-p "(" 1))
           (nxt) (nxt) (let ((b (parse-expr))) (expect :punct ")") (push (list :reserved b) items)))
          ((and (pk-is :ident "align") (or (pk-is :nl nil 1) (punct-p "}" 1)))
           (nxt) (push (list :align) items))
          (t
           (let* ((name (tok-val (expect :ident)))
                  (type (parse-type))
                  (default (when (punct-p "=") (nxt) (parse-default)))
                  (attrs (parse-attr-section)))
             
             (push (list :field name type default attrs) items))))))
    (nreverse items)))

(defun parse-default ()
  (cond ((pk-is :string) (list :string (tok-val (nxt))))
        ((punct-p "{")
         (nxt)
         (let ((names '()))
           (loop (loop while (or (punct-p ",") (pk-is :nl)) do (nxt))
                 (when (punct-p "}") (nxt) (return))
                 (push (tok-val (expect :ident)) names))
           (list :set (nreverse names))))
        (t (list :expr (parse-expr)))))

(defparameter *int-types*
  '(("int8" t 8) ("int16" t 16) ("int32" t 32) ("int64" t 64)
    ("uint8" nil 8) ("uint16" nil 16) ("uint32" nil 32) ("uint64" nil 64)))

(defun parse-paren-args (count)
  (expect :punct "(")
  (let ((args (list (parse-expr))))
    (loop repeat (1- count) do (expect :punct ",") (push (parse-expr) args))
    (expect :punct ")")
    (nreverse args)))

(defun parse-type ()
  "Type = [?] [ [Bound] ] [?] (Scalar | Map), as a list form."
  (let ((optional nil) (bound nil) (elem-optional nil))
    (when (punct-p "?") (nxt) (setf optional t))
    (when (punct-p "[")
      (nxt)
      (setf bound
            (cond ((punct-p "]") (list :unbounded))
                  ((punct-p "..") (nxt) (list :upto (parse-expr)))
                  (t (let ((a (parse-expr)))
                       (if (punct-p "..")
                           (progn (nxt) (list :range a (parse-expr)))
                           (list :exact a))))))
      (expect :punct "]")
      (when (punct-p "?") (nxt) (setf elem-optional t)))
    (let ((base (if (pk-is :ident "map")
                    (progn (nxt) (expect :punct "[")
                           (let ((k (parse-scalar))) (expect :punct "]")
                             (list :map k (parse-type))))
                    (parse-scalar))))
      (when elem-optional (setf base (list :optional base)))
      (when bound (setf base (list :array bound base)))
      (when optional (setf base (list :optional base)))
      base)))

(defun parse-scalar ()
  (cond
    ((punct-p "*") (nxt) (list :ptr (tok-val (expect :ident))))
    (t
     (let ((w (tok-val (expect :ident))))
       (let ((it (assoc w *int-types* :test #'string=)))
         (cond
           (it (list :int (second it) (third it)))
           ((string= w "int128") (list :i128))
           ((string= w "uint128") (list :u128))
           ((string= w "bool") (list :bool))
           ((string= w "float32") (list :float 32))
           ((string= w "float64") (list :float 64))
           ((string= w "timestamp") (list :timestamp))
           ((string= w "bits") (list :bits (first (parse-paren-args 1))))
           ((string= w "string") (if (punct-p "(") (list :string (first (parse-paren-args 1))) (list :string nil)))
           ((string= w "bytes") (if (punct-p "(") (list :bytes (first (parse-paren-args 1))) (list :bytes nil)))
           ((string= w "fixed") (cons :fixed (cons t (parse-paren-args 2))))
           ((string= w "ufixed") (cons :fixed (cons nil (parse-paren-args 2))))
           (t (list :named w))))))))

;;; Expressions: + - * / % with unary minus, parentheses, E.Max, F.Count.

(defun parse-expr () (parse-additive))
(defun parse-additive ()
  (let ((l (parse-multiplicative)))
    (loop while (or (punct-p "+") (punct-p "-"))
          do (let ((op (tok-val (nxt)))) (setf l (list (if (string= op "+") :+ :-) l (parse-multiplicative)))))
    l))
(defun parse-multiplicative ()
  (let ((l (parse-unary)))
    (loop while (or (punct-p "*") (punct-p "/") (punct-p "%"))
          do (let ((op (tok-val (nxt))))
               (setf l (list (cond ((string= op "*") :*) ((string= op "/") :/) (t :%)) l (parse-unary)))))
    l))
(defun parse-unary ()
  (cond ((punct-p "-") (nxt) (list :neg (parse-unary)))
        ((punct-p "+") (nxt) (parse-unary))
        (t (parse-primary))))
(defun parse-primary ()
  (cond ((pk-is :int) (list :lit (tok-val (nxt))))
        ((pk-is :float) (list :flit (tok-val (nxt))))
        ((punct-p "(") (nxt) (prog1 (parse-expr) (expect :punct ")")))
        ((pk-is :ident)
         (let ((name (tok-val (nxt))))
           (if (and (punct-p ".") (pk-is :ident nil 1))
               (progn (nxt) (list :member name (tok-val (nxt))))
               (list :ref name))))
        (t (error "schema parse: expression expected at line ~a" (tok-line (pk))))))

;;; ------------------------------------------------------------------
;;; Resolved declarations

(defvar *unit*)

(defstruct unit
  (decls (make-hash-table :test 'equal))  ; name -> decl
  (consts (make-hash-table :test 'equal)) ; name -> (expr . value-or-:pending)
  (tables-by-id (make-hash-table)))       ; wire type id -> struct decl (table/fixed)

(defstruct sdecl name class attrs fields was wire-id json-array integrity
           (by-id (make-hash-table)) (by-key (make-hash-table :test 'equal)) resolved)
(defstruct field name index type default-form default attrs wire-id key guard required total deprecated)
(defstruct edecl name variants max)          ; variants: vector of variant, index = ordinal-1
(defstruct variant name ordinal wire-id deprecated)
(defstruct fdecl name variants width)        ; variants: list of names, bit order
(defstruct udecl name arms)                  ; arms: vector of arm, index = tag-1
(defstruct arm name tag type wire-id)

(defun attr (attrs key) (assoc key attrs :test #'string=))
(defun attr-string (attrs key)
  (let ((a (attr attrs key))) (when (and a (eq (first (cdr a)) :string)) (second (cdr a)))))

;;; Unit loading

(defun schema-files (path)
  (let ((p (probe-file path)))
    (unless p (error "schema: no such path ~a" path))
    (if (and (null (pathname-name p)) (null (pathname-type p)))
        (sort (mapcar #'namestring (directory (merge-pathnames "*.schema" p))) #'string<)
        (list (namestring p)))))

(defun read-text-file (path)
  (octets-to-string (read-file-octets path)))

(defun load-unit (paths)
  "PATHS: the schema paths of one manifest `unit` line (dirs or files)."
  (let ((unit (make-unit)) (forms '()))
    (dolist (p paths)
      (dolist (f (schema-files p))
        (setf forms (append forms (parse-schema-text (read-text-file f))))))
    (dolist (d forms)
      (ecase (first d)
        (:package nil)
        (:const (setf (gethash (second d) (unit-consts unit)) (cons (third d) :unresolved)))
        (:enum (setf (gethash (second d) (unit-decls unit)) (list :enum-form d)))
        (:flags (setf (gethash (second d) (unit-decls unit)) (list :flags-form d)))
        (:union (setf (gethash (second d) (unit-decls unit)) (list :union-form d)))
        (:struct (setf (gethash (second d) (unit-decls unit)) (list :struct-form d)))))
    ;; build enum/flags first (consts may use E.Max), then structs and unions
    (let ((*unit* unit))
      (maphash (lambda (k v) (declare (ignore v)) (find-decl k)) (unit-decls unit))
      (maphash (lambda (k v)
                 (declare (ignore k))
                 (when (sdecl-p v) (resolve-struct v)))
               (unit-decls unit))
      (maphash (lambda (k v)
                 (declare (ignore k))
                 (when (udecl-p v) (resolve-union v)))
               (unit-decls unit))
      (maphash (lambda (k v)
                 (declare (ignore k))
                 (when (and (sdecl-p v) (member (sdecl-class v) '(:table :fixed)))
                   (setf (gethash (sdecl-wire-id v) (unit-tables-by-id unit)) v)))
               (unit-decls unit)))
    unit))

(defun find-decl (name &optional (unit *unit*))
  "The resolved declaration NAME; shells are created lazily so references
are order-free (SPEC.md §4.2)."
  (let ((d (gethash name (unit-decls unit))))
    (cond
      ((null d) (error "schema: unknown name ~a" name))
      ((and (consp d) (keywordp (first d)))
       (let ((form (second d)))
         (ecase (first d)
           (:enum-form
            (let* ((vs (fourth form))
                   (e (make-edecl :name name :max (length vs))))
              (setf (edecl-variants e)
                    (coerce (loop for (vname vattrs) in vs for i from 1
                                  collect (make-variant :name vname :ordinal i
                                                        :wire-id (fnv1a64 (or (attr-string vattrs "was") vname))
                                                        :deprecated (and (attr vattrs "deprecated") t)))
                            'vector))
              (setf (gethash name (unit-decls unit)) e)))
           (:flags-form
            (let* ((vs (mapcar #'first (fourth form)))
                   (mx (attr (third form) "max")))
              (setf (gethash name (unit-decls unit))
                    (make-fdecl :name name :variants vs
                                :width (if mx (eval-int (second (cdr mx))) (length vs))))))
           (:union-form
            (setf (gethash name (unit-decls unit)) (make-udecl :name name :arms form)))
           (:struct-form
            (let* ((attrs (fourth form))
                   (was (attr-string attrs "was")))
              (setf (gethash name (unit-decls unit))
                    (make-sdecl :name name :class (third form) :attrs attrs
                                :fields (fifth form) :was was
                                :wire-id (fnv1a64 (or was name))
                                :json-array (equal (attr-string attrs "json") "array")
                                :integrity (equal (attr-string attrs "integrity") "crc32c"))))))))
      (t d))))

;;; Constant evaluation (SPEC.md §4.2): exact, integers and rationals.

(defun eval-expr (e)
  (ecase (first e)
    (:lit (second e))
    (:flit (second e))
    (:neg (- (eval-expr (second e))))
    (:+ (+ (eval-expr (second e)) (eval-expr (third e))))
    (:- (- (eval-expr (second e)) (eval-expr (third e))))
    (:* (* (eval-expr (second e)) (eval-expr (third e))))
    (:/ (let ((a (eval-expr (second e))) (b (eval-expr (third e))))
          (if (and (integerp a) (integerp b)) (truncate a b) (/ a b))))
    (:% (rem (eval-expr (second e)) (eval-expr (third e))))
    (:member (let ((d (find-decl (second e))) (m (third e)))
               (cond ((and (edecl-p d) (string= m "Max")) (edecl-max d))
                     ((and (fdecl-p d) (string= m "Count")) (length (fdecl-variants d)))
                     (t (error "schema: bad member ~a.~a" (second e) m)))))
    (:ref (let ((c (gethash (second e) (unit-consts *unit*))))
            (unless c (error "schema: unknown constant ~a" (second e)))
            (when (eq (cdr c) :pending) (error "schema: constant cycle at ~a" (second e)))
            (when (eq (cdr c) :unresolved)
              (setf (cdr c) :pending)
              (setf (cdr c) (eval-expr (car c))))
            (cdr c)))))

(defun eval-int (e)
  (let ((v (eval-expr e)))
    (unless (integerp v) (error "schema: integer expected, got ~a" v))
    v))

;;; Type descriptors

(defun int-kind (signed width)
  (if signed (ecase width (8 2) (16 3) (32 4) (64 5)) (ecase width (8 6) (16 7) (32 8) (64 9))))

(defun resolve-type (form attrs &key ((:table-field tf) nil))
  (declare (ignore tf))
  "FORM from the parser plus the field's ATTRS -> a type descriptor (list)."
  (labels ((rng () (let ((mn (attr attrs "min")) (mx (attr attrs "max")))
                     (values (and mn (eval-expr (second (cdr mn)))) (and mx (eval-expr (second (cdr mx))))))))
    (ecase (first form)
      (:int (destructuring-bind (signed width) (rest form)
              (multiple-value-bind (mn mx) (rng)
                (let ((lo (if signed (- (expt 2 (1- width))) 0))
                      (hi (if signed (1- (expt 2 (1- width))) (1- (expt 2 width)))))
                  ;; SPEC.md §4.6: one-sided ranges on table fields; `max` alone on unsigned.
                  
                  (when (and mx (not mn)) (setf mn lo))
                  (when (and mn (not mx)) (setf mx hi))
                  (list :int signed width (or mn lo) (or mx hi))))))
      (:i128 (multiple-value-bind (mn mx) (rng)
               (list :i128 (or mn (- (expt 2 127))) (or mx (1- (expt 2 127))))))
      (:u128 (list :u128))
      (:bool (list :bool))
      (:timestamp (list :timestamp))
      (:float (let ((res (attr attrs "resolution")))
                (multiple-value-bind (mn mx) (rng)
                  (list :float (second form) (when (and res mn mx) (list mn mx (eval-expr (second (cdr res)))))))))
      (:bits (list :bits (eval-int (second form))))
      (:string (list :string (and (second form) (eval-int (second form)))))
      (:bytes (list :bytes (and (second form) (eval-int (second form)))))
      (:fixed (destructuring-bind (signed i f) (rest form)
                (let ((ii (eval-int i)) (ff (eval-int f)))
                  (multiple-value-bind (mn mx) (rng)
                    (let* ((w (+ ii ff))
                           (lo (if signed (- (expt 2 (1- w))) 0))
                           (hi (if signed (1- (expt 2 (1- w))) (1- (expt 2 w)))))
                      (list :fixed signed ii ff
                            (if mn (* mn (expt 2 ff)) lo)
                            (if mx (* mx (expt 2 ff)) hi)))))))
      (:ptr (let ((name (second form)))
              (cond ((string= name "bytes") (list :ptr :bytes))
                    ((string= name "string") (list :ptr :string))
                    (t (list :ptr (find-decl name))))))
      (:named (let ((d (find-decl (second form))))
                (etypecase d
                  (edecl (list :enum d))
                  (fdecl (list :flags d))
                  (sdecl (list :struct d))
                  (udecl (list :union d)))))
      (:optional (list :optional (resolve-type (second form) attrs)))
      (:map (list :map (resolve-type (second form) nil) (resolve-type (third form) attrs)))
      (:array
       (let* ((bound (second form))
              (elem (resolve-type (third form) attrs)))
         (ecase (first bound)
           (:unbounded (list :array :unbounded 0 nil elem))
           (:upto (list :array :counted 0 (eval-int (second bound)) elem))
           (:range (list :array :counted (eval-int (second bound)) (eval-int (third bound)) elem))
           (:exact
            (let ((b (second bound)))
              (if (and (eq (first b) :ref) (nth-value 1 (gethash (second b) (unit-decls *unit*)))
                       (edecl-p (find-decl (second b))))
                  (list :array :keyed 0 (find-decl (second b)) elem)
                  (let ((n (eval-int b))) (list :array :fixed n n elem)))))))))))

(defun type-kind (td)
  "§3's mapping from a declaration-side type to the neutral wire kind."
  (ecase (first td)
    (:int (int-kind (second td) (third td)))
    (:bits (let ((n (second td))) (cond ((<= n 8) 6) ((<= n 16) 7) ((<= n 32) 8) (t 9))))
    (:bool 1)
    (:float (if (= (second td) 32) 10 11))
    (:enum 30)
    (:flags 9)
    (:string 12)
    (:bytes 14)
    (:struct 13)
    (:union 15)
    (:array (if (eq (second td) :keyed) 16 14))
    (:map 14)
    (:ptr 17)
    (:i128 18) (:u128 19)
    (:fixed (let ((w (+ (third td) (fourth td))))
              (+ (if (second td) 20 25) (ecase w (8 0) (16 1) (32 2) (64 3) (128 4)))))
    (:timestamp 36)
    (:optional (type-kind (second td)))))

(defun elem-kind (td)
  "The element kind an array of TD carries: a `?string(M)` element is a
one-field body (§2.13), kind 13."
  (if (eq (first td) :optional) 13 (type-kind td)))

(defun resolve-struct (s)
  (unless (sdecl-resolved s)
    (setf (sdecl-resolved s) t)
    (let ((out '()) (idx 0))
      (labels ((walk (items guard)
                 (dolist (it items)
                   (ecase (first it)
                     (:field
                      (destructuring-bind (name tform dform attrs) (rest it)
                        (let* ((td (resolve-type tform attrs :table-field t))
                               (was (attr-string attrs "was"))
                               (f (make-field :name name :index idx :type td :default-form dform
                                              :attrs attrs :wire-id (fnv1a64 (or was name))
                                              :key (or (attr-string attrs "json") name)
                                              :guard guard
                                              :required (and (attr attrs "required") t)
                                              :deprecated (and (attr attrs "deprecated") t)
                                              :total (let ((tt (attr attrs "total"))) (and tt (eval-int (second (cdr tt))))))))
                          (incf idx)
                          (push f out))))
                     (:if
                      (destructuring-bind (g neg then else) (rest it)
                        (walk then (append guard (list (cons g (not neg)))))
                        (when else (walk else (append guard (list (cons g neg)))))))
                     ((:constfield :reserved :align) nil)))))
        (walk (sdecl-fields s) '()))
      (let ((fields (coerce (nreverse out) 'vector)))
        ;; guards name an earlier field of the same body: resolve to indices
        (loop for f across fields
              do (setf (field-guard f)
                       (mapcar (lambda (g) (cons (field-index (find (car g) fields :key #'field-name :test #'string=))
                                                 (cdr g)))
                               (field-guard f))))
        (setf (sdecl-fields s) fields)
        (loop for f across fields
              do (setf (gethash (field-wire-id f) (sdecl-by-id s)) f)
                 (setf (gethash (field-key f) (sdecl-by-key s)) f)
                 (setf (field-default f) (field-default-value f)))))))

(defun resolve-union (u)
  (when (consp (udecl-arms u))
    (let ((form (udecl-arms u)))
      (setf (udecl-arms u)
            (coerce (loop for (name tform attrs) in (fourth form) for i from 1
                          collect (make-arm :name name :tag i
                                            :type (and tform (resolve-type tform attrs))
                                            :wire-id (fnv1a64 (or (attr-string attrs "was") name))))
                    'vector))))
  u)

(defun union-arms (u) (resolve-union u) (udecl-arms u))
(defun struct-fields (s) (resolve-struct s) (sdecl-fields s))
