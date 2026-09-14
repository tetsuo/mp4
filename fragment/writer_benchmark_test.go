package fragment

import (
	"io"
	"testing"

	"github.com/tetsuo/mp4/track"
)

func BenchmarkWriterPrepare(b *testing.B) {
	samples, _ := buildSamples()
	frag := Fragment{Samples: samples, SequenceNum: 1}
	w := NewWriter(io.Discard)

	b.ReportAllocs()
	for b.Loop() {
		if err := w.Prepare(&frag); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriterPrepareTrack(b *testing.B) {
	samples, _ := buildSamples()
	frag := Fragment{Samples: samples, SequenceNum: 1}
	w := NewWriter(io.Discard)

	b.ReportAllocs()
	for b.Loop() {
		if err := w.PrepareTrack(&frag, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriterPrepareSixTracks(b *testing.B) {
	samples := make([]track.Sample, 600)
	for i := range samples {
		samples[i].TrackID = uint32(i%6 + 1)
		samples[i].Duration = 1024
		samples[i].SetSize(128, true)
	}
	frag := Fragment{Samples: samples, SequenceNum: 1}
	w := NewWriter(io.Discard)
	if err := w.Prepare(&frag); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := w.Prepare(&frag); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWriterPrepareSixTracksHasNoWarmAllocations(t *testing.T) {
	samples := make([]track.Sample, 600)
	for i := range samples {
		samples[i].TrackID = uint32(i%6 + 1)
		samples[i].Duration = 1024
		samples[i].SetSize(128, true)
	}
	frag := Fragment{Samples: samples, SequenceNum: 1}
	w := NewWriter(io.Discard)
	if err := w.Prepare(&frag); err != nil {
		t.Fatal(err)
	}
	var prepareErr error
	allocs := testing.AllocsPerRun(100, func() {
		prepareErr = w.Prepare(&frag)
	})
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	if allocs != 0 {
		t.Fatalf("warm six-track prepare allocations = %v, want 0", allocs)
	}
}
