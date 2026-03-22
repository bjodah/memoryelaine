;;; memoryelaine-state.el --- State management for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Manages search state (global) and detail buffer state (buffer-local).
;; State mutations go through functions to enable future refactoring.

;;; Code:

;;; --- Search state (global) ---

(defvar memoryelaine-state--query ""
  "Current query DSL string.")

(defvar memoryelaine-state--limit 50
  "Current page size.")

(defvar memoryelaine-state--offset 0
  "Current pagination offset.")

(defvar memoryelaine-state--total 0
  "Total number of matching entries from last query.")

(defvar memoryelaine-state--summaries nil
  "List of summary alists from the last query response.")

(defvar memoryelaine-state--loading nil
  "Non-nil when a search request is in flight.")

(defvar memoryelaine-state--generation 0
  "Generation counter for the current search.
Used to discard stale responses.")

(defvar memoryelaine-state--recording t
  "Current recording state from the server.")

(defun memoryelaine-state-set-query (query)
  "Set the search QUERY and reset pagination."
  (setq memoryelaine-state--query query
        memoryelaine-state--offset 0))

(defun memoryelaine-state-set-results (summaries total)
  "Update search results with SUMMARIES list and TOTAL count."
  (setq memoryelaine-state--summaries summaries
        memoryelaine-state--total total))

(defun memoryelaine-state-set-loading (loading)
  "Set the LOADING flag."
  (setq memoryelaine-state--loading loading))

(defun memoryelaine-state-next-generation ()
  "Increment and return the next search generation number."
  (setq memoryelaine-state--generation (1+ memoryelaine-state--generation)))

(defun memoryelaine-state-set-recording (state)
  "Set the recording STATE."
  (setq memoryelaine-state--recording state))

(defun memoryelaine-state-next-page ()
  "Advance to the next page if possible.  Return non-nil if advanced."
  (when (< (+ memoryelaine-state--offset memoryelaine-state--limit)
           memoryelaine-state--total)
    (setq memoryelaine-state--offset
          (+ memoryelaine-state--offset memoryelaine-state--limit))
    t))

(defun memoryelaine-state-prev-page ()
  "Go back to the previous page if possible.  Return non-nil if moved."
  (when (> memoryelaine-state--offset 0)
    (setq memoryelaine-state--offset
          (max 0 (- memoryelaine-state--offset memoryelaine-state--limit)))
    t))

(defun memoryelaine-state-current-page ()
  "Return the current 1-based page number."
  (1+ (/ memoryelaine-state--offset memoryelaine-state--limit)))

(defun memoryelaine-state-total-pages ()
  "Return the total number of pages."
  (max 1 (ceiling memoryelaine-state--total memoryelaine-state--limit)))

(defun memoryelaine-state-has-more ()
  "Return non-nil if there are more pages."
  (< (+ memoryelaine-state--offset memoryelaine-state--limit)
     memoryelaine-state--total))

;;; --- Detail state (buffer-local) ---

(defvar-local memoryelaine-state--entry-id nil
  "ID of the currently displayed log entry.")

(defvar-local memoryelaine-state--metadata nil
  "Full metadata alist from the detail endpoint.")

(defvar-local memoryelaine-state--stream-view nil
  "Stream view metadata alist.")

(defvar-local memoryelaine-state--resp-view-mode 'raw
  "Current response view mode: `raw' or `assembled'.")

(defvar-local memoryelaine-state--req-body-state 'none
  "Request body loading state: `none', `preview', or `full'.")

(defvar-local memoryelaine-state--resp-body-state 'none
  "Response body loading state: `none', `preview', or `full'.")

(defvar-local memoryelaine-state--req-body nil
  "Cached request body content (string or nil).")

(defvar-local memoryelaine-state--resp-body nil
  "Cached response body content (string or nil).")

(defvar-local memoryelaine-state--resp-body-assembled nil
  "Cached assembled response body content (string or nil).")

(defvar-local memoryelaine-state--req-body-info nil
  "Alist with body response metadata for request.")

(defvar-local memoryelaine-state--resp-body-info nil
  "Alist with body response metadata for response.")

(defvar-local memoryelaine-state--resp-body-assembled-state 'none
  "Assembled response body loading state: `none', `preview', or `full'.")

(defvar-local memoryelaine-state--resp-body-assembled-info nil
  "Alist with body response metadata for assembled response.")

(defvar-local memoryelaine-state--detail-loading nil
  "Non-nil when detail data is being fetched.")

(defvar-local memoryelaine-state--detail-generation 0
  "Generation counter for detail requests.")

(defun memoryelaine-state-detail-init (entry-id)
  "Initialize detail state for ENTRY-ID."
  (setq memoryelaine-state--entry-id entry-id
        memoryelaine-state--metadata nil
        memoryelaine-state--stream-view nil
        memoryelaine-state--resp-view-mode 'raw
        memoryelaine-state--req-body-state 'none
        memoryelaine-state--resp-body-state 'none
        memoryelaine-state--req-body nil
        memoryelaine-state--resp-body nil
        memoryelaine-state--resp-body-assembled nil
        memoryelaine-state--req-body-info nil
        memoryelaine-state--resp-body-info nil
        memoryelaine-state--resp-body-assembled-state 'none
        memoryelaine-state--resp-body-assembled-info nil
        memoryelaine-state--detail-loading nil
        memoryelaine-state--detail-generation 0))

(defun memoryelaine-state-detail-set-metadata (metadata stream-view)
  "Set detail METADATA and STREAM-VIEW data."
  (setq memoryelaine-state--metadata metadata
        memoryelaine-state--stream-view stream-view))

(defun memoryelaine-state-detail-set-body (part mode content body-info)
  "Cache body CONTENT and BODY-INFO for PART and MODE.
PART is \"req\" or \"resp\".  MODE is \"raw\" or \"assembled\"."
  (let ((full (not (alist-get 'truncated body-info))))
    (pcase part
      ("req"
       (setq memoryelaine-state--req-body content
             memoryelaine-state--req-body-info body-info
             memoryelaine-state--req-body-state (if full 'full 'preview)))
      ("resp"
       (if (string= mode "assembled")
           (setq memoryelaine-state--resp-body-assembled content
                 memoryelaine-state--resp-body-assembled-info body-info
                 memoryelaine-state--resp-body-assembled-state (if full 'full 'preview))
         (setq memoryelaine-state--resp-body content
               memoryelaine-state--resp-body-info body-info
               memoryelaine-state--resp-body-state (if full 'full 'preview)))))))

(defun memoryelaine-state-detail-next-generation ()
  "Increment and return the next detail generation number."
  (setq memoryelaine-state--detail-generation
        (1+ memoryelaine-state--detail-generation)))

(provide 'memoryelaine-state)
;;; memoryelaine-state.el ends here
