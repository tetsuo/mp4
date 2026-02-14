package bmff

// maxDepth limits the reader/writer nesting stack.
const maxDepth = 16

// readerFrame stores parent state when entering a container box.
type readerFrame struct {
	end    int // parent's iteration end boundary
	boxEnd int // position to resume after exiting this container
}

// Reader provides streaming parsing of ISOBMFF boxes.
type Reader struct {
	buf []byte
	pos int // next position to parse from
	end int // iteration end boundary

	// Current box state
	boxType   BoxType
	boxSize   uint64
	boxStart  int
	boxEnd    int
	dataStart int

	// Full box fields
	version uint8
	flags   uint32

	// Nesting stack
	stack [maxDepth]readerFrame
	depth int
}

// NewReader creates a Reader for the given buffer.
func NewReader(buf []byte) Reader {
	return Reader{
		buf: buf,
		end: len(buf),
	}
}

// Next advances to the next sibling box. Returns false if no more boxes.
func (r *Reader) Next() bool {
	// Skip past current box
	if r.boxEnd > r.pos {
		r.pos = r.boxEnd
	}

	if r.end-r.pos < 8 {
		return false
	}

	r.boxStart = r.pos
	size := uint64(be.Uint32(r.buf[r.pos:]))
	copy(r.boxType[:], r.buf[r.pos+4:r.pos+8])
	ptr := r.pos + 8

	// Extended size
	if size == 1 {
		if r.end-r.pos < 16 {
			return false
		}
		size = be.Uint64(r.buf[ptr:])
		ptr += 8
	}

	// Size 0 means box extends to end of data
	if size == 0 {
		size = uint64(r.end - r.pos)
	}

	r.boxSize = size
	r.boxEnd = r.boxStart + int(size)

	if r.boxEnd > r.end {
		return false
	}

	// Parse full box header if applicable
	if IsFullBox(r.boxType) {
		if r.boxEnd-ptr < 4 {
			return false
		}
		vf := be.Uint32(r.buf[ptr:])
		r.version = uint8(vf >> 24)
		r.flags = vf & 0x00ffffff
		ptr += 4
	} else {
		r.version = 0
		r.flags = 0
	}

	r.dataStart = ptr
	return true
}

// Type returns the current box's type.
func (r *Reader) Type() BoxType { return r.boxType }

// Size returns the current box's total size including header.
func (r *Reader) Size() uint64 { return r.boxSize }

// Version returns the version field for full boxes.
func (r *Reader) Version() uint8 { return r.version }

// Flags returns the flags field for full boxes.
func (r *Reader) Flags() uint32 { return r.flags }

// Offset returns the byte offset of the current box's start in the buffer.
func (r *Reader) Offset() int { return r.boxStart }

// DataOffset returns the byte offset where the current box's data begins.
func (r *Reader) DataOffset() int { return r.dataStart }

// HeaderSize returns the size of the current box's header in bytes.
func (r *Reader) HeaderSize() int { return r.dataStart - r.boxStart }

// Data returns the current box's data (after all headers).
// Note that, the returned slice points into the original buffer.
func (r *Reader) Data() []byte {
	return r.buf[r.dataStart:r.boxEnd]
}

// RawBox returns the entire current box including headers.
// Note that, the returned slice points into the original buffer.
func (r *Reader) RawBox() []byte {
	return r.buf[r.boxStart:r.boxEnd]
}

// Depth returns the current nesting depth (0 at top level).
func (r *Reader) Depth() int { return r.depth }

// Enter descends into the current container box to iterate its children.
// After Enter, call Next to advance to the first child box.
// Call Exit when done to return to the parent level.
//
// For boxes like stsd or dref that have an entry count before child boxes,
// call Skip(4) after Enter to skip past the count field.
//
// For sample entry boxes like avc1 (78 bytes) or mp4a (28 bytes),
// call Skip with the fixed header size after Enter to reach child boxes.
func (r *Reader) Enter() {
	r.stack[r.depth] = readerFrame{
		end:    r.end,
		boxEnd: r.boxEnd,
	}
	r.depth++
	r.end = r.boxEnd
	r.pos = r.dataStart
	r.boxEnd = r.dataStart // prevent Next from skipping
}

// Exit returns to the parent container level.
// After Exit, the next call to Next will advance to the next sibling.
func (r *Reader) Exit() {
	r.depth--
	f := r.stack[r.depth]
	r.end = f.end
	r.pos = f.boxEnd
	r.boxEnd = f.boxEnd
}

// Skip advances the data position by n bytes within the current container.
// Use after Enter to skip fixed-size headers before child boxes.
func (r *Reader) Skip(n int) {
	r.pos += n
	r.boxEnd = r.pos
}

// EntryCount reads the uint32 entry count at the start of box data.
// Used for boxes like stsd and dref that begin with a count field.
func (r *Reader) EntryCount() uint32 {
	data := r.Data()
	return be.Uint32(data[0:4])
}

// ReadMvhd extracts key fields from an mvhd box.
// Returns timescale, duration, and nextTrackId.
func (r *Reader) ReadMvhd() (timescale uint32, duration uint64, nextTrackId uint32) {
	data := r.Data()
	version := r.Version()
	if version == 1 {
		// v1: ctime(8)+mtime(8)+timescale(4)+duration(8)+rate(4)+volume(2)+reserved(10)+matrix(36)+predefined(24)+nextTrackId(4) = 108
		timescale = be.Uint32(data[16:20])
		duration = be.Uint64(data[20:28])
		nextTrackId = be.Uint32(data[104:108])
	} else {
		// v0: ctime(4)+mtime(4)+timescale(4)+duration(4)+rate(4)+volume(2)+reserved(10)+matrix(36)+predefined(24)+nextTrackId(4) = 96
		timescale = be.Uint32(data[8:12])
		duration = uint64(be.Uint32(data[12:16]))
		nextTrackId = be.Uint32(data[92:96])
	}
	return
}

// ReadTkhd extracts key fields from a tkhd box.
// Returns trackId, duration, width, height.
// Width and height are 16.16 fixed-point values; shift right by 16 for pixels.
func (r *Reader) ReadTkhd() (trackId uint32, duration uint64, width, height uint32) {
	data := r.Data()
	version := r.Version()
	if version == 1 {
		// v1: ctime(8)+mtime(8)+trackId(4)+reserved(4)+duration(8)
		trackId = be.Uint32(data[16:20])
		duration = be.Uint64(data[24:32])
		// +reserved(8)+layer(2)+altGroup(2)+volume(2)+reserved(2)+matrix(36)+width(4)+height(4)
		width = be.Uint32(data[84:88])
		height = be.Uint32(data[88:92])
	} else {
		// v0: ctime(4)+mtime(4)+trackId(4)+reserved(4)+duration(4)
		trackId = be.Uint32(data[8:12])
		duration = uint64(be.Uint32(data[16:20]))
		// +reserved(8)+layer(2)+altGroup(2)+volume(2)+reserved(2)+matrix(36)+width(4)+height(4)
		width = be.Uint32(data[72:76])
		height = be.Uint32(data[76:80])
	}
	return
}

// ReadMdhd extracts key fields from an mdhd box.
// Returns timescale, duration, and language code.
func (r *Reader) ReadMdhd() (timescale uint32, duration uint64, language uint16) {
	data := r.Data()
	version := r.Version()
	if version == 1 {
		// v1: ctime(8)+mtime(8)+timescale(4)+duration(8)+lang(2)+quality(2)
		timescale = be.Uint32(data[16:20])
		duration = be.Uint64(data[20:28])
		language = be.Uint16(data[28:30])
	} else {
		// v0: ctime(4)+mtime(4)+timescale(4)+duration(4)+lang(2)+quality(2)
		timescale = be.Uint32(data[8:12])
		duration = uint64(be.Uint32(data[12:16]))
		language = be.Uint16(data[16:18])
	}
	return
}

// ReadHdlr extracts the handler type from an hdlr box.
// Returns the 4-byte handler type string.
func (r *Reader) ReadHdlr() [4]byte {
	data := r.Data()
	var t [4]byte
	copy(t[:], data[4:8])
	return t
}

// ReadHdlrName extracts the handler name from an hdlr box.
func (r *Reader) ReadHdlrName() string {
	data := r.Data()
	if len(data) <= 20 {
		return ""
	}
	end := 20
	for end < len(data) && data[end] != 0 {
		end++
	}
	return string(data[20:end])
}

// ReadMehd extracts the fragment duration from an mehd box.
func (r *Reader) ReadMehd() (fragmentDuration uint64) {
	data := r.Data()
	version := r.Version()
	if version == 1 {
		fragmentDuration = be.Uint64(data[0:8])
	} else {
		fragmentDuration = uint64(be.Uint32(data[0:4]))
	}
	return
}

// ReadTrex extracts fields from a trex box.
// Returns trackId, default sample description index, default sample duration,
// default sample size, and default sample flags.
func (r *Reader) ReadTrex() (trackId, defSampleDescIdx, defSampleDuration, defSampleSize, defSampleFlags uint32) {
	data := r.Data()
	trackId = be.Uint32(data[0:4])
	defSampleDescIdx = be.Uint32(data[4:8])
	defSampleDuration = be.Uint32(data[8:12])
	defSampleSize = be.Uint32(data[12:16])
	defSampleFlags = be.Uint32(data[16:20])
	return
}

// ReadMfhd extracts the sequence number from an mfhd box.
func (r *Reader) ReadMfhd() (sequenceNumber uint32) {
	data := r.Data()
	sequenceNumber = be.Uint32(data[0:4])
	return
}

// ReadTfhd extracts the track ID from a tfhd box.
func (r *Reader) ReadTfhd() (trackId uint32) {
	data := r.Data()
	trackId = be.Uint32(data[0:4])
	return
}

// ReadTfdt extracts the base media decode time from a tfdt box.
func (r *Reader) ReadTfdt() (baseMediaDecodeTime uint64) {
	data := r.Data()
	version := r.Version()
	if version == 1 {
		baseMediaDecodeTime = be.Uint64(data[0:8])
	} else {
		baseMediaDecodeTime = uint64(be.Uint32(data[0:4]))
	}
	return
}
