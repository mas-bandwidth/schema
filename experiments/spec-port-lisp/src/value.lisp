;;;; value.lisp -- in-memory values, construction defaults and elision tests.
;;;;
;;;; A value of a table or type is a TVAL: its declaration and one slot per
;;;; field, in declaration order. Scalars are integers (floats as their bit
;;;; patterns, fixed-point as the raw scaled integer, timestamps as
;;;; nanoseconds), text and bytes are octet vectors, an optional field holds
;;;; :absent or its value, a union is NIL (None) or (tag . payload), an array
;;;; is a simple-vector, a map is a list of (key . value), and a pointer slot
;;;; holds NIL, a TVAL (node identity is EQ identity) or a BLOB.

(in-package :spec-port)

(defstruct tval decl slots)
(defstruct blob kind data)          ; kind :bytes or :string

(defun float-default-bits (value width)
  (let ((b (rational-to-float-bits (rational value) width)))
    (when (eq b :inf) (error "schema: float default out of range"))
    b))

(defun type-default (td)
  "SPEC.md §4.2's construction rule for a type with no field-level default."
  (ecase (first td)
    ((:int :bits :i128 :u128 :timestamp :fixed :enum :flags :float) 0)
    (:bool nil)
    ((:string :bytes) (make-octets 0))
    (:struct (make-default-tval (second td)))
    (:union nil)
    (:map nil)
    (:ptr nil)
    (:optional :absent)
    (:array
     (destructuring-bind (mode lo hi elem) (rest td)
       (ecase mode
         (:fixed (let ((v (make-array hi))) (dotimes (i hi v) (setf (aref v i) (type-default elem)))))
         (:keyed (let* ((n (edecl-max hi)) (v (make-array n))) (dotimes (i n v) (setf (aref v i) (type-default elem)))))
         ;; SPEC.md §4.2: a [A..B] count is born at A
         (:counted (let ((v (make-array lo))) (dotimes (i lo v) (setf (aref v i) (type-default elem)))))
         (:unbounded (vector)))))))

(defun field-default-value (f)
  "The fresh value of field F: its specified default, else the type's."
  (let ((td (field-type f)) (d (field-default-form f)))
    (if (null d)
        (type-default td)
        (ecase (first td)
          ((:int :bits :i128 :u128 :timestamp) (eval-int (second d)))
          (:fixed (* (eval-expr (second d)) (expt 2 (fourth td))))
          (:float (float-default-bits (eval-expr (second d)) (second td)))
          (:bool (let ((e (second d)))
                   (cond ((equal e '(:ref "true")) t) ((equal e '(:ref "false")) nil)
                         (t (error "schema: bad bool default")))))
          (:enum (let* ((name (second (second d)))
                        (v (find name (edecl-variants (second td)) :key #'variant-name :test #'string=)))
                   (unless v (error "schema: unknown variant ~a" name))
                   (variant-ordinal v)))
          (:flags (let ((names (fdecl-variants (second td))) (mask 0))
                    (dolist (n (second d) mask)
                      (setf mask (logior mask (ash 1 (position n names :test #'string=)))))))
          ((:string :bytes) (string-to-octets (second d)))))))

(defun make-default-tval (decl)
  (let* ((fields (struct-fields decl))
         (slots (make-array (length fields))))
    (loop for f across fields for i from 0
          do (setf (aref slots i) (copy-default (field-default f))))
    (make-tval :decl decl :slots slots)))

(defun copy-default (v)
  "Defaults are shared templates; hand out fresh mutable copies."
  (cond ((tval-p v) (make-tval :decl (tval-decl v) :slots (map 'vector #'copy-default (tval-slots v))))
        ((and (vectorp v) (not (typep v 'octets))) (map 'vector #'copy-default v))
        ((typep v 'octets) (copy-seq v))
        (t v)))

(defun guard-true-p (tv f)
  "§3: a field under a false guard is elided; SPEC.md §4.4 guards are bools."
  (every (lambda (g) (eq (and (aref (tval-slots tv) (car g)) t) (cdr g))) (field-guard f)))

;;; Default comparison, for the writer's elision table (§22.2).

(defun compressed-p (td) (and (eq (first td) :float) (third td)))

(defun float-equal-default (td v d)
  (if (compressed-p td)
      ;; compared ==, so -0 equals +0 (§22.2's compressed row)
      (let ((w (second td)))
        (= (float-bits-rational v w) (float-bits-rational d w)))
      (= v d)))

(defun value-equal (td a b)
  "Equality of two values of type TD, floats by bit pattern."
  (ecase (first td)
    ((:int :bits :i128 :u128 :timestamp :fixed :enum :flags) (eql a b))
    (:float (float-equal-default td a b))
    (:bool (eq (and a t) (and b t)))
    ((:string :bytes) (equalp a b))
    (:struct (loop for f across (struct-fields (second td))
                   always (value-equal (field-type f) (aref (tval-slots a) (field-index f))
                                       (aref (tval-slots b) (field-index f)))))
    (:union (or (and (null a) (null b))
                (and a b (= (car a) (car b))
                     (let ((arm (aref (union-arms (second td)) (1- (car a)))))
                       (or (null (arm-type arm)) (value-equal (arm-type arm) (cdr a) (cdr b)))))))
    (:optional (or (and (eq a :absent) (eq b :absent))
                   (and (not (eq a :absent)) (not (eq b :absent)) (value-equal (second td) a b))))
    (:ptr (eq a b))
    (:map (and (= (length a) (length b))
               (every (lambda (x y) (and (value-equal (second td) (car x) (car y))
                                         (value-equal (third td) (cdr x) (cdr y))))
                      a b)))
    (:array (and (= (length a) (length b))
                 (every (lambda (x y) (value-equal (fifth td) x y)) a b)))))
