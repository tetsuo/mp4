package track_test

import (
	"os"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

const fuzzSample = "../test-data/big-buck-bunny-480p-30sec.mp4"

// FuzzParseTracks feeds arbitrary bytes to the moov parser. A malformed moov
// must return an error rather than panic. The seeds include a real moov when
// available and a moov holding a truncated mvhd, which reaches a field
// extractor with fewer bytes than the box layout requires.
func FuzzParseTracks(f *testing.F) {
	if data, err := os.ReadFile(fuzzSample); err == nil {
		r := mp4.NewReader(data)
		for r.Next() {
			if r.Type() == mp4.TypeMoov {
				f.Add(append([]byte(nil), r.RawBox()...))
				break
			}
		}
	}
	f.Add([]byte(nil))
	f.Add([]byte{0, 0, 0, 8, 'm', 'o', 'o', 'v'})
	f.Add([]byte{
		0, 0, 0, 0x18, 'm', 'o', 'o', 'v',
		0, 0, 0, 0x10, 'm', 'v', 'h', 'd', 0, 0, 0, 0, 0, 0, 0, 0,
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		tracks, _, _, err := track.ParseTracks(data)
		if err != nil {
			return
		}
		for _, tr := range tracks {
			_ = tr.Codec()
		}
	})
}
