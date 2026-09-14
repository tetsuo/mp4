// Package clip cuts a time range out of a progressive MP4 and writes it as a
// new progressive MP4. Nothing is re-encoded: the coded samples inside the
// range are copied byte for byte and a fresh moov describes them.
package clip

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

// Result describes the clip that was written. Start and Duration are what the
// clip presents, which is what was asked for. MediaStart is where its media
// really begins: the cut is moved back to a keyframe, and the frames between
// that keyframe and the requested start are carried so the first presented
// frame has something to decode from, but an edit list keeps them off the
// timeline.
type Result struct {
	Start      float64
	End        float64
	Duration   float64
	MediaStart float64
	Size       int64
}

// maxClipSamples bounds the sample tables one clip may build. The tables are
// held in memory while the file is written, and a request for a range that
// large is a mistake rather than a clip.
const maxClipSamples = 2 << 20

// undeterminedLanguage is the ISO 639-2 code "und" packed as mdhd stores it,
// five bits per letter.
const undeterminedLanguage = 0x55C4

var errNoTracks = errors.New("mp4: no playable track in the requested range")

// Cut writes the part of src between start and end, in seconds, to dst. The
// clip begins at the last keyframe at or before start, because a clip starting
// between keyframes has no first frame to decode from. Metadata precedes the
// media in the output, so a player can start it without reading to the end.
func Cut(dst io.Writer, src io.ReaderAt, size int64, start, end float64) (Result, error) {
	if start < 0 || !(end > start) {
		return Result{}, errors.New("mp4: clip range must start at or after zero and end after it")
	}
	moovBuf, err := readMoov(src, size)
	if err != nil {
		return Result{}, err
	}
	tracks, movieTimescale, _, err := track.ParseTracks(moovBuf)
	if err != nil {
		return Result{}, err
	}
	if movieTimescale == 0 {
		movieTimescale = 1000
	}

	// The video track decides where the clip can begin; every other track is
	// then cut at the same instant so the result stays in sync.
	video, syncIndex := keyframeAtOrBefore(tracks, start)
	base := start
	if video != nil {
		base = float64(video.Samples[syncIndex].DTS) / float64(video.TimeScale)
	}

	var out []*clipTrack
	samples := 0
	for _, t := range tracks {
		if len(t.Samples) == 0 || t.TimeScale == 0 {
			continue
		}
		first, last := selectRange(t, base, end)
		if t == video {
			first = syncIndex
		}
		if first < 0 || last < first {
			continue
		}
		ct := newClipTrack(t, t.Samples[first:last+1], start)
		samples += len(ct.samples)
		if samples > maxClipSamples {
			return Result{}, fmt.Errorf("mp4: clip covers more than %d samples", maxClipSamples)
		}
		out = append(out, ct)
	}
	if len(out) == 0 {
		return Result{}, errNoTracks
	}

	order := interleave(out)
	payload := int64(0)
	for _, ct := range out {
		for i := range ct.samples {
			payload += int64(ct.samples[i].Size())
		}
	}

	movieDuration := uint64(0)
	for _, ct := range out {
		if d := ct.movieDuration(movieTimescale); d > movieDuration {
			movieDuration = d
		}
	}

	// The header is built twice. Chunk offsets are absolute file positions, so
	// the first pass measures the header with placeholder offsets and the
	// second fills in the real ones. Entry counts do not change between the
	// two, so neither does the length.
	ftyp := buildFtyp()
	buf := make([]byte, moovBound(out))
	assignOffsets(out, order, 0)
	moov, err := buildMoov(buf, out, movieTimescale, movieDuration)
	if err != nil {
		return Result{}, err
	}
	mediaStart := int64(len(ftyp)) + int64(len(moov)) + 8
	if mediaStart+payload > math.MaxUint32 {
		return Result{}, errors.New("mp4: clip is too large for 32-bit chunk offsets")
	}
	assignOffsets(out, order, mediaStart)
	moov, err = buildMoov(buf, out, movieTimescale, movieDuration)
	if err != nil {
		return Result{}, err
	}
	if int64(len(ftyp))+int64(len(moov))+8 != mediaStart {
		return Result{}, errors.New("mp4: header length changed after chunk offsets were assigned")
	}

	if _, err := dst.Write(ftyp); err != nil {
		return Result{}, err
	}
	if _, err := dst.Write(moov); err != nil {
		return Result{}, err
	}
	var header [8]byte
	be32(header[0:4], uint32(payload+8))
	copy(header[4:8], mp4.TypeMdat[:])
	if _, err := dst.Write(header[:]); err != nil {
		return Result{}, err
	}
	copyBuf := make([]byte, 512<<10)
	for _, ref := range order {
		s := &out[ref.track].samples[ref.sample]
		section := io.NewSectionReader(src, s.Offset, int64(s.Size()))
		if _, err := io.CopyBuffer(dst, section, copyBuf); err != nil {
			return Result{}, err
		}
	}

	duration := float64(movieDuration) / float64(movieTimescale)
	return Result{
		Start:      start,
		End:        start + duration,
		Duration:   duration,
		MediaStart: base,
		Size:       mediaStart + payload,
	}, nil
}

// clipTrack is one source track reduced to the samples inside the clip, with
// the tables the new file needs.
type clipTrack struct {
	src       *track.Track
	samples   []track.Sample
	offsets   []uint32 // new file position of each sample
	sizes     []uint32
	stts      []mp4.SttsEntry
	ctts      []mp4.CttsEntry
	sync      []uint32 // 1-based indices of the keyframes
	duration  uint64   // media timescale
	presented uint64   // media timescale, the part the edit list shows
	mediaAt   int64    // media time the presentation starts at
}

// newClipTrack reduces one track to the samples of the clip. want is the time
// the clip is meant to begin at; the samples before it are kept because the
// first presented frame is decoded from them, and the edit list built from
// mediaAt keeps them off the timeline.
func newClipTrack(t *track.Track, samples []track.Sample, want float64) *clipTrack {
	ct := &clipTrack{src: t, samples: samples}
	ct.offsets = make([]uint32, len(samples))
	ct.sizes = make([]uint32, len(samples))

	// Composition offsets are written unsigned, so a track that carries
	// negative ones is shifted until they are not. The shift moves every
	// presentation time by the same amount and the edit list takes it back.
	shift := int32(0)
	for i := range samples {
		if off := samples[i].PresentationOffset; off < 0 && -off > shift {
			shift = -off
		}
	}

	dts := int64(0)
	firstPTS := int64(math.MaxInt64)
	anyOffset := false
	for i := range samples {
		s := &samples[i]
		ct.sizes[i] = s.Size()
		if s.IsSync() {
			ct.sync = append(ct.sync, uint32(i+1))
		}
		if n := len(ct.stts); n > 0 && ct.stts[n-1].Duration == s.Duration {
			ct.stts[n-1].Count++
		} else {
			ct.stts = append(ct.stts, mp4.SttsEntry{Count: 1, Duration: s.Duration})
		}
		offset := s.PresentationOffset + shift
		if offset != 0 {
			anyOffset = true
		}
		if n := len(ct.ctts); n > 0 && ct.ctts[n-1].Offset == offset {
			ct.ctts[n-1].Count++
		} else {
			ct.ctts = append(ct.ctts, mp4.CttsEntry{Count: 1, Offset: offset})
		}
		if pts := dts + int64(offset); pts < firstPTS {
			firstPTS = pts
		}
		dts += int64(s.Duration)
		ct.duration += uint64(s.Duration)
	}
	if !anyOffset {
		ct.ctts = nil
	}
	if firstPTS == math.MaxInt64 {
		firstPTS = 0
	}
	// The lead-in is whatever separates this track's first sample from the
	// time the clip is meant to begin at, measured on this track's own clock
	// rather than on the video's, so both tracks present from the same moment
	// however their samples happen to be aligned. Rebasing the samples onto a
	// timeline that starts at zero moves every presentation time by the same
	// amount, so the lead-in is already the time to present from; it is only
	// held back to the first frame, for a track whose earliest presentation
	// comes after its earliest decode.
	skip := int64(math.Round(want*float64(t.TimeScale))) - samples[0].DTS
	ct.mediaAt = min(max(skip, firstPTS), int64(ct.duration))
	ct.presented = ct.duration - uint64(max(ct.mediaAt, 0))
	return ct
}

// movieDuration returns the length this track puts on the movie timeline,
// which is the presented part rather than everything the file carries.
func (ct *clipTrack) movieDuration(movieTimescale uint32) uint64 {
	return uint64(float64(ct.presented) / float64(ct.src.TimeScale) * float64(movieTimescale))
}

// keyframeAtOrBefore returns the video track and the index of the last
// keyframe presented at or before at. The first keyframe is used when the
// requested time precedes all of them.
func keyframeAtOrBefore(tracks []*track.Track, at float64) (*track.Track, int) {
	for _, t := range tracks {
		if t.Kind != track.TrackVideo || len(t.Samples) == 0 || t.TimeScale == 0 {
			continue
		}
		scale := float64(t.TimeScale)
		best, firstSync := -1, -1
		for i := range t.Samples {
			s := &t.Samples[i]
			if !s.IsSync() {
				continue
			}
			if firstSync < 0 {
				firstSync = i
			}
			// Decode order is not presentation order once there are B
			// frames, so every keyframe is considered rather than stopping
			// at the first one past the requested time.
			if float64(s.PTS())/scale <= at {
				if best < 0 || s.PTS() > t.Samples[best].PTS() {
					best = i
				}
			}
		}
		if best < 0 {
			best = firstSync
		}
		if best < 0 {
			best = 0
		}
		return t, best
	}
	return nil, 0
}

// selectRange returns the first and last sample of t that overlap the half
// open interval, in decode order.
func selectRange(t *track.Track, start, end float64) (int, int) {
	scale := float64(t.TimeScale)
	first, last := -1, -1
	for i := range t.Samples {
		s := &t.Samples[i]
		from := float64(s.DTS) / scale
		to := from + float64(s.Duration)/scale
		if to <= start || from >= end {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	return first, last
}

// sampleRef points at one sample of one clip track.
type sampleRef struct{ track, sample int }

// interleave orders every sample of every track by decode time, so a player
// reading the file forward always has the next sample of each track nearby.
func interleave(tracks []*clipTrack) []sampleRef {
	total := 0
	for _, ct := range tracks {
		total += len(ct.samples)
	}
	next := make([]int, len(tracks))
	order := make([]sampleRef, 0, total)
	for len(order) < total {
		best, bestTime := -1, math.Inf(1)
		for i, ct := range tracks {
			if next[i] >= len(ct.samples) {
				continue
			}
			at := float64(ct.samples[next[i]].DTS) / float64(ct.src.TimeScale)
			if at < bestTime {
				best, bestTime = i, at
			}
		}
		if best < 0 {
			break
		}
		order = append(order, sampleRef{best, next[best]})
		next[best]++
	}
	return order
}

// assignOffsets lays the samples out in the media box in interleaved order,
// starting at mediaStart bytes into the file.
func assignOffsets(tracks []*clipTrack, order []sampleRef, mediaStart int64) {
	at := mediaStart
	for _, ref := range order {
		ct := tracks[ref.track]
		ct.offsets[ref.sample] = uint32(at)
		at += int64(ct.samples[ref.sample].Size())
	}
}

// moovBound is an upper bound on the header this clip needs. The sample tables
// dominate it: at worst every sample contributes its own entry to the decode
// time, composition offset, size, chunk offset and keyframe tables.
func moovBound(tracks []*clipTrack) int {
	size := 4096
	for _, ct := range tracks {
		size += 4096 + len(ct.src.StsdRaw()) + len(ct.src.HdlrRaw()) + len(ct.src.DinfRaw())
		size += 28 * len(ct.samples)
	}
	return size
}

func buildFtyp() []byte {
	buf := make([]byte, 64)
	w := mp4.NewWriter(buf)
	w.WriteFtyp([4]byte{'i', 's', 'o', 'm'}, 512, [][4]byte{
		{'i', 's', 'o', 'm'}, {'i', 's', 'o', '2'}, {'a', 'v', 'c', '1'}, {'m', 'p', '4', '1'},
	})
	return w.Bytes()
}

func buildMoov(buf []byte, tracks []*clipTrack, movieTimescale uint32, movieDuration uint64) ([]byte, error) {
	nextTrackID := uint32(1)
	for _, ct := range tracks {
		if ct.src.ID >= nextTrackID {
			nextTrackID = ct.src.ID + 1
		}
	}
	w := mp4.NewWriter(buf)
	w.StartBox(mp4.TypeMoov)
	w.WriteMvhd(movieTimescale, movieDuration, nextTrackID)
	for _, ct := range tracks {
		writeTrak(&w, ct, movieTimescale)
	}
	w.EndBox()
	if err := w.Err(); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func writeTrak(w *mp4.Writer, ct *clipTrack, movieTimescale uint32) {
	t := ct.src
	trackDuration := ct.movieDuration(movieTimescale)

	w.StartBox(mp4.TypeTrak)
	var width, height uint32
	if t.Kind == track.TrackVideo {
		width, height = uint32(t.Width)<<16, uint32(t.Height)<<16
	}
	// The enabled, in-movie and in-preview flags are carried over so a track
	// the source had disabled stays that way.
	w.WriteTkhd(t.TkhdFlags(), t.ID, trackDuration, width, height)
	if ct.mediaAt > 0 {
		// The clip begins at a keyframe, which is usually earlier than what
		// was asked for, and composition offsets can delay the first frame
		// past zero as well. This maps the movie's zero onto the frame the
		// clip is meant to open on; the earlier frames stay in the file for
		// the decoder and stay off the timeline.
		w.StartBox(mp4.TypeEdts)
		w.WriteElst([]mp4.ElstEntry{{
			SegmentDuration: trackDuration, MediaTime: ct.mediaAt, MediaRateInt: 1,
		}})
		w.EndBox()
	}

	w.StartBox(mp4.TypeMdia)
	w.WriteMdhd(t.TimeScale, ct.duration, undeterminedLanguage)
	if raw := t.HdlrRaw(); len(raw) > 0 {
		w.Write(raw)
	} else if t.Kind == track.TrackVideo {
		w.WriteHdlr([4]byte{'v', 'i', 'd', 'e'}, "VideoHandler")
	} else {
		w.WriteHdlr([4]byte{'s', 'o', 'u', 'n'}, "SoundHandler")
	}

	w.StartBox(mp4.TypeMinf)
	if t.Kind == track.TrackVideo {
		w.WriteVmhd()
	} else {
		w.WriteSmhd()
	}
	if raw := t.DinfRaw(); len(raw) > 0 {
		w.Write(raw)
	} else {
		w.StartBox(mp4.TypeDinf)
		w.WriteDref()
		w.EndBox()
	}

	descIdx := t.SampleDescIdx
	if descIdx == 0 {
		descIdx = 1
	}
	w.StartBox(mp4.TypeStbl)
	w.Write(t.StsdRaw())
	w.WriteStts(ct.stts)
	if len(ct.ctts) > 0 {
		w.WriteCtts(ct.ctts)
	}
	// One sample per chunk keeps the mapping trivial and lets the samples be
	// interleaved in whatever order playback wants to read them.
	w.WriteStsc([]mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: 1, SampleDescriptionId: descIdx}})
	w.WriteStsz(0, ct.sizes)
	w.WriteStco(ct.offsets)
	if t.Kind == track.TrackVideo && len(ct.sync) > 0 {
		w.WriteStss(ct.sync)
	}
	w.EndBox() // stbl
	w.EndBox() // minf
	w.EndBox() // mdia
	w.EndBox() // trak
}

// readMoov returns the complete moov box, which holds every sample table the
// cut needs.
func readMoov(src io.ReaderAt, size int64) ([]byte, error) {
	sc := mp4.NewScanner(io.NewSectionReader(src, 0, size))
	for sc.Next() {
		entry := sc.Entry()
		if entry.Type != mp4.TypeMoov {
			continue
		}
		buf := make([]byte, entry.Size)
		if err := sc.ReadBox(buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("mp4: file has no moov box")
}

func be32(dst []byte, v uint32) {
	dst[0], dst[1], dst[2], dst[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}
