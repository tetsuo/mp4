package fragment

import (
	"encoding/binary"
	"io"

	"github.com/tetsuo/mp4"
)

// Writer writes fragmented MP4 segments.
type Writer struct {
	w       io.Writer
	buf     []byte       // moof build buffer
	copyBuf [131072]byte // sample data copy buffer
	mdatHdr [8]byte      // mdat box header (size + 'mdat'), reused per fragment

	// State carried from Prepare to the body writers. moof points into buf and
	// holds the finished moof bytes; mdatPayload is the sum of the sample sizes,
	// so the body size is known before any media byte is read.
	moof        []byte
	mdatPayload int64

	// Per-track scratch buffers.
	sampleIdx [maxTracks][]int
	trunBuf   [maxTracks][]mp4.TrunEntry
	ranges    []byteRange
}

type byteRange struct {
	offset int64
	size   int64
}

// NewWriter creates a fragment writer which writes to the provided [io.Writer].
func NewWriter(w io.Writer) *Writer {
	wr := &Writer{
		w:      w,
		buf:    make([]byte, 65536),
		ranges: make([]byteRange, 0, 1024),
	}
	for i := range wr.sampleIdx {
		wr.sampleIdx[i] = make([]int, 0, 512)
	}
	for i := range wr.trunBuf {
		wr.trunBuf[i] = make([]mp4.TrunEntry, 0, 512)
	}
	return wr
}

// Reset prepares the [Writer] for reuse with a new [io.Writer].
func (w *Writer) Reset(dst io.Writer) {
	w.w = dst
}

// WriteInit writes the init segment (ftyp+moov).
func (w *Writer) WriteInit(s *InitSegment) error {
	_, err := w.w.Write(s.buf)
	return err
}

// WriteFragment writes a single moof+mdat fragment.
// src is used to read sample data at the offsets specified in fragment samples.
func (w *Writer) WriteFragment(frag *Fragment, src io.ReaderAt) error {
	if err := w.Prepare(frag); err != nil {
		return err
	}
	return w.WriteBodyRange(w.w, src, 0, w.BodySize())
}

// BodySize returns the byte size of the body (moof + mdat) of the fragment last
// passed to [Writer.Prepare]. It is moofSize + 8 + sum(sample sizes), all known
// from sample metadata, so the size is available before any media is read.
func (w *Writer) BodySize() int64 {
	return int64(len(w.moof)) + 8 + w.mdatPayload
}

// Prepare builds the moof for frag from sample metadata alone, reading no media.
// It records the moof bytes and the mdat payload size so [Writer.BodySize] and
// [Writer.WriteBodyRange] can write the body, or a byte window of it, afterward.
// The recorded state is valid until the next call to Prepare on this writer.
func (w *Writer) Prepare(frag *Fragment) error {
	// Group samples by track
	var trackIDs [maxTracks]uint32
	groupCount := 0

	for i := range w.sampleIdx {
		w.sampleIdx[i] = w.sampleIdx[i][:0]
	}

	for i := range frag.Samples {
		s := &frag.Samples[i]
		found := false
		for g := 0; g < groupCount; g++ {
			if trackIDs[g] == s.TrackID {
				w.sampleIdx[g] = append(w.sampleIdx[g], i)
				found = true
				break
			}
		}
		if !found {
			if groupCount >= maxTracks {
				continue
			}
			trackIDs[groupCount] = s.TrackID
			w.sampleIdx[groupCount] = append(w.sampleIdx[groupCount], i)
			groupCount++
		}
	}

	// Build byte ranges for mdat (contiguous runs of sample data)
	w.ranges = w.ranges[:0]
	for g := 0; g < groupCount; g++ {
		for _, idx := range w.sampleIdx[g] {
			s := &frag.Samples[idx]
			sz := int64(s.Size())
			if len(w.ranges) > 0 {
				last := &w.ranges[len(w.ranges)-1]
				if last.offset+last.size == s.Offset {
					last.size += sz
					continue
				}
			}
			w.ranges = append(w.ranges, byteRange{offset: s.Offset, size: sz})
		}
	}

	// Total mdat payload size
	var mdatPayload int64
	for i := range w.ranges {
		mdatPayload += w.ranges[i].size
	}

	mdatHeaderSize := int32(8)

	// Build trun entries per track group
	type trafInfo struct {
		trackID uint32
		baseDTS int64
		hasCtts bool
	}
	var trafs [maxTracks]trafInfo

	for g := 0; g < groupCount; g++ {
		ti := &trafs[g]
		ti.trackID = trackIDs[g]
		w.trunBuf[g] = w.trunBuf[g][:0]

		indices := w.sampleIdx[g]
		if len(indices) > 0 {
			ti.baseDTS = frag.Samples[indices[0]].DTS
		}

		for _, idx := range indices {
			s := &frag.Samples[idx]
			entry := mp4.TrunEntry{
				Duration: s.Duration,
				Size:     s.Size(),
				Flags:    sampleFlags(s.IsSync()),
			}
			if s.PresentationOffset != 0 {
				entry.CompositionTimeOffset = s.PresentationOffset
				ti.hasCtts = true
			}
			w.trunBuf[g] = append(w.trunBuf[g], entry)
		}
	}

	// Track data layout in mdat
	var trackDataOffsets [maxTracks]int32
	var accum int32
	for g := 0; g < groupCount; g++ {
		trackDataOffsets[g] = accum
		for _, idx := range w.sampleIdx[g] {
			accum += int32(frag.Samples[idx].Size())
		}
	}

	// Build moof, recording trun data_offset positions for backpatching
	type trunPatch struct {
		pos       int
		dataStart int32
	}
	var patches [maxTracks]trunPatch
	patchCount := 0

	mw := mp4.NewWriter(w.buf)
	mw.Reset()

	mw.StartBox(mp4.TypeMoof)
	mw.WriteMfhd(frag.SequenceNum)

	for g := 0; g < groupCount; g++ {
		ti := &trafs[g]
		entries := w.trunBuf[g]
		n := len(entries)

		// Move constant fields into tfhd defaults so the trun only carries what
		// varies. Sample sizes always differ, so they stay per-sample. Sample
		// flags differ only at the first sample (a keyframe) unless the segment
		// holds interior keyframes, in which case they stay per-sample.
		var defaultDuration, defaultFlags uint32
		sameDuration, sameFlags, firstDiffers := true, true, false
		if n > 0 {
			defaultDuration = entries[0].Duration
			ref := 0
			if n > 1 {
				ref = 1
			}
			defaultFlags = entries[ref].Flags
			for i := 1; i < n; i++ {
				if entries[i].Duration != defaultDuration {
					sameDuration = false
				}
				if entries[i].Flags != defaultFlags {
					sameFlags = false
				}
			}
			firstDiffers = entries[0].Flags != defaultFlags
		}

		tfhdFlags := uint32(mp4.TfhdDefaultBaseIsMoof | mp4.TfhdDefaultSampleFlagsPresent)
		if sameDuration {
			tfhdFlags |= mp4.TfhdDefaultSampleDurationPresent
		}

		trunFlags := uint32(mp4.TrunDataOffsetPresent | mp4.TrunSampleSizePresent)
		if !sameDuration {
			trunFlags |= mp4.TrunSampleDurationPresent
		}
		if !sameFlags {
			trunFlags |= mp4.TrunSampleFlagsPresent
		} else if firstDiffers {
			trunFlags |= mp4.TrunFirstSampleFlagsPresent
		}
		if ti.hasCtts {
			trunFlags |= mp4.TrunSampleCompositionTimeOffsetPresent
		}

		var firstSampleFlags uint32
		if n > 0 {
			firstSampleFlags = entries[0].Flags
		}

		mw.StartBox(mp4.TypeTraf)

		mw.WriteTfhd(tfhdFlags, ti.trackID, defaultDuration, 0, defaultFlags)
		mw.WriteTfdt(uint64(ti.baseDTS))

		trunStart := mw.Len()
		if patchCount < maxTracks {
			patches[patchCount] = trunPatch{
				pos:       trunStart + 16, // offset to data_offset field
				dataStart: trackDataOffsets[g],
			}
			patchCount++
		}
		mw.WriteTrun(trunFlags, 0, firstSampleFlags, entries)

		mw.EndBox() // traf
	}

	mw.EndBox() // moof

	moofSize := int32(mw.Len())

	// Backpatch data_offset fields
	moofBytes := mw.Bytes()
	for i := 0; i < patchCount; i++ {
		p := patches[i]
		dataOffset := moofSize + mdatHeaderSize + p.dataStart
		binary.BigEndian.PutUint32(moofBytes[p.pos:], uint32(dataOffset))
	}

	w.moof = moofBytes
	w.mdatPayload = mdatPayload
	return nil
}

// WriteBodyRange writes the body bytes in [start, end) to dst, reading sample
// bytes from src only for the portion of the window that overlaps the sample
// data. Offsets are relative to the start of the body: the moof occupies
// [0, moofSize), the 8-byte mdat header [moofSize, moofSize+8), and the sample
// data the remainder up to [Writer.BodySize]. It must be called after
// [Writer.Prepare]. start and end must satisfy 0 <= start <= end <= BodySize.
func (w *Writer) WriteBodyRange(dst io.Writer, src io.ReaderAt, start, end int64) error {
	moofSize := int64(len(w.moof))

	// moof region [0, moofSize)
	if start < moofSize {
		e := min(end, moofSize)
		if _, err := dst.Write(w.moof[start:e]); err != nil {
			return err
		}
	}

	// mdat header region [moofSize, moofSize+8)
	const mdatHdrSize = 8
	dataStart := moofSize + mdatHdrSize
	if start < dataStart && end > moofSize {
		hdr := w.mdatHdr[:]
		binary.BigEndian.PutUint32(hdr[:4], uint32(w.mdatPayload+mdatHdrSize))
		copy(hdr[4:], "mdat")
		s := max(start, moofSize) - moofSize
		e := min(end, dataStart) - moofSize
		if _, err := dst.Write(hdr[s:e]); err != nil {
			return err
		}
	}

	// sample data region [dataStart, dataStart+mdatPayload)
	if end <= dataStart {
		return nil
	}
	return w.writeDataRange(dst, src, max(start, dataStart)-dataStart, end-dataStart)
}

// writeDataRange writes the sample-data bytes in [lo, hi) to dst, where the
// offsets are relative to the start of the concatenated sample data. It maps the
// window onto the source byte ranges recorded by Prepare and reads only the
// overlapping source bytes, in fixed-size chunks through the reused copy buffer.
func (w *Writer) writeDataRange(dst io.Writer, src io.ReaderAt, lo, hi int64) error {
	var pos int64 // running start of the current range within the data region
	for i := range w.ranges {
		rng := &w.ranges[i]
		rEnd := pos + rng.size
		if rEnd <= lo {
			pos = rEnd
			continue
		}
		if pos >= hi {
			break
		}
		from := max(lo, pos)
		to := min(hi, rEnd)
		off := rng.offset + (from - pos)
		remaining := to - from
		for remaining > 0 {
			n := min(int64(len(w.copyBuf)), remaining)
			nr, err := src.ReadAt(w.copyBuf[:n], off)
			if err != nil && err != io.EOF {
				return err
			}
			if nr == 0 {
				break
			}
			if _, err := dst.Write(w.copyBuf[:nr]); err != nil {
				return err
			}
			off += int64(nr)
			remaining -= int64(nr)
		}
		pos = rEnd
	}
	return nil
}

func sampleFlags(isSync bool) uint32 {
	if isSync {
		return 0x02000000
	}
	return 0x01010000
}
