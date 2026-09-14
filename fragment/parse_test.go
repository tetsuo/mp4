package fragment

import (
	"bytes"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

// buildSamples returns a two-track sample set laid over a synthetic data
// buffer: four video samples (a sync sample, then three with composition
// offsets) and five audio samples. Offsets index into the returned data.
func buildSamples() ([]track.Sample, []byte) {
	var data []byte
	var samples []track.Sample
	add := func(trackID uint32, dts int64, dur uint32, cts int32, sync bool, size int) {
		sm := track.Sample{
			Offset:             int64(len(data)),
			DTS:                dts,
			TrackID:            trackID,
			Duration:           dur,
			PresentationOffset: cts,
		}
		sm.SetSize(uint32(size), sync)
		samples = append(samples, sm)
		for range size {
			data = append(data, byte(len(data)))
		}
	}
	add(1, 9000, 3000, 0, true, 500)
	add(1, 12000, 3000, 6000, false, 120)
	add(1, 15000, 3000, 3000, false, 80)
	add(1, 18000, 3000, 0, false, 90)
	for i := range 5 {
		add(2, 4800+int64(i)*1024, 1024, 0, true, 60+i)
	}
	return samples, data
}

// sampleBytes returns the sample's bytes out of src.
func sampleBytes(src []byte, s track.Sample) []byte {
	return src[s.Offset : s.Offset+int64(s.Size())]
}

// TestParseSegmentRoundTrip writes a fragment with the Writer and parses it
// back, proving every field and every sample byte survives, including when a
// styp box precedes the moof as in a packaged segment.
func TestParseSegmentRoundTrip(t *testing.T) {
	samples, src := buildSamples()

	var seg bytes.Buffer
	hdr := mp4.NewWriter(make([]byte, 64))
	hdr.WriteStyp([4]byte{'m', 's', 'd', 'h'}, 0, [][4]byte{{'m', 's', 'd', 'h'}})
	seg.Write(hdr.Bytes())

	w := NewWriter(&seg)
	if err := w.WriteFragment(&Fragment{Samples: samples, SequenceNum: 7}, bytes.NewReader(src)); err != nil {
		t.Fatal(err)
	}

	got, seq, err := ParseSegment(seg.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if seq != 7 {
		t.Fatalf("sequence %d, want 7", seq)
	}
	if len(got) != len(samples) {
		t.Fatalf("%d samples, want %d", len(got), len(samples))
	}
	// The writer groups samples by track in first-appearance order, which is
	// already the order buildSamples emits, so the lists align index by index.
	for i, want := range samples {
		g := got[i]
		if g.TrackID != want.TrackID || g.DTS != want.DTS || g.Duration != want.Duration ||
			g.PresentationOffset != want.PresentationOffset || g.Size() != want.Size() || g.IsSync() != want.IsSync() {
			t.Fatalf("sample %d: got %+v (size %d sync %v), want %+v (size %d sync %v)",
				i, g, g.Size(), g.IsSync(), want, want.Size(), want.IsSync())
		}
		if !bytes.Equal(sampleBytes(seg.Bytes(), g), sampleBytes(src, want)) {
			t.Fatalf("sample %d bytes differ", i)
		}
	}
}

// TestParseSegmentMultiMoof proves a segment holding several moof+mdat pairs
// (the low-latency parts layout) parses into the concatenated sample list.
func TestParseSegmentMultiMoof(t *testing.T) {
	samples, src := buildSamples()
	var seg bytes.Buffer
	w := NewWriter(&seg)
	if err := w.WriteFragment(&Fragment{Samples: samples[:4], SequenceNum: 1}, bytes.NewReader(src)); err != nil {
		t.Fatal(err)
	}
	w.Reset(&seg)
	if err := w.WriteFragment(&Fragment{Samples: samples[4:], SequenceNum: 2}, bytes.NewReader(src)); err != nil {
		t.Fatal(err)
	}

	got, seq, err := ParseSegment(seg.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("sequence %d, want the first moof's 1", seq)
	}
	if len(got) != len(samples) {
		t.Fatalf("%d samples, want %d", len(got), len(samples))
	}
	for i, want := range samples {
		g := got[i]
		if g.TrackID != want.TrackID || g.DTS != want.DTS || g.Size() != want.Size() {
			t.Fatalf("sample %d: got %+v, want %+v", i, g, want)
		}
		if !bytes.Equal(sampleBytes(seg.Bytes(), g), sampleBytes(src, want)) {
			t.Fatalf("sample %d bytes differ", i)
		}
	}
}

// TestParseSegmentRejectsGarbage proves the parser reports empty and
// truncated inputs rather than fabricating samples.
func TestParseSegmentRejectsGarbage(t *testing.T) {
	if _, _, err := ParseSegment(nil); err == nil {
		t.Fatal("nil input parsed")
	}
	if _, _, err := ParseSegment([]byte("not an mp4 segment")); err == nil {
		t.Fatal("garbage parsed")
	}

	samples, src := buildSamples()
	var seg bytes.Buffer
	w := NewWriter(&seg)
	if err := w.WriteFragment(&Fragment{Samples: samples, SequenceNum: 1}, bytes.NewReader(src)); err != nil {
		t.Fatal(err)
	}
	// Truncating the mdat leaves trun entries pointing past the end.
	if _, _, err := ParseSegment(seg.Bytes()[:seg.Len()-40]); err == nil {
		t.Fatal("truncated segment parsed")
	}
}
