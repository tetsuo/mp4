#!/usr/bin/env bash
# Generates synthetic ISOBMFF files for benchmarking the track parser. Each file
# carries an H.264 video track and an AAC audio track. B-frames produce a ctts
# table and faststart places the moov box before mdat.
set -euo pipefail

dir="$(cd "$(dirname "$0")" && pwd)"

gen() {
	local dur="$1" name="$2"
	ffmpeg -y -loglevel error \
		-f lavfi -i "testsrc=duration=${dur}:size=640x480:rate=30" \
		-f lavfi -i "sine=frequency=440:duration=${dur}:sample_rate=48000" \
		-c:v libx264 -preset veryfast -pix_fmt yuv420p \
		-c:a aac -b:a 128k \
		-movflags +faststart \
		"${dir}/${name}"
	echo "wrote ${name}"
}

gen 10 h264-aac-10s.mp4
gen 60 h264-aac-60s.mp4
