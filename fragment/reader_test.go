package fragment_test

import (
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/fragment"
)

const testFile = "../video-media-samples/big-buck-bunny-480p-30sec.mp4"

func openTestFile(t testing.TB) *os.File {
	t.Helper()
	f, err := os.Open(testFile)
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}
	return f
}

func TestNewReader(t *testing.T) {
	f := openTestFile(t)
	defer f.Close()

	_, initSeg, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	if len(initSeg.Tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(initSeg.Tracks))
	}

	vt := initSeg.VideoTrack()
	if vt == nil {
		t.Fatal("no video track")
	}
	if vt.Codec() != "avc1.64001e" {
		t.Errorf("video codec = %q, want avc1.64001e", vt.Codec())
	}
	if vt.Width != 854 || vt.Height != 480 {
		t.Errorf("video size = %dx%d, want 854x480", vt.Width, vt.Height)
	}
	if vt.TimeScale != 12288 {
		t.Errorf("video timescale = %d, want 12288", vt.TimeScale)
	}

	at := initSeg.AudioTrack()
	if at == nil {
		t.Fatal("no audio track")
	}
	if at.Codec() != "mp4a.40.2" {
		t.Errorf("audio codec = %q, want mp4a.40.2", at.Codec())
	}
	if at.SampleRate != 48000 {
		t.Errorf("audio sample rate = %d, want 48000", at.SampleRate)
	}

	buf := initSeg.Bytes()
	if len(buf) < 8 {
		t.Fatal("init segment too small")
	}
	mr := mp4.NewReader(buf)
	if !mr.Next() {
		t.Fatal("expected ftyp in init segment")
	}
	if mr.Type() != mp4.TypeFtyp {
		t.Errorf("first box = %s, want ftyp", mr.Type())
	}
	if !mr.Next() {
		t.Fatal("expected moov in init segment")
	}
	if mr.Type() != mp4.TypeMoov {
		t.Errorf("second box = %s, want moov", mr.Type())
	}
}

func TestReadFragments(t *testing.T) {
	f := openTestFile(t)
	defer f.Close()

	frag, initSeg, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	fragCount := 0
	totalSamples := 0
	for {
		fr, err := frag.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		fragCount++
		totalSamples += len(fr.Samples)
	}

	if fragCount == 0 {
		t.Fatal("expected at least one fragment")
	}

	vt := initSeg.VideoTrack()
	if totalSamples < len(vt.Samples) {
		t.Errorf("total samples = %d, want at least %d (video samples)", totalSamples, len(vt.Samples))
	}

	t.Logf("fragments=%d totalSamples=%d", fragCount, totalSamples)
}

func TestWriteFragment(t *testing.T) {
	f := openTestFile(t)
	defer f.Close()

	frag, initSeg, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := frag.SetTimeRange(5, 10); err != nil {
		t.Fatal(err)
	}

	out, err := os.CreateTemp("", "fragment-test-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(out.Name())
	defer out.Close()

	w := fragment.NewWriter(out)
	if err := w.WriteInit(initSeg); err != nil {
		t.Fatal(err)
	}

	fragCount := 0
	for {
		fr, err := frag.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteFragment(fr, f); err != nil {
			t.Fatal(err)
		}
		fragCount++
	}

	if fragCount == 0 {
		t.Fatal("expected at least one fragment")
	}

	out.Seek(0, io.SeekStart)
	verifyOutputBoxes(t, out)
}

func TestSeek(t *testing.T) {
	f := openTestFile(t)
	defer f.Close()

	frag, _, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	frag1, err := frag.ReadFragment()
	if err != nil {
		t.Fatal(err)
	}
	frag1Len := len(frag1.Samples)

	if err := frag.Seek(0); err != nil {
		t.Fatal(err)
	}

	frag2, err := frag.ReadFragment()
	if err != nil {
		t.Fatal(err)
	}

	if len(frag2.Samples) != frag1Len {
		t.Errorf("after seek, fragment has %d samples, want %d", len(frag2.Samples), frag1Len)
	}
}

func TestReset(t *testing.T) {
	f := openTestFile(t)
	defer f.Close()

	// A separate reader gives ground-truth samples from a fresh parse.
	g := openTestFile(t)
	defer g.Close()
	_, fresh, err := fragment.NewReader(g)
	if err != nil {
		t.Fatal(err)
	}
	want := fresh.VideoTrack().Samples

	r, _, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	init2, err := r.Reset(f)
	if err != nil {
		t.Fatal(err)
	}

	got := init2.VideoTrack().Samples
	if !reflect.DeepEqual(got, want) {
		t.Errorf("video samples after Reset differ from a fresh parse (got %d, want %d)", len(got), len(want))
	}

	fragCount := 0
	for {
		if _, err := r.ReadFragment(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		fragCount++
	}
	if fragCount == 0 {
		t.Fatal("no fragments after Reset")
	}
}

func TestSetTimeRangeValidation(t *testing.T) {
	f := openTestFile(t)
	defer f.Close()

	frag, _, err := fragment.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		start, end float64
		wantErr    bool
	}{
		{0, 0, false},  // valid: entire file
		{5, 10, false}, // valid: normal range
		{0, 10, false}, // valid: from start
		{-1, 10, true}, // invalid: negative start
		{0, -1, true},  // invalid: negative end
		{10, 5, true},  // invalid: end before start
		{10, 10, true}, // invalid: zero-length range
	}

	for _, tt := range tests {
		err := frag.SetTimeRange(tt.start, tt.end)
		if (err != nil) != tt.wantErr {
			t.Errorf("SetTimeRange(%v, %v): err=%v, wantErr=%v", tt.start, tt.end, err, tt.wantErr)
		}
	}
}

func verifyOutputBoxes(t *testing.T, f *os.File) {
	t.Helper()
	stat, _ := f.Stat()
	data := make([]byte, stat.Size())
	f.ReadAt(data, 0)

	moofCount := 0
	mdatCount := 0
	hasFtyp := false
	hasMoov := false

	mr := mp4.NewReader(data)
	for mr.Next() {
		switch mr.Type() {
		case mp4.TypeFtyp:
			hasFtyp = true
		case mp4.TypeMoov:
			hasMoov = true
		case mp4.TypeMoof:
			moofCount++
			mr.Enter()
			trafCount := 0
			for mr.Next() {
				if mr.Type() == mp4.TypeTraf {
					trafCount++
				}
			}
			mr.Exit()
			if trafCount == 0 {
				t.Error("moof has no traf children")
			}
		case mp4.TypeMdat:
			mdatCount++
		}
	}

	if !hasFtyp {
		t.Error("missing ftyp")
	}
	if !hasMoov {
		t.Error("missing moov")
	}
	if moofCount == 0 {
		t.Error("no moof boxes")
	}
	if mdatCount == 0 {
		t.Error("no mdat boxes")
	}
	if moofCount != mdatCount {
		t.Errorf("moof count (%d) != mdat count (%d)", moofCount, mdatCount)
	}

	t.Logf("output: ftyp=%v moov=%v moof=%d mdat=%d", hasFtyp, hasMoov, moofCount, mdatCount)
}
