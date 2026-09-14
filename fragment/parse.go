package fragment

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/track"
)

// ErrNoSamples is returned by ParseSegment when the data holds no moof with
// samples.
var ErrNoSamples = errors.New("fragment: segment has no samples")

// sampleIsNonSync is the bit in a sample's flags that marks it as not a sync
// sample (ISO/IEC 14496-12, sample_is_non_sync_sample).
const sampleIsNonSync = 0x00010000

// ParseSegment reads a packaged CMAF media segment back into its samples: any
// leading boxes (styp, sidx) are skipped and every moof's track runs are
// decoded, so a segment holding several moof+mdat pairs (low-latency parts)
// parses as one flat sample list. Each sample's Offset is its absolute byte
// position in data, so data itself serves as the [Writer]'s source when the
// samples are re-packaged. The returned sequence number is the first moof's.
//
// The parser covers the layout this package's [Writer] produces (per-traf
// tfdt, data offsets relative to the moof start or an explicit base) and
// validates that every sample's bytes lie inside data.
func ParseSegment(data []byte) ([]track.Sample, uint32, error) {
	var (
		samples []track.Sample
		seq     uint32
		seqSet  bool
	)
	r := mp4.NewReader(data)
	for r.Next() {
		if r.Type() != mp4.TypeMoof {
			continue
		}
		moofStart := int64(r.Offset())
		r.Enter()
		for r.Next() {
			switch r.Type() {
			case mp4.TypeMfhd:
				if !seqSet {
					seq = r.ReadMfhd()
					seqSet = true
				}
			case mp4.TypeTraf:
				var err error
				samples, err = parseTraf(&r, len(data), moofStart, samples)
				if err != nil {
					return nil, 0, err
				}
			}
		}
		r.Exit()
	}
	if len(samples) == 0 {
		return nil, 0, ErrNoSamples
	}
	return samples, seq, nil
}

// parseTraf decodes one traf's samples and appends them to samples. r is
// positioned on the traf box; dataLen bounds the sample byte ranges.
func parseTraf(r *mp4.Reader, dataLen int, moofStart int64, samples []track.Sample) ([]track.Sample, error) {
	var (
		trackID     uint32
		baseOffset  = moofStart
		defDuration uint32
		defSize     uint32
		defFlags    uint32
		dts         int64
	)
	r.Enter()
	defer r.Exit()
	for r.Next() {
		switch r.Type() {
		case mp4.TypeTfhd:
			d := r.Data()
			fl := r.Flags()
			if len(d) < 4 {
				return nil, errors.New("fragment: short tfhd")
			}
			trackID = binary.BigEndian.Uint32(d)
			p := 4
			if fl&mp4.TfhdBaseDataOffsetPresent != 0 {
				if len(d) < p+8 {
					return nil, errors.New("fragment: short tfhd")
				}
				baseOffset = int64(binary.BigEndian.Uint64(d[p:]))
				p += 8
			}
			if fl&mp4.TfhdSampleDescriptionIndexPresent != 0 {
				p += 4
			}
			if fl&mp4.TfhdDefaultSampleDurationPresent != 0 {
				if len(d) < p+4 {
					return nil, errors.New("fragment: short tfhd")
				}
				defDuration = binary.BigEndian.Uint32(d[p:])
				p += 4
			}
			if fl&mp4.TfhdDefaultSampleSizePresent != 0 {
				if len(d) < p+4 {
					return nil, errors.New("fragment: short tfhd")
				}
				defSize = binary.BigEndian.Uint32(d[p:])
				p += 4
			}
			if fl&mp4.TfhdDefaultSampleFlagsPresent != 0 {
				if len(d) < p+4 {
					return nil, errors.New("fragment: short tfhd")
				}
				defFlags = binary.BigEndian.Uint32(d[p:])
			}
		case mp4.TypeTfdt:
			dts = int64(r.ReadTfdt())
		case mp4.TypeTrun:
			flags := r.Flags()
			if flags&mp4.TrunDataOffsetPresent == 0 {
				return nil, errors.New("fragment: trun without data offset")
			}
			it := mp4.NewTrunIter(r.Data(), flags)
			off := baseOffset + int64(it.DataOffset())
			first := true
			for {
				e, ok := it.Next()
				if !ok {
					break
				}
				dur := e.Duration
				if flags&mp4.TrunSampleDurationPresent == 0 {
					dur = defDuration
				}
				size := e.Size
				if flags&mp4.TrunSampleSizePresent == 0 {
					size = defSize
				}
				sflags := e.Flags
				if flags&mp4.TrunSampleFlagsPresent == 0 {
					sflags = defFlags
					if first && flags&mp4.TrunFirstSampleFlagsPresent != 0 {
						sflags = it.FirstSampleFlags()
					}
				}
				if off < 0 || off+int64(size) > int64(dataLen) {
					return nil, fmt.Errorf("fragment: sample bytes [%d, %d) outside segment of %d bytes",
						off, off+int64(size), dataLen)
				}
				sm := track.Sample{
					Offset:             off,
					DTS:                dts,
					TrackID:            trackID,
					Duration:           dur,
					PresentationOffset: e.CompositionTimeOffset,
				}
				sm.SetSize(size, sflags&sampleIsNonSync == 0)
				samples = append(samples, sm)
				off += int64(size)
				dts += int64(dur)
				first = false
			}
		}
	}
	return samples, nil
}
