#!/usr/bin/env bash
# Generates deterministic ISOBMFF fixtures used by tests and benchmarks.
set -euo pipefail

dir="$(cd "$(dirname "$0")" && pwd)"

# Progressive H.264/AAC files with B-frames and a front-loaded moov.
gen_av() {
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

gen_av 10 h264-aac-10s.mp4
gen_av 60 h264-aac-60s.mp4

# Audio-only input exercises fragment cuts without a video clock.
ffmpeg -y -loglevel error \
	-f lavfi -i "sine=frequency=440:duration=10:sample_rate=48000" \
	-c:a aac -b:a 128k -movflags +faststart \
	"${dir}/aac-10s.mp4"
echo "wrote aac-10s.mp4"

# Exact one-second GOPs exercise fragments containing interior keyframes.
ffmpeg -y -loglevel error \
	-f lavfi -i "testsrc=duration=10:size=640x480:rate=30" \
	-f lavfi -i "sine=frequency=440:duration=10:sample_rate=48000" \
	-c:v libx264 -preset veryfast -pix_fmt yuv420p \
	-g 30 -keyint_min 30 -sc_threshold 0 \
	-c:a aac -b:a 128k -movflags +faststart \
	"${dir}/h264-aac-10s-gop1.mp4"
echo "wrote h264-aac-10s-gop1.mp4"

# Two aligned video renditions and two audio tracks exercise muxed track
# selection and fragment-boundary validation.
ffmpeg -y -loglevel error \
	-f lavfi -i "testsrc=duration=3:size=160x90:rate=10" \
	-f lavfi -i "testsrc2=duration=3:size=128x72:rate=10" \
	-f lavfi -i "sine=frequency=440:duration=3:sample_rate=48000" \
	-f lavfi -i "sine=frequency=880:duration=3:sample_rate=48000" \
	-map 0:v -map 1:v -map 2:a -map 3:a \
	-c:v libx264 -preset veryfast -profile:v baseline -pix_fmt yuv420p \
	-g 10 -keyint_min 10 -sc_threshold 0 \
	-c:a aac -b:a 64k -movflags +faststart \
	"${dir}/h264-2video-2audio-3s.mp4"
echo "wrote h264-2video-2audio-3s.mp4"

# More than four tracks exercises dynamically-sized reader and writer state.
audio_inputs=()
audio_maps=()
for i in 0 1 2 3 4 5; do
	audio_inputs+=( -f lavfi -i "sine=frequency=$((440 + i * 110)):duration=1:sample_rate=48000" )
	audio_maps+=( -map "${i}:a" )
done
ffmpeg -y -loglevel error \
	"${audio_inputs[@]}" "${audio_maps[@]}" \
	-c:a aac -b:a 32k -movflags +faststart \
	"${dir}/aac-6track-1s.mp4"
echo "wrote aac-6track-1s.mp4"

# Small audio fixtures cover channel metadata and ALAC sample entries.
ffmpeg -y -loglevel error \
	-f lavfi -i "sine=frequency=440:duration=2:sample_rate=48000" \
	-ac 2 -c:a aac -b:a 128k -movflags +faststart \
	"${dir}/aac-stereo-2s.m4a"
echo "wrote aac-stereo-2s.m4a"

ffmpeg -y -loglevel error \
	-f lavfi -i "anullsrc=channel_layout=5.1:sample_rate=48000" -t 2 \
	-c:a alac -movflags +faststart \
	"${dir}/alac-5.1-2s.m4a"
echo "wrote alac-5.1-2s.m4a"
