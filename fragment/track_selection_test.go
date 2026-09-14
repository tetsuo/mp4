package fragment_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/fragment"
	"github.com/tetsuo/mp4/track"
)

const muxedTestFile = "../test-data/h264-aac-10s.mp4"
const multiTrackTestFile = "../test-data/h264-2video-2audio-3s.mp4"
const sixTrackTestFile = "../test-data/aac-6track-1s.mp4"

var trackInitSink []byte

func TestReaderPreservesAllPlayableTracks(t *testing.T) {
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []uint32{1, 2, 3, 4}
	gotIDs := make([]uint32, len(init.Tracks))
	for i := range init.Tracks {
		gotIDs[i] = init.Tracks[i].ID
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("reader track ids = %v, want %v", gotIDs, wantIDs)
	}
	initTrackIDs, trexIDs, _ := initIDs(t, init.Bytes())
	if !slices.Equal(initTrackIDs, wantIDs) || !slices.Equal(trexIDs, wantIDs) {
		t.Fatalf("init track ids = %v, trex ids = %v, want %v", initTrackIDs, trexIDs, wantIDs)
	}

	if err := r.SetTargetDuration(1); err != nil {
		t.Fatal(err)
	}
	fr, err := r.ReadFragment()
	if err != nil {
		t.Fatal(err)
	}
	original := slices.Clone(fr.Samples)
	for _, selected := range init.Tracks {
		t.Run("track-"+strconv.FormatUint(uint64(selected.ID), 10), func(t *testing.T) {
			start, end := r.TrackRun(selected.ID)
			want := samplesForTrack(original, selected.ID)
			if end-start != len(want) || len(want) == 0 {
				t.Fatalf("track run = [%d,%d), fragment samples = %d", start, end, len(want))
			}
			if selected.Kind == track.TrackVideo && !want[0].IsSync() {
				t.Fatal("video fragment does not start with a sync sample")
			}

			trackInit, err := init.TrackBytes(make([]byte, 0, len(init.Bytes())), selected.ID)
			if err != nil {
				t.Fatal(err)
			}
			trackIDs, trackTrexIDs, _ := initIDs(t, trackInit)
			if !slices.Equal(trackIDs, []uint32{selected.ID}) || !slices.Equal(trackTrexIDs, []uint32{selected.ID}) {
				t.Fatalf("selected init track ids = %v, trex ids = %v", trackIDs, trackTrexIDs)
			}
			verifySelectedFragment(t, f, fr, original, selected)
		})
	}
	if start, end := r.TrackRun(0); start != 0 || end != 0 {
		t.Fatalf("unknown track run = [%d,%d), want [0,0)", start, end)
	}
	if !slices.Equal(fr.Samples, original) {
		t.Fatal("track writes mutated the source fragment")
	}
}

func TestReaderSupportsMoreThanFourTracks(t *testing.T) {
	f, err := os.Open(sixTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []uint32{1, 2, 3, 4, 5, 6}
	gotIDs := make([]uint32, len(init.Tracks))
	for i := range init.Tracks {
		gotIDs[i] = init.Tracks[i].ID
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("reader track ids = %v, want %v", gotIDs, wantIDs)
	}
	if err := r.SetTargetDuration(0.25); err != nil {
		t.Fatal(err)
	}

	counts := make(map[uint32]int, len(wantIDs))
	for {
		fr, err := r.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for i := range fr.Samples {
			counts[fr.Samples[i].TrackID]++
		}
	}
	for _, id := range wantIDs {
		if counts[id] == 0 {
			t.Fatalf("track %d has no fragmented samples", id)
		}
	}

	var readErr error
	allocs := testing.AllocsPerRun(100, func() {
		readErr = r.Seek(0)
		if readErr != nil {
			return
		}
		for {
			_, readErr = r.ReadFragment()
			if readErr == io.EOF {
				readErr = nil
				return
			}
			if readErr != nil {
				return
			}
		}
	})
	if readErr != nil {
		t.Fatal(readErr)
	}
	if allocs != 0 {
		t.Fatalf("warm six-track read allocations = %v, want 0", allocs)
	}
}

func TestSixTrackFragmentRoundTripFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	f, err := os.Open(sixTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetTargetDuration(0.25); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	w := fragment.NewWriter(&output)
	if err := w.WriteInit(init); err != nil {
		t.Fatal(err)
	}
	for {
		fr, err := r.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteFragment(fr, f); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(t.TempDir(), "six-track.mp4")
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	want := []string{"audio", "audio", "audio", "audio", "audio", "audio"}
	if got := strings.Fields(string(probe)); !slices.Equal(got, want) {
		t.Fatalf("ffprobe stream types = %v, want %v", got, want)
	}
}

func TestTrackInitContainsOnlySelectedTrack(t *testing.T) {
	f, err := os.Open(muxedTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	_, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	video, audio := init.VideoTrack(), init.AudioTrack()
	if video == nil || audio == nil {
		t.Fatal("fixture must contain video and audio")
	}

	for _, tc := range []struct {
		name     string
		selected *track.Track
	}{{"video", video}, {"audio", audio}} {
		t.Run(tc.name, func(t *testing.T) {
			selected := tc.selected
			buf := make([]byte, 0, len(init.Bytes()))
			got, err := init.TrackBytes(buf, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
			trackIDs, trexIDs, nextTrackID := initIDs(t, got)
			if !slices.Equal(trackIDs, []uint32{selected.ID}) {
				t.Fatalf("init track ids = %v, want [%d]", trackIDs, selected.ID)
			}
			if !slices.Equal(trexIDs, []uint32{selected.ID}) {
				t.Fatalf("init trex ids = %v, want [%d]", trexIDs, selected.ID)
			}
			if nextTrackID <= selected.ID {
				t.Fatalf("init next track id = %d, want greater than %d", nextTrackID, selected.ID)
			}
		})
	}

	if _, err := init.TrackBytes(nil, 0); err == nil {
		t.Fatal("unknown track id was accepted")
	}
}

func TestWriteFragmentTrackPreservesSelectedSamples(t *testing.T) {
	f, err := os.Open(muxedTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetTargetDuration(2); err != nil {
		t.Fatal(err)
	}
	fr, err := r.ReadFragment()
	if err != nil {
		t.Fatal(err)
	}
	original := slices.Clone(fr.Samples)
	if err := fragment.NewWriter(io.Discard).PrepareTrack(fr, 0); err != fragment.ErrTrackNotFound {
		t.Fatalf("unknown track error = %v, want %v", err, fragment.ErrTrackNotFound)
	}

	for _, tc := range []struct {
		name     string
		selected *track.Track
	}{{"video", init.VideoTrack()}, {"audio", init.AudioTrack()}} {
		t.Run(tc.name, func(t *testing.T) {
			verifySelectedFragment(t, f, fr, original, tc.selected)
		})
	}

	if !slices.Equal(fr.Samples, original) {
		t.Fatal("track writes mutated the source fragment")
	}
}

func TestWriterSupportsMoreThanFourMuxedTracks(t *testing.T) {
	samples := make([]track.Sample, 5)
	for i := range samples {
		samples[i].Offset = int64(i)
		samples[i].TrackID = uint32(i + 1)
		samples[i].Duration = 1
		samples[i].SetSize(1, true)
	}
	fr := &fragment.Fragment{Samples: samples, SequenceNum: 1}
	var out bytes.Buffer
	w := fragment.NewWriter(&out)
	if err := w.WriteFragment(fr, bytes.NewReader([]byte{1, 2, 3, 4, 5})); err != nil {
		t.Fatal(err)
	}
	got, sequence, err := fragment.ParseSegment(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if sequence != 1 || len(got) != len(samples) {
		t.Fatalf("sequence = %d, samples = %d", sequence, len(got))
	}
	for i := range got {
		if got[i].TrackID != samples[i].TrackID {
			t.Fatalf("sample %d track id = %d, want %d", i, got[i].TrackID, samples[i].TrackID)
		}
	}
}

func TestWriterGrowsMoofForLargeMultitrackFragment(t *testing.T) {
	samples := make([]track.Sample, 5000)
	for i := range samples {
		samples[i].TrackID = uint32(i%6 + 1)
		samples[i].Duration = 1024
		samples[i].SetSize(0, true)
	}
	fr := &fragment.Fragment{Samples: samples, SequenceNum: 7}
	var out bytes.Buffer
	if err := fragment.NewWriter(&out).WriteFragment(fr, bytes.NewReader(nil)); err != nil {
		t.Fatal(err)
	}
	got, sequence, err := fragment.ParseSegment(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if sequence != 7 || len(got) != len(samples) {
		t.Fatalf("sequence = %d, samples = %d, want 7 and %d", sequence, len(got), len(samples))
	}
	counts := make(map[uint32]int, 6)
	for i := range got {
		counts[got[i].TrackID]++
	}
	for id := uint32(1); id <= 6; id++ {
		if counts[id] == 0 {
			t.Fatalf("track %d has no parsed samples", id)
		}
	}
}

func TestReaderRejectsUnalignedVideoTrack(t *testing.T) {
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetTargetDuration(1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFragment(); err != nil {
		t.Fatal(err)
	}
	secondVideo := init.Tracks[1]
	if secondVideo.Kind != track.TrackVideo {
		t.Fatal("fixture track 2 must be video")
	}
	_, next := r.TrackRun(secondVideo.ID)
	if next >= len(secondVideo.Samples) || !secondVideo.Samples[next].IsSync() {
		t.Fatal("fixture video tracks must have an aligned second fragment")
	}
	secondVideo.Samples[next].SetSize(secondVideo.Samples[next].Size(), false)
	if _, err := r.ReadFragment(); err != fragment.ErrUnalignedVideoTrack {
		t.Fatalf("unaligned read error = %v, want %v", err, fragment.ErrUnalignedVideoTrack)
	}
}

func TestSelectedVideoDrivesItsOwnFragmentBoundaries(t *testing.T) {
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	firstVideo, selected := init.Tracks[0], init.Tracks[1]
	if firstVideo.Kind != track.TrackVideo || selected.Kind != track.TrackVideo {
		t.Fatal("fixture tracks 1 and 2 must be video")
	}
	for i := 1; i < len(firstVideo.Samples); i++ {
		firstVideo.Samples[i].SetSize(firstVideo.Samples[i].Size(), false)
	}
	if err := r.SetTargetDuration(1); err != nil {
		t.Fatal(err)
	}
	if err := r.SelectTrack(selected.ID); err != nil {
		t.Fatal(err)
	}

	fragments := 0
	for {
		fr, err := r.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(fr.Samples) == 0 || !fr.Samples[0].IsSync() {
			t.Fatalf("fragment %d does not start with a selected-track sync sample", fragments)
		}
		for i := range fr.Samples {
			if fr.Samples[i].TrackID != selected.ID {
				t.Fatalf("fragment %d sample %d track id = %d, want %d", fragments, i, fr.Samples[i].TrackID, selected.ID)
			}
		}
		fragments++
	}
	if fragments < 2 {
		t.Fatalf("selected track produced %d fragment, want at least 2", fragments)
	}
}

func TestSelectedTrackIgnoresUnalignedVideoTrack(t *testing.T) {
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	selected, otherVideo := init.Tracks[0], init.Tracks[1]
	if selected.Kind != track.TrackVideo || otherVideo.Kind != track.TrackVideo {
		t.Fatal("fixture tracks 1 and 2 must be video")
	}
	unaligned := false
	for i := 1; i < len(otherVideo.Samples); i++ {
		if otherVideo.Samples[i].IsSync() {
			otherVideo.Samples[i].SetSize(otherVideo.Samples[i].Size(), false)
			unaligned = true
			break
		}
	}
	if !unaligned {
		t.Fatal("fixture second video must have another sync sample")
	}
	if err := r.SetTargetDuration(1); err != nil {
		t.Fatal(err)
	}
	if err := r.SelectTrack(selected.ID); err != nil {
		t.Fatal(err)
	}

	fragments := 0
	for {
		fr, err := r.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for i := range fr.Samples {
			if fr.Samples[i].TrackID != selected.ID {
				t.Fatalf("fragment %d sample %d track id = %d, want %d", fragments, i, fr.Samples[i].TrackID, selected.ID)
			}
		}
		fragments++
	}
	if fragments < 2 {
		t.Fatalf("selected track produced %d fragment, want at least 2", fragments)
	}
}

func TestSelectTrackRejectsUnknownTrack(t *testing.T) {
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, _, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SelectTrack(0); err != fragment.ErrTrackNotFound {
		t.Fatalf("error = %v, want %v", err, fragment.ErrTrackNotFound)
	}
}

func TestSelectedTracksFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	type output struct {
		track *track.Track
		buf   bytes.Buffer
		w     *fragment.Writer
	}
	outputs := make([]output, len(init.Tracks))
	for i, selected := range init.Tracks {
		trackInit, err := init.TrackBytes(nil, selected.ID)
		if err != nil {
			t.Fatal(err)
		}
		outputs[i].track = selected
		outputs[i].buf.Write(trackInit)
		outputs[i].w = fragment.NewWriter(&outputs[i].buf)
	}
	for {
		fr, err := r.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for i := range outputs {
			selected := outputs[i].track
			if len(samplesForTrack(fr.Samples, selected.ID)) == 0 {
				continue
			}
			if err := outputs[i].w.WriteFragmentTrack(fr, selected.ID, f); err != nil {
				t.Fatal(err)
			}
		}
	}

	for i := range outputs {
		out := &outputs[i]
		t.Run("track-"+strconv.FormatUint(uint64(out.track.ID), 10), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "track.mp4")
			if err := os.WriteFile(path, out.buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", path).Output()
			if err != nil {
				t.Fatalf("ffprobe: %v", err)
			}
			want := "audio"
			if out.track.Kind == track.TrackVideo {
				want = "video"
			}
			if got := strings.Fields(string(probe)); !slices.Equal(got, []string{want}) {
				t.Fatalf("ffprobe stream types = %v, want [%s]", got, want)
			}
		})
	}
}

func verifySelectedFragment(t *testing.T, src io.ReaderAt, fr *fragment.Fragment, original []track.Sample, selected *track.Track) {
	t.Helper()
	var out bytes.Buffer
	w := fragment.NewWriter(&out)
	if err := w.WriteFragmentTrack(fr, selected.ID, src); err != nil {
		t.Fatal(err)
	}

	got, seq, err := fragment.ParseSegment(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if seq != fr.SequenceNum {
		t.Fatalf("sequence = %d, want %d", seq, fr.SequenceNum)
	}
	want := samplesForTrack(original, selected.ID)
	if len(got) != len(want) {
		t.Fatalf("samples = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !sameSample(got[i], want[i]) {
			t.Fatalf("sample %d metadata = %+v, want %+v", i, got[i], want[i])
		}
		wantBytes := make([]byte, want[i].Size())
		if _, err := src.ReadAt(wantBytes, want[i].Offset); err != nil && err != io.EOF {
			t.Fatal(err)
		}
		gotBytes := out.Bytes()[got[i].Offset : got[i].Offset+int64(got[i].Size())]
		if !bytes.Equal(gotBytes, wantBytes) {
			t.Fatalf("sample %d media bytes changed", i)
		}
	}
}

func BenchmarkTrackInitBytes(b *testing.B) {
	f, err := os.Open(muxedTestFile)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()

	_, init, err := fragment.NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	video := init.VideoTrack()
	if video == nil {
		b.Fatal("fixture must contain video")
	}

	dst := make([]byte, 0, len(init.Bytes()))
	dst, err = init.TrackBytes(dst, video.ID)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		dst, err = init.TrackBytes(dst[:0], video.ID)
		if err != nil {
			b.Fatal(err)
		}
	}
	trackInitSink = dst
}

func BenchmarkReaderFragments(b *testing.B) {
	benchmarkReaderFragments(b, muxedTestFile)
}

func BenchmarkReaderFourTracks(b *testing.B) {
	benchmarkReaderFragments(b, multiTrackTestFile)
}

func BenchmarkReaderSixTracks(b *testing.B) {
	benchmarkReaderFragments(b, sixTrackTestFile)
}

func BenchmarkReaderSelectedTrack(b *testing.B) {
	f, err := os.Open(multiTrackTestFile)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()

	r, init, err := fragment.NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	if err := r.SetTargetDuration(1); err != nil {
		b.Fatal(err)
	}
	if err := r.SelectTrack(init.Tracks[1].ID); err != nil {
		b.Fatal(err)
	}
	for {
		if _, err := r.ReadFragment(); err == io.EOF {
			break
		} else if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := r.Seek(0); err != nil {
			b.Fatal(err)
		}
		for {
			if _, err := r.ReadFragment(); err == io.EOF {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func benchmarkReaderFragments(b *testing.B, path string) {
	b.Helper()
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()

	r, _, err := fragment.NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	if err := r.SetTargetDuration(1); err != nil {
		b.Fatal(err)
	}
	for {
		if _, err := r.ReadFragment(); err == io.EOF {
			break
		} else if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := r.Seek(0); err != nil {
			b.Fatal(err)
		}
		for {
			if _, err := r.ReadFragment(); err == io.EOF {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func samplesForTrack(samples []track.Sample, trackID uint32) []track.Sample {
	out := make([]track.Sample, 0, len(samples))
	for i := range samples {
		if samples[i].TrackID == trackID {
			out = append(out, samples[i])
		}
	}
	return out
}

func sameSample(a, b track.Sample) bool {
	return a.TrackID == b.TrackID && a.DTS == b.DTS && a.Duration == b.Duration &&
		a.PresentationOffset == b.PresentationOffset && a.Size() == b.Size() &&
		a.IsSync() == b.IsSync()
}

func initIDs(t *testing.T, data []byte) (trackIDs, trexIDs []uint32, nextTrackID uint32) {
	t.Helper()
	r := mp4.NewReader(data)
	for r.Next() {
		if r.Type() != mp4.TypeMoov {
			continue
		}
		r.Enter()
		for r.Next() {
			switch r.Type() {
			case mp4.TypeMvhd:
				_, _, nextTrackID = r.ReadMvhd()
			case mp4.TypeTrak:
				r.Enter()
				for r.Next() {
					if r.Type() == mp4.TypeTkhd {
						id, _, _, _ := r.ReadTkhd()
						trackIDs = append(trackIDs, id)
					}
				}
				r.Exit()
			case mp4.TypeMvex:
				r.Enter()
				for r.Next() {
					if r.Type() == mp4.TypeTrex {
						id, _, _, _, _ := r.ReadTrex()
						trexIDs = append(trexIDs, id)
					}
				}
				r.Exit()
			}
		}
		r.Exit()
	}
	return trackIDs, trexIDs, nextTrackID
}
