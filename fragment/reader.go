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
	// ErrInvalidDuration is returned when a non-positive target duration is set.
	ErrInvalidDuration = errors.New("invalid target duration")
	// ErrTrackNotFound is returned when a requested track is not present.
	ErrTrackNotFound = errors.New("track not found")
	// ErrUnalignedVideoTrack is returned when video tracks do not share fragment boundaries.
	ErrUnalignedVideoTrack = errors.New("unaligned video track")
)

const (
	// DefaultMaxMoovSize is the default limit for moov box size (50 MB).
	DefaultMaxMoovSize = 50 << 20

	// minFragmentDuration is the minimum fragment duration in seconds.
	// Fragments shorter than this will be merged with the next fragment
	// (unless a sync point forces a break).
	minFragmentDuration = 1

	// defaultMovieTimescale is written into the init segment's mvhd when the
	// source movie timescale is missing or zero.
	defaultMovieTimescale = 1000
)

// InitSegment holds parsed initialization data (ftyp+moov) for a fragmented MP4.
type InitSegment struct {
	Tracks   []*track.Track
	Duration uint64
	buf      []byte

	movieTimescale uint32
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

// MovieTimescale returns the source movie's timescale used by Duration.
func (s *InitSegment) MovieTimescale() uint32 {
	return s.movieTimescale
}

// TrackBytes builds an initialization segment containing only trackID. dst is
// reused when it has enough capacity, allowing callers that build many
// renditions to retain their own buffers without an allocation after warmup.
func (s *InitSegment) TrackBytes(dst []byte, trackID uint32) ([]byte, error) {
	selected := track.FindTrack(s.Tracks, trackID)
	if selected == nil {
		return nil, ErrTrackNotFound
	}
	tracks := [1]*track.Track{selected}
	return buildInitSegment(dst, tracks[:], s.movieTimescale, s.Duration), nil
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

	trackState []readerTrackState

	selectedTrackID uint32

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

type readerTrackState struct {
	dtsBase    int64
	sampleIdx  int
	runStart   int
	runEnd     int
	dtsBaseSet bool
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
		trackState:     f.trackState[:0],
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

// SelectTrack makes trackID the fragment cut driver and excludes every other
// track from subsequent fragments. Passing an unknown track ID returns
// ErrTrackNotFound. Selection resets reading to the configured time range.
func (f *Reader) SelectTrack(trackID uint32) error {
	if f.initSeg == nil {
		return ErrReaderNotInitialized
	}
	if track.FindTrack(f.initSeg.Tracks, trackID) == nil {
		return ErrTrackNotFound
	}
	f.selectedTrackID = trackID
	f.sequenceNum = 1
	f.initTrackIndices()
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

	tracks, movieTimescale, duration, err := track.ParseTracksInto(f.allTracks, moovBuf)
	if err != nil {
		return nil, err
	}
	f.allTracks = tracks

	// Retain every playable track in source order. The parsed allTracks slice is
	// kept separately so Reset can reuse every Track and its sample storage.
	f.filtered = f.filtered[:0]
	for _, t := range tracks {
		if t.Kind == track.TrackVideo || t.Kind == track.TrackAudio {
			f.filtered = append(f.filtered, t)
		}
	}
	if len(f.filtered) == 0 {
		return nil, ErrNoPlayableTracks
	}

	if movieTimescale == 0 {
		movieTimescale = defaultMovieTimescale
	}
	initSeg := &f.initSegStorage
	initSeg.Tracks = f.filtered
	initSeg.Duration = duration
	initSeg.movieTimescale = movieTimescale
	f.initBuf = buildInitSegment(f.initBuf, initSeg.Tracks, movieTimescale, initSeg.Duration)
	initSeg.buf = f.initBuf

	f.initSeg = initSeg
	f.sequenceNum = 1
	f.resizeTrackState(len(initSeg.Tracks))

	f.initTrackIndices()
	return initSeg, nil
}

func (f *Reader) resizeTrackState(n int) {
	f.trackState = resizeAndClear(f.trackState, n)
}

func resizeAndClear[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

func (f *Reader) initTrackIndices() {
	clear(f.trackState)

	driver := f.cutTrack()
	driverIdx := f.getTrackIndex(driver.ID)
	idx := 0
	if f.startTime > 0 {
		requireSync := driver.Kind == track.TrackVideo
		idx = f.findSampleAfter(driver, f.startTime, requireSync)
		if f.endTime > 0 && idx < len(driver.Samples) {
			endTimeScaled := int64(f.endTime * float64(driver.TimeScale))
			if driver.Samples[idx].PTS() >= endTimeScaled {
				idx = f.findSampleBefore(driver, f.startTime, requireSync)
			}
		}
	}
	f.trackState[driverIdx].sampleIdx = idx
	var driverStartPTS int64
	if idx < len(driver.Samples) {
		driverStartPTS = driver.Samples[idx].PTS()
	}

	for i, t := range f.initSeg.Tracks {
		if i == driverIdx {
			continue
		}
		if f.startTime > 0 {
			startTicks := driverStartPTS * int64(t.TimeScale) / int64(driver.TimeScale)
			idx := sort.Search(len(t.Samples), func(j int) bool {
				return t.Samples[j].PTS() >= startTicks
			})
			f.trackState[i].sampleIdx = idx
		}
	}
}

func (f *Reader) cutTrack() *track.Track {
	if f.selectedTrackID != 0 {
		return track.FindTrack(f.initSeg.Tracks, f.selectedTrackID)
	}
	if video := f.initSeg.VideoTrack(); video != nil {
		return video
	}
	return f.initSeg.AudioTrack()
}

func (f *Reader) findSampleAfter(track *track.Track, timeSeconds float64, requireSync bool) int {
	scaledTime := int64(timeSeconds * float64(track.TimeScale))
	n := len(track.Samples)
	idx := sort.Search(n, func(i int) bool {
		return track.Samples[i].PTS() >= scaledTime
	})
	if idx >= n {
		return n - 1
	}
	for requireSync && idx < n && !track.Samples[idx].IsSync() {
		idx++
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

func (f *Reader) findSampleBefore(track *track.Track, timeSeconds float64, requireSync bool) int {
	scaledTime := int64(timeSeconds * float64(track.TimeScale))
	n := len(track.Samples)
	idx := max(sort.Search(n, func(i int) bool {
		return track.Samples[i].PTS() > scaledTime
	})-1, 0)
	for requireSync && idx > 0 && !track.Samples[idx].IsSync() {
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
func (f *Reader) appendSample(state *readerTrackState, s track.Sample) {
	if !state.dtsBaseSet {
		state.dtsBase = s.DTS
		state.dtsBaseSet = true
	}
	s.DTS -= state.dtsBase
	f.fragSamples = append(f.fragSamples, s)
}

// ReadFragment returns the next fragment. A track selected with SelectTrack
// drives cuts by itself. Otherwise video drives cuts when present, and audio
// drives cuts for audio-only input.
// Returns [io.EOF] when there are no more fragments to read.
func (f *Reader) ReadFragment() (*Fragment, error) {
	if f.initSeg == nil {
		return nil, ErrReaderNotInitialized
	}

	driver := f.cutTrack()
	driverIdx := f.getTrackIndex(driver.ID)
	driverState := &f.trackState[driverIdx]
	driverSampleIdx := driverState.sampleIdx

	if driverSampleIdx >= len(driver.Samples) {
		return nil, io.EOF
	}

	var endTimeScaled int64
	if f.endTime > 0 {
		endTimeScaled = int64(f.endTime * float64(driver.TimeScale))
	}

	if endTimeScaled > 0 && driver.Samples[driverSampleIdx].PTS() >= endTimeScaled {
		return nil, io.EOF
	}

	startDTS := driver.Samples[driverSampleIdx].DTS
	threshold := int64(f.targetDuration * float64(driver.TimeScale))
	lastDriverIdx := driverSampleIdx

	for lastDriverIdx < len(driver.Samples) {
		s := driver.Samples[lastDriverIdx]
		if endTimeScaled > 0 && s.PTS() >= endTimeScaled {
			break
		}
		cutPoint := driver.Kind == track.TrackAudio || s.IsSync()
		if endTimeScaled == 0 && lastDriverIdx > driverSampleIdx && cutPoint {
			if s.DTS-startDTS >= threshold {
				break
			}
		}
		lastDriverIdx++
	}

	if lastDriverIdx == driverSampleIdx {
		return nil, io.EOF
	}

	if f.selectedTrackID != 0 {
		for i := range f.trackState {
			f.trackState[i].runStart = 0
			f.trackState[i].runEnd = 0
		}
	}
	driverState.runStart = driverSampleIdx
	driverState.runEnd = lastDriverIdx

	need := lastDriverIdx - driverSampleIdx

	fragStartPTS := driver.Samples[driverSampleIdx].PTS()
	var fragEndPTS int64
	if lastDriverIdx < len(driver.Samples) {
		fragEndPTS = driver.Samples[lastDriverIdx].PTS()
	} else {
		lastSample := driver.Samples[lastDriverIdx-1]
		fragEndPTS = lastSample.DTS + int64(lastSample.Duration)
	}

	if f.selectedTrackID == 0 {
		for i, t := range f.initSeg.Tracks {
			if i == driverIdx {
				continue
			}
			startTicks := fragStartPTS * int64(t.TimeScale) / int64(driver.TimeScale)
			endTicks := fragEndPTS * int64(t.TimeScale) / int64(driver.TimeScale)
			state := &f.trackState[i]
			idx := state.sampleIdx
			n := len(t.Samples)
			for idx < n && t.Samples[idx].PTS() < startTicks {
				idx++
			}
			start := idx
			for idx < n && t.Samples[idx].PTS() < endTicks {
				idx++
			}
			if t.Kind == track.TrackVideo && start < idx && !t.Samples[start].IsSync() {
				return nil, ErrUnalignedVideoTrack
			}
			state.runStart = start
			state.runEnd = idx
			need += idx - start
		}
	}

	if cap(f.fragSamples) < need {
		f.fragSamples = make([]track.Sample, 0, need)
	}
	f.fragSamples = f.fragSamples[:0]

	for i := driverSampleIdx; i < lastDriverIdx; i++ {
		f.appendSample(driverState, driver.Samples[i])
	}
	driverState.sampleIdx = lastDriverIdx

	if f.selectedTrackID == 0 {
		for i, t := range f.initSeg.Tracks {
			if i == driverIdx {
				continue
			}
			state := &f.trackState[i]
			start, end := state.runStart, state.runEnd
			for j := start; j < end; j++ {
				f.appendSample(state, t.Samples[j])
			}
			state.sampleIdx = end
		}
	}

	f.frag.Samples = f.fragSamples
	f.frag.SequenceNum = f.sequenceNum
	f.sequenceNum++
	return &f.frag, nil
}

// TrackRun returns a track's sample range [start, end) in the most recent
// fragment returned by ReadFragment. It returns (0, 0) for an unknown track.
func (f *Reader) TrackRun(trackID uint32) (start, end int) {
	i := f.getTrackIndex(trackID)
	if i < 0 {
		return 0, 0
	}
	return f.trackState[i].runStart, f.trackState[i].runEnd
}

// VideoRun returns the first video track's sample range [start, end) in the
// most recent fragment returned by ReadFragment.
func (f *Reader) VideoRun() (start, end int) {
	video := f.initSeg.VideoTrack()
	if video == nil {
		return 0, 0
	}
	return f.TrackRun(video.ID)
}

// AudioRun returns the first audio track's sample range [start, end) in the
// most recent fragment, or (0, 0) when there is no audio track.
func (f *Reader) AudioRun() (start, end int) {
	audio := f.initSeg.AudioTrack()
	if audio == nil {
		return 0, 0
	}
	return f.TrackRun(audio.ID)
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

// buildInitSegment constructs ftyp+moov for fragmented MP4, reusing dst when
// it has enough capacity.
func buildInitSegment(dst []byte, tracks []*track.Track, movieTimescale uint32, duration uint64) []byte {
	if movieTimescale == 0 {
		movieTimescale = defaultMovieTimescale
	}

	estSize := 256
	for _, track := range tracks {
		estSize += 256 + len(track.HdlrRaw()) + len(track.DinfRaw()) + len(track.StsdRaw()) + len(track.TkhdRaw()) + len(track.MdhdRaw())
	}

	if cap(dst) < estSize {
		dst = make([]byte, estSize)
	}
	buf := dst[:estSize]
	w := mp4.NewWriter(buf)

	w.WriteFtyp([4]byte{'i', 's', 'o', '5'}, 0,
		[][4]byte{{'i', 's', 'o', '5'}, {'a', 'v', 'c', '1'}})

	w.StartBox(mp4.TypeMoov)
	{
		nextTrackID := uint32(1)
		for _, track := range tracks {
			if track.ID >= nextTrackID {
				nextTrackID = track.ID + 1
			}
		}
		w.WriteMvhd(movieTimescale, 0, nextTrackID)

		for _, track := range tracks {
			writeInitTrak(&w, movieTimescale, track)
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

func writeInitTrak(w *mp4.Writer, movieTimescale uint32, track *track.Track) {
	w.StartBox(mp4.TypeTrak)
	{
		writeTkhdZeroDuration(w, track)

		if mt, ok := track.EditMediaTime(); ok && track.TimeScale > 0 {
			segDur := track.Duration * uint64(movieTimescale) / uint64(track.TimeScale)
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
