# General

The assembled view (stiched-together stream data packets) should be the default view, (a user who needs to see indiviual parts for e.g. debugging can always opt to do so).

There should be default short-cuts for "export raw to file" for both request body, response body (both for assembled and non-assembled view). In emacs and the TUI the user should be prompted for a path to save to. In the web ui, a download should be initiated. Default names, when needed should be:
- either request-body.txt or request-body.json (when parseable as json)
- either response-body-assembled.txt or response-body-assembled.json (when parseable as json)
- ...or: response-body-parts.txt

## Detail view

Currently, the response body in its assembled view only shows the stitched together content (both in the web-ui and the emacs client, possibly also the TUI?). The so-called 'reasoning_content' is missing in the assembled view. We would like there to be two sections in the response body assembled view. The first—detaulf folded—section, when present, would contain the reasoning content, and the second—default expanded—section would contain the stitched together 'content'.
