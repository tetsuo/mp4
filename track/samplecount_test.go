package track_test

import (
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

// patchFirstStsz overwrites the sample_size and sample_count fields of the first
// stsz box in buf. A non-zero sample size selects the constant-size form, which
// carries no per-sample table.
func patchFirstStsz(buf []byte, sampleSize, count uint32) bool {
	var walk func(r *mp4.Reader) bool
	walk = func(r *mp4.Reader) bool {
		for r.Next() {
			if r.Type() == mp4.TypeStsz {
				off := r.DataOffset()
				binary.BigEndian.PutUint32(buf[off:off+4], sampleSize)
				binary.BigEndian.PutUint32(buf[off+4:off+8], count)
				return true
			}
			if mp4.IsContainerBox(r.Type()) {
				r.Enter()
				found := walk(r)
				r.Exit()
				if found {
					return true
				}
			}
		}
		return false
	}
	r := mp4.NewReader(buf)
	return walk(&r)
}

// TestParseTracksRejectsImplausibleSampleCount patches a track's stsz to declare
// far more samples than the file describes. The parser must reject the count
// rather than allocate for it: 1<<30 samples would otherwise need about 32 GB,
// since Sample is 32 bytes. Both stsz forms are covered, since a per-sample
// table is bounded by its entries and a constant-size box by a fixed cap.
func TestParseTracksRejectsImplausibleSampleCount(t *testing.T) {
	cases := []struct {
		name       string
		sampleSize uint32
	}{
		{"per-sample", 0},
		{"constant-size", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			moov := readMoov(t, "../test-data/big-buck-bunny-480p-30sec.mp4")
			if !patchFirstStsz(moov, tc.sampleSize, 1<<30) {
				t.Fatal("no stsz box found to patch")
			}

			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			tracks, _, _, _ := track.ParseTracks(moov)
			runtime.ReadMemStats(&after)

			if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
				t.Errorf("ParseTracks allocated %d bytes for a bogus stsz count, want bounded", grew)
			}
			// No real sample file approaches this many samples, so a surviving
			// track this large means the bogus count was trusted.
			for _, tr := range tracks {
				if len(tr.Samples) > 1<<20 {
					t.Errorf("track kept %d samples from a bogus count, want it dropped", len(tr.Samples))
				}
			}
		})
	}
}
