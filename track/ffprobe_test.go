package track_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

type ffStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SampleRate string `json:"sample_rate"`
	Channels   int    `json:"channels"`
	NbFrames   string `json:"nb_frames"`
}

type ffProbe struct {
	Streams []ffStream `json:"streams"`
}

// codecFamily maps an RFC 6381 codec string to the codec name ffprobe reports.
func codecFamily(codec string) string {
	switch {
	case strings.HasPrefix(codec, "avc1"):
		return "h264"
	case strings.HasPrefix(codec, "av01"):
		return "av1"
	case strings.HasPrefix(codec, "mp4a"):
		return "aac"
	}
	return codec
}

func readMoov(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}
	r := mp4.NewReader(data)
	for r.Next() {
		if r.Type() == mp4.TypeMoov {
			return append([]byte(nil), r.RawBox()...)
		}
	}
	t.Fatalf("no moov box in %s", path)
	return nil
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

// TestParityWithFFprobe checks the parser against ffprobe for each sample file:
// the audio+video stream count, codec family, video dimensions, audio sample
// rate, per-track sample count, and the video sync-sample count against
// ffprobe's keyframe count. It is skipped when ffprobe or a file is absent.
func TestParityWithFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}

	files := []string{
		"../test-data/h264-aac-10s.mp4",
		"../test-data/h264-aac-60s.mp4",
		"../test-data/aac-stereo-2s.m4a",
		"../test-data/alac-5.1-2s.m4a",
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := os.Stat(path); err != nil {
				t.Skipf("test file not available: %v", err)
			}

			tracks, _, _, err := track.ParseTracks(readMoov(t, path))
			if err != nil {
				t.Fatal(err)
			}

			out, err := exec.Command("ffprobe", "-v", "error",
				"-print_format", "json", "-show_streams", path).Output()
			if err != nil {
				t.Fatalf("ffprobe: %v", err)
			}
			var probe ffProbe
			if err := json.Unmarshal(out, &probe); err != nil {
				t.Fatal(err)
			}

			var ffVideo, ffAudio *ffStream
			ffCount := 0
			for i := range probe.Streams {
				switch probe.Streams[i].CodecType {
				case "video":
					ffVideo = &probe.Streams[i]
					ffCount++
				case "audio":
					ffAudio = &probe.Streams[i]
					ffCount++
				}
			}
			if len(tracks) != ffCount {
				t.Fatalf("track count = %d, ffprobe audio+video streams = %d", len(tracks), ffCount)
			}

			var libVideo, libAudio *track.Track
			for _, tr := range tracks {
				switch tr.Kind {
				case track.TrackVideo:
					libVideo = tr
				case track.TrackAudio:
					libAudio = tr
				}
			}

			if ffVideo != nil {
				if libVideo == nil {
					t.Fatal("ffprobe found a video stream, parser did not")
				}
				if got := codecFamily(libVideo.Codec()); got != ffVideo.CodecName {
					t.Errorf("video codec = %q (%q), ffprobe = %q", libVideo.Codec(), got, ffVideo.CodecName)
				}
				if int(libVideo.Width) != ffVideo.Width || int(libVideo.Height) != ffVideo.Height {
					t.Errorf("video dimensions = %dx%d, ffprobe = %dx%d",
						libVideo.Width, libVideo.Height, ffVideo.Width, ffVideo.Height)
				}
				if n := atoi(t, ffVideo.NbFrames); n != len(libVideo.Samples) {
					t.Errorf("video sample count = %d, ffprobe frames = %d", len(libVideo.Samples), n)
				}
				sync := 0
				for i := range libVideo.Samples {
					if libVideo.Samples[i].IsSync() {
						sync++
					}
				}
				if kf := ffprobeKeyframes(t, path); sync != kf {
					t.Errorf("video sync samples = %d, ffprobe keyframes = %d", sync, kf)
				}
			}

			if ffAudio != nil {
				if libAudio == nil {
					t.Fatal("ffprobe found an audio stream, parser did not")
				}
				if got := codecFamily(libAudio.Codec()); got != ffAudio.CodecName {
					t.Errorf("audio codec = %q (%q), ffprobe = %q", libAudio.Codec(), got, ffAudio.CodecName)
				}
				if sr := atoi(t, ffAudio.SampleRate); sr != int(libAudio.SampleRate) {
					t.Errorf("audio sample rate = %d, ffprobe = %d", libAudio.SampleRate, sr)
				}
				if int(libAudio.ChannelCount) != ffAudio.Channels {
					t.Errorf("audio channel count = %d, ffprobe = %d", libAudio.ChannelCount, ffAudio.Channels)
				}
				if n := atoi(t, ffAudio.NbFrames); n != len(libAudio.Samples) {
					t.Errorf("audio sample count = %d, ffprobe frames = %d", len(libAudio.Samples), n)
				}
			}
		})
	}
}

// ffprobeKeyframes counts the keyframes in the first video stream.
func ffprobeKeyframes(t *testing.T, path string) int {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-skip_frame", "nokey", "-show_entries", "frame=pts_time",
		"-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe keyframes: %v", err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}
