# Changelog

All notable changes are listed here. The project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0]

First release.

- `sphinx gaze` inspects PNG, JPEG, PDF, DOCX, XLSX and PPTX files against 18 rules and prints text, JSON, SARIF or Markdown.
- `sphinx reveal` extracts what files hide: pixels cropped away by the aCropalypse bug, text under boxes in PDFs, earlier PDF revisions, stale thumbnails, motion-photo videos, tracked deletions, hidden sheets, speaker notes and attachments.
- `sphinx seal` writes cleaned copies of images and Office files and re-inspects them.
- The website runs the same engine in the browser through WebAssembly.
- GitHub Action, pre-commit hook and Docker image.
