;;; memoryelaine-show.el --- Detail/show buffer for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Displays a single log entry with metadata, headers, and on-demand
;; body loading.  Reuses a single *memoryelaine-entry* buffer.

;;; Code:

(require 'cl-lib)
(require 'json)
(require 'memoryelaine-log)
(require 'memoryelaine-http)
(require 'memoryelaine-json-view)
(require 'memoryelaine-state)

(declare-function memoryelaine-search-select-entry "memoryelaine-search" (entry-id))

(defvar memoryelaine-show-buffer-name "*memoryelaine-entry*"
  "Name of the detail/show buffer.")

(defvar-local memoryelaine-show--section-positions nil
  "Sorted list of section start positions in the current detail buffer.")

(defvar memoryelaine-show-copy-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "h") #'memoryelaine-show-copy-request-headers)
    (define-key map (kbd "b") #'memoryelaine-show-copy-request-body)
    (define-key map (kbd "H") #'memoryelaine-show-copy-response-headers)
    (define-key map (kbd "B") #'memoryelaine-show-copy-response-body)
    map)
  "Prefix keymap for raw copy commands in `memoryelaine-show-mode'.")

(defvar memoryelaine-show-export-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "b") #'memoryelaine-show-save-request-raw-body)
    (define-key map (kbd "B") #'memoryelaine-show-save-response-raw-body)
    (define-key map (kbd "c") #'memoryelaine-show-save-response-assembled-content)
    (define-key map (kbd "R") #'memoryelaine-show-save-response-assembled-reasoning)
    map)
  "Prefix keymap for body export commands in `memoryelaine-show-mode'.")

(defvar memoryelaine-show-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "q") #'quit-window)
    (define-key map (kbd "g") #'memoryelaine-show-refresh)
    (define-key map (kbd "v") #'memoryelaine-show-toggle-view)
    (define-key map (kbd "t") #'memoryelaine-show-fetch-full-bodies)
    (define-key map (kbd "c") #'memoryelaine-show-open-conversation)
    (define-key map (kbd "n") #'next-line)
    (define-key map (kbd "p") #'previous-line)
    (define-key map (kbd "j") #'memoryelaine-show-open-request-json-view)
    (define-key map (kbd "J") #'memoryelaine-show-open-response-json-view)
    (define-key map (kbd "M-n") #'memoryelaine-show-next-section)
    (define-key map (kbd "M-p") #'memoryelaine-show-previous-section)
    (define-key map (kbd "C-M-n") #'memoryelaine-show-next-entry)
    (define-key map (kbd "C-M-p") #'memoryelaine-show-previous-entry)
    (define-key map (kbd "w") memoryelaine-show-copy-map)
    (define-key map (kbd "x") memoryelaine-show-export-map)
    map)
  "Keymap for `memoryelaine-show-mode'.")

(define-derived-mode memoryelaine-show-mode special-mode "MemoryElaine-Show"
  "Major mode for viewing a single memoryelaine log entry."
  (setq buffer-read-only t
        truncate-lines nil
        word-wrap t)
  (visual-line-mode 1)
  (add-hook 'kill-buffer-hook #'memoryelaine-http-cancel-all nil t))

;; --- Entry point ---

(defun memoryelaine-show-entry (entry-id)
  "Display log ENTRY-ID in the show buffer.
Fetches metadata and preview bodies."
  (let ((buf (get-buffer-create memoryelaine-show-buffer-name)))
    (with-current-buffer buf
      (unless (derived-mode-p 'memoryelaine-show-mode)
        (memoryelaine-show-mode))
      (memoryelaine-http-cancel-all)
      (memoryelaine-state-detail-init entry-id)
      (memoryelaine-show--render-loading)
      (memoryelaine-show--fetch-metadata entry-id))
    (pop-to-buffer buf)))

;; --- Fetching ---

(defun memoryelaine-show--fetch-metadata (entry-id)
  "Fetch metadata for ENTRY-ID and then fetch preview bodies."
  (let ((gen (memoryelaine-state-detail-next-generation))
        (buf (get-buffer memoryelaine-show-buffer-name)))
    (memoryelaine-http-get
     (format "/api/logs/%d" entry-id) nil
     (lambda (status data err)
       (when (and (buffer-live-p buf)
                  (= gen (buffer-local-value 'memoryelaine-state--detail-generation buf)))
         (with-current-buffer buf
           (if err
               (memoryelaine-log-error "Detail fetch failed: %s" err)
             (if (and (>= status 200) (< status 300))
                 (let ((entry (alist-get 'entry data))
                       (sv (alist-get 'stream_view data)))
                   (memoryelaine-state-detail-set-metadata entry sv)
                   (when (and (memoryelaine-show--json-true-p (alist-get 'assembled_available sv))
                              (equal (alist-get 'default_mode sv) "assembled"))
                     (setq memoryelaine-state--resp-view-mode 'assembled))
                   (memoryelaine-show--render)
                   ;; Fetch preview bodies
                   (when (alist-get 'has_request_body entry)
                     (memoryelaine-show--fetch-body entry-id "req" "raw" nil))
                   (when (alist-get 'has_response_body entry)
                     (memoryelaine-show--fetch-body entry-id "resp" "raw" nil)
                     (when (memoryelaine-show--json-true-p (alist-get 'assembled_available sv))
                       (memoryelaine-show--fetch-body entry-id "resp" "assembled" nil "all"))))
               (memoryelaine-log-error "Detail error: HTTP %d" status)))))))))

(defun memoryelaine-show--fetch-body (entry-id part mode full &optional section)
  "Fetch body for ENTRY-ID.
PART is \"req\" or \"resp\".  MODE is \"raw\" or \"assembled\".
FULL is non-nil to fetch the complete body.
Optional SECTION selects assembled sub-content: \"all\", \"content\", or \"reasoning\"."
  (let ((buf (get-buffer memoryelaine-show-buffer-name))
        (gen memoryelaine-state--detail-generation)
        (params `(("part" . ,part)
                  ("mode" . ,mode))))
    (when (and section (string= mode "assembled") (string= part "resp"))
      (push (cons "section" section) params))
    (when full
      (push '("full" . "true") params))
    ;; Pass ellipsis on preview/display fetches (not canonical full).
    (when (and (not full)
               (boundp 'memoryelaine-show-string-ellipsis-limit)
               (integerp (symbol-value 'memoryelaine-show-string-ellipsis-limit))
               (> (symbol-value 'memoryelaine-show-string-ellipsis-limit) 0))
      (push (cons "ellipsis"
                   (number-to-string (symbol-value 'memoryelaine-show-string-ellipsis-limit)))
            params))
    (memoryelaine-http-get
     (format "/api/logs/%d/body" entry-id) params
     (lambda (status data err)
       (when (and (buffer-live-p buf)
                  (= gen (buffer-local-value 'memoryelaine-state--detail-generation buf))
                  (= entry-id (buffer-local-value 'memoryelaine-state--entry-id buf)))
         (with-current-buffer buf
           (if err
               (memoryelaine-log-error "Body fetch failed (%s): %s" part err)
             (when (and (>= status 200) (< status 300))
               (let ((content (or (alist-get 'content data) ""))
                     (available (alist-get 'available data)))
                 (if (memoryelaine-show--json-true-p available)
                     (memoryelaine-state-detail-set-body part mode content data)
                   (memoryelaine-log-debug "Body not available (%s): %s"
                                           part (alist-get 'reason data)))
                 (memoryelaine-show--render))))))))))

;; --- Rendering ---

(defun memoryelaine-show--render-loading ()
  "Render a loading indicator in the show buffer."
  (let ((inhibit-read-only t))
    (erase-buffer)
    (setq memoryelaine-show--section-positions nil)
    (insert "Loading...\n")))

(defun memoryelaine-show--insert-heading (heading)
  "Insert HEADING as a bold section line and track its start position."
  (push (point) memoryelaine-show--section-positions)
  (insert (propertize heading 'face 'bold)))

(defun memoryelaine-show--render ()
  "Render the current detail state into the show buffer."
  (let ((inhibit-read-only t)
        (entry memoryelaine-state--metadata)
        (sv memoryelaine-state--stream-view)
        (pos (point)))
    (erase-buffer)
    (setq memoryelaine-show--section-positions nil)
    (when entry
      ;; Title
      (memoryelaine-show--insert-heading
       (format "Log #%d\n" memoryelaine-state--entry-id))
      (insert "\n")
      ;; Metadata
      (memoryelaine-show--insert-field "Time" (memoryelaine-show--format-time-range
                                               (alist-get 'ts_start entry)
                                               (alist-get 'ts_end entry)))
      (memoryelaine-show--insert-field "Duration" (let ((d (alist-get 'duration_ms entry)))
                                                    (if d (format "%dms" d) "—")))
      (memoryelaine-show--insert-field "Client" (or (alist-get 'client_ip entry) "—"))
      (memoryelaine-show--insert-field "Method" (or (alist-get 'request_method entry) "—"))
      (memoryelaine-show--insert-field "Path" (or (alist-get 'request_path entry) "—"))
      (memoryelaine-show--insert-field "Upstream" (or (alist-get 'upstream_url entry) "—"))
      (memoryelaine-show--insert-field "Status" (let ((s (alist-get 'status_code entry)))
                                                  (if s (number-to-string s) "—")))
      (let ((err (alist-get 'error entry)))
        (when (and err (not (eq err :null)))
          (memoryelaine-show--insert-field "Error" err)))
      (insert "\n")

      ;; Request Headers
      (memoryelaine-show--insert-heading "─── Request Headers ───\n")
      (memoryelaine-show--insert-headers (alist-get 'req_headers entry))
      (insert "\n")

      ;; Request Body
      (memoryelaine-show--insert-heading
       (format "─── Request Body (%s%s) ───\n"
               (memoryelaine-show--format-bytes (alist-get 'req_bytes entry))
               (if (alist-get 'req_truncated entry) ", TRUNCATED" "")))
      (memoryelaine-show--insert-body "req")
      (insert "\n")

      ;; Response Headers
      (memoryelaine-show--insert-heading "─── Response Headers ───\n")
      (memoryelaine-show--insert-headers (alist-get 'resp_headers entry))
      (insert "\n")

      ;; Response Body (with stream view info)
      (memoryelaine-show--insert-heading
       (format "─── Response Body (%s%s) ───\n"
               (memoryelaine-show--format-bytes (alist-get 'resp_bytes entry))
               (if (alist-get 'resp_truncated entry) ", TRUNCATED" "")))
      ;; Stream view status
      (when sv
        (let ((assembled (alist-get 'assembled_available sv))
              (reason (alist-get 'reason sv))
              (has-content (alist-get 'has_content sv)))
          (cond
           ((and assembled (eq memoryelaine-state--resp-view-mode 'assembled))
            (if (memoryelaine-show--json-true-p has-content)
                (insert "[Stream View: Assembled] ")
              (insert "[Stream View: Assembled (content missing)] ")))
           (assembled
            (insert "[Stream View: Raw — press v for assembled] "))
           ((and reason (not (string= reason "unsupported_path"))
                 (not (string= reason "missing_body")))
            (insert (format "[Stream View: Raw (assembled unavailable: %s)] " reason))))))
      (insert "\n")
      (memoryelaine-show--insert-body "resp")
      (insert "\n")

      ;; Help line
      (insert "\n")
      (insert (propertize
               "q:back  g:refresh  v:toggle view  t:load full bodies  c:conversation  j/J:req/resp json  n/p:line  M-n/M-p:section  C-M-n/C-M-p:entry  w h/b/H/B:copy raw  x b/B/c/R:download"
               'face 'shadow)))
    (setq memoryelaine-show--section-positions
          (nreverse memoryelaine-show--section-positions))
    (goto-char (min pos (point-max)))))

(defun memoryelaine-show--insert-field (label value)
  "Insert a metadata LABEL: VALUE line."
  (insert (format "%-12s %s\n" (concat label ":") value)))

(defun memoryelaine-show--insert-headers (headers)
  "Insert HEADERS alist as formatted key: value lines."
  (if (or (null headers) (eq headers :null)
          (and (listp headers) (null (car headers))))
      (insert "  (none)\n")
    (dolist (pair headers)
      (let ((key (car pair))
            (val (cdr pair)))
        (insert (format "  %s: %s\n"
                        key
                        (if (and (listp val) (not (eq val :null)))
                            (mapconcat (lambda (v) (format "%s" v)) val ", ")
                          (format "%s" val))))))))

(defun memoryelaine-show--maybe-pretty-print-json (content)
  "Attempt to pretty-print CONTENT as JSON.  Return formatted string or original."
  (if (and content
           (> (length content) 0)
           (memq (aref content 0) '(?\{ ?\[)))
      (condition-case nil
          (progn
            (json-parse-string content) ; validate
            (with-temp-buffer
              (insert content)
              (json-pretty-print-buffer)
              (buffer-string)))
        (error content))
    content))

(defun memoryelaine-show--compact-json-string (value &optional default-empty-object)
  "Return VALUE as compact JSON.
When DEFAULT-EMPTY-OBJECT is non-nil, nil values are encoded as `{}`."
  (if (and default-empty-object (null value))
      "{}"
    (let ((json-object-type 'alist))
      (json-encode value))))

(defun memoryelaine-show--copy-raw (label content)
  "Copy CONTENT to the kill ring and announce LABEL."
  (kill-new content)
  (message "memoryelaine: copied %s" label))

(defun memoryelaine-show--json-true-p (value)
  "Return non-nil when VALUE is the JSON boolean true."
  (eq value t))

(defun memoryelaine-show--insert-body (part)
  "Insert body content for PART (\"req\" or \"resp\").
Shows preview/full content with size info, or a placeholder."
  (let* ((is-resp (string= part "resp"))
         (view-mode memoryelaine-state--resp-view-mode)
         (body (cond
                ((and is-resp (eq view-mode 'assembled))
                 memoryelaine-state--resp-body-assembled)
                (is-resp memoryelaine-state--resp-body)
                (t memoryelaine-state--req-body)))
         (body-info (cond
                     ((and is-resp (eq view-mode 'assembled))
                      memoryelaine-state--resp-body-assembled-info)
                     (is-resp memoryelaine-state--resp-body-info)
                     (t memoryelaine-state--req-body-info)))
         (body-state (cond
                      ((and is-resp (eq view-mode 'assembled))
                       memoryelaine-state--resp-body-assembled-state)
                      (is-resp memoryelaine-state--resp-body-state)
                      (t memoryelaine-state--req-body-state))))
    (cond
     ((eq body-state 'none)
      (insert "  [Not loaded]\n"))
     (body
     (let ((included (alist-get 'included_bytes body-info))
            (total (alist-get 'total_bytes body-info))
            (truncated (alist-get 'truncated body-info))
            (ellipsized (alist-get 'ellipsized body-info))
            (complete (alist-get 'complete body-info))
            (sections (and is-resp (eq view-mode 'assembled)
                           memoryelaine-state--resp-body-assembled-sections)))
        (cond
         ((memoryelaine-show--json-true-p truncated)
          (insert (propertize
                   (format "  [Preview: %s / %s — press t to load full]\n"
                           (memoryelaine-show--format-bytes included)
                           (memoryelaine-show--format-bytes total))
                   'face 'warning)))
         ((and (memoryelaine-show--json-true-p ellipsized)
               (not (memoryelaine-show--json-true-p complete)))
          (insert (propertize
                   "  [Display: long strings shortened — press t for full body]\n"
                   'face 'warning))))
        (if (and (listp sections) sections)
            (progn
              (dolist (section sections)
                (let* ((kind (or (alist-get 'kind section) "section"))
                       (label (or (alist-get 'label section) kind))
                       (content (or (alist-get 'content section) "")))
                  (insert (format "  [%s]\n" label))
                  (if (and (string= kind "content")
                           (string= content ""))
                      (insert (propertize "  (content missing)\n" 'face 'italic))
                    (let ((pretty (memoryelaine-show--maybe-pretty-print-json content)))
                      (insert pretty)
                      (unless (string-suffix-p "\n" pretty)
                        (insert "\n")))))))
          (insert (memoryelaine-show--maybe-pretty-print-json body))
          (unless (string-suffix-p "\n" body)
            (insert "\n")))))
     (t
      (insert "  (empty)\n")))))

;; --- Interactive commands ---

(defun memoryelaine-show-refresh ()
  "Refresh the current entry's metadata and preview bodies."
  (interactive)
  (when memoryelaine-state--entry-id
    (memoryelaine-http-cancel-all)
    (memoryelaine-state-detail-init memoryelaine-state--entry-id)
    (memoryelaine-show--render-loading)
    (memoryelaine-show--fetch-metadata memoryelaine-state--entry-id)))

(defun memoryelaine-show-toggle-view ()
  "Toggle between raw and assembled response view."
  (interactive)
  (let ((sv memoryelaine-state--stream-view))
    (when (and sv (alist-get 'assembled_available sv))
      (if (eq memoryelaine-state--resp-view-mode 'raw)
          (progn
            (setq memoryelaine-state--resp-view-mode 'assembled)
            ;; Fetch assembled if not cached
            (unless memoryelaine-state--resp-body-assembled
              (memoryelaine-show--fetch-body memoryelaine-state--entry-id
                                             "resp" "assembled" nil "all")))
        (setq memoryelaine-state--resp-view-mode 'raw))
      (memoryelaine-show--render))))

(defun memoryelaine-show-fetch-full-bodies ()
  "Fetch full request and response bodies."
  (interactive)
  (when memoryelaine-state--entry-id
    (let ((entry memoryelaine-state--metadata))
      (when (and entry (alist-get 'has_request_body entry))
        (memoryelaine-show--fetch-body memoryelaine-state--entry-id "req" "raw" t))
      (when (and entry (alist-get 'has_response_body entry))
        (memoryelaine-show--fetch-body memoryelaine-state--entry-id "resp" "raw" t)
        ;; Also fetch full assembled body if available
        (when (and memoryelaine-state--stream-view
                   (alist-get 'assembled_available memoryelaine-state--stream-view))
          (memoryelaine-show--fetch-body memoryelaine-state--entry-id "resp" "assembled" t "all"))))))

(defun memoryelaine-show-open-conversation ()
  "Open the conversation/thread view for the current chat entry."
  (interactive)
  (let ((entry memoryelaine-state--metadata))
    (cond
     ((null entry)
      (message "memoryelaine: no entry loaded"))
     ((not (string-suffix-p "/chat/completions"
                            (or (alist-get 'request_path entry) "")))
      (message "memoryelaine: conversation view only available for chat/completions"))
     ((alist-get 'req_truncated entry)
      (message "memoryelaine: conversation view not available for truncated requests"))
     (t
      (require 'memoryelaine-thread)
      (memoryelaine-thread-open memoryelaine-state--entry-id)))))

(defun memoryelaine-show--with-full-body (part mode callback &optional section)
  "Ensure the canonical full body for PART and MODE is cached, then call CALLBACK.
CALLBACK is called with no arguments in the show buffer once the body
is available.  If the body is already cached as `full', CALLBACK runs
immediately.  Otherwise a fetch is issued and CALLBACK runs in the
response handler."
  (let* ((assembled (and (string= part "resp") (string= mode "assembled")))
         (body-state (cond
                      (assembled memoryelaine-state--resp-body-assembled-state)
                      ((string= part "resp") memoryelaine-state--resp-body-state)
                      (t memoryelaine-state--req-body-state))))
    (if (eq body-state 'full)
        (funcall callback)
      (let ((entry-id memoryelaine-state--entry-id)
            (gen memoryelaine-state--detail-generation)
            (buf (current-buffer)))
        (message "memoryelaine: fetching full %s body…" part)
        (memoryelaine-http-get
         (format "/api/logs/%d/body" entry-id)
         (append `(("part" . ,part) ("mode" . ,mode) ("full" . "true"))
                 (when (and assembled section)
                   `(("section" . ,section))))
         (lambda (status data err)
           (when (and (buffer-live-p buf)
                      (= gen (buffer-local-value 'memoryelaine-state--detail-generation buf))
                      (= entry-id (buffer-local-value 'memoryelaine-state--entry-id buf)))
             (with-current-buffer buf
               (if err
                   (memoryelaine-log-error "Full body fetch failed (%s): %s" part err)
                 (when (and (>= status 200) (< status 300))
                   (let ((content (or (alist-get 'content data) ""))
                         (available (alist-get 'available data)))
                     (when (memoryelaine-show--json-true-p available)
                       (memoryelaine-state-detail-set-body part mode content data)
                       (memoryelaine-show--render)
                       (funcall callback)))))))))))))

(defun memoryelaine-show-open-request-json-view ()
  "Open the current request body in the JSON inspector."
  (interactive)
  (cond
   ((null memoryelaine-state--metadata)
    (message "memoryelaine: no entry loaded"))
   ((eq memoryelaine-state--req-body-state 'none)
    (message "memoryelaine: request body not loaded yet"))
   ((null memoryelaine-state--req-body)
    (message "memoryelaine: request body is empty"))
   (t
    (memoryelaine-show--with-full-body
     "req" "raw"
     (lambda ()
       (memoryelaine-json-view-open
        (format "Log #%d Request JSON" memoryelaine-state--entry-id)
        memoryelaine-state--req-body))))))

(defun memoryelaine-show-open-response-json-view ()
  "Open the current response body in the JSON inspector."
  (interactive)
  (let* ((assembled (eq memoryelaine-state--resp-view-mode 'assembled))
         (body-state (if assembled
                         memoryelaine-state--resp-body-assembled-state
                       memoryelaine-state--resp-body-state))
         (body (if assembled
                   memoryelaine-state--resp-body-assembled
                 memoryelaine-state--resp-body))
         (mode (if assembled "assembled" "raw"))
         (mode-label (if assembled "Assembled Response JSON" "Response JSON")))
    (cond
     ((null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded"))
     ((eq body-state 'none)
      (message "memoryelaine: response body not loaded yet"))
     ((null body)
      (message "memoryelaine: response body is empty"))
     (t
      (memoryelaine-show--with-full-body
       "resp" mode
       (lambda ()
         (let ((full-body (if assembled
                              memoryelaine-state--resp-body-assembled
                            memoryelaine-state--resp-body)))
           (memoryelaine-json-view-open
            (format "Log #%d %s" memoryelaine-state--entry-id mode-label)
            full-body))))))))

(defun memoryelaine-show-copy-request-headers ()
  "Copy the request headers as compact raw JSON."
  (interactive)
  (if (null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded")
    (memoryelaine-show--copy-raw
     "request headers"
     (memoryelaine-show--compact-json-string
      (alist-get 'req_headers memoryelaine-state--metadata) t))))

(defun memoryelaine-show-copy-request-body ()
  "Copy the raw request body.
Auto-fetches the canonical full body if only a preview is cached."
  (interactive)
  (cond
   ((null memoryelaine-state--metadata)
    (message "memoryelaine: no entry loaded"))
   ((eq memoryelaine-state--req-body-state 'none)
    (message "memoryelaine: request body not loaded yet"))
   (t
    (memoryelaine-show--with-full-body
     "req" "raw"
     (lambda ()
       (memoryelaine-show--copy-raw
        "request body"
        (or memoryelaine-state--req-body "")))))))

(defun memoryelaine-show-copy-response-headers ()
  "Copy the response headers as compact raw JSON."
  (interactive)
  (if (null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded")
    (memoryelaine-show--copy-raw
     "response headers"
     (memoryelaine-show--compact-json-string
      (alist-get 'resp_headers memoryelaine-state--metadata) t))))

(defun memoryelaine-show-copy-response-body ()
  "Copy the raw response body for the current response view mode.
Auto-fetches the canonical full body if only a preview is cached."
  (interactive)
  (let* ((assembled (eq memoryelaine-state--resp-view-mode 'assembled))
         (body-state (if assembled
                         memoryelaine-state--resp-body-assembled-state
                       memoryelaine-state--resp-body-state))
         (body (if assembled
                   memoryelaine-state--resp-body-assembled
                 memoryelaine-state--resp-body))
         (mode (if assembled "assembled" "raw"))
         (label (if assembled
                    "assembled response body"
                  "response body")))
    (cond
     ((null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded"))
     ((eq body-state 'none)
      (message "memoryelaine: response body not loaded yet"))
     (t
      (memoryelaine-show--with-full-body
       "resp" mode
       (lambda ()
         (let ((full-body (if assembled
                              memoryelaine-state--resp-body-assembled
                            memoryelaine-state--resp-body)))
           (memoryelaine-show--copy-raw label (or full-body "")))))))))

(defun memoryelaine-show--body-is-json-p (content)
  "Return non-nil when CONTENT parses as JSON."
  (and content
       (> (length content) 0)
       (condition-case nil
           (progn
             (json-parse-string content)
             t)
         (error nil))))

(defun memoryelaine-show--default-export-filename (part mode section content)
  "Compute a default export filename for PART, MODE, SECTION, and CONTENT."
  (let ((ext (if (memoryelaine-show--body-is-json-p content) "json" "txt")))
    (cond
     ((string= part "req") (format "request-body.%s" ext))
     ((string= mode "raw") (format "response-body-parts.%s" ext))
     ((string= section "reasoning") (format "response-reasoning-content.%s" ext))
     (t (format "response-body-assembled.%s" ext)))))

(defun memoryelaine-show--write-export-file (content default-filename label)
  "Prompt for a save path and write CONTENT using DEFAULT-FILENAME and LABEL."
  (let ((path (read-file-name
               (format "Save %s to: " label)
               default-directory
               default-filename)))
    (with-temp-buffer
      (insert (or content ""))
      (write-region (point-min) (point-max) path nil 'silent))
    (message "memoryelaine: saved %s to %s" label path)))

(defun memoryelaine-show--fetch-full-body-content (part mode section callback)
  "Fetch canonical full body for PART and MODE, then call CALLBACK with content.
SECTION may be nil, \"all\", \"content\", or \"reasoning\"."
  (let ((entry-id memoryelaine-state--entry-id)
        (gen memoryelaine-state--detail-generation)
        (buf (current-buffer))
        (params (append `(("part" . ,part) ("mode" . ,mode) ("full" . "true"))
                        (when section `(("section" . ,section))))))
    (memoryelaine-http-get
     (format "/api/logs/%d/body" entry-id)
     params
     (lambda (status data err)
       (when (and (buffer-live-p buf)
                  (= gen (buffer-local-value 'memoryelaine-state--detail-generation buf))
                  (= entry-id (buffer-local-value 'memoryelaine-state--entry-id buf)))
         (with-current-buffer buf
           (cond
            (err
             (memoryelaine-log-error "Export fetch failed (%s/%s): %s" part mode err))
            ((or (< status 200) (>= status 300))
             (memoryelaine-log-error "Export fetch error: HTTP %d" status))
            ((not (memoryelaine-show--json-true-p (alist-get 'available data)))
             (message "memoryelaine: body unavailable (%s)"
                      (or (alist-get 'reason data) "not available")))
            (t
             (funcall callback (or (alist-get 'content data) ""))))))))))

(defun memoryelaine-show-save-request-raw-body ()
  "Save canonical full request body to a file."
  (interactive)
  (if (null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded")
    (memoryelaine-show--fetch-full-body-content
     "req" "raw" nil
     (lambda (content)
       (memoryelaine-show--write-export-file
        content
        (memoryelaine-show--default-export-filename "req" "raw" "all" content)
        "request body")))))

(defun memoryelaine-show-save-response-raw-body ()
  "Save canonical full raw response body to a file."
  (interactive)
  (if (null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded")
    (memoryelaine-show--fetch-full-body-content
     "resp" "raw" nil
     (lambda (content)
       (memoryelaine-show--write-export-file
        content
        (memoryelaine-show--default-export-filename "resp" "raw" "all" content)
        "raw response body")))))

(defun memoryelaine-show-save-response-assembled-content ()
  "Save canonical full assembled content section to a file."
  (interactive)
  (if (null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded")
    (memoryelaine-show--fetch-full-body-content
     "resp" "assembled" "content"
     (lambda (content)
       (memoryelaine-show--write-export-file
        content
        (memoryelaine-show--default-export-filename "resp" "assembled" "content" content)
        "assembled response content")))))

(defun memoryelaine-show-save-response-assembled-reasoning ()
  "Save canonical full assembled reasoning section to a file."
  (interactive)
  (if (null memoryelaine-state--metadata)
      (message "memoryelaine: no entry loaded")
    (memoryelaine-show--fetch-full-body-content
     "resp" "assembled" "reasoning"
     (lambda (content)
       (memoryelaine-show--write-export-file
        content
        (memoryelaine-show--default-export-filename "resp" "assembled" "reasoning" content)
        "assembled response reasoning")))))

(defun memoryelaine-show--jump-section (direction)
  "Jump to the next or previous section according to DIRECTION."
  (let* ((current (line-beginning-position))
         (positions memoryelaine-show--section-positions)
         (target (if (> direction 0)
                     (cl-find-if (lambda (pos) (> pos current)) positions)
                   (car (last (cl-remove-if-not (lambda (pos) (< pos current))
                                                positions))))))
    (if target
        (goto-char target)
      (message "memoryelaine: no %s section"
               (if (> direction 0) "next" "previous")))))

(defun memoryelaine-show-next-section ()
  "Jump to the next title row in the current detail buffer."
  (interactive)
  (memoryelaine-show--jump-section 1))

(defun memoryelaine-show-previous-section ()
  "Jump to the previous title row in the current detail buffer."
  (interactive)
  (memoryelaine-show--jump-section -1))

(defun memoryelaine-show--open-neighbor-entry (direction)
  "Open the neighboring search result according to DIRECTION."
  (let ((entry-id (memoryelaine-state-summary-neighbor-id
                   memoryelaine-state--entry-id direction)))
    (if entry-id
        (progn
          (when (fboundp 'memoryelaine-search-select-entry)
            (memoryelaine-search-select-entry entry-id))
          (memoryelaine-show-entry entry-id))
      (message "memoryelaine: no %s entry in current results"
               (if (> direction 0) "next" "previous")))))

(defun memoryelaine-show-next-entry ()
  "Open the next entry from the current search results."
  (interactive)
  (memoryelaine-show--open-neighbor-entry 1))

(defun memoryelaine-show-previous-entry ()
  "Open the previous entry from the current search results."
  (interactive)
  (memoryelaine-show--open-neighbor-entry -1))

;; --- Formatting helpers ---

(defun memoryelaine-show--format-time-range (start end)
  "Format time range from START to END (millisecond timestamps)."
  (let ((s (format-time-string "%Y-%m-%d %H:%M:%S" (seconds-to-time (/ start 1000.0)))))
    (if end
        (format "%s → %s" s (format-time-string "%H:%M:%S" (seconds-to-time (/ end 1000.0))))
      s)))

(defun memoryelaine-show--format-bytes (n)
  "Format N bytes as a human-readable string."
  (cond
   ((or (null n) (zerop n)) "0 B")
   ((< n 1024) (format "%d B" n))
   ((< n 1048576) (format "%.1f KB" (/ n 1024.0)))
   (t (format "%.1f MB" (/ n 1048576.0)))))

(provide 'memoryelaine-show)
;;; memoryelaine-show.el ends here
