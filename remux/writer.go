package remux

import (
	"encoding/binary"
	"io"

	"github.com/tetsuo/mp4"
)

// Writer holds reusable buffers for writing fragmented MP4 streams.
//
// A Writer is NOT safe for concurrent use. Use one Writer per goroutine,
// or protect with a mutex.
type Writer struct {
	trunEntries []mp4.TrunEntry
	ranges      []byteRange
	copyBuf     []byte
	moofBuf     []byte
	mdatHdr     [8]byte
}

// NewWriter creates a Writer with pre-allocated buffers.
func NewWriter() *Writer {
	return &Writer{
		trunEntries: make([]mp4.TrunEntry, 0, 512),
		ranges:      make([]byteRange, 0, 64),
		copyBuf:     make([]byte, 32768),
		moofBuf:     make([]byte, 0, 8192),
	}
}

// WriteTo writes a complete fragmented MP4 stream for a single track to w.
// rs must support Seek+Read (e.g. *[os.File]).
// For concurrent use with a shared file, use [Writer.WriteToFrom] with [io.ReaderAt] instead.
func (wr *Writer) WriteTo(w io.Writer, rs io.ReadSeeker, track *Track, startTime float64, endTime float64) error {
	firstSample, dtsOffset, endTimeScaled := wr.resolveRange(track, startTime, endTime)

	if _, err := w.Write(track.InitSegment()); err != nil {
		return err
	}

	var seqNum uint32 = 1
	sample := firstSample

	for sample < len(track.Samples) {
		if endTimeScaled > 0 {
			pts := track.Samples[sample].DTS + int64(track.Samples[sample].PresentationOffset)
			if pts >= endTimeScaled {
				break
			}
		}

		var mdatSize int64
		var nextSample int
		var trunVersion uint8
		wr.trunEntries, wr.ranges, mdatSize, nextSample, trunVersion = generateFragment(track, sample, endTimeScaled, wr.trunEntries, wr.ranges)
		if len(wr.trunEntries) == 0 {
			break
		}

		baseMediaDecodeTime := uint32(track.Samples[sample].DTS - dtsOffset)
		var err error
		wr.moofBuf, err = writeMoof(w, seqNum, track.TrackID, baseMediaDecodeTime, wr.trunEntries, trunVersion, wr.moofBuf)
		if err != nil {
			return err
		}

		binary.BigEndian.PutUint32(wr.mdatHdr[:4], uint32(8+mdatSize))
		copy(wr.mdatHdr[4:8], "mdat")
		if _, err := w.Write(wr.mdatHdr[:]); err != nil {
			return err
		}

		for _, r := range wr.ranges {
			if _, err := rs.Seek(r.Start, io.SeekStart); err != nil {
				return err
			}
			remaining := r.End - r.Start
			for remaining > 0 {
				n := min(int64(len(wr.copyBuf)), remaining)
				nr, err := rs.Read(wr.copyBuf[:n])
				if nr > 0 {
					if _, werr := w.Write(wr.copyBuf[:nr]); werr != nil {
						return werr
					}
					remaining -= int64(nr)
				}
				if err != nil {
					if err == io.EOF && remaining == 0 {
						break
					}
					return err
				}
			}
		}

		seqNum++
		sample = nextSample
	}

	return nil
}

// WriteToFrom writes a complete fragmented MP4 stream using an [io.ReaderAt].
// Unlike [Writer.WriteTo], this is safe to use with a single shared *[os.File] from multiple
// goroutines (each with their own Writer), because [io.ReaderAt.ReadAt] does not mutate file position.
func (wr *Writer) WriteToFrom(w io.Writer, ra io.ReaderAt, track *Track, startTime float64, endTime float64) error {
	firstSample, dtsOffset, endTimeScaled := wr.resolveRange(track, startTime, endTime)

	if _, err := w.Write(track.InitSegment()); err != nil {
		return err
	}

	var seqNum uint32 = 1
	sample := firstSample

	for sample < len(track.Samples) {
		if endTimeScaled > 0 {
			pts := track.Samples[sample].DTS + int64(track.Samples[sample].PresentationOffset)
			if pts >= endTimeScaled {
				break
			}
		}

		var mdatSize int64
		var nextSample int
		var trunVersion uint8
		wr.trunEntries, wr.ranges, mdatSize, nextSample, trunVersion = generateFragment(track, sample, endTimeScaled, wr.trunEntries, wr.ranges)
		if len(wr.trunEntries) == 0 {
			break
		}

		baseMediaDecodeTime := uint32(track.Samples[sample].DTS - dtsOffset)
		var err error
		wr.moofBuf, err = writeMoof(w, seqNum, track.TrackID, baseMediaDecodeTime, wr.trunEntries, trunVersion, wr.moofBuf)
		if err != nil {
			return err
		}

		binary.BigEndian.PutUint32(wr.mdatHdr[:4], uint32(8+mdatSize))
		copy(wr.mdatHdr[4:8], "mdat")
		if _, err := w.Write(wr.mdatHdr[:]); err != nil {
			return err
		}

		for _, r := range wr.ranges {
			off := r.Start
			remaining := r.End - r.Start
			for remaining > 0 {
				n := min(int64(len(wr.copyBuf)), remaining)
				nr, err := ra.ReadAt(wr.copyBuf[:n], off)
				if nr > 0 {
					if _, werr := w.Write(wr.copyBuf[:nr]); werr != nil {
						return werr
					}
					off += int64(nr)
					remaining -= int64(nr)
				}
				if err != nil {
					if err == io.EOF && remaining == 0 {
						break
					}
					return err
				}
			}
		}

		seqNum++
		sample = nextSample
	}

	return nil
}

func (wr *Writer) resolveRange(track *Track, startTime float64, endTime float64) (firstSample int, dtsOffset int64, endTimeScaled int64) {
	firstSample = track.FindSampleAfter(startTime)

	if endTime > 0 && firstSample < len(track.Samples) {
		pts := track.Samples[firstSample].DTS + int64(track.Samples[firstSample].PresentationOffset)
		endScaled := int64(endTime * float64(track.TimeScale))
		if pts >= endScaled {
			firstSample = track.FindSampleBefore(startTime)
		}
	}

	dtsOffset = track.Samples[firstSample].DTS

	if endTime > 0 {
		endTimeScaled = int64(endTime * float64(track.TimeScale))
	}

	return
}
