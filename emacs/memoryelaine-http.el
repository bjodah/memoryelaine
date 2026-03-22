;;; memoryelaine-http.el --- Async HTTP via curl for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Async HTTP layer built on curl.  Each request gets a generation counter
;; to suppress stale responses.  Provides structured response parsing and
;; error handling with friendly messages for common curl exit codes.

;;; Code:

(require 'json)
(require 'memoryelaine-log)
(require 'memoryelaine-auth)

(defvar memoryelaine-http--generation 0
  "Monotonically increasing request generation counter.")

(defvar memoryelaine-http--active-processes nil
  "List of active curl process objects.")

(defun memoryelaine-http--next-generation ()
  "Increment and return the next generation number."
  (setq memoryelaine-http--generation (1+ memoryelaine-http--generation)))

(defun memoryelaine-http--curl-error-message (exit-code)
  "Return a friendly error message for curl EXIT-CODE."
  (pcase exit-code
    (6 "Could not resolve host")
    (7 "Connection refused — is memoryelaine running?")
    (22 "HTTP error (see log for details)")
    (28 "Request timed out")
    (35 "SSL/TLS handshake failed")
    (52 "Empty response from server")
    (56 "Network data receive error")
    (_ (format "curl failed with exit code %d" exit-code))))

(defun memoryelaine-http--build-url (path &optional params)
  "Build a full URL from PATH and optional PARAMS alist.
PATH should start with /."
  (let ((base (symbol-value 'memoryelaine-base-url)))
    (concat base path
            (when params
              (concat "?"
                      (mapconcat
                       (lambda (pair)
                         (format "%s=%s"
                                 (url-hexify-string (car pair))
                                 (url-hexify-string (format "%s" (cdr pair)))))
                       params
                       "&"))))))

(defun memoryelaine-http--build-curl-args (url method)
  "Build curl argument list for URL with METHOD.
Includes Basic Auth header and JSON accept header."
  (let* ((creds (memoryelaine-auth-get-credentials))
         (auth-header (format "Authorization: Basic %s"
                              (base64-encode-string
                               (format "%s:%s" (car creds) (cdr creds))
                               t))))
    (list "--silent" "--show-error"
          "--max-time" "30"
          "-X" method
          "-H" auth-header
          "-H" "Accept: application/json"
          "--write-out" "\n__MEMORYELAINE_HTTP_STATUS__:%{http_code}"
          url)))

(defun memoryelaine-http--parse-response (output)
  "Parse curl OUTPUT into (status-code . body-string).
Expects the output to end with __MEMORYELAINE_HTTP_STATUS__:NNN."
  (let ((marker "__MEMORYELAINE_HTTP_STATUS__:"))
    (if (string-match (concat (regexp-quote marker) "\\([0-9]+\\)") output)
        (let ((status (string-to-number (match-string 1 output)))
              (body (substring output 0 (match-beginning 0))))
          ;; Trim trailing newline before marker
          (when (string-suffix-p "\n" body)
            (setq body (substring body 0 -1)))
          (cons status body))
      (cons 0 output))))

(defun memoryelaine-http--parse-json (body)
  "Parse BODY as JSON, returning a plist or nil on error."
  (condition-case err
      (json-parse-string body :object-type 'alist :array-type 'list)
    (error
     (memoryelaine-log-error "JSON parse error: %s" (error-message-string err))
     nil)))

(defun memoryelaine-http-request (method path params callback)
  "Make an async HTTP request.
METHOD is \"GET\", \"POST\", etc.
PATH is the API path (e.g., \"/api/logs\").
PARAMS is an alist of query parameters or nil.
CALLBACK is called with (STATUS-CODE JSON-DATA ERR-STRING).

Returns the generation number for this request.
Stale responses (where generation has advanced) are silently dropped."
  (let* ((generation (memoryelaine-http--next-generation))
         (url (memoryelaine-http--build-url path params))
         (args (memoryelaine-http--build-curl-args url method))
         (buf (generate-new-buffer " *memoryelaine-curl*"))
         (curl-program (symbol-value 'memoryelaine-curl-program))
         (proc (apply #'start-process
                      "memoryelaine-curl" buf curl-program args)))
    (memoryelaine-log-debug "HTTP %s %s (gen=%d)" method url generation)
    (push proc memoryelaine-http--active-processes)
    (set-process-sentinel
     proc
     (lambda (process _event)
       (setq memoryelaine-http--active-processes
             (delq process memoryelaine-http--active-processes))
       (let ((exit-code (process-exit-status process)))
         (if (< generation memoryelaine-http--generation)
             ;; Stale — generation has advanced, discard
             (progn
               (memoryelaine-log-debug "Discarding stale response (gen=%d, current=%d)"
                                       generation memoryelaine-http--generation)
               (kill-buffer (process-buffer process)))
           ;; Current — process the response
           (unwind-protect
               (if (not (zerop exit-code))
                   (let ((err-msg (memoryelaine-http--curl-error-message exit-code)))
                     (memoryelaine-log-error "curl error: %s (exit=%d)" err-msg exit-code)
                     (funcall callback 0 nil err-msg))
                 (let* ((output (with-current-buffer (process-buffer process)
                                  (buffer-string)))
                        (parsed (memoryelaine-http--parse-response output))
                        (status (car parsed))
                        (body (cdr parsed)))
                   (if (= status 401)
                       (progn
                         (memoryelaine-auth-on-401)
                         (funcall callback 401 nil "Authentication failed"))
                     (let ((json-data (memoryelaine-http--parse-json body)))
                       (funcall callback status json-data nil)))))
             (when (buffer-live-p (process-buffer process))
               (kill-buffer (process-buffer process))))))))
    generation))

(defun memoryelaine-http-get (path params callback)
  "Convenience wrapper for GET requests.
PATH, PARAMS, and CALLBACK as in `memoryelaine-http-request'."
  (memoryelaine-http-request "GET" path params callback))

(defun memoryelaine-http-put (path params body-alist callback)
  "Make an async PUT request with a JSON body.
PATH, PARAMS as in `memoryelaine-http-request'.
BODY-ALIST is encoded as JSON.
CALLBACK is called with (STATUS-CODE JSON-DATA ERR-STRING)."
  (let* ((generation (memoryelaine-http--next-generation))
         (url (memoryelaine-http--build-url path params))
         (creds (memoryelaine-auth-get-credentials))
         (auth-header (format "Authorization: Basic %s"
                              (base64-encode-string
                               (format "%s:%s" (car creds) (cdr creds))
                               t)))
         (json-body (json-serialize body-alist))
         (curl-program (symbol-value 'memoryelaine-curl-program))
         (buf (generate-new-buffer " *memoryelaine-curl*"))
         (proc (start-process
                "memoryelaine-curl" buf curl-program
                "--silent" "--show-error"
                "--max-time" "30"
                "-X" "PUT"
                "-H" auth-header
                "-H" "Content-Type: application/json"
                "-H" "Accept: application/json"
                "--write-out" "\n__MEMORYELAINE_HTTP_STATUS__:%{http_code}"
                "-d" json-body
                url)))
    (memoryelaine-log-debug "HTTP PUT %s (gen=%d)" url generation)
    (push proc memoryelaine-http--active-processes)
    (set-process-sentinel
     proc
     (lambda (process _event)
       (setq memoryelaine-http--active-processes
             (delq process memoryelaine-http--active-processes))
       (let ((exit-code (process-exit-status process)))
         (if (< generation memoryelaine-http--generation)
             (progn
               (memoryelaine-log-debug "Discarding stale PUT response (gen=%d)" generation)
               (kill-buffer (process-buffer process)))
           (unwind-protect
               (if (not (zerop exit-code))
                   (let ((err-msg (memoryelaine-http--curl-error-message exit-code)))
                     (memoryelaine-log-error "curl error: %s (exit=%d)" err-msg exit-code)
                     (funcall callback 0 nil err-msg))
                 (let* ((output (with-current-buffer (process-buffer process)
                                  (buffer-string)))
                        (parsed (memoryelaine-http--parse-response output))
                        (status (car parsed))
                        (body (cdr parsed))
                        (json-data (memoryelaine-http--parse-json body)))
                   (funcall callback status json-data nil)))
             (when (buffer-live-p (process-buffer process))
               (kill-buffer (process-buffer process))))))))
    generation))

(defun memoryelaine-http-cancel-all ()
  "Cancel all active HTTP requests."
  (dolist (proc memoryelaine-http--active-processes)
    (when (process-live-p proc)
      (delete-process proc)))
  (setq memoryelaine-http--active-processes nil)
  (memoryelaine-log-debug "Cancelled all active requests"))

(provide 'memoryelaine-http)
;;; memoryelaine-http.el ends here
