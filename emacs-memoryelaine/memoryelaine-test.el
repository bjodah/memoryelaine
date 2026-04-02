;;; memoryelaine-test.el --- Tests for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; ERT tests for memoryelaine Emacs package.

;;; Code:

(require 'ert)
(require 'cl-lib)

;; Add package directory to load path
(add-to-list 'load-path (file-name-directory (or load-file-name buffer-file-name)))

(require 'memoryelaine)

;;; --- Log tests ---

(ert-deftest memoryelaine-test-log-message ()
  "Test that log messages are written to the log buffer."
  (let ((memoryelaine-log-buffer-name "*memoryelaine-test-log*"))
    (memoryelaine-log-info "test message %d" 42)
    (with-current-buffer memoryelaine-log-buffer-name
      (should (string-match-p "\\[INFO\\] test message 42" (buffer-string))))
    (kill-buffer memoryelaine-log-buffer-name)))

(ert-deftest memoryelaine-test-log-error ()
  "Test that error messages go to log buffer."
  (let ((memoryelaine-log-buffer-name "*memoryelaine-test-log-err*"))
    (memoryelaine-log-error "something broke: %s" "details")
    (with-current-buffer memoryelaine-log-buffer-name
      (should (string-match-p "\\[ERROR\\] something broke: details" (buffer-string))))
    (kill-buffer memoryelaine-log-buffer-name)))

(ert-deftest memoryelaine-test-log-debug ()
  "Test that debug messages go to log buffer."
  (let ((memoryelaine-log-buffer-name "*memoryelaine-test-log-dbg*"))
    (memoryelaine-log-debug "debug info: %s" "value")
    (with-current-buffer memoryelaine-log-buffer-name
      (should (string-match-p "\\[DEBUG\\] debug info: value" (buffer-string))))
    (kill-buffer memoryelaine-log-buffer-name)))

;;; --- Auth tests ---

(ert-deftest memoryelaine-test-auth-explicit ()
  "Test that explicit credentials are used."
  (let ((memoryelaine--cached-credentials nil)
        (memoryelaine-username "testuser")
        (memoryelaine-password "testpass"))
    ;; Clear cache to force re-lookup
    (memoryelaine-auth-clear-cache)
    (let ((creds (memoryelaine-auth--try-explicit)))
      (should (equal creds '("testuser" . "testpass"))))))

(ert-deftest memoryelaine-test-auth-explicit-nil ()
  "Test that nil explicit credentials return nil."
  (let ((memoryelaine-username nil)
        (memoryelaine-password nil))
    (should (null (memoryelaine-auth--try-explicit)))))

(ert-deftest memoryelaine-test-auth-cache ()
  "Test that credentials are cached."
  (let ((memoryelaine--cached-credentials '("cached" . "creds"))
        (memoryelaine-username "other")
        (memoryelaine-password "other"))
    (should (equal (memoryelaine-auth-get-credentials) '("cached" . "creds")))))

(ert-deftest memoryelaine-test-auth-clear-cache ()
  "Test that clearing cache works."
  (let ((memoryelaine--cached-credentials '("old" . "creds")))
    (memoryelaine-auth-clear-cache)
    (should (null memoryelaine--cached-credentials))))

(ert-deftest memoryelaine-test-auth-url-host ()
  "Test host extraction from base URL."
  (let ((memoryelaine-base-url "http://myhost:13845"))
    (should (equal (memoryelaine-auth--url-host) "myhost"))))

;;; --- HTTP tests ---

(ert-deftest memoryelaine-test-http-parse-response ()
  "Test parsing curl response with status marker."
  (let ((output "{ \"key\": \"value\" }\n__MEMORYELAINE_HTTP_STATUS__:200"))
    (let ((parsed (memoryelaine-http--parse-response output)))
      (should (= (car parsed) 200))
      (should (equal (cdr parsed) "{ \"key\": \"value\" }")))))

(ert-deftest memoryelaine-test-http-parse-response-no-marker ()
  "Test parsing curl response without status marker."
  (let ((output "some raw text"))
    (let ((parsed (memoryelaine-http--parse-response output)))
      (should (= (car parsed) 0))
      (should (equal (cdr parsed) "some raw text")))))

(ert-deftest memoryelaine-test-http-parse-json ()
  "Test JSON parsing."
  (let ((result (memoryelaine-http--parse-json "{\"id\": 42, \"name\": \"test\"}")))
    (should (= (alist-get 'id result) 42))
    (should (equal (alist-get 'name result) "test"))))

(ert-deftest memoryelaine-test-http-parse-json-false-is-keyword ()
  "Test JSON false parsing matches Emacs json semantics."
  (let ((result (memoryelaine-http--parse-json "{\"recording\": false}")))
    (should (eq (alist-get 'recording result) :false))))

(ert-deftest memoryelaine-test-http-json-encode-object-false ()
  "Test JSON object encoding preserves false booleans."
  (should (equal (memoryelaine-http--json-encode-object '((recording . :json-false)))
                 "{\"recording\":false}")))

(ert-deftest memoryelaine-test-http-parse-json-invalid ()
  "Test JSON parsing with invalid input."
  (let ((memoryelaine-log-buffer-name "*memoryelaine-test-json-err*"))
    (should (null (memoryelaine-http--parse-json "not json")))
    (when (get-buffer memoryelaine-log-buffer-name)
      (kill-buffer memoryelaine-log-buffer-name))))

(ert-deftest memoryelaine-test-http-build-url ()
  "Test URL building with params."
  (let ((memoryelaine-base-url "http://localhost:13845"))
    (should (equal (memoryelaine-http--build-url "/api/logs" nil)
                   "http://localhost:13845/api/logs"))
    (let ((url (memoryelaine-http--build-url "/api/logs"
                                             '(("limit" . "50") ("offset" . "0")))))
      (should (string-match-p "limit=50" url))
      (should (string-match-p "offset=0" url)))))

(ert-deftest memoryelaine-test-http-curl-error-message ()
  "Test curl error code translation."
  (should (string-match-p "Connection refused" (memoryelaine-http--curl-error-message 7)))
  (should (string-match-p "timed out" (memoryelaine-http--curl-error-message 28)))
  (should (string-match-p "exit code 99" (memoryelaine-http--curl-error-message 99))))

(ert-deftest memoryelaine-test-http-request-returns-process ()
  "Test that memoryelaine-http-request returns a process object."
  (let ((memoryelaine-base-url "http://localhost:9999")
        (memoryelaine-curl-program "curl")
        (memoryelaine--cached-credentials '("user" . "pass")))
    (let ((result (memoryelaine-http-request "GET" "/api/test" nil #'ignore)))
      (unwind-protect
          (should (processp result))
        (when (processp result)
          (delete-process result))))))

(ert-deftest memoryelaine-test-http-cancel-all-is-buffer-local ()
  "Cancelling requests in one buffer must not kill another buffer's requests."
  (let ((search-proc nil)
        (detail-proc nil))
    (unwind-protect
        (with-temp-buffer
          (let ((search-buf (current-buffer)))
            (setq search-proc (start-process "memoryelaine-test-search" nil "sleep" "10"))
            (setq memoryelaine-http--active-processes (list search-proc))
            (with-temp-buffer
              (setq detail-proc (start-process "memoryelaine-test-detail" nil "sleep" "10"))
              (setq memoryelaine-http--active-processes (list detail-proc))
              (memoryelaine-http-cancel-all)
              (should-not (process-live-p detail-proc)))
            (with-current-buffer search-buf
              (should (process-live-p search-proc)))))
      (dolist (proc (list search-proc detail-proc))
        (when (process-live-p proc)
          (delete-process proc))))))

;;; --- State tests ---

(ert-deftest memoryelaine-test-state-query ()
  "Test setting query resets offset."
  (let ((memoryelaine-state--offset 100)
        (memoryelaine-state--query "old"))
    (memoryelaine-state-set-query "new")
    (should (equal memoryelaine-state--query "new"))
    (should (= memoryelaine-state--offset 0))))

(ert-deftest memoryelaine-test-state-pagination ()
  "Test page navigation."
  (let ((memoryelaine-state--offset 0)
        (memoryelaine-state--limit 50)
        (memoryelaine-state--total 120))
    (should (memoryelaine-state-next-page))
    (should (= memoryelaine-state--offset 50))
    (should (memoryelaine-state-next-page))
    (should (= memoryelaine-state--offset 100))
    (should-not (memoryelaine-state-next-page))
    (should (= memoryelaine-state--offset 100))
    (should (memoryelaine-state-prev-page))
    (should (= memoryelaine-state--offset 50))))

(ert-deftest memoryelaine-test-state-page-numbers ()
  "Test page number calculations."
  (let ((memoryelaine-state--offset 50)
        (memoryelaine-state--limit 50)
        (memoryelaine-state--total 120))
    (should (= (memoryelaine-state-current-page) 2))
    (should (= (memoryelaine-state-total-pages) 3))
    (should (memoryelaine-state-has-more))))

(ert-deftest memoryelaine-test-state-results ()
  "Test setting results."
  (let ((memoryelaine-state--summaries nil)
        (memoryelaine-state--total 0))
    (memoryelaine-state-set-results '(a b c) 42)
    (should (equal memoryelaine-state--summaries '(a b c)))
    (should (= memoryelaine-state--total 42))))

(ert-deftest memoryelaine-test-state-detail-init ()
  "Test detail state initialization."
  (with-temp-buffer
    (memoryelaine-state-detail-init 99)
    (should (= memoryelaine-state--entry-id 99))
    (should (null memoryelaine-state--metadata))
    (should (eq memoryelaine-state--resp-view-mode 'raw))
    (should (eq memoryelaine-state--req-body-state 'none))))

(ert-deftest memoryelaine-test-state-detail-set-body ()
  "Test body caching in detail state."
  (with-temp-buffer
    (memoryelaine-state-detail-init 1)
    (memoryelaine-state-detail-set-body "req" "raw" "request body"
                                        '((truncated . t) (included_bytes . 100) (total_bytes . 500)))
    (should (equal memoryelaine-state--req-body "request body"))
    (should (eq memoryelaine-state--req-body-state 'preview))
    ;; Now set full body
    (memoryelaine-state-detail-set-body "req" "raw" "full request body"
                                        '((truncated) (included_bytes . 500) (total_bytes . 500)))
    (should (eq memoryelaine-state--req-body-state 'full))))

(ert-deftest memoryelaine-test-state-detail-set-body-assembled ()
  "Test assembled response body metadata is tracked independently."
  (with-temp-buffer
    (memoryelaine-state-detail-init 1)
    (memoryelaine-state-detail-set-body "resp" "raw" "raw body"
                                        '((truncated . t) (included_bytes . 100) (total_bytes . 500)))
    (memoryelaine-state-detail-set-body "resp" "assembled" "assembled body"
                                        '((truncated . t) (included_bytes . 12) (total_bytes . 24)))
    (should (equal memoryelaine-state--resp-body "raw body"))
    (should (equal memoryelaine-state--resp-body-assembled "assembled body"))
    (should (eq memoryelaine-state--resp-body-state 'preview))
    (should (eq memoryelaine-state--resp-body-assembled-state 'preview))
    (should (= (alist-get 'included_bytes memoryelaine-state--resp-body-assembled-info) 12))))

(ert-deftest memoryelaine-test-state-generation ()
  "Test generation counter."
  (let ((memoryelaine-state--generation 0))
    (should (= (memoryelaine-state-next-generation) 1))
    (should (= memoryelaine-state--generation 1))))

;;; --- Search formatting tests ---

(ert-deftest memoryelaine-test-search-format-bytes ()

(ert-deftest memoryelaine-test-search-fetch-recording-state-normalizes-false ()
  "Fetching recording state should normalize JSON false to nil."
  (let ((memoryelaine-search-buffer-name "*memoryelaine-test-search*"))
    (unwind-protect
        (with-current-buffer (get-buffer-create memoryelaine-search-buffer-name)
          (memoryelaine-search-mode)
          (cl-letf (((symbol-function 'memoryelaine-http-get)
                     (lambda (_path _params callback)
                       (funcall callback 200 '((recording . :false)) nil))))
            (setq memoryelaine-state--recording t)
            (memoryelaine-search--fetch-recording-state)
            (should (null memoryelaine-state--recording))))
      (when (get-buffer memoryelaine-search-buffer-name)
        (kill-buffer memoryelaine-search-buffer-name)))))

(ert-deftest memoryelaine-test-search-toggle-recording-sends-false-and-normalizes-response ()
  "Toggling recording off should send JSON false and store nil state."
  (let ((memoryelaine-search-buffer-name "*memoryelaine-test-search*")
        (captured-body nil)
        (captured-message nil))
    (unwind-protect
        (with-current-buffer (get-buffer-create memoryelaine-search-buffer-name)
          (memoryelaine-search-mode)
          (cl-letf (((symbol-function 'memoryelaine-http-put)
                     (lambda (_path _params body callback)
                       (setq captured-body body)
                       (funcall callback 200 '((recording . :false)) nil)))
                    ((symbol-function 'message)
                     (lambda (fmt &rest args)
                       (setq captured-message (apply #'format fmt args)))))
            (setq memoryelaine-state--recording t)
            (memoryelaine-search-toggle-recording)
            (should (equal captured-body '((recording . :json-false))))
            (should (null memoryelaine-state--recording))
            (should (equal captured-message "memoryelaine: recording PAUSED"))))
      (when (get-buffer memoryelaine-search-buffer-name)
        (kill-buffer memoryelaine-search-buffer-name)))))
  "Test byte formatting."
  (should (equal (memoryelaine-search--format-bytes 0) "—"))
  (should (equal (memoryelaine-search--format-bytes nil) "—"))
  (should (equal (memoryelaine-search--format-bytes 512) "512 B"))
  (should (equal (memoryelaine-search--format-bytes 2048) "2.0 KB"))
  (should (equal (memoryelaine-search--format-bytes 5242880) "5.0 MB")))

;;; --- Show formatting tests ---

(ert-deftest memoryelaine-test-show-format-bytes ()
  "Test show buffer byte formatting."
  (should (equal (memoryelaine-show--format-bytes 0) "0 B"))
  (should (equal (memoryelaine-show--format-bytes nil) "0 B"))
  (should (equal (memoryelaine-show--format-bytes 1024) "1.0 KB"))
  (should (equal (memoryelaine-show--format-bytes 1048576) "1.0 MB")))

(ert-deftest memoryelaine-test-show-format-time-range ()
  "Test time range formatting."
  ;; Just verify it returns a string without errors
  (let ((result (memoryelaine-show--format-time-range 1700000000000 1700000001000)))
    (should (stringp result))
    (should (string-match-p "→" result))))

(ert-deftest memoryelaine-test-show-insert-body-uses-assembled-metadata ()
  "Assembled response previews should use assembled byte metadata."
  (with-temp-buffer
    (memoryelaine-state-detail-init 1)
    (setq memoryelaine-state--resp-view-mode 'assembled)
    (memoryelaine-state-detail-set-body "resp" "raw" "raw body"
                                        '((truncated . t) (included_bytes . 100) (total_bytes . 500)))
    (memoryelaine-state-detail-set-body "resp" "assembled" "assembled body"
                                        '((truncated . t) (included_bytes . 12) (total_bytes . 24)))
    (memoryelaine-show--insert-body "resp")
    (let ((rendered (buffer-string)))
      (should (string-match-p "12 B / 24 B" rendered))
      (should-not (string-match-p "100 B / 500 B" rendered)))))

(provide 'memoryelaine-test)
;;; memoryelaine-test.el ends here
