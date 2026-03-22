;;; memoryelaine-show.el --- Detail/show buffer for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Displays a single log entry with metadata, headers, and on-demand
;; body loading.  Reuses a single *memoryelaine-entry* buffer.

;;; Code:

(require 'memoryelaine-log)
(require 'memoryelaine-http)
(require 'memoryelaine-state)

(defvar memoryelaine-show-buffer-name "*memoryelaine-entry*"
  "Name of the detail/show buffer.")

(defvar memoryelaine-show-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "q") #'quit-window)
    (define-key map (kbd "g") #'memoryelaine-show-refresh)
    (define-key map (kbd "v") #'memoryelaine-show-toggle-view)
    (define-key map (kbd "t") #'memoryelaine-show-fetch-full-bodies)
    map)
  "Keymap for `memoryelaine-show-mode'.")

(define-derived-mode memoryelaine-show-mode special-mode "MemoryElaine-Show"
  "Major mode for viewing a single memoryelaine log entry."
  (setq buffer-read-only t))

;; --- Entry point ---

(defun memoryelaine-show-entry (entry-id)
  "Display log ENTRY-ID in the show buffer.
Fetches metadata and preview bodies."
  (let ((buf (get-buffer-create memoryelaine-show-buffer-name)))
    (with-current-buffer buf
      (unless (derived-mode-p 'memoryelaine-show-mode)
        (memoryelaine-show-mode))
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
                   (memoryelaine-show--render)
                   ;; Fetch preview bodies
                   (when (alist-get 'has_request_body entry)
                     (memoryelaine-show--fetch-body entry-id "req" "raw" nil))
                   (when (alist-get 'has_response_body entry)
                     (memoryelaine-show--fetch-body entry-id "resp" "raw" nil)))
               (memoryelaine-log-error "Detail error: HTTP %d" status)))))))))

(defun memoryelaine-show--fetch-body (entry-id part mode full)
  "Fetch body for ENTRY-ID.
PART is \"req\" or \"resp\".  MODE is \"raw\" or \"assembled\".
FULL is non-nil to fetch the complete body."
  (let ((buf (get-buffer memoryelaine-show-buffer-name))
        (params `(("part" . ,part)
                  ("mode" . ,mode))))
    (when full
      (push '("full" . "true") params))
    (memoryelaine-http-get
     (format "/api/logs/%d/body" entry-id) params
     (lambda (status data err)
       (when (buffer-live-p buf)
         (with-current-buffer buf
           (if err
               (memoryelaine-log-error "Body fetch failed (%s): %s" part err)
             (when (and (>= status 200) (< status 300))
               (let ((content (or (alist-get 'content data) ""))
                     (available (alist-get 'available data)))
                 (if available
                     (memoryelaine-state-detail-set-body part mode content data)
                   (memoryelaine-log-debug "Body not available (%s): %s"
                                           part (alist-get 'reason data)))
                 (memoryelaine-show--render))))))))))

;; --- Rendering ---

(defun memoryelaine-show--render-loading ()
  "Render a loading indicator in the show buffer."
  (let ((inhibit-read-only t))
    (erase-buffer)
    (insert "Loading...\n")))

(defun memoryelaine-show--render ()
  "Render the current detail state into the show buffer."
  (let ((inhibit-read-only t)
        (entry memoryelaine-state--metadata)
        (sv memoryelaine-state--stream-view)
        (pos (point)))
    (erase-buffer)
    (when entry
      ;; Title
      (insert (propertize (format "Log #%d\n" memoryelaine-state--entry-id)
                          'face 'bold))
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
        (when err
          (memoryelaine-show--insert-field "Error" err)))
      (insert "\n")

      ;; Request Headers
      (insert (propertize "─── Request Headers ───\n" 'face 'bold))
      (memoryelaine-show--insert-headers (alist-get 'req_headers entry))
      (insert "\n")

      ;; Request Body
      (insert (propertize (format "─── Request Body (%s%s) ───\n"
                                  (memoryelaine-show--format-bytes (alist-get 'req_bytes entry))
                                  (if (alist-get 'req_truncated entry) ", TRUNCATED" ""))
                          'face 'bold))
      (memoryelaine-show--insert-body "req")
      (insert "\n")

      ;; Response Headers
      (insert (propertize "─── Response Headers ───\n" 'face 'bold))
      (memoryelaine-show--insert-headers (alist-get 'resp_headers entry))
      (insert "\n")

      ;; Response Body (with stream view info)
      (insert (propertize (format "─── Response Body (%s%s) ───\n"
                                  (memoryelaine-show--format-bytes (alist-get 'resp_bytes entry))
                                  (if (alist-get 'resp_truncated entry) ", TRUNCATED" ""))
                          'face 'bold))
      ;; Stream view status
      (when sv
        (let ((assembled (alist-get 'assembled_available sv))
              (reason (alist-get 'reason sv)))
          (cond
           ((and assembled (eq memoryelaine-state--resp-view-mode 'assembled))
            (insert "[Stream View: Assembled] "))
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
      (insert (propertize "q:back  g:refresh  v:toggle view  t:load full bodies"
                          'face 'shadow)))
    (goto-char (min pos (point-max)))))

(defun memoryelaine-show--insert-field (label value)
  "Insert a metadata LABEL: VALUE line."
  (insert (format "%-12s %s\n" (concat label ":") value)))

(defun memoryelaine-show--insert-headers (headers)
  "Insert HEADERS alist as formatted key: value lines."
  (if (or (null headers) (and (listp headers) (null (car headers))))
      (insert "  (none)\n")
    (dolist (pair headers)
      (let ((key (car pair))
            (val (cdr pair)))
        (insert (format "  %s: %s\n"
                        key
                        (if (listp val)
                            (mapconcat (lambda (v) (format "%s" v)) val ", ")
                          (format "%s" val))))))))

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
         (body-info (if is-resp
                        memoryelaine-state--resp-body-info
                      memoryelaine-state--req-body-info))
         (body-state (if is-resp
                         memoryelaine-state--resp-body-state
                       memoryelaine-state--req-body-state)))
    (cond
     ((eq body-state 'none)
      (insert "  [Not loaded]\n"))
     (body
      (let ((included (alist-get 'included_bytes body-info))
            (total (alist-get 'total_bytes body-info))
            (truncated (alist-get 'truncated body-info)))
        (when (and included total truncated)
          (insert (propertize
                   (format "  [Preview: %s / %s — press t to load full]\n"
                           (memoryelaine-show--format-bytes included)
                           (memoryelaine-show--format-bytes total))
                   'face 'warning)))
        (insert body)
        (unless (string-suffix-p "\n" body)
          (insert "\n"))))
     (t
      (insert "  (empty)\n")))))

;; --- Interactive commands ---

(defun memoryelaine-show-refresh ()
  "Refresh the current entry's metadata and preview bodies."
  (interactive)
  (when memoryelaine-state--entry-id
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
                                             "resp" "assembled" nil)))
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
        (memoryelaine-show--fetch-body memoryelaine-state--entry-id "resp" "raw" t)))))

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
