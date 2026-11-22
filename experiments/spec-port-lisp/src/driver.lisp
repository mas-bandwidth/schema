;;;; driver.lisp -- the conformance driver (test/conformance/README.md).
;;;;
;;;;   driver <manifest> list
;;;;   driver <manifest> <surface> <outdir>
;;;;
;;;; and, for this port's own fast checks against the committed answers:
;;;;
;;;;   driver <manifest> check <surface>

(in-package :spec-port)

(defvar *manifest-units* (make-hash-table :test 'equal))   ; key -> paths
(defvar *loaded-units* (make-hash-table :test 'equal))     ; key -> unit
(defvar *instances* '())
(defvar *manifest-lines* '())

(defparameter +surfaces+ '("wire" "json-read" "json-write"))

(defun read-manifest (path)
  (setf *instances* '() *manifest-lines* '())
  (clrhash *manifest-units*)
  (with-open-file (s path :external-format :utf-8)
    (loop for line = (read-line s nil)
          while line
          do (let ((f (split-ws line)))
               (when (and f (not (starts-with (first f) "#")))
                 (push f *manifest-lines*)
                 (cond ((string= (first f) "unit")
                        (setf (gethash (second f) *manifest-units*) (cddr f)))
                       ((string= (first f) "instance")
                        (push f *instances*)))))))
  (setf *instances* (nreverse *instances*) *manifest-lines* (nreverse *manifest-lines*)))

(defun unit-for (key)
  (or (gethash key *loaded-units*)
      (setf (gethash key *loaded-units*)
            (let ((paths (gethash key *manifest-units*)))
              (unless paths (error "manifest: no unit ~a" key))
              (load-unit paths)))))

(defun root-decl (unit name)
  (let ((*unit* unit)) (find-decl name)))

(defun instance-flags (inst) (nthcdr 5 inst))

(defun answer-file (outdir name) (concatenate 'string outdir "/" name))

(defun write-text-file (path text)
  (write-file-octets path (string-to-octets text)))

(defmacro with-unit ((unit-var key) &body body)
  `(let* ((,unit-var (unit-for ,key)) (*unit* ,unit-var))
     ,@body))

(defun variable-root-p (decl) (member (sdecl-class decl) '(:table :type)))

;;; One answer per case: (values :bytes octets) | (values :refused) | (values :absent)

(defun answer-wire (inst)
  (destructuring-bind (kw name ukey root wire &rest flags) inst
    (declare (ignore kw name flags))
    (with-unit (u ukey)
      (let ((decl (root-decl u root)))
        (if (not (variable-root-p decl))
            (values :absent nil)    ; the fixed form (form 3) is outside this port's scope
            (handler-case (values :bytes (wire-save (wire-load decl (read-file-octets wire))))
              (refusal () (values :refused nil))))))))

(defun json-golden-path (name) (format nil "testdata/conformance/tables/json/~a.json" name))

(defun answer-json-read (inst)
  (destructuring-bind (kw name ukey root wire &rest flags) inst
    (declare (ignore kw wire flags))
    (with-unit (u ukey)
      (let ((decl (root-decl u root)))
        (if (not (variable-root-p decl))
            (values :absent nil)
            (handler-case (values :bytes (wire-save (json-read-value decl (read-file-octets (json-golden-path name)))))
              (refusal () (values :refused nil))))))))

(defun answer-json-write (inst)
  (destructuring-bind (kw name ukey root wire &rest flags) inst
    (declare (ignore kw name flags))
    (with-unit (u ukey)
      (let ((decl (root-decl u root)))
        (if (not (variable-root-p decl))
            (values :absent nil)
            (handler-case (values :bytes (json-write-value (wire-load decl (read-file-octets wire))))
              (refusal () (values :refused nil))))))))

(defun surface-cases (surface)
  "The instance lines a surface answers."
  (cond ((string= surface "wire") *instances*)
        (t (remove-if (lambda (i) (member "no-text" (instance-flags i) :test #'string=)) *instances*))))

(defun answer (surface inst)
  (cond ((string= surface "wire") (answer-wire inst))
        ((string= surface "json-read") (answer-json-read inst))
        ((string= surface "json-write") (answer-json-write inst))))

(defun run-surface (surface outdir)
  (ensure-directories-exist (concatenate 'string outdir "/"))
  (dolist (inst (surface-cases surface))
    (let* ((name (second inst))
           (file (if (string= surface "json-write") (concatenate 'string name ".json") name)))
      (multiple-value-bind (kind bytes)
          (handler-case (answer surface inst)
            (error (e) (format *error-output* "~a ~a: ~a~%" surface name e) (values :error nil)))
        (ecase kind
          (:bytes (write-file-octets (answer-file outdir file) bytes))
          (:refused (write-text-file (answer-file outdir file) (format nil "refused~%")))
          (:absent (write-file-octets (answer-file outdir (concatenate 'string name ".absent")) (make-octets 0)))
          (:error nil))))))

;;; This port's own check against the committed answers (fast checks only).

(defun expected (surface inst)
  (let ((name (second inst)) (flags (instance-flags inst)))
    (cond ((member "wire-refused" flags :test #'string=) :refused)
          ((string= surface "json-write") (read-file-octets (json-golden-path name)))
          (t (read-file-octets (fifth inst))))))

(defun check-surface (surface &key verbose)
  (let ((pass 0) (fail 0) (absent 0) (failed '()))
    (dolist (inst (surface-cases surface))
      (multiple-value-bind (kind bytes)
          (handler-case (answer surface inst)
            (error (e) (values :error (format nil "~a" e))))
        (let ((want (expected surface inst)))
          (cond ((eq kind :absent) (incf absent))
                ((or (and (eq want :refused) (eq kind :refused))
                     (and (eq kind :bytes) (not (eq want :refused)) (equalp bytes want)))
                 (incf pass))
                (t (incf fail)
                   (push (format nil "~a: ~a~@[ ~a~]" (second inst) kind
                                 (when (and verbose (eq kind :error)) bytes))
                         failed))))))
    (format t "~a: pass ~a fail ~a absent ~a~%" surface pass fail absent)
    (dolist (f (reverse failed)) (format t "  FAIL ~a~%" f))
    (zerop fail)))

(defun main (args)
  (handler-case
      (destructuring-bind (manifest cmd &rest rest) args
        (read-manifest manifest)
        (cond
          ((string= cmd "list") (dolist (s +surfaces+) (format t "~a~%" s)) 0)
          ((string= cmd "check")
           (let ((ok t))
             (dolist (s (or rest +surfaces+)) (unless (check-surface s :verbose t) (setf ok nil)))
             (if ok 0 1)))
          ((member cmd +surfaces+ :test #'string=) (run-surface cmd (first rest)) 0)
          (t 2)))
    (error (e) (format *error-output* "driver: ~a~%" e) 1)))
