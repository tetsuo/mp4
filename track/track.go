package track

import (
	"errors"
	"fmt"

	"github.com/tetsuo/mp4"
)

// TrackKind classifies a track as video, audio, or unknown.
type TrackKind int

const (
	TrackUnknown TrackKind = iota
	TrackVideo
	TrackAudio
)

// trackRaw holds internal parsing state and raw box data.
type trackRaw struct {
	// Raw sub-slices of moov buffer.
	stsd []byte
	tkhd []byte // tkhd data (after version+flags header)
	mdhd []byte // mdhd data (after version+flags header)
	hdlr []byte // entire hdlr raw box
	dinf []byte // entire dinf raw box

	tkhdVersion uint8
	tkhdFlags   uint32
	mdhdVersion uint8
	hasVmhd     bool
	hasDinf     bool

	elstMediaTime int64 // edit list media time (media timescale)
	hasElst       bool

	// Raw sample table data.
	stszData    []byte
	sttsData    []byte
	stscData    []byte
	cttsData    []byte
	cttsVersion uint8
	stssData    []byte
	stcoData    []byte
	co64Data    []byte
	hasCo64     bool
	sampleCount uint32

	// Codec string builder buffer.
	codecBuf [24]byte
	codecLen uint8
}

// Track holds metadata for one track parsed from a moov box.
type Track struct {
	ID        uint32
	Kind      TrackKind
	TimeScale uint32
	Duration  uint64

	Width        uint16
	Height       uint16
	ChannelCount uint16
	SampleRate   uint32

	Samples       []Sample
	SampleDescIdx uint32

	raw trackRaw
}

// Codec returns the MIME codec string (e.g. "avc1.64001e", "mp4a.40.2").
func (t *Track) Codec() string { return string(t.raw.codecBuf[:t.raw.codecLen]) }

// StsdRaw returns the raw stsd box data (entire box including header).
func (t *Track) StsdRaw() []byte { return t.raw.stsd }

// TkhdRaw returns the tkhd box data (after version+flags header).
func (t *Track) TkhdRaw() []byte { return t.raw.tkhd }

// MdhdRaw returns the mdhd box data (after version+flags header).
func (t *Track) MdhdRaw() []byte { return t.raw.mdhd }

// HdlrRaw returns the entire hdlr raw box.
func (t *Track) HdlrRaw() []byte { return t.raw.hdlr }

// DinfRaw returns the entire dinf raw box.
func (t *Track) DinfRaw() []byte { return t.raw.dinf }

// TkhdVersion returns the version field of the tkhd box.
func (t *Track) TkhdVersion() uint8 { return t.raw.tkhdVersion }

// TkhdFlags returns the flags field of the tkhd box.
func (t *Track) TkhdFlags() uint32 { return t.raw.tkhdFlags }

// MdhdVersion returns the version field of the mdhd box.
func (t *Track) MdhdVersion() uint8 { return t.raw.mdhdVersion }

// HasVmhd returns true if the track has a vmhd box (video media header).
func (t *Track) HasVmhd() bool { return t.raw.hasVmhd }

// HasDinf returns true if the track has a dinf box (data information).
func (t *Track) HasDinf() bool { return t.raw.hasDinf }

// EditMediaTime returns the media time of the track's edit list and whether the
// track has one. It reproduces the initial composition offset, so the first
// frame is presented at time zero.
func (t *Track) EditMediaTime() (int64, bool) {
	return t.raw.elstMediaTime, t.raw.hasElst
}

// FindTrack returns the track with the given ID, or nil.
func FindTrack(tracks []*Track, id uint32) *Track {
	for _, t := range tracks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (t *Track) setCodec(s string) {
	n := copy(t.raw.codecBuf[:], s)
	t.raw.codecLen = uint8(n)
}

func (t *Track) appendCodec(s string) {
	n := copy(t.raw.codecBuf[t.raw.codecLen:], s)
	t.raw.codecLen += uint8(n)
}

// appendAvcCProfile appends "XXYYZZ" hex profile to the codec buffer.
func (t *Track) appendAvcCProfile(profile, compat, level byte) {
	i := t.raw.codecLen
	t.raw.codecBuf[i+0] = hexChars[profile>>4]
	t.raw.codecBuf[i+1] = hexChars[profile&0x0f]
	t.raw.codecBuf[i+2] = hexChars[compat>>4]
	t.raw.codecBuf[i+3] = hexChars[compat&0x0f]
	t.raw.codecBuf[i+4] = hexChars[level>>4]
	t.raw.codecBuf[i+5] = hexChars[level&0x0f]
	t.raw.codecLen += 6
}

// appendAv1CProfile appends ".P.LLT.DD" to the codec buffer from av1C record
// data: seq_profile, 2-digit seq_level_idx, tier (M or H), and bit depth.
func (t *Track) appendAv1CProfile(d []byte) {
	seqProfile := d[1] >> 5
	seqLevelIdx := d[1] & 0x1f
	seqTier := d[2] >> 7
	highBitdepth := (d[2] >> 6) & 0x01
	twelveBit := (d[2] >> 5) & 0x01

	bitDepth := byte(8)
	if highBitdepth == 1 {
		if seqProfile == 2 && twelveBit == 1 {
			bitDepth = 12
		} else {
			bitDepth = 10
		}
	}
	tier := byte('M')
	if seqTier == 1 {
		tier = 'H'
	}

	i := t.raw.codecLen
	b := t.raw.codecBuf[:]
	b[i+0] = '.'
	b[i+1] = '0' + seqProfile
	b[i+2] = '.'
	b[i+3] = '0' + seqLevelIdx/10
	b[i+4] = '0' + seqLevelIdx%10
	b[i+5] = tier
	b[i+6] = '.'
	b[i+7] = '0' + bitDepth/10
	b[i+8] = '0' + bitDepth%10
	t.raw.codecLen += 9
}

// appendEsdsCodec appends ".OTI.audioConfig" to the codec buffer from esds data.
func (t *Track) appendEsdsCodec(data []byte) {
	oti, audioConfig := parseEsds(data)
	if oti == 0 {
		return
	}
	t.appendCodec(".")
	if oti >= 16 {
		t.raw.codecBuf[t.raw.codecLen] = hexChars[oti>>4]
		t.raw.codecLen++
	}
	t.raw.codecBuf[t.raw.codecLen] = hexChars[oti&0x0f]
	t.raw.codecLen++
	if audioConfig > 0 {
		t.appendCodec(".")
		if audioConfig >= 10 {
			t.raw.codecBuf[t.raw.codecLen] = '0' + audioConfig/10
			t.raw.codecLen++
		}
		t.raw.codecBuf[t.raw.codecLen] = '0' + audioConfig%10
		t.raw.codecLen++
	}
}

// parseEsds extracts OTI and audio object type from esds box data.
func parseEsds(data []byte) (oti, audioConfig byte) {
	if len(data) < 2 {
		return 0, 0
	}
	ptr, end := 0, len(data)
	if data[ptr] != 0x03 {
		return 0, 0
	}
	ptr++
	ptr = skipDescLen(data, ptr, end)
	if ptr < 0 || ptr+3 > end {
		return 0, 0
	}
	flags := data[ptr+2]
	ptr += 3
	if flags&0x80 != 0 {
		ptr += 2
	}
	if flags&0x40 != 0 {
		if ptr >= end {
			return 0, 0
		}
		ptr += 1 + int(data[ptr])
	}
	if flags&0x20 != 0 {
		ptr += 2
	}
	if ptr >= end || data[ptr] != 0x04 {
		return 0, 0
	}
	ptr++
	ptr = skipDescLen(data, ptr, end)
	if ptr < 0 || ptr+13 > end {
		return 0, 0
	}
	oti = data[ptr]
	if oti == 0 {
		return 0, 0
	}
	ptr += 13
	if ptr >= end || data[ptr] != 0x05 {
		return oti, 0
	}
	ptr++
	ptr = skipDescLen(data, ptr, end)
	if ptr < 0 || ptr >= end {
		return oti, 0
	}
	audioConfig = (data[ptr] & 0xf8) >> 3
	return oti, audioConfig
}

func skipDescLen(data []byte, ptr, end int) int {
	for ptr < end {
		b := data[ptr]
		ptr++
		if b&0x80 == 0 {
			return ptr
		}
	}
	return -1
}

// Sample represents a single media sample. The sync-sample flag is stored in
// the high bit of the size field, which keeps the struct at 32 bytes. Read the
// size and the flag through the Size and IsSync methods.
type Sample struct {
	Offset             int64
	DTS                int64
	TrackID            uint32
	size               uint32
	Duration           uint32
	PresentationOffset int32
}

// syncBit marks a sync sample in the high bit of Sample.size.
const syncBit uint32 = 1 << 31

// Size returns the sample size in bytes.
func (s Sample) Size() uint32 { return s.size &^ syncBit }

// IsSync reports whether the sample is a sync sample (keyframe).
func (s Sample) IsSync() bool { return s.size&syncBit != 0 }

// PTS returns the presentation timestamp.
func (s Sample) PTS() int64 {
	return s.DTS + int64(s.PresentationOffset)
}

// TrackSampleStats holds aggregated stats for samples belonging to one track.
type TrackSampleStats struct {
	TrackID     uint32
	TimeScale   uint32
	Duration    uint64
	EarliestPTS int64
	SampleCount int
}

// CollectTrackSampleStats aggregates sample count, duration, and earliest PTS
// per track. The returned slice contains only tracks that have at least one sample.
func CollectTrackSampleStats(dst []TrackSampleStats, tracks []*Track, samples []Sample) []TrackSampleStats {
	if cap(dst) < len(tracks) {
		dst = make([]TrackSampleStats, len(tracks))
	} else {
		dst = dst[:len(tracks)]
	}

	for i, t := range tracks {
		dst[i] = TrackSampleStats{
			TrackID:     t.ID,
			TimeScale:   t.TimeScale,
			EarliestPTS: -1,
		}
	}

	for i := range samples {
		s := &samples[i]
		for j := range dst {
			if dst[j].TrackID != s.TrackID {
				continue
			}
			st := &dst[j]
			st.SampleCount++
			st.Duration += uint64(s.Duration)
			pts := s.PTS()
			if st.EarliestPTS < 0 || pts < st.EarliestPTS {
				st.EarliestPTS = pts
			}
			break
		}
	}

	out := dst[:0]
	for i := range dst {
		if dst[i].SampleCount > 0 {
			out = append(out, dst[i])
		}
	}
	return out
}

var (
	htVide = [4]byte{'v', 'i', 'd', 'e'}
	htSoun = [4]byte{'s', 'o', 'u', 'n'}
)

var (
	ErrMoovNotFound = errors.New("moov box not found in buffer")
	ErrInvalidTrack = errors.New("invalid track data")
	ErrCorruptData  = errors.New("corrupt data")
)

// ParseTracks parses a moov box buffer and returns the tracks found with
// their samples fully populated. The moov buffer must include the box header
// (the full top-level moov box). The movie duration (from mvhd) is also returned.
//
// Returns an error if the moov box is not found, if no playable tracks are
// found, or if sample tables cannot be parsed for any track.
func ParseTracks(moovBuf []byte) ([]*Track, uint64, error) {
	return ParseTracksInto(nil, moovBuf)
}

// ParseTracksInto behaves like ParseTracks but reuses the Track structs in dst
// and their sample slices, which avoids most allocations when the new file has
// the same shape as the previous one. Pass the slice returned by an earlier
// call as dst. The tracks in dst must not be used after this call.
func ParseTracksInto(dst []*Track, moovBuf []byte) ([]*Track, uint64, error) {
	mr := mp4.NewReader(moovBuf)
	if !mr.Next() || mr.Type() != mp4.TypeMoov {
		return nil, 0, ErrMoovNotFound
	}

	var tracks []*Track
	var duration uint64
	reused := 0

	mr.Enter()
	for mr.Next() {
		switch mr.Type() {
		case mp4.TypeMvhd:
			_, dur, _ := mr.ReadMvhd()
			duration = dur
		case mp4.TypeTrak:
			var t *Track
			if reused < len(dst) {
				t = dst[reused]
				resetTrack(t)
			} else {
				t = &Track{}
			}
			reused++
			if parseTrakInto(&mr, t) {
				tracks = append(tracks, t)
			}
		}
	}
	mr.Exit()

	// Parse samples for all tracks; filter out those that fail
	var valid []*Track
	for _, t := range tracks {
		if err := t.parseSamples(); err != nil {
			continue
		}
		valid = append(valid, t)
	}

	return valid, duration, nil
}

// resetTrack clears t for reuse while keeping the backing array of its sample
// slice, so a later parse can refill it without allocating.
func resetTrack(t *Track) {
	samples := t.Samples[:0]
	*t = Track{}
	t.Samples = samples
}

// parseTrakInto fills track from a trak box and reports whether it is a valid,
// playable track.
func parseTrakInto(mr *mp4.Reader, track *Track) bool {
	mr.Enter()
	defer mr.Exit()

	for mr.Next() {
		switch mr.Type() {
		case mp4.TypeTkhd:
			track.raw.tkhdVersion = mr.Version()
			track.raw.tkhdFlags = mr.Flags()
			track.raw.tkhd = mr.Data()
			trackId, _, w, h := mr.ReadTkhd()
			track.ID = trackId
			track.Width = uint16(w >> 16)
			track.Height = uint16(h >> 16)
		case mp4.TypeEdts:
			parseEdts(mr, track)
		case mp4.TypeMdia:
			parseMdia(mr, track)
		}
	}

	return track.ID != 0 && track.raw.codecLen != 0
}

// parseEdts reads the first edit list entry's media time, used to reproduce the
// initial composition offset in the output's init segment.
func parseEdts(mr *mp4.Reader, track *Track) {
	mr.Enter()
	defer mr.Exit()
	for mr.Next() {
		if mr.Type() == mp4.TypeElst {
			if mt, ok := mr.ReadElst(); ok {
				track.raw.elstMediaTime = mt
				track.raw.hasElst = true
			}
			return
		}
	}
}

func parseMdia(mr *mp4.Reader, track *Track) {
	mr.Enter()
	defer mr.Exit()

	var handlerType [4]byte

	for mr.Next() {
		switch mr.Type() {
		case mp4.TypeMdhd:
			track.raw.mdhdVersion = mr.Version()
			track.raw.mdhd = mr.Data()
			ts, dur, _ := mr.ReadMdhd()
			track.TimeScale = ts
			track.Duration = dur
		case mp4.TypeHdlr:
			track.raw.hdlr = mr.RawBox()
			handlerType = mr.ReadHdlr()
		case mp4.TypeMinf:
			parseMinf(mr, track, handlerType)
		}
	}
}

func parseMinf(mr *mp4.Reader, track *Track, handlerType [4]byte) {
	mr.Enter()
	defer mr.Exit()

	for mr.Next() {
		switch mr.Type() {
		case mp4.TypeVmhd:
			track.raw.hasVmhd = true
		case mp4.TypeSmhd:
			track.raw.hasVmhd = false
		case mp4.TypeDinf:
			track.raw.hasDinf = true
			track.raw.dinf = mr.RawBox()
		case mp4.TypeStbl:
			parseStbl(mr, track, handlerType)
		}
	}
}

func parseStbl(mr *mp4.Reader, track *Track, handlerType [4]byte) {
	mr.Enter()
	defer mr.Exit()

	for mr.Next() {
		switch mr.Type() {
		case mp4.TypeStsd:
			track.raw.stsd = mr.RawBox()
			parseStsd(mr, track, handlerType)
		case mp4.TypeStsz:
			track.raw.stszData = mr.Data()
		case mp4.TypeStts:
			track.raw.sttsData = mr.Data()
		case mp4.TypeStsc:
			track.raw.stscData = mr.Data()
		case mp4.TypeCtts:
			track.raw.cttsData = mr.Data()
			track.raw.cttsVersion = mr.Version()
		case mp4.TypeStss:
			track.raw.stssData = mr.Data()
		case mp4.TypeStco:
			track.raw.stcoData = mr.Data()
		case mp4.TypeCo64:
			track.raw.co64Data = mr.Data()
			track.raw.hasCo64 = true
		}
	}

	if track.raw.stszData != nil {
		stszIt := mp4.NewStszIter(track.raw.stszData)
		track.raw.sampleCount = stszIt.Count()
	}

	// Extract SampleDescIdx from first stsc entry (needed for init segment writing)
	if track.raw.stscData != nil {
		stscIt := mp4.NewStscIter(track.raw.stscData)
		if entry, ok := stscIt.Next(); ok {
			track.SampleDescIdx = entry.SampleDescriptionId
		}
	}
}

func parseStsd(mr *mp4.Reader, track *Track, handlerType [4]byte) {
	data := mr.Data()
	if len(data) < 4 {
		return
	}

	mr.Enter()
	defer mr.Exit()
	mr.Skip(4)

	if !mr.Next() {
		return
	}

	entryType := mr.Type()
	entryData := mr.Data()

	switch handlerType {
	case htVide:
		track.Kind = TrackVideo
		if len(entryData) < 78 {
			track.setCodec(entryType.String())
			return
		}
		v := mp4.ReadVisualSampleEntry(entryData)
		track.Width = v.Width
		track.Height = v.Height
		switch entryType {
		case mp4.TypeAvc1:
			track.setCodec("avc1")
			if d := childBox(mr, v.ChildOffset, mp4.TypeAvcC); len(d) >= 4 {
				track.appendCodec(".")
				track.appendAvcCProfile(d[1], d[2], d[3])
			}
		case mp4.TypeAv01:
			track.setCodec("av01")
			if d := childBox(mr, v.ChildOffset, mp4.TypeAv1C); len(d) >= 3 {
				track.appendAv1CProfile(d)
			}
		default:
			track.setCodec(entryType.String())
		}
	case htSoun:
		track.Kind = TrackAudio
		switch entryType {
		case mp4.TypeMp4a:
			track.setCodec("mp4a")
			if len(entryData) >= 28 {
				a := mp4.ReadAudioSampleEntry(entryData)
				track.ChannelCount = a.ChannelCount
				track.SampleRate = a.SampleRate >> 16
				if d := childBox(mr, a.ChildOffset, mp4.TypeEsds); d != nil {
					track.appendEsdsCodec(d)
				}
			}
		default:
			track.setCodec(entryType.String())
		}
	default:
		track.Kind = TrackUnknown
		track.setCodec(entryType.String())
	}
}

// childBox enters the current sample entry, skips its fixed header of
// childOffset bytes, and returns the data of the first child box of boxType, or
// nil if none is found.
func childBox(mr *mp4.Reader, childOffset int, boxType mp4.BoxType) []byte {
	mr.Enter()
	defer mr.Exit()
	mr.Skip(childOffset)
	for mr.Next() {
		if mr.Type() == boxType {
			return mr.Data()
		}
	}
	return nil
}

// parseSamples parses sample table data and populates track.Samples.
// Returns an error if required sample table data is missing or corrupt.
func (t *Track) parseSamples() error {
	if t.raw.stszData == nil || t.raw.sttsData == nil || t.raw.stscData == nil {
		return fmt.Errorf("track %d: %w: missing required sample table data (stsz/stts/stsc)", t.ID, ErrInvalidTrack)
	}
	if t.raw.stcoData == nil && t.raw.co64Data == nil {
		return fmt.Errorf("track %d: %w: missing chunk offset data (stco/co64)", t.ID, ErrInvalidTrack)
	}

	stszIt := mp4.NewStszIter(t.raw.stszData)
	numSamples := int(stszIt.Count())
	if numSamples == 0 {
		t.Samples = t.Samples[:0]
		return nil
	}

	var samples []Sample
	if cap(t.Samples) >= numSamples {
		samples = t.Samples[:numSamples]
	} else {
		samples = make([]Sample, numSamples)
	}

	stscIt := mp4.NewStscIter(t.raw.stscData)
	sttsIt := mp4.NewSttsIter(t.raw.sttsData)

	var cttsIt mp4.CttsIter
	hasCtts := t.raw.cttsData != nil
	if hasCtts {
		cttsIt = mp4.NewCttsIter(t.raw.cttsData, t.raw.cttsVersion)
	}

	hasSync := t.raw.stssData != nil
	var syncIt mp4.Uint32Iter
	if hasSync {
		syncIt = mp4.NewUint32Iter(t.raw.stssData)
	}

	curStsc, ok := stscIt.Next()
	if !ok {
		return fmt.Errorf("track %d: %w: empty stsc table", t.ID, ErrInvalidTrack)
	}
	var nextStsc mp4.StscEntry
	haveNextStsc := false
	if e, ok := stscIt.Next(); ok {
		nextStsc = e
		haveNextStsc = true
	}

	curStts, ok := sttsIt.Next()
	if !ok {
		return fmt.Errorf("track %d: %w: empty stts table", t.ID, ErrInvalidTrack)
	}
	sttsRemaining := int(curStts.Count)

	var curCtts mp4.CttsEntry
	cttsRemaining := 0
	if hasCtts {
		if e, ok := cttsIt.Next(); ok {
			curCtts = e
			cttsRemaining = int(e.Count)
		}
	}

	var nextSync uint32
	haveSync := false
	if hasSync {
		if v, ok := syncIt.Next(); ok {
			nextSync = v
			haveSync = true
		}
	}

	var chunkOffset int64
	var chunkIdx uint32

	var stcoIt mp4.Uint32Iter
	var co64It mp4.Co64Iter
	if t.raw.hasCo64 {
		co64It = mp4.NewCo64Iter(t.raw.co64Data)
		if v, ok := co64It.Next(); ok {
			chunkOffset = int64(v)
		}
	} else {
		stcoIt = mp4.NewUint32Iter(t.raw.stcoData)
		if v, ok := stcoIt.Next(); ok {
			chunkOffset = int64(v)
		}
	}
	chunkIdx = 1

	sampleInChunk := uint32(0)
	var offsetInChunk int64
	var dts int64

	for i := range numSamples {
		size, ok := stszIt.Next()
		if !ok {
			return fmt.Errorf("track %d: %w: stsz iterator exhausted at sample %d/%d", t.ID, ErrCorruptData, i, numSamples)
		}

		var presOff int32
		if hasCtts && cttsRemaining > 0 {
			presOff = curCtts.Offset
		}

		isSync := true
		if hasSync {
			isSync = haveSync && nextSync == uint32(i+1)
		}

		packedSize := size
		if isSync {
			packedSize |= syncBit
		}
		samples[i] = Sample{
			Offset:             offsetInChunk + chunkOffset,
			DTS:                dts,
			TrackID:            t.ID,
			size:               packedSize,
			Duration:           curStts.Duration,
			PresentationOffset: presOff,
		}

		if i+1 >= numSamples {
			break
		}

		sampleInChunk++
		offsetInChunk += int64(size)
		if sampleInChunk >= curStsc.SamplesPerChunk {
			sampleInChunk = 0
			offsetInChunk = 0
			chunkIdx++
			if t.raw.hasCo64 {
				if v, ok := co64It.Next(); ok {
					chunkOffset = int64(v)
				}
			} else {
				if v, ok := stcoIt.Next(); ok {
					chunkOffset = int64(v)
				}
			}
			if haveNextStsc && chunkIdx >= nextStsc.FirstChunk {
				curStsc = nextStsc
				if e, ok := stscIt.Next(); ok {
					nextStsc = e
				} else {
					haveNextStsc = false
				}
			}
		}

		dts += int64(curStts.Duration)
		sttsRemaining--
		if sttsRemaining <= 0 {
			if e, ok := sttsIt.Next(); ok {
				curStts = e
				sttsRemaining = int(e.Count)
			}
		}

		if hasCtts {
			cttsRemaining--
			if cttsRemaining <= 0 {
				if e, ok := cttsIt.Next(); ok {
					curCtts = e
					cttsRemaining = int(e.Count)
				}
			}
		}

		if isSync && hasSync {
			if v, ok := syncIt.Next(); ok {
				nextSync = v
			} else {
				haveSync = false
			}
		}
	}

	t.Samples = samples
	t.SampleDescIdx = curStsc.SampleDescriptionId
	return nil
}

const hexChars = "0123456789abcdef"
