# Changelog

All notable changes are listed here. The project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0]

First release.

- `sphinx gaze` inspects PNG, JPEG, WebP, SVG, PDF, DOCX, XLSX, PPTX and zip files against 23 rules and prints text, JSON, SARIF or Markdown.
- `sphinx reveal` extracts what files hide: pixels cropped away by the aCropalypse bug, text under boxes and invisible text in PDFs, earlier PDF revisions, form data, stale thumbnails, motion-photo videos, tracked deletions, hidden sheets and columns, speaker notes, attachments and the file names a `.DS_Store` records.
- Hidden instructions for AI tools are detected in text readers cannot see.
- PDFs that open without a password are decrypted (standard security handler, revisions 2 to 6).
- `sphinx seal` writes cleaned copies of images, PDFs, Office files, SVGs and zip archives and re-inspects them.
- The website runs the same engine in the browser through WebAssembly, takes many files or a folder at once, exports reports and works offline.
- GitHub Action, pre-commit hook and Docker image.
