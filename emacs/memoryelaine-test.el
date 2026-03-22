;;; memoryelaine-test.el --- Tests for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; ERT tests for memoryelaine Emacs package.

;;; Code:

(require 'ert)

;; Add package directory to load path
(add-to-list 'load-path (file-name-directory (or load-file-name buffer-file-name)))

(require 'memoryelaine)
(require 'memoryelaine-log)
(require 'memoryelaine-auth)
(require 'memoryelaine-http)
(require 'memoryelaine-state)
(require 'memoryelaine-search)
(require 'memoryelaine-show)

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
  (let ((memoryelaine-base-url "http://myhost:8080"))
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

(ert-deftest memoryelaine-test-http-parse-json-invalid ()
  "Test JSON parsing with invalid input."
  (let ((memoryelaine-log-buffer-name "*memoryelaine-test-json-err*"))
    (should (null (memoryelaine-http--parse-json "not json")))
    (when (get-buffer memoryelaine-log-buffer-name)
      (kill-buffer memoryelaine-log-buffer-name))))

(ert-deftest memoryelaine-test-http-build-url ()
  "Test URL building with params."
  (let ((memoryelaine-base-url "http://localhost:8080"))
    (should (equal (memoryelaine-http--build-url "/api/logs" nil)
                   "http://localhost:8080/api/logs"))
    (let ((url (memoryelaine-http--build-url "/api/logs"
                                             '(("limit" . "50") ("offset" . "0")))))
      (should (string-match-p "limit=50" url))
      (should (string-match-p "offset=0" url)))))

(ert-deftest memoryelaine-test-http-curl-error-message ()
  "Test curl error code translation."
  (should (string-match-p "Connection refused" (memoryelaine-http--curl-error-message 7)))
  (should (string-match-p "timed out" (memoryelaine-http--curl-error-message 28)))
  (should (string-match-p "exit code 99" (memoryelaine-http--curl-error-message 99))))

(ert-deftest memoryelaine-test-http-generation-counter ()
  "Test that generation counter increments."
  (let ((memoryelaine-http--generation 0))
    (should (= (memoryelaine-http--next-generation) 1))
    (should (= (memoryelaine-http--next-generation) 2))
    (should (= memoryelaine-http--generation 2))))

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

(ert-deftest memoryelaine-test-state-generation ()
  "Test generation counter."
  (let ((memoryelaine-state--generation 0))
    (should (= (memoryelaine-state-next-generation) 1))
    (should (= memoryelaine-state--generation 1))))

;;; --- Search formatting tests ---

(ert-deftest memoryelaine-test-search-format-bytes ()
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

(provide 'memoryelaine-test)
;;; memoryelaine-test.el ends here
