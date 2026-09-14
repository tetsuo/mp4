package clip_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/clip"
	"github.com/tetsuo/mp4/track"
)

// sampleMP4 is a short progressive recording. It is not checked in; the test
// is skipped when it is absent.
const sampleMP4 = "../test-data/h264-aac-10s.mp4"

func TestCut(t *testing.T) {
	src, err := os.ReadFile(sampleMP4)
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	reader := bytes.NewReader(src)

	const from, to = 3.0, 7.0
	var out bytes.Buffer
	result, err := clip.Cut(&out, reader, int64(len(src)), from, to)
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	// What the clip presents is what was asked for, however far back the
	// keyframe it decodes from happens to be.
	if result.Start != from {
		t.Errorf("clip presents from %.3fs, asked for %.3fs", result.Start, from)
	}
	if want := to - from; result.Duration < want-0.1 || result.Duration > want+0.1 {
		t.Errorf("clip presents %.3fs, asked for %.3fs", result.Duration, want)
	}
	if result.MediaStart > from {
		t.Errorf("clip's media starts at %.3fs, after the requested %.3fs", result.MediaStart, from)
	}
	if got := int64(out.Len()); got != result.Size {
		t.Errorf("wrote %d bytes, reported %d", got, result.Size)
	}

	// The result must be readable by the same parser, which is what a player
	// does with it.
	clipped := out.Bytes()
	moov := findBox(t, clipped, mp4.TypeMoov)
	tracks, timescale, _, err := track.ParseTracks(moov)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if timescale == 0 {
		t.Fatal("clip declares no movie timescale")
	}
	if len(tracks) == 0 {
		t.Fatal("clip has no tracks")
	}

	sawVideo := false
	for _, tr := range tracks {
		if len(tr.Samples) == 0 {
			t.Errorf("track %d carries no samples", tr.ID)
			continue
		}
		if tr.Kind == track.TrackVideo {
			sawVideo = true
			if !tr.Samples[0].IsSync() {
				t.Error("the first video sample is not a keyframe, so the clip cannot be decoded from its start")
			}
		}
		// Every sample must point inside the file it was written into.
		for i := range tr.Samples {
			s := &tr.Samples[i]
			if s.Offset < 0 || s.Offset+int64(s.Size()) > int64(len(clipped)) {
				t.Fatalf("track %d sample %d points outside the clip", tr.ID, i)
			}
		}
		// The media carried runs from the keyframe, so it covers the lead-in
		// as well as everything presented.
		length := float64(0)
		for i := range tr.Samples {
			length += float64(tr.Samples[i].Duration) / float64(tr.TimeScale)
		}
		if want := result.End - result.MediaStart; length < want-0.5 || length > want+0.5 {
			t.Errorf("track %d carries %.3fs, expected %.3fs of media", tr.ID, length, want)
		}
		// An edit list holds the lead-in off the timeline, so playback opens
		// on the frame that was asked for rather than on the keyframe.
		lead := result.Start - result.MediaStart
		mediaTime, hasEdit := tr.EditMediaTime()
		if lead > 0.05 && !hasEdit {
			t.Errorf("track %d skips %.3fs of lead-in but carries no edit list", tr.ID, lead)
		}
		if hasEdit {
			at := float64(mediaTime) / float64(tr.TimeScale)
			if at < lead-0.05 || at > lead+0.05 {
				t.Errorf("track %d presents from %.3fs into its media, expected %.3fs", tr.ID, at, lead)
			}
		}
	}
	if !sawVideo {
		t.Error("clip has no video track")
	}
}

func TestCutRejectsEmptyRange(t *testing.T) {
	if _, err := clip.Cut(&bytes.Buffer{}, bytes.NewReader(nil), 0, 5, 5); err == nil {
		t.Fatal("an end at the start was accepted")
	}
}

func findBox(t *testing.T, data []byte, want mp4.BoxType) []byte {
	t.Helper()
	sc := mp4.NewScanner(bytes.NewReader(data))
	for sc.Next() {
		entry := sc.Entry()
		if entry.Type == want {
			return data[entry.Offset : entry.Offset+entry.Size]
		}
	}
	t.Fatalf("clip has no %s box", want)
	return nil
}
