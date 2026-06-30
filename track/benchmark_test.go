package track_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

var benchFiles = []string{
	"../test-data/h264-aac-10s.mp4",
	"../test-data/h264-aac-60s.mp4",
}

// loadMoov reads path and returns a copy of its top-level moov box, including
// the box header, which is the input ParseTracks expects.
func loadMoov(b *testing.B, path string) []byte {
	b.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		b.Skipf("test file not available: %v", err)
	}
	r := mp4.NewReader(data)
	for r.Next() {
		if r.Type() == mp4.TypeMoov {
			raw := r.RawBox()
			moov := make([]byte, len(raw))
			copy(moov, raw)
			return moov
		}
	}
	b.Skipf("no moov box in %s", path)
	return nil
}

func BenchmarkParseTracks(b *testing.B) {
	for _, path := range benchFiles {
		moov := loadMoov(b, path)
		b.Run(filepath.Base(path), func(b *testing.B) {
			b.SetBytes(int64(len(moov)))
			b.ReportAllocs()
			for b.Loop() {
				tracks, _, err := track.ParseTracks(moov)
				if err != nil {
					b.Fatal(err)
				}
				if len(tracks) == 0 {
					b.Fatal("no tracks parsed")
				}
			}
		})
	}
}

func BenchmarkParseTracksAndCodec(b *testing.B) {
	for _, path := range benchFiles {
		moov := loadMoov(b, path)
		b.Run(filepath.Base(path), func(b *testing.B) {
			b.SetBytes(int64(len(moov)))
			b.ReportAllocs()
			var sink int
			for b.Loop() {
				tracks, _, err := track.ParseTracks(moov)
				if err != nil {
					b.Fatal(err)
				}
				for _, t := range tracks {
					sink += len(t.Codec())
				}
			}
			_ = sink
		})
	}
}

func BenchmarkCollectTrackSampleStats(b *testing.B) {
	for _, path := range benchFiles {
		moov := loadMoov(b, path)
		tracks, _, err := track.ParseTracks(moov)
		if err != nil {
			b.Fatal(err)
		}
		var samples []track.Sample
		for _, t := range tracks {
			samples = append(samples, t.Samples...)
		}
		var dst []track.TrackSampleStats
		b.Run(filepath.Base(path), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dst = track.CollectTrackSampleStats(dst[:0], tracks, samples)
			}
		})
	}
}
