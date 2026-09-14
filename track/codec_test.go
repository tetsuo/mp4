package track_test

import (
	"testing"

	"github.com/tetsuo/mp4/track"
)

// TestParseAV1CodecString checks the av01 codec string the track parser derives
// from the av1C configuration record for each combination of seq_profile,
// seq_level_idx, seq_tier, and bit depth. The fixtures are produced by
// test-data/generate-av1.sh and skipped when absent.
func TestParseAV1CodecString(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"../test-data/av1-main8-level8.mp4", "av01.0.08M.08"},
		{"../test-data/av1-main8-level12.mp4", "av01.0.12M.08"},
		{"../test-data/av1-hightier10.mp4", "av01.0.12H.10"},
		{"../test-data/av1-profile2-12bit.mp4", "av01.2.08M.12"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			tracks, _, _, err := track.ParseTracks(readMoov(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			var vt *track.Track
			for _, tr := range tracks {
				if tr.Kind == track.TrackVideo {
					vt = tr
					break
				}
			}
			if vt == nil {
				t.Fatal("no video track parsed")
			}
			if got := vt.Codec(); got != tc.want {
				t.Errorf("Codec() = %q, want %q", got, tc.want)
			}
		})
	}
}
