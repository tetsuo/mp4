package fragment_test

import (
	"io"
	"testing"

	"github.com/tetsuo/mp4/fragment"
	"github.com/tetsuo/mp4/track"
)

func BenchmarkNewReader(b *testing.B) {
	f := openTestFile(b)
	defer f.Close()

	b.ReportAllocs()
	for b.Loop() {
		f.Seek(0, io.SeekStart)
		if _, _, err := fragment.NewReader(f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderReset(b *testing.B) {
	f := openTestFile(b)
	defer f.Close()

	r, _, err := fragment.NewReader(f)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		f.Seek(0, io.SeekStart)
		if _, err := r.Reset(f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadWriteFragments(b *testing.B) {
	f := openTestFile(b)
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		b.Fatal(err)
	}

	w := fragment.NewWriter(io.Discard)

	b.SetBytes(info.Size())
	b.ReportAllocs()
	for b.Loop() {
		f.Seek(0, io.SeekStart)
		frag, initSeg, err := fragment.NewReader(f)
		if err != nil {
			b.Fatal(err)
		}

		w.Reset(io.Discard)
		if err := w.WriteInit(initSeg); err != nil {
			b.Fatal(err)
		}

		for {
			fr, err := frag.ReadFragment()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			if err := w.WriteFragment(fr, f); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteFragmentsOnly(b *testing.B) {
	f := openTestFile(b)
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		b.Fatal(err)
	}

	frag, initSeg, err := fragment.NewReader(f)
	if err != nil {
		b.Fatal(err)
	}

	var frags []*fragment.Fragment
	for {
		fr, err := frag.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			b.Fatal(err)
		}
		samples := make([]track.Sample, len(fr.Samples))
		copy(samples, fr.Samples)
		frags = append(frags, &fragment.Fragment{
			Samples:     samples,
			SequenceNum: fr.SequenceNum,
		})
	}

	w := fragment.NewWriter(io.Discard)

	b.SetBytes(info.Size())
	b.ReportAllocs()
	for b.Loop() {
		w.Reset(io.Discard)
		if err := w.WriteInit(initSeg); err != nil {
			b.Fatal(err)
		}
		for _, fr := range frags {
			if err := w.WriteFragment(fr, f); err != nil {
				b.Fatal(err)
			}
		}
	}
}
