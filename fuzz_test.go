package mp4_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/tetsuo/mp4"
)

// fuzzSample is a real MP4 used to seed the corpus when it is present. Starting
// the mutator from valid box structure reaches parsing code that purely random
// bytes rarely would.
const fuzzSample = "test-data/big-buck-bunny-480p-30sec.mp4"

// seedBoxes adds a real file when available plus small hand-built boxes,
// including malformed ones, so the corpus covers realistic and degenerate
// structures. The last two seeds are a truncated mvhd, which reaches a field
// extractor with fewer bytes than the box layout requires.
func seedBoxes(f *testing.F) {
	if data, err := os.ReadFile(fuzzSample); err == nil {
		f.Add(data)
	}
	f.Add([]byte(nil))
	f.Add([]byte{0, 0, 0, 8, 'f', 't', 'y', 'p'})
	f.Add([]byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0, 0, 0, 0, 0, 0, 0}) // 64-bit size 0
	f.Add([]byte{255, 255, 255, 255, 'f', 'r', 'e', 'e'})                 // oversized declared size
	f.Add([]byte{0, 0, 0, 0x10, 'm', 'v', 'h', 'd', 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{
		0, 0, 0, 0x18, 'm', 'o', 'o', 'v',
		0, 0, 0, 0x10, 'm', 'v', 'h', 'd', 0, 0, 0, 0, 0, 0, 0, 0,
	})
}

// FuzzScanner drives the streaming top-level box scanner over arbitrary input.
// No input may panic; malformed data must surface through Err instead.
func FuzzScanner(f *testing.F) {
	seedBoxes(f)
	buf := make([]byte, 1<<20)
	f.Fuzz(func(t *testing.T, data []byte) {
		sc := mp4.NewScanner(bytes.NewReader(data))
		for sc.Next() {
			e := sc.Entry()
			if n := e.DataSize(); n >= 0 && n <= int64(len(buf)) {
				_ = sc.ReadBody(buf[:n])
			}
		}
		_ = sc.Err()
	})
}

// FuzzReader walks arbitrary bytes as a box tree and calls the typed field
// extractor for whichever box type it lands on. It bounds its own descent below
// the reader's nesting limit so the harness itself cannot overflow the stack.
// No input may panic.
func FuzzReader(f *testing.F) {
	seedBoxes(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		r := mp4.NewReader(data)
		walkBoxes(&r, 0)
	})
}

// fuzzMaxDepth caps the walker below the reader's internal nesting limit.
const fuzzMaxDepth = 12

func walkBoxes(r *mp4.Reader, depth int) {
	for r.Next() {
		_ = r.Size()
		_ = r.Version()
		_ = r.Flags()
		_ = r.Data()
		_ = r.RawBox()
		readTypedFields(r)
		if depth < fuzzMaxDepth {
			r.Enter()
			walkBoxes(r, depth+1)
			r.Exit()
		}
	}
}

// readTypedFields calls the extractor matching the current box type. Each reads
// fixed offsets, so a truncated box must not read out of bounds.
func readTypedFields(r *mp4.Reader) {
	switch r.Type() {
	case mp4.TypeMvhd:
		r.ReadMvhd()
	case mp4.TypeTkhd:
		r.ReadTkhd()
	case mp4.TypeMdhd:
		r.ReadMdhd()
	case mp4.TypeHdlr:
		r.ReadHdlr()
		r.ReadHdlrName()
	case mp4.TypeMehd:
		r.ReadMehd()
	case mp4.TypeTrex:
		r.ReadTrex()
	case mp4.TypeMfhd:
		r.ReadMfhd()
	case mp4.TypeTfhd:
		r.ReadTfhd()
	case mp4.TypeTfdt:
		r.ReadTfdt()
	case mp4.TypeElst:
		r.ReadElst()
	case mp4.TypeStsd, mp4.TypeDref:
		r.EntryCount()
	}
}
