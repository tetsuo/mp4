#!/usr/bin/env bash
# Generates AV1 fixtures for codec-string tests. libsvtav1 emits profile 0,
# main-tier streams, so the av1C fields are patched to cover the combinations
# the codec string builder handles.
set -euo pipefail

dir="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
base="${tmp}/base.mp4"

ffmpeg -y -loglevel error \
	-f lavfi -i "testsrc=duration=1:size=320x240:rate=30" \
	-c:v libsvtav1 -pix_fmt yuv420p -g 30 \
	-movflags +faststart "$base"

# patch writes av1C byte 1 (seq_profile<<5 | seq_level_idx) and byte 2
# (seq_tier<<7 | high_bitdepth<<6 | twelve_bit<<5), then saves the file.
patch() {
	python3 - "$base" "$1" "$2" "${dir}/$3" <<'PY'
import sys

src, b1, b2, out = sys.argv[1], int(sys.argv[2], 16), int(sys.argv[3], 16), sys.argv[4]
data = bytearray(open(src, "rb").read())
i = data.find(b"av1C")
if i < 0:
    raise SystemExit("av1C box not found")
data[i + 5] = b1
data[i + 6] = b2
open(out, "wb").write(data)
PY
	echo "wrote $3"
}

patch 08 00 av1-main8-level8.mp4   # av01.0.08M.08
patch 0c 00 av1-main8-level12.mp4  # av01.0.12M.08
patch 0c c0 av1-hightier10.mp4     # av01.0.12H.10
patch 48 60 av1-profile2-12bit.mp4 # av01.2.08M.12
