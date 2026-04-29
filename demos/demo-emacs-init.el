;;; demo-emacs-init.el --- Emacs init for demo recording  -*- lexical-binding: t; -*-
;; Loaded by the VHS tape and GUI script to configure memoryelaine for the demo.
(require 'memoryelaine)
(setq memoryelaine-base-url "http://127.0.0.1:18677"
      memoryelaine-username "demo"
      memoryelaine-password "demo1234")
(memoryelaine)
