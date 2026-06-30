// Package fragment provides streaming MP4 fragmentation.
//
// It reads a standard MP4 file and produces fragmented MP4 (fMP4)
// segments suitable for adaptive streaming protocols like HLS and DASH.
//
// Basic usage:
//
//	reader, initSeg, err := fragment.NewReader(file)
//	if err != nil { ... }
//
//	writer := fragment.NewWriter(output)
//	writer.WriteInit(initSeg)
//
//	for {
//	    frag, err := reader.ReadFragment()
//	    if err == io.EOF { break }
//	    if err != nil { ... }
//	    writer.WriteFragment(frag, file)
//	}
//
// The [Reader] parses the moov box on construction and produces
// an [InitSegment] containing the ftyp+moov.
// Subsequent calls to [Reader.ReadFragment] produce fragments
// containing moof+mdat box pairs.
//
// Use [Reader.SetTimeRange] to limit output to a specific time window,
// or [Reader.Seek] to reposition to a different time.
package fragment

import (
	"errors"
	"io"
	"sort"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

var (
	// ErrNoMoov is returned when the input file has no moov box.
	ErrNoMoov = errors.New("moov box not found")
	// ErrNoPlayableTracks is returned when no video or audio tracks are found.
	ErrNoPlayableTracks = errors.New("no playable tracks found")
	// ErrMoovTooLarge is returned when the moov box exceeds the size limit.
	ErrMoovTooLarge = errors.New("moov box exceeds size limit")
	// ErrInvalidTimeRange is returned when an invalid time range is specified.
	ErrInvalidTimeRange = errors.New("invalid time range")
	// ErrReaderNotInitialized is returned when ReadFragment or Seek is called before initialization.
	ErrReaderNotInitialized = errors.New("reader not initialized")
	// ErrNoVideoTrack is returned when no video track is found in the init segment.
	ErrNoVideoTrack = errors.New("no video track")
	// ErrInvalidDuration is returned when a non-positive target duration is set.
	ErrInvalidDuration = errors.New("invalid target duration")
)

const (
	// DefaultMaxMoovSize is the default limit for moov box size (50 MB).
	DefaultMaxMoovSize = 50 << 20

	// minFragmentDuration is the minimum fragment duration in seconds.
	// Fragments shorter than this will be merged with the next fragment
	// (unless a sync point forces a break).
	minFragmentDuration = 1

	// maxTracks is the maximum number of concurrent tracks supported.
	// Limited to 4 to keep per-track arrays on the stack and avoid
	// heap allocations in the hot path.
	maxTracks = 4

	// movieTimescale is the timescale written into the init segment's mvhd.
	movieTimescale = 1000
)

// InitSegment holds parsed initialization data (ftyp+moov) for a fragmented MP4.
type InitSegment struct {
	Tracks   []*track.Track
	Duration uint64
	buf      []byte
}

// VideoTrack returns the first video track, or nil.
func (s *InitSegment) VideoTrack() *track.Track {
	for _, t := range s.Tracks {
		if t.Kind == track.TrackVideo {
			return t
		}
	}
	return nil
}

// AudioTrack returns the first audio track, or nil.
func (s *InitSegment) AudioTrack() *track.Track {
	for _, t := range s.Tracks {
		if t.Kind == track.TrackAudio {
			return t
		}
	}
	return nil
}

// Bytes returns the serialized init segment (ftyp+moov).
func (s *InitSegment) Bytes() []byte {
	return s.buf
}

// Fragment represents samples for one moof+mdat pair.
//
// Returned by [Reader.ReadFragment]; valid until the next ReadFragment call.
// Copy Samples if you need to retain the data.
type Fragment struct {
	Samples     []track.Sample
	SequenceNum uint32
}

// Reader reads a standard MP4 file and produces fragmented MP4 segments.
type Reader struct {
	rs      io.ReadSeeker
	sc      mp4.Scanner
	initSeg *InitSegment // points to initSegStorage once initialized

	trackIdx     [maxTracks]int
	dtsBase      [maxTracks]int64
	dtsBaseSet   [maxTracks]bool
	trackCount   int
	videoTrackID uint32

	videoRun [2]int // last fragment's video sample range [start, end)
	audioRun [2]int // last fragment's audio sample range [start, end)

	sequenceNum    uint32
	startTime      float64
	endTime        float64
	targetDuration float64

	frag        Fragment
	fragSamples []track.Sample

	allTracks []*track.Track
	moovBuf   []byte
	initBuf   []byte

	initSegStorage InitSegment    // reused backing for the returned init segment
	filtered       []*track.Track // reused backing for initSegStorage.Tracks
}

// NewReader creates a new fragment reader. It parses the moov box from
// the provided reader and builds the init segment for fragmented MP4 output.
// The returned [InitSegment] contains the serialized ftyp+moov and the parsed
// track information.
func NewReader(rs io.ReadSeeker) (*Reader, *InitSegment, error) {
	f := &Reader{rs: rs, targetDuration: minFragmentDuration}
	initSeg, err := f.readInit()
	if err != nil {
		return nil, nil, err
	}
	return f, initSeg, nil
}

// Reset reinitializes the Reader to read from rs, keeping the moov, init, and
// sample-window buffers allocated for a previous file. It parses the moov box
// and returns a fresh init segment, like NewReader. The InitSegment and
// Fragment returned by an earlier call must not be used after Reset.
func (f *Reader) Reset(rs io.ReadSeeker) (*InitSegment, error) {
	*f = Reader{
		rs:             rs,
		allTracks:      f.allTracks,
		moovBuf:        f.moovBuf[:0],
		initBuf:        f.initBuf[:0],
		fragSamples:    f.fragSamples[:0],
		filtered:       f.filtered[:0],
		targetDuration: f.targetDuration,
	}
	return f.readInit()
}

// SetTimeRange sets the start and end time (in seconds) for reading.
// An end time of 0 means read to the end of the file.
//
// Returns [ErrInvalidTimeRange] if start or end are negative, or if
// end is non-zero and less than or equal to start.
//
// SetTimeRange may be called at any time to change the range; it
// reinitializes the internal track positions.
func (f *Reader) SetTimeRange(startTime, endTime float64) error {
	if startTime < 0 || endTime < 0 {
		return ErrInvalidTimeRange
	}
	if endTime > 0 && startTime >= endTime {
		return ErrInvalidTimeRange
	}
	f.startTime = startTime
	f.endTime = endTime
	f.initTrackIndices()
	return nil
}

// SetTargetDuration sets the minimum segment duration in seconds. Each call to
// ReadFragment runs until the first sync sample at or after this duration. The
// default is one second.
func (f *Reader) SetTargetDuration(seconds float64) error {
	if seconds <= 0 {
		return ErrInvalidDuration
	}
	f.targetDuration = seconds
	return nil
}

// readInit parses the moov box and builds the init segment.
func (f *Reader) readInit() (*InitSegment, error) {
	if f.initSeg != nil {
		return f.initSeg, nil
	}

	moovBuf, err := f.findMoov()
	if err != nil {
		return nil, err
	}

	tracks, duration, err := track.ParseTracksInto(f.allTracks, moovBuf)
	if err != nil {
		return nil, err
	}
	f.allTracks = tracks

	// Filter to first video + first audio only.
	f.filtered = f.filtered[:0]
	hasVideo, hasAudio := false, false
	for _, t := range tracks {
		if t.Kind == track.TrackVideo && !hasVideo {
			f.filtered = append(f.filtered, t)
			hasVideo = true
		} else if t.Kind == track.TrackAudio && !hasAudio {
			f.filtered = append(f.filtered, t)
			hasAudio = true
		}
	}
	if len(f.filtered) == 0 {
		return nil, ErrNoPlayableTracks
	}

	initSeg := &f.initSegStorage
	initSeg.Tracks = f.filtered
	initSeg.Duration = duration
	initSeg.buf = f.buildInitSegment(initSeg.Tracks, initSeg.Duration)

	f.initSeg = initSeg
	f.trackCount = len(initSeg.Tracks)
	f.sequenceNum = 1

	if vt := initSeg.VideoTrack(); vt != nil {
		f.videoTrackID = vt.ID
	}

	f.initTrackIndices()
	return initSeg, nil
}

func (f *Reader) initTrackIndices() {
	f.dtsBaseSet = [maxTracks]bool{}
	f.dtsBase = [maxTracks]int64{}

	videoTrack := f.initSeg.VideoTrack()
	var videoStartPTS int64

	for i, t := range f.initSeg.Tracks {
		if t.Kind == track.TrackVideo && f.startTime > 0 {
			idx := f.findSampleAfter(t, f.startTime)
			n := len(t.Samples)

			if f.endTime > 0 && idx < n {
				endTimeScaled := int64(f.endTime * float64(t.TimeScale))
				if t.Samples[idx].PTS() >= endTimeScaled {
					idx = f.findSampleBefore(t, f.startTime)
				}
			}

			f.trackIdx[i] = idx
			if idx < n {
				videoStartPTS = t.Samples[idx].PTS()
			}
		} else if t.Kind == track.TrackVideo {
			f.trackIdx[i] = 0
		}
	}

	for i, t := range f.initSeg.Tracks {
		if t.Kind == track.TrackVideo {
			continue
		}
		if f.startTime > 0 && videoTrack != nil {
			audioStartTicks := videoStartPTS * int64(t.TimeScale) / int64(videoTrack.TimeScale)
			idx := sort.Search(len(t.Samples), func(j int) bool {
				return t.Samples[j].PTS() >= audioStartTicks
			})
			f.trackIdx[i] = idx
		} else {
			f.trackIdx[i] = 0
		}
	}
}

func (f *Reader) findSampleAfter(track *track.Track, timeSeconds float64) int {
	scaledTime := int64(timeSeconds * float64(track.TimeScale))
	n := len(track.Samples)
	idx := sort.Search(n, func(i int) bool {
		return track.Samples[i].PTS() >= scaledTime
	})
	if idx >= n {
		return n - 1
	}
	for idx < n && !track.Samples[idx].IsSync() {
		idx++
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

func (f *Reader) findSampleBefore(track *track.Track, timeSeconds float64) int {
	scaledTime := int64(timeSeconds * float64(track.TimeScale))
	n := len(track.Samples)
	idx := max(sort.Search(n, func(i int) bool {
		return track.Samples[i].PTS() > scaledTime
	})-1, 0)
	for idx > 0 && !track.Samples[idx].IsSync() {
		idx--
	}
	return idx
}

func (f *Reader) getTrackIndex(trackID uint32) int {
	for i, track := range f.initSeg.Tracks {
		if track.ID == trackID {
			return i
		}
	}
	return -1
}

// appendSample adds a sample with DTS rebased relative to the first sample per track.
func (f *Reader) appendSample(trackIdx int, s track.Sample) {
	if !f.dtsBaseSet[trackIdx] {
		f.dtsBase[trackIdx] = s.DTS
		f.dtsBaseSet[trackIdx] = true
	}
	s.DTS -= f.dtsBase[trackIdx]
	f.fragSamples = append(f.fragSamples, s)
}

// ReadFragment returns the next fragment.
// Returns [io.EOF] when there are no more fragments to read.
func (f *Reader) ReadFragment() (*Fragment, error) {
	if f.initSeg == nil {
		return nil, ErrReaderNotInitialized
	}

	videoTrack := f.initSeg.VideoTrack()
	if videoTrack == nil {
		return nil, ErrNoVideoTrack
	}

	videoTrackIdx := f.getTrackIndex(videoTrack.ID)
	videoSampleIdx := f.trackIdx[videoTrackIdx]

	if videoSampleIdx >= len(videoTrack.Samples) {
		return nil, io.EOF
	}

	var endTimeScaled int64
	if f.endTime > 0 {
		endTimeScaled = int64(f.endTime * float64(videoTrack.TimeScale))
	}

	if endTimeScaled > 0 && videoTrack.Samples[videoSampleIdx].PTS() >= endTimeScaled {
		return nil, io.EOF
	}

	startDTS := videoTrack.Samples[videoSampleIdx].DTS
	threshold := int64(f.targetDuration * float64(videoTrack.TimeScale))
	lastVideoIdx := videoSampleIdx

	for lastVideoIdx < len(videoTrack.Samples) {
		s := videoTrack.Samples[lastVideoIdx]
		if endTimeScaled > 0 && s.PTS() >= endTimeScaled {
			break
		}
		if endTimeScaled == 0 && lastVideoIdx > videoSampleIdx && s.IsSync() {
			if s.DTS-startDTS >= threshold {
				break
			}
		}
		lastVideoIdx++
	}

	if lastVideoIdx == videoSampleIdx {
		return nil, io.EOF
	}

	f.videoRun = [2]int{videoSampleIdx, lastVideoIdx}
	f.audioRun = [2]int{}

	var audioStart [maxTracks]int
	var audioEnd [maxTracks]int
	need := lastVideoIdx - videoSampleIdx

	fragStartPTS := videoTrack.Samples[videoSampleIdx].PTS()
	var fragEndPTS int64
	if lastVideoIdx < len(videoTrack.Samples) {
		fragEndPTS = videoTrack.Samples[lastVideoIdx].PTS()
	} else {
		lastSample := videoTrack.Samples[lastVideoIdx-1]
		fragEndPTS = lastSample.DTS + int64(lastSample.Duration)
	}

	for i, t := range f.initSeg.Tracks {
		if t.Kind == track.TrackVideo {
			continue
		}
		startTicks := fragStartPTS * int64(t.TimeScale) / int64(videoTrack.TimeScale)
		endTicks := fragEndPTS * int64(t.TimeScale) / int64(videoTrack.TimeScale)
		idx := f.trackIdx[i]
		n := len(t.Samples)
		for idx < n && t.Samples[idx].PTS() < startTicks {
			idx++
		}
		audioStart[i] = idx
		for idx < n && t.Samples[idx].PTS() < endTicks {
			idx++
		}
		audioEnd[i] = idx
		if t.Kind == track.TrackAudio {
			f.audioRun = [2]int{audioStart[i], audioEnd[i]}
		}
		need += idx - audioStart[i]
	}

	if cap(f.fragSamples) < need {
		f.fragSamples = make([]track.Sample, 0, need)
	}
	f.fragSamples = f.fragSamples[:0]

	// Append video samples
	for i := videoSampleIdx; i < lastVideoIdx; i++ {
		f.appendSample(videoTrackIdx, videoTrack.Samples[i])
	}
	f.trackIdx[videoTrackIdx] = lastVideoIdx

	for i, t := range f.initSeg.Tracks {
		if t.Kind == track.TrackVideo {
			continue
		}
		// Append audio samples
		for j := audioStart[i]; j < audioEnd[i]; j++ {
			f.appendSample(i, t.Samples[j])
		}
		f.trackIdx[i] = audioEnd[i]
	}

	f.frag.Samples = f.fragSamples
	f.frag.SequenceNum = f.sequenceNum
	f.sequenceNum++
	return &f.frag, nil
}

// VideoRun returns the video track's sample range [start, end) in the most
// recent fragment returned by ReadFragment.
func (f *Reader) VideoRun() (start, end int) {
	return f.videoRun[0], f.videoRun[1]
}

// AudioRun returns the audio track's sample range [start, end) in the most
// recent fragment, or (0, 0) when there is no audio track.
func (f *Reader) AudioRun() (start, end int) {
	return f.audioRun[0], f.audioRun[1]
}

// Seek repositions reading to the given time (in seconds).
// The next ReadFragment call will produce a fragment starting at or
// near the requested time (snapped to the nearest sync point).
func (f *Reader) Seek(timeSeconds float64) error {
	if f.initSeg == nil {
		return ErrReaderNotInitialized
	}
	f.startTime = timeSeconds
	f.initTrackIndices()
	return nil
}

// findMoov scans for the moov box and returns its raw bytes.
func (f *Reader) findMoov() ([]byte, error) {
	maxSize := int64(DefaultMaxMoovSize)

	if _, err := f.rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	f.sc.Reset(f.rs)
	for f.sc.Next() {
		e := f.sc.Entry()
		if e.Type != mp4.TypeMoov {
			continue
		}
		if e.Size > maxSize {
			return nil, ErrMoovTooLarge
		}
		if e.Size > int64(^uint(0)>>1) {
			return nil, ErrMoovTooLarge
		}
		needed := int(e.Size)
		if cap(f.moovBuf) < needed {
			f.moovBuf = make([]byte, needed)
		}
		buf := f.moovBuf[:needed]
		if err := f.sc.ReadBox(buf); err != nil {
			return nil, err
		}
		return buf, nil
	}

	if err := f.sc.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNoMoov
}

// buildInitSegment constructs ftyp+moov for fragmented MP4.
func (f *Reader) buildInitSegment(tracks []*track.Track, duration uint64) []byte {
	estSize := 256
	for _, track := range tracks {
		estSize += 256 + len(track.HdlrRaw()) + len(track.DinfRaw()) + len(track.StsdRaw()) + len(track.TkhdRaw()) + len(track.MdhdRaw())
	}

	if cap(f.initBuf) < estSize {
		f.initBuf = make([]byte, estSize)
	}
	buf := f.initBuf[:estSize]
	w := mp4.NewWriter(buf)

	w.WriteFtyp([4]byte{'i', 's', 'o', '5'}, 0,
		[][4]byte{{'i', 's', 'o', '5'}, {'a', 'v', 'c', '1'}})

	w.StartBox(mp4.TypeMoov)
	{
		w.WriteMvhd(movieTimescale, 0, uint32(len(tracks)+1))

		for _, track := range tracks {
			writeInitTrak(&w, track)
		}

		w.StartBox(mp4.TypeMvex)
		{
			w.WriteMehd(duration)
			for _, track := range tracks {
				w.WriteTrex(track.ID, track.SampleDescIdx, 0, 0, 0)
			}
		}
		w.EndBox()
	}
	w.EndBox()

	return w.Bytes()
}

func writeInitTrak(w *mp4.Writer, track *track.Track) {
	w.StartBox(mp4.TypeTrak)
	{
		writeTkhdZeroDuration(w, track)

		if mt, ok := track.EditMediaTime(); ok && track.TimeScale > 0 {
			segDur := track.Duration * movieTimescale / uint64(track.TimeScale)
			w.StartBox(mp4.TypeEdts)
			w.WriteElst([]mp4.ElstEntry{{
				SegmentDuration: segDur,
				MediaTime:       mt,
				MediaRateInt:    1,
			}})
			w.EndBox()
		}

		w.StartBox(mp4.TypeMdia)
		{
			writeMdhdZeroDuration(w, track)

			if track.HdlrRaw() != nil {
				w.Write(track.HdlrRaw())
			}

			w.StartBox(mp4.TypeMinf)
			{
				if track.HasVmhd() {
					w.WriteVmhd()
				} else {
					w.WriteSmhd()
				}

				if track.HasDinf() && track.DinfRaw() != nil {
					w.Write(track.DinfRaw())
				} else {
					w.StartBox(mp4.TypeDinf)
					w.WriteDref()
					w.EndBox()
				}

				w.StartBox(mp4.TypeStbl)
				{
					if track.StsdRaw() != nil {
						w.Write(track.StsdRaw())
					}
					w.WriteStts(nil)
					w.WriteStsc(nil)
					w.WriteStsz(0, nil)
					w.WriteStco(nil)
				}
				w.EndBox()
			}
			w.EndBox()
		}
		w.EndBox()
	}
	w.EndBox()
}

func writeTkhdZeroDuration(w *mp4.Writer, track *track.Track) {
	data := track.TkhdRaw()
	if data == nil {
		return
	}
	w.StartFullBox(mp4.TypeTkhd, track.TkhdVersion(), track.TkhdFlags())
	start := w.Len()
	w.Write(data)
	out := w.Bytes()[start : start+len(data)]
	if track.TkhdVersion() == 1 {
		clear(out[24:32])
	} else {
		clear(out[16:20])
	}
	w.EndBox()
}

func writeMdhdZeroDuration(w *mp4.Writer, track *track.Track) {
	data := track.MdhdRaw()
	if data == nil {
		return
	}
	w.StartFullBox(mp4.TypeMdhd, track.MdhdVersion(), 0)
	start := w.Len()
	w.Write(data)
	out := w.Bytes()[start : start+len(data)]
	if track.MdhdVersion() == 1 {
		clear(out[20:28])
	} else {
		clear(out[12:16])
	}
	w.EndBox()
}
