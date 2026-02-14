package bmff

import (
	"encoding/binary"
	"math"
)

var be = binary.BigEndian

const uint32Max = math.MaxUint32

// StszIter iterates over sample sizes in an stsz box.
type StszIter struct {
	buf        []byte
	sampleSize uint32
	count      uint32
	index      uint32
}

// NewStszIter creates an iterator from stsz box data.
func NewStszIter(data []byte) StszIter {
	if len(data) < 8 {
		return StszIter{}
	}
	return StszIter{
		buf:        data,
		sampleSize: be.Uint32(data[0:4]),
		count:      be.Uint32(data[4:8]),
	}
}

// Count returns the total number of samples.
func (it *StszIter) Count() uint32 { return it.count }

// Next returns the next sample size. Returns (0, false) when done.
func (it *StszIter) Next() (uint32, bool) {
	if it.index >= it.count {
		return 0, false
	}
	var size uint32
	if it.sampleSize != 0 {
		size = it.sampleSize
	} else {
		offset := 8 + int(it.index)*4
		if offset+4 > len(it.buf) {
			return 0, false
		}
		size = be.Uint32(it.buf[offset:])
	}
	it.index++
	return size, true
}

// Co64Iter iterates over uint64 chunk offsets in a co64 box.
type Co64Iter struct {
	buf   []byte
	count uint32
	index uint32
}

// NewCo64Iter creates an iterator from co64 box data.
func NewCo64Iter(data []byte) Co64Iter {
	if len(data) < 4 {
		return Co64Iter{}
	}
	return Co64Iter{
		buf:   data,
		count: be.Uint32(data[0:4]),
	}
}

// Count returns the total number of entries.
func (it *Co64Iter) Count() uint32 { return it.count }

// Next returns the next chunk offset. Returns (0, false) when done.
func (it *Co64Iter) Next() (uint64, bool) {
	if it.index >= it.count {
		return 0, false
	}
	offset := 4 + int(it.index)*8
	if offset+8 > len(it.buf) {
		return 0, false
	}
	v := be.Uint64(it.buf[offset:])
	it.index++
	return v, true
}

// SttsEntry is a time-to-sample entry.
type SttsEntry struct {
	Count    uint32
	Duration uint32
}

// SttsIter iterates over stts entries.
type SttsIter struct {
	buf   []byte
	count uint32
	index uint32
}

// NewSttsIter creates an iterator from stts box data.
func NewSttsIter(data []byte) SttsIter {
	if len(data) < 4 {
		return SttsIter{}
	}
	return SttsIter{
		buf:   data,
		count: be.Uint32(data[0:4]),
	}
}

// Count returns the total number of entries.
func (it *SttsIter) Count() uint32 { return it.count }

// Next returns the next entry. Returns false when done.
func (it *SttsIter) Next() (SttsEntry, bool) {
	if it.index >= it.count {
		return SttsEntry{}, false
	}
	offset := 4 + int(it.index)*8
	if offset+8 > len(it.buf) {
		return SttsEntry{}, false
	}
	e := SttsEntry{
		Count:    be.Uint32(it.buf[offset:]),
		Duration: be.Uint32(it.buf[offset+4:]),
	}
	it.index++
	return e, true
}

// CttsEntry is a composition offset entry.
type CttsEntry struct {
	Count  uint32
	Offset int32 // Signed offset (version 1), or unsigned treated as signed (version 0)
}

// CttsIter iterates over ctts entries.
type CttsIter struct {
	buf     []byte
	count   uint32
	index   uint32
	version uint8
}

// NewCttsIter creates an iterator from ctts box data.
// version should be 0 or 1 from the ctts box version field.
// Version 0: offsets are uint32 (but interpreted as composition time offset)
// Version 1: offsets are int32 (signed composition time offset)
func NewCttsIter(data []byte, version uint8) CttsIter {
	if len(data) < 4 {
		return CttsIter{}
	}
	return CttsIter{
		buf:     data,
		count:   be.Uint32(data[0:4]),
		version: version,
	}
}

// Count returns the total number of entries.
func (it *CttsIter) Count() uint32 { return it.count }

// Next returns the next entry. Returns false when done.
func (it *CttsIter) Next() (CttsEntry, bool) {
	if it.index >= it.count {
		return CttsEntry{}, false
	}
	offset := 4 + int(it.index)*8
	if offset+8 > len(it.buf) {
		return CttsEntry{}, false
	}
	e := CttsEntry{
		Count: be.Uint32(it.buf[offset:]),
	}
	// Version 0: uint32 offset (but typically small positive values)
	// Version 1: int32 offset (can be negative)
	if it.version == 0 {
		// In version 0, the value is uint32 but should be interpreted as offset
		e.Offset = int32(be.Uint32(it.buf[offset+4:]))
	} else {
		// In version 1, the value is explicitly signed
		e.Offset = int32(be.Uint32(it.buf[offset+4:]))
	}
	it.index++
	return e, true
}

// StscEntry is a sample-to-chunk entry.
type StscEntry struct {
	FirstChunk          uint32
	SamplesPerChunk     uint32
	SampleDescriptionId uint32
}

// StscIter iterates over stsc entries.
type StscIter struct {
	buf   []byte
	count uint32
	index uint32
}

// NewStscIter creates an iterator from stsc box data.
func NewStscIter(data []byte) StscIter {
	if len(data) < 4 {
		return StscIter{}
	}
	return StscIter{
		buf:   data,
		count: be.Uint32(data[0:4]),
	}
}

// Count returns the total number of entries.
func (it *StscIter) Count() uint32 { return it.count }

// Next returns the next entry. Returns false when done.
func (it *StscIter) Next() (StscEntry, bool) {
	if it.index >= it.count {
		return StscEntry{}, false
	}
	offset := 4 + int(it.index)*12
	if offset+12 > len(it.buf) {
		return StscEntry{}, false
	}
	e := StscEntry{
		FirstChunk:          be.Uint32(it.buf[offset:]),
		SamplesPerChunk:     be.Uint32(it.buf[offset+4:]),
		SampleDescriptionId: be.Uint32(it.buf[offset+8:]),
	}
	it.index++
	return e, true
}

// ElstEntry is an edit list entry.
type ElstEntry struct {
	SegmentDuration uint64
	MediaTime       int64
	MediaRateInt    int16
	MediaRateFrac   int16
}

// ElstIter iterates over elst entries.
type ElstIter struct {
	buf     []byte
	count   uint32
	index   uint32
	version uint8
}

// NewElstIter creates an iterator from elst box data with the given version.
func NewElstIter(data []byte, version uint8) ElstIter {
	if len(data) < 4 {
		return ElstIter{}
	}
	return ElstIter{
		buf:     data,
		count:   be.Uint32(data[0:4]),
		version: version,
	}
}

// Count returns the total number of entries.
func (it *ElstIter) Count() uint32 { return it.count }

// Next returns the next entry. Returns false when done.
func (it *ElstIter) Next() (ElstEntry, bool) {
	if it.index >= it.count {
		return ElstEntry{}, false
	}
	var e ElstEntry
	if it.version == 1 {
		stride := 20
		offset := 4 + int(it.index)*stride
		if offset+stride > len(it.buf) {
			return ElstEntry{}, false
		}
		e.SegmentDuration = be.Uint64(it.buf[offset:])
		e.MediaTime = int64(be.Uint64(it.buf[offset+8:]))
		e.MediaRateInt = int16(be.Uint16(it.buf[offset+16:]))
		e.MediaRateFrac = int16(be.Uint16(it.buf[offset+18:]))
	} else {
		stride := 12
		offset := 4 + int(it.index)*stride
		if offset+stride > len(it.buf) {
			return ElstEntry{}, false
		}
		e.SegmentDuration = uint64(be.Uint32(it.buf[offset:]))
		e.MediaTime = int64(int32(be.Uint32(it.buf[offset+4:])))
		e.MediaRateInt = int16(be.Uint16(it.buf[offset+8:]))
		e.MediaRateFrac = int16(be.Uint16(it.buf[offset+10:]))
	}
	it.index++
	return e, true
}

// TrunEntry is a track run sample entry.
type TrunEntry struct {
	Duration              uint32
	Size                  uint32
	Flags                 uint32
	CompositionTimeOffset int32
}

// Trun flags.
const (
	TrunDataOffsetPresent                  = 0x000001
	TrunFirstSampleFlagsPresent            = 0x000004
	TrunSampleDurationPresent              = 0x000100
	TrunSampleSizePresent                  = 0x000200
	TrunSampleFlagsPresent                 = 0x000400
	TrunSampleCompositionTimeOffsetPresent = 0x000800
)

// Tfhd flags (Track Fragment Header Box).
const (
	TfhdBaseDataOffsetPresent         = 0x000001
	TfhdSampleDescriptionIndexPresent = 0x000002
	TfhdDefaultSampleDurationPresent  = 0x000008
	TfhdDefaultSampleSizePresent      = 0x000010
	TfhdDefaultSampleFlagsPresent     = 0x000020
	TfhdDurationIsEmpty               = 0x010000
	TfhdDefaultBaseIsMoof             = 0x020000
)

// TrunIter iterates over trun entries.
type TrunIter struct {
	buf              []byte
	flags            uint32
	count            uint32
	index            uint32
	dataOffset       int32
	firstSampleFlags uint32
	stride           int
	entriesStart     int
}

// NewTrunIter creates an iterator from trun box data with the given flags.
func NewTrunIter(data []byte, flags uint32) TrunIter {
	if len(data) < 4 {
		return TrunIter{}
	}
	it := TrunIter{
		buf:   data,
		flags: flags,
		count: be.Uint32(data[0:4]),
	}
	ptr := 4
	if flags&TrunDataOffsetPresent != 0 {
		if ptr+4 > len(data) {
			return TrunIter{}
		}
		it.dataOffset = int32(be.Uint32(data[ptr:]))
		ptr += 4
	}
	if flags&TrunFirstSampleFlagsPresent != 0 {
		if ptr+4 > len(data) {
			return TrunIter{}
		}
		it.firstSampleFlags = be.Uint32(data[ptr:])
		ptr += 4
	}
	it.entriesStart = ptr

	if flags&TrunSampleDurationPresent != 0 {
		it.stride += 4
	}
	if flags&TrunSampleSizePresent != 0 {
		it.stride += 4
	}
	if flags&TrunSampleFlagsPresent != 0 {
		it.stride += 4
	}
	if flags&TrunSampleCompositionTimeOffsetPresent != 0 {
		it.stride += 4
	}
	return it
}

// Count returns the total number of samples.
func (it *TrunIter) Count() uint32 { return it.count }

// DataOffset returns the trun data offset.
func (it *TrunIter) DataOffset() int32 { return it.dataOffset }

// FirstSampleFlags returns the first sample flags, if present.
func (it *TrunIter) FirstSampleFlags() uint32 { return it.firstSampleFlags }

// Next returns the next sample entry. Returns false when done.
func (it *TrunIter) Next() (TrunEntry, bool) {
	if it.index >= it.count {
		return TrunEntry{}, false
	}
	offset := it.entriesStart + int(it.index)*it.stride
	if offset+it.stride > len(it.buf) {
		return TrunEntry{}, false
	}
	var e TrunEntry
	p := offset
	if it.flags&TrunSampleDurationPresent != 0 {
		e.Duration = be.Uint32(it.buf[p:])
		p += 4
	}
	if it.flags&TrunSampleSizePresent != 0 {
		e.Size = be.Uint32(it.buf[p:])
		p += 4
	}
	if it.flags&TrunSampleFlagsPresent != 0 {
		e.Flags = be.Uint32(it.buf[p:])
		p += 4
	}
	if it.flags&TrunSampleCompositionTimeOffsetPresent != 0 {
		e.CompositionTimeOffset = int32(be.Uint32(it.buf[p:]))
	}
	it.index++
	return e, true
}

// Uint32Iter iterates over uint32 entries (stco, stss).
type Uint32Iter struct {
	buf   []byte
	count uint32
	index uint32
}

// NewUint32Iter creates an iterator from box data containing a count + uint32 entries.
func NewUint32Iter(data []byte) Uint32Iter {
	if len(data) < 4 {
		return Uint32Iter{}
	}
	return Uint32Iter{
		buf:   data,
		count: be.Uint32(data[0:4]),
	}
}

// Count returns the total number of entries.
func (it *Uint32Iter) Count() uint32 { return it.count }

// Next returns the next entry. Returns (0, false) when done.
func (it *Uint32Iter) Next() (uint32, bool) {
	if it.index >= it.count {
		return 0, false
	}
	offset := 4 + int(it.index)*4
	if offset+4 > len(it.buf) {
		return 0, false
	}
	v := be.Uint32(it.buf[offset:])
	it.index++
	return v, true
}

// FtypInfo holds parsed fields from an ftyp box.
type FtypInfo struct {
	MajorBrand   [4]byte
	MinorVersion uint32
	Compatible   [][4]byte
}

// ReadFtyp parses an ftyp box.
func ReadFtyp(data []byte) FtypInfo {
	f := FtypInfo{
		MinorVersion: be.Uint32(data[4:8]),
	}
	copy(f.MajorBrand[:], data[0:4])
	for i := 8; i+4 <= len(data); i += 4 {
		var b [4]byte
		copy(b[:], data[i:i+4])
		f.Compatible = append(f.Compatible, b)
	}
	return f
}

// VisualSampleEntry holds parsed fields from a visual sample entry (e.g. avc1).
type VisualSampleEntry struct {
	DataReferenceIndex uint16
	Width              uint16
	Height             uint16
	HResolution        uint32 // 16.16 fixed point
	VResolution        uint32 // 16.16 fixed point
	FrameCount         uint16
	CompressorName     string
	Depth              uint16
	ChildOffset        int // byte offset within data where child boxes begin
}

// ReadVisualSampleEntry parses a visual sample entry from box data.
// Child boxes (e.g. avcC) start at ChildOffset within the data.
func ReadVisualSampleEntry(data []byte) VisualSampleEntry {
	nameLen := min(int(data[42]), 31)
	return VisualSampleEntry{
		DataReferenceIndex: be.Uint16(data[6:8]),
		Width:              be.Uint16(data[24:26]),
		Height:             be.Uint16(data[26:28]),
		HResolution:        be.Uint32(data[28:32]),
		VResolution:        be.Uint32(data[32:36]),
		FrameCount:         be.Uint16(data[40:42]),
		CompressorName:     string(data[43 : 43+nameLen]),
		Depth:              be.Uint16(data[74:76]),
		ChildOffset:        78,
	}
}

// AudioSampleEntry holds parsed fields from an audio sample entry (e.g. mp4a).
type AudioSampleEntry struct {
	DataReferenceIndex uint16
	ChannelCount       uint16
	SampleSize         uint16
	SampleRate         uint32 // 16.16 fixed point
	ChildOffset        int    // byte offset within data where child boxes begin
}

// ReadAudioSampleEntry parses an audio sample entry from box data.
// Child boxes (e.g. esds) start at ChildOffset within the data.
func ReadAudioSampleEntry(data []byte) AudioSampleEntry {
	return AudioSampleEntry{
		DataReferenceIndex: be.Uint16(data[6:8]),
		ChannelCount:       be.Uint16(data[16:18]),
		SampleSize:         be.Uint16(data[18:20]),
		SampleRate:         be.Uint32(data[24:28]),
		ChildOffset:        28,
	}
}

// ReadAvcC extracts the codec profile string from avcC box data.
// Returns a string like "64001f" for use in MIME type codec parameters.
func ReadAvcC(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	var buf [6]byte
	buf[0] = hexDigit(data[1] >> 4)
	buf[1] = hexDigit(data[1] & 0x0f)
	buf[2] = hexDigit(data[2] >> 4)
	buf[3] = hexDigit(data[2] & 0x0f)
	buf[4] = hexDigit(data[3] >> 4)
	buf[5] = hexDigit(data[3] & 0x0f)
	return string(buf[:])
}

const hexChars = "0123456789abcdef"

// hexDigit returns the lowercase hex character for a 4-bit nibble.
func hexDigit(b byte) byte {
	return hexChars[b&0x0f]
}
