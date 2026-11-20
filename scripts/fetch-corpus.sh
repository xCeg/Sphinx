#!/bin/sh
# Fetches the public test corpora used to calibrate Sphinx, pinned to the
# commits the README's numbers were measured on.
set -eu
dest=${1:?usage: fetch-corpus.sh DIR}
mkdir -p "$dest"
cd "$dest"

fetch() {
	name=$1 url=$2 rev=$3
	shift 3
	if [ ! -d "$name" ]; then
		git clone --quiet --filter=blob:none --no-checkout "$url" "$name"
	fi
	if [ "$#" -gt 0 ]; then
		git -C "$name" sparse-checkout set "$@"
	fi
	git -C "$name" checkout --quiet "$rev"
}

fetch pdf-samples https://github.com/py-pdf/sample-files 80d67b91f3e28909756ca7807bbe8f43b599d12f
fetch exif-samples https://github.com/ianare/exif-samples 5332a9cf0220e9c6c93f88a187daa90808051e10
fetch poi https://github.com/apache/poi f66927954a29cf1f5b86614fdf1816e6321c8aea \
	test-data/document test-data/spreadsheet test-data/slideshow
fetch pdfjs https://github.com/mozilla/pdf.js 62466e5b8f75e14f288abdc10fc2deda94c86c26 test/pdfs
