package track_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

// spsFromStsd digs the first SPS out of the avcC inside a raw stsd box, the
// same bytes an RTMP publisher sends in its AVC sequence header.
func spsFromStsd(stsd []byte) []byte {
	for i := 4; i+8 < len(stsd); i++ {
		if string(stsd[i:i+4]) != "avcC" {
			continue
		}
		p := stsd[i+4:]
		// version, profile, compat, level, lengthSize, SPS count, SPS length.
		if len(p) < 8 || p[0] != 1 {
			return nil
		}
		n := int(binary.BigEndian.Uint16(p[6:8]))
		if 8+n > len(p) {
			return nil
		}
		return p[8 : 8+n]
	}
	return nil
}

// TestParseSPSAgainstParsedTracks checks ParseSPS against the sample-table
// parser on every local H.264 sample: the SPS-derived dimensions and codec
// bytes must match what the moov says.
func TestParseSPSAgainstParsedTracks(t *testing.T) {
	files, _ := filepath.Glob("../test-data/*.mp4")
	if len(files) == 0 {
		t.Skip("no sample files")
	}
	checked := 0
	for _, path := range files {
		moov, tracks := parseFile(t, path)
		if moov == nil {
			continue
		}
		for _, tr := range tracks {
			if tr.Kind != track.TrackVideo || tr.Codec()[:4] != "avc1" {
				continue
			}
			sps := spsFromStsd(tr.StsdRaw())
			if sps == nil {
				t.Errorf("%s: no SPS in stsd", path)
				continue
			}
			info, err := track.ParseSPS(sps)
			if err != nil {
				t.Errorf("%s: ParseSPS: %v", path, err)
				continue
			}
			if info.Width != tr.Width || info.Height != tr.Height {
				t.Errorf("%s: SPS %dx%d, track %dx%d", path, info.Width, info.Height, tr.Width, tr.Height)
			}
			want := fmt.Sprintf("avc1.%02x%02x%02x", info.Profile, info.Compat, info.Level)
			if got := tr.Codec(); got != want {
				t.Errorf("%s: SPS codec %q, track codec %q", path, want, got)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no H.264 tracks checked")
	}
}

// parseFile parses path's moov into tracks, returning nils when the file has
// none (not an error: some samples are other codecs or formats).
func parseFile(t *testing.T, path string) ([]byte, []*track.Track) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := mp4.NewScanner(f)
	for sc.Next() {
		e := sc.Entry()
		if e.Type != mp4.TypeMoov {
			continue
		}
		buf := make([]byte, e.Size)
		if err := sc.ReadBox(buf); err != nil {
			t.Fatal(err)
		}
		tracks, _, _, err := track.ParseTracks(buf)
		if err != nil {
			t.Fatal(err)
		}
		return buf, tracks
	}
	return nil, nil
}

func TestParseSPSRejectsMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		{0x67},
		{0x67, 0x64, 0x00},             // too short for profile bytes
		{0x68, 0x64, 0x00, 0x1f, 0xac}, // PPS, not SPS
		{0x67, 0x64, 0x00, 0x1f},       // no RBSP at all
	}
	for _, c := range cases {
		if _, err := track.ParseSPS(c); err == nil {
			t.Errorf("ParseSPS(%x): no error", c)
		}
	}
}

func FuzzParseSPS(f *testing.F) {
	f.Add([]byte{0x67, 0x64, 0x00, 0x1f, 0xac, 0xd9, 0x40, 0x50, 0x05, 0xbb, 0x01, 0x6c, 0x80, 0x00, 0x00, 0x03, 0x00, 0x80, 0x00, 0x00, 0x1e, 0x07, 0x8c, 0x18, 0xcb})
	f.Add([]byte{0x67, 0x42, 0xc0, 0x1e, 0xd9, 0x00, 0xb4, 0x3d, 0xa1, 0x00, 0x00, 0x03, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := track.ParseSPS(data)
		if err == nil && (info.Width == 0 || info.Height == 0) {
			t.Fatalf("accepted SPS with zero dimension: %+v", info)
		}
	})
}
