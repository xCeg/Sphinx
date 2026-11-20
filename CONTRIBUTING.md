# Contributing

Thanks for helping the Sphinx ask better questions.

## The most useful contributions

1. **A file Sphinx gets wrong.** A leak it misses, or a warning about something that is not hidden. Open a [false positive](.github/ISSUE_TEMPLATE/false-positive.yml) or [bug](.github/ISSUE_TEMPLATE/bug.yml) issue. Never attach a file with real personal data; describe how it was produced, or make a minimal copy with invented content.
2. **A new kind of leak**, with a public incident or a reproducible way to create it.
3. **A new format**: HEIC, ODF, EML, PSD and SVG are open.

## Working on the code

```sh
go test ./...          # unit and end-to-end tests
make lint              # gofmt and go vet
go generate ./...      # rebuild docs/rules.md after editing the rule catalog
make serve             # build the website and serve it on localhost:8000
```

Go 1.22 or later, no other dependency. The module has no third-party imports and should keep it that way: Sphinx reads untrusted files, and every dependency is code that runs on them.

Layout:

| Path | What lives there |
|---|---|
| `sphinx.go` | public API: `Inspect`, `Reveal`, `Seal` |
| `internal/leak` | severities, findings, the rule catalog |
| `internal/pngfile`, `jpegfile`, `pdf`, `ooxml` | one package per format |
| `internal/inflate` | DEFLATE decoder that tolerates missing history (aCropalypse recovery) |
| `internal/exif`, `xmp`, `photo` | metadata shared by image formats |
| `internal/cli`, `internal/render` | the command and its output formats |
| `cmd/sphinx-wasm`, `site/` | the website, which runs Sphinx in the browser |
| `scripts/` | calibration, demo files, screenshots |

## Rules for changes

- Every new check comes with a test for the leak **and** a test for the closest legitimate case that must stay silent. `internal/pdf/pdf_test.go` (`TestLegitimateFillsAreNotRedactions`) shows the pattern.
- Changes to detection are run against the calibration corpus (`scripts/fetch-corpus.sh`, then `go run ./scripts/calibrate DIR`) and the before/after numbers go in the pull request.
- Messages are written for someone who has never heard of metadata. Say what the file reveals, not how the format stores it.
- Commit messages are imperative and explain why.
