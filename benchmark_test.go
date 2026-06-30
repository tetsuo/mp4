package mp4_test

import (
	"io"
	"os"
	"testing"

	"github.com/tetsuo/mp4"
)

func loadTestFile(b *testing.B) []byte {
	b.Helper()
	data, err := os.ReadFile("video-media-samples/big-buck-bunny-480p-30sec.mp4")
	if err != nil {
		b.Skipf("test file not available: %v", err)
	}
	return data
}

// metaSize returns the combined size of the top-level moov and moof boxes.
func metaSize(data []byte) int64 {
	var n int64
	r := mp4.NewReader(data)
	for r.Next() {
		if t := r.Type(); t == mp4.TypeMoov || t == mp4.TypeMoof {
			n += int64(r.Size())
		}
	}
	return n
}

func BenchmarkReaderParse(b *testing.B) {
	data := loadTestFile(b)

	// The reader skips the mdat payload, so throughput is measured over the
	// moov/moof bytes rather than the whole file.
	b.SetBytes(metaSize(data))

	for b.Loop() {
		r := mp4.NewReader(data)
		for r.Next() {
			if mp4.IsContainerBox(r.Type()) {
				r.Enter()
				walkBench(&r)
				r.Exit()
			}
		}
	}
}

func walkBench(r *mp4.Reader) {
	for r.Next() {
		if r.Type() == mp4.TypeStsd {
			r.Enter()
			r.Skip(4)
			if r.Next() {
				switch r.Type() {
				case mp4.TypeAvc1:
					_ = mp4.ReadVisualSampleEntry(r.Data())
				case mp4.TypeMp4a:
					_ = mp4.ReadAudioSampleEntry(r.Data())
				}
			}
			r.Exit()
			continue
		}
		if mp4.IsContainerBox(r.Type()) {
			r.Enter()
			walkBench(r)
			r.Exit()
		}
	}
}

func BenchmarkStszIter(b *testing.B) {
	data := loadTestFile(b)

	// Find stsz box data
	r := mp4.NewReader(data)
	var stszData []byte
	var findStsz func(*mp4.Reader)
	findStsz = func(r *mp4.Reader) {
		for r.Next() {
			if r.Type() == mp4.TypeStsz {
				stszData = make([]byte, len(r.Data()))
				copy(stszData, r.Data())
				return
			}
			if mp4.IsContainerBox(r.Type()) {
				r.Enter()
				findStsz(r)
				r.Exit()
				if stszData != nil {
					return
				}
			}
		}
	}
	findStsz(&r)
	if stszData == nil {
		b.Skip("no stsz found")
	}

	b.SetBytes(int64(len(stszData)))

	for b.Loop() {
		it := mp4.NewStszIter(stszData)
		for {
			_, ok := it.Next()
			if !ok {
				break
			}
		}
	}
}

func BenchmarkWriterBuild(b *testing.B) {
	buf := make([]byte, 4096)

	for b.Loop() {
		w := mp4.NewWriter(buf)
		w.WriteFtyp([4]byte{'i', 's', 'o', '5'}, 0,
			[][4]byte{{'i', 's', 'o', '5'}, {'a', 'v', 'c', '1'}})

		w.StartBox(mp4.TypeMoov)
		w.WriteMvhd(1000, 30000, 3)

		w.StartBox(mp4.TypeTrak)
		w.WriteTkhd(0x03, 1, 30000, 1920<<16, 1080<<16)
		w.StartBox(mp4.TypeMdia)
		w.WriteMdhd(12288, 368640, 0x55C4)
		w.WriteHdlr([4]byte{'v', 'i', 'd', 'e'}, "VideoHandler")
		w.EndBox() // mdia
		w.EndBox() // trak

		w.StartBox(mp4.TypeMvex)
		w.WriteTrex(1, 1, 0, 0, 0)
		w.EndBox() // mvex

		w.EndBox() // moov
		_ = w.Bytes()
	}
}

func BenchmarkScannerParse(b *testing.B) {
	path := "video-media-samples/big-buck-bunny-480p-30sec.mp4"
	f, err := os.Open(path)
	if err != nil {
		b.Skipf("test file not available: %v", err)
	}
	defer f.Close()

	// The loop reads only moov/moof bodies; total their size for SetBytes.
	sc := mp4.NewScanner(f)
	var metaBytes int64
	for sc.Next() {
		if t := sc.Entry().Type; t == mp4.TypeMoov || t == mp4.TypeMoof {
			metaBytes += sc.Entry().Size
		}
	}
	if err := sc.Err(); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(metaBytes)

	var buf []byte

	for b.Loop() {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			b.Fatal(err)
		}
		sc.Reset(f)
		for sc.Next() {
			e := sc.Entry()
			if e.Type == mp4.TypeMoov || e.Type == mp4.TypeMoof {
				if int64(cap(buf)) < e.DataSize() {
					buf = make([]byte, e.DataSize())
				} else {
					buf = buf[:e.DataSize()]
				}
				if err := sc.ReadBody(buf); err != nil {
					b.Fatal(err)
				}
				r := mp4.NewReader(buf)
				walkBench(&r)
			}
		}
		if err := sc.Err(); err != nil {
			b.Fatal(err)
		}
	}
}
