package bmff

// writerFrame tracks the start offset of a box for size backpatching.
type writerFrame struct {
	offset int
}

// Writer encodes ISOBMFF boxes into a byte buffer.
type Writer struct {
	buf   []byte
	pos   int
	stack [maxDepth]writerFrame
	depth int
}

// NewWriter creates a Writer that writes into buf.
func NewWriter(buf []byte) Writer {
	return Writer{buf: buf[:cap(buf)]}
}

// Bytes returns the written data.
func (w *Writer) Bytes() []byte {
	return w.buf[:w.pos]
}

// Len returns the number of bytes written.
func (w *Writer) Len() int { return w.pos }

// Write appends raw bytes. Implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	copy(w.buf[w.pos:], p)
	w.pos += len(p)
	return len(p), nil
}

// putUint8 appends a single byte.
func (w *Writer) putUint8(v byte) {
	w.buf[w.pos] = v
	w.pos++
}

// putUint16 appends a big-endian uint16.
func (w *Writer) putUint16(v uint16) {
	be.PutUint16(w.buf[w.pos:], v)
	w.pos += 2
}

// putUint32 appends a big-endian uint32.
func (w *Writer) putUint32(v uint32) {
	be.PutUint32(w.buf[w.pos:], v)
	w.pos += 4
}

// putUint64 appends a big-endian uint64.
func (w *Writer) putUint64(v uint64) {
	be.PutUint64(w.buf[w.pos:], v)
	w.pos += 8
}

// putInt32 appends a big-endian int32.
func (w *Writer) putInt32(v int32) {
	w.putUint32(uint32(v))
}

// putZeros appends n zero bytes.
func (w *Writer) putZeros(n int) {
	clear(w.buf[w.pos : w.pos+n])
	w.pos += n
}

// putBytes appends raw bytes.
func (w *Writer) putBytes(p []byte) {
	copy(w.buf[w.pos:], p)
	w.pos += len(p)
}

// putFixedString writes a fixed-length string field with null padding.
func (w *Writer) putFixedString(s string, length int) {
	n := copy(w.buf[w.pos:w.pos+length], s)
	clear(w.buf[w.pos+n : w.pos+length])
	w.pos += length
}

// Reset resets the writer position to 0.
func (w *Writer) Reset() {
	w.pos = 0
	w.depth = 0
}

// StartBox begins a new box. Write content, then call EndBox.
func (w *Writer) StartBox(t BoxType) {
	w.stack[w.depth] = writerFrame{offset: w.pos}
	w.depth++
	w.putUint32(0) // placeholder size
	w.putBytes(t[:])
}

// StartFullBox begins a new full box with version and flags.
func (w *Writer) StartFullBox(t BoxType, version uint8, flags uint32) {
	w.StartBox(t)
	vf := (uint32(version) << 24) | (flags & 0x00ffffff)
	w.putUint32(vf)
}

// EndBox finishes the current box by backpatching its size.
func (w *Writer) EndBox() {
	w.depth--
	f := w.stack[w.depth]
	size := uint32(w.pos - f.offset)
	be.PutUint32(w.buf[f.offset:], size)
}

// WriteFtyp writes a complete ftyp box.
func (w *Writer) WriteFtyp(brand [4]byte, brandVersion uint32, compat [][4]byte) {
	w.StartBox(TypeFtyp)
	w.putBytes(brand[:])
	w.putUint32(brandVersion)
	for _, c := range compat {
		w.putBytes(c[:])
	}
	w.EndBox()
}

// WriteMvhd writes a complete mvhd box.
func (w *Writer) WriteMvhd(timescale uint32, duration uint64, nextTrackId uint32) {
	if duration > uint32Max {
		w.StartFullBox(TypeMvhd, 1, 0)
		w.putUint64(0) // creation time
		w.putUint64(0) // modification time
		w.putUint32(timescale)
		w.putUint64(duration)
	} else {
		w.StartFullBox(TypeMvhd, 0, 0)
		w.putUint32(0) // creation time
		w.putUint32(0) // modification time
		w.putUint32(timescale)
		w.putUint32(uint32(duration))
	}
	w.putUint32(0x00010000) // rate 1.0
	w.putUint16(0x0100)     // volume 1.0
	w.putZeros(10)          // reserved
	// Identity matrix
	w.putUint32(0x00010000)
	w.putZeros(4)
	w.putZeros(4)
	w.putZeros(4)
	w.putUint32(0x00010000)
	w.putZeros(4)
	w.putZeros(4)
	w.putZeros(4)
	w.putUint32(0x40000000)
	w.putZeros(24) // predefined
	w.putUint32(nextTrackId)
	w.EndBox()
}

// WriteTkhd writes a complete tkhd box.
func (w *Writer) WriteTkhd(flags uint32, trackId uint32, duration uint64, width, height uint32) {
	if duration > uint32Max {
		w.StartFullBox(TypeTkhd, 1, flags)
		w.putUint64(0) // creation time
		w.putUint64(0) // modification time
		w.putUint32(trackId)
		w.putUint32(0) // reserved
		w.putUint64(duration)
	} else {
		w.StartFullBox(TypeTkhd, 0, flags)
		w.putUint32(0) // creation time
		w.putUint32(0) // modification time
		w.putUint32(trackId)
		w.putUint32(0) // reserved
		w.putUint32(uint32(duration))
	}
	w.putZeros(8)  // reserved
	w.putUint16(0) // layer
	w.putUint16(0) // alternate group
	w.putUint16(0) // volume
	w.putUint16(0) // reserved
	// Identity matrix
	w.putUint32(0x00010000)
	w.putZeros(4)
	w.putZeros(4)
	w.putZeros(4)
	w.putUint32(0x00010000)
	w.putZeros(4)
	w.putZeros(4)
	w.putZeros(4)
	w.putUint32(0x40000000)
	w.putUint32(width)
	w.putUint32(height)
	w.EndBox()
}

// WriteMdhd writes a complete mdhd box.
func (w *Writer) WriteMdhd(timescale uint32, duration uint64, language uint16) {
	if duration > uint32Max {
		w.StartFullBox(TypeMdhd, 1, 0)
		w.putUint64(0) // creation time
		w.putUint64(0) // modification time
		w.putUint32(timescale)
		w.putUint64(duration)
	} else {
		w.StartFullBox(TypeMdhd, 0, 0)
		w.putUint32(0) // creation time
		w.putUint32(0) // modification time
		w.putUint32(timescale)
		w.putUint32(uint32(duration))
	}
	w.putUint16(language)
	w.putUint16(0) // quality
	w.EndBox()
}

// WriteHdlr writes a complete hdlr box.
func (w *Writer) WriteHdlr(handlerType [4]byte, name string) {
	w.StartFullBox(TypeHdlr, 0, 0)
	w.putUint32(0) // predefined
	w.putBytes(handlerType[:])
	w.putZeros(12) // reserved
	w.putBytes([]byte(name))
	w.putUint8(0) // null terminator
	w.EndBox()
}

// WriteVmhd writes a complete vmhd box.
func (w *Writer) WriteVmhd() {
	w.StartFullBox(TypeVmhd, 0, 1)
	w.putUint16(0) // graphicsmode
	w.putZeros(6)  // opcolor
	w.EndBox()
}

// WriteSmhd writes a complete smhd box.
func (w *Writer) WriteSmhd() {
	w.StartFullBox(TypeSmhd, 0, 0)
	w.putUint16(0) // balance
	w.putUint16(0) // reserved
	w.EndBox()
}

// WriteDref writes a dref box with a single self-referencing url entry.
func (w *Writer) WriteDref() {
	w.StartFullBox(TypeDref, 0, 0)
	w.putUint32(1) // entry count
	// url entry: self-contained
	w.StartFullBox(BoxType{'u', 'r', 'l', ' '}, 0, 1)
	w.EndBox()
	w.EndBox()
}

// WriteStsz writes a complete stsz box from an iterator or raw entries.
func (w *Writer) WriteStsz(sampleSize uint32, entries []uint32) {
	w.StartFullBox(TypeStsz, 0, 0)
	w.putUint32(sampleSize)
	w.putUint32(uint32(len(entries)))
	if sampleSize == 0 {
		for _, e := range entries {
			w.putUint32(e)
		}
	}
	w.EndBox()
}

// WriteStco writes a complete stco box.
func (w *Writer) WriteStco(entries []uint32) {
	w.StartFullBox(TypeStco, 0, 0)
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		w.putUint32(e)
	}
	w.EndBox()
}

// WriteCo64 writes a complete co64 box.
func (w *Writer) WriteCo64(entries []uint64) {
	w.StartFullBox(TypeCo64, 0, 0)
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		w.putUint64(e)
	}
	w.EndBox()
}

// WriteStss writes a complete stss box.
func (w *Writer) WriteStss(entries []uint32) {
	w.StartFullBox(TypeStss, 0, 0)
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		w.putUint32(e)
	}
	w.EndBox()
}

// WriteStts writes a complete stts box.
func (w *Writer) WriteStts(entries []SttsEntry) {
	w.StartFullBox(TypeStts, 0, 0)
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		w.putUint32(e.Count)
		w.putUint32(e.Duration)
	}
	w.EndBox()
}

// WriteCtts writes a complete ctts box.
func (w *Writer) WriteCtts(entries []CttsEntry) {
	w.StartFullBox(TypeCtts, 0, 0)
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		w.putUint32(e.Count)
		w.putUint32(uint32(e.Offset))
	}
	w.EndBox()
}

// WriteStsc writes a complete stsc box.
func (w *Writer) WriteStsc(entries []StscEntry) {
	w.StartFullBox(TypeStsc, 0, 0)
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		w.putUint32(e.FirstChunk)
		w.putUint32(e.SamplesPerChunk)
		w.putUint32(e.SampleDescriptionId)
	}
	w.EndBox()
}

// WriteElst writes a complete elst box.
func (w *Writer) WriteElst(entries []ElstEntry) {
	// Determine if v1 is needed
	v1 := false
	for _, e := range entries {
		if e.SegmentDuration > uint32Max || e.MediaTime > int64(int32(e.MediaTime)) {
			v1 = true
			break
		}
	}
	if v1 {
		w.StartFullBox(TypeElst, 1, 0)
	} else {
		w.StartFullBox(TypeElst, 0, 0)
	}
	w.putUint32(uint32(len(entries)))
	for _, e := range entries {
		if v1 {
			w.putUint64(e.SegmentDuration)
			w.putUint64(uint64(e.MediaTime))
		} else {
			w.putUint32(uint32(e.SegmentDuration))
			w.putUint32(uint32(e.MediaTime))
		}
		w.putUint16(uint16(e.MediaRateInt))
		w.putUint16(uint16(e.MediaRateFrac))
	}
	w.EndBox()
}

// WriteMehd writes a complete mehd box.
func (w *Writer) WriteMehd(fragmentDuration uint64) {
	if fragmentDuration > uint32Max {
		w.StartFullBox(TypeMehd, 1, 0)
		w.putUint64(fragmentDuration)
	} else {
		w.StartFullBox(TypeMehd, 0, 0)
		w.putUint32(uint32(fragmentDuration))
	}
	w.EndBox()
}

// WriteTrex writes a complete trex box.
func (w *Writer) WriteTrex(trackId, descIdx, defDuration, defSize, defFlags uint32) {
	w.StartFullBox(TypeTrex, 0, 0)
	w.putUint32(trackId)
	w.putUint32(descIdx)
	w.putUint32(defDuration)
	w.putUint32(defSize)
	w.putUint32(defFlags)
	w.EndBox()
}

// WriteMfhd writes a complete mfhd box.
func (w *Writer) WriteMfhd(sequenceNumber uint32) {
	w.StartFullBox(TypeMfhd, 0, 0)
	w.putUint32(sequenceNumber)
	w.EndBox()
}

// WriteTfhd writes a complete tfhd box.
func (w *Writer) WriteTfhd(flags uint32, trackId uint32) {
	w.StartFullBox(TypeTfhd, 0, flags)
	w.putUint32(trackId)
	w.EndBox()
}

// WriteTfdt writes a complete tfdt box.
func (w *Writer) WriteTfdt(baseMediaDecodeTime uint64) {
	if baseMediaDecodeTime > uint32Max {
		w.StartFullBox(TypeTfdt, 1, 0)
		w.putUint64(baseMediaDecodeTime)
	} else {
		w.StartFullBox(TypeTfdt, 0, 0)
		w.putUint32(uint32(baseMediaDecodeTime))
	}
	// TODO: maybe always use version 1 (64-bit)?
	// w.StartFullBox(TypeTfdt, 1, 0)
	// w.putUint64(baseMediaDecodeTime)
	w.EndBox()
}

// WriteTrun writes a complete trun box.
func (w *Writer) WriteTrun(flags uint32, dataOffset int32, entries []TrunEntry) {
	w.StartFullBox(TypeTrun, 0, flags)
	w.putUint32(uint32(len(entries)))
	if flags&TrunDataOffsetPresent != 0 {
		w.putInt32(dataOffset)
	}
	for _, e := range entries {
		if flags&TrunSampleDurationPresent != 0 {
			w.putUint32(e.Duration)
		}
		if flags&TrunSampleSizePresent != 0 {
			w.putUint32(e.Size)
		}
		if flags&TrunSampleFlagsPresent != 0 {
			w.putUint32(e.Flags)
		}
		if flags&TrunSampleCompositionTimeOffsetPresent != 0 {
			w.putInt32(e.CompositionTimeOffset)
		}
	}
	w.EndBox()
}

// WriteVisualSampleEntry writes the 78-byte visual sample entry header.
// The caller must start the box (e.g. avc1) and end it after writing children.
func (w *Writer) WriteVisualSampleEntry(dataRefIdx, width, height, frameCount, depth uint16, compressor string) {
	w.putZeros(6)           // reserved
	w.putUint16(dataRefIdx) // data reference index
	w.putZeros(16)          // predefined + reserved
	w.putUint16(width)      // width
	w.putUint16(height)     // height
	w.putUint32(0x00480000) // hresolution 72 dpi
	w.putUint32(0x00480000) // vresolution 72 dpi
	w.putZeros(4)           // reserved
	w.putUint16(frameCount) // frame count
	nameLen := min(len(compressor), 31)
	w.putUint8(byte(nameLen))
	w.putFixedString(compressor, 31)
	w.putUint16(depth)  // depth
	w.putUint16(0xffff) // predefined = -1
}

// WriteAudioSampleEntry writes the 28-byte audio sample entry header.
// The caller must start the box (e.g. mp4a) and end it after writing children.
func (w *Writer) WriteAudioSampleEntry(dataRefIdx, channelCount, sampleSize uint16, sampleRate uint32) {
	w.putZeros(6)             // reserved
	w.putUint16(dataRefIdx)   // data reference index
	w.putZeros(8)             // reserved
	w.putUint16(channelCount) // channel count
	w.putUint16(sampleSize)   // sample size
	w.putZeros(4)             // predefined + reserved
	w.putUint32(sampleRate)   // sample rate (16.16 fixed point)
}

// WriteStyp writes a segment type box (same format as ftyp).
func (w *Writer) WriteStyp(brand [4]byte, brandVersion uint32, compat [][4]byte) {
	w.StartBox(TypeStyp)
	w.putBytes(brand[:])
	w.putUint32(brandVersion)
	for _, c := range compat {
		w.putBytes(c[:])
	}
	w.EndBox()
}

// SidxEntry represents one reference in a sidx box.
type SidxEntry struct {
	ReferenceType  bool   // false = media, true = sub-sidx
	ReferencedSize uint32 // size in bytes of the referenced material
	SubsegDuration uint32 // duration in timescale units
	StartsWithSAP  bool   // starts with a stream access point
	SAPType        uint8  // SAP type (1-6)
}

// WriteSidx writes a segment index box (version 1, 64-bit times).
func (w *Writer) WriteSidx(trackID uint32, timescale uint32, earliestPTS uint64, firstOffset uint64, entries []SidxEntry) {
	w.StartFullBox(TypeSidx, 1, 0)
	w.putUint32(trackID) // reference_ID
	w.putUint32(timescale)
	w.putUint64(earliestPTS)          // earliest_presentation_time
	w.putUint64(firstOffset)          // first_offset
	w.putUint16(0)                    // reserved
	w.putUint16(uint16(len(entries))) // reference_count
	for _, e := range entries {
		var refTypeAndSize uint32
		if e.ReferenceType {
			refTypeAndSize = 0x80000000
		}
		refTypeAndSize |= e.ReferencedSize & 0x7FFFFFFF
		w.putUint32(refTypeAndSize)
		w.putUint32(e.SubsegDuration)
		var sapField uint32
		if e.StartsWithSAP {
			sapField = 0x80000000
		}
		sapField |= uint32(e.SAPType) << 28
		w.putUint32(sapField)
	}
	w.EndBox()
}
