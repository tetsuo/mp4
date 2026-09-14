package track_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/tetsuo/mp4/track"
)

func TestCodedDimensionsStaySeparateFromTrackHeaderDisplayDimensions(t *testing.T) {
	moov := readMoov(t, "../test-data/h264-aac-10s.mp4")
	marker := bytes.Index(moov, []byte("tkhd"))
	if marker < 0 || marker+88 > len(moov) {
		t.Fatal("fixture has no usable tkhd")
	}
	version := moov[marker+4]
	offset := marker + 8 + 72
	if version == 1 {
		offset = marker + 8 + 84
	}
	if offset+8 > len(moov) {
		t.Fatal("truncated tkhd")
	}
	binary.BigEndian.PutUint32(moov[offset:], 1111<<16)
	binary.BigEndian.PutUint32(moov[offset+4:], 777<<16)

	tracks, _, _, err := track.ParseTracks(moov)
	if err != nil {
		t.Fatal(err)
	}
	video := tracks[0]
	if video.DisplayWidth != 1111 || video.DisplayHeight != 777 {
		t.Fatalf("display dimensions = %dx%d", video.DisplayWidth, video.DisplayHeight)
	}
	if video.Width == video.DisplayWidth || video.Height == video.DisplayHeight {
		t.Fatalf("coded dimensions were overwritten: coded=%dx%d display=%dx%d",
			video.Width, video.Height, video.DisplayWidth, video.DisplayHeight)
	}
}
