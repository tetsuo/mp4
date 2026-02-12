package remux

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"github.com/tetsuo/mp4"
)

var be = binary.BigEndian

// Sample represents a single decoded sample (frame) with all metadata needed for remuxing.
type Sample struct {
	Offset             int64
	Size               uint32
	Duration           uint32
	DTS                int64
	PresentationOffset int32
	Sync               bool
}

// Track holds the parsed metadata for one track (audio or video).
type Track struct {
	TrackID   uint32
	TimeScale uint32
	Codec     string
	Mime      string

	Samples []Sample

	// initBuf is the pre-encoded ftyp+moov for this track's init segment.
	initBuf []byte
	// defaultSampleDescriptionIndex from the last stsc entry.
	defaultSampleDescriptionIndex uint32
}

// Remuxer holds parsed MP4 metadata and writes fragmented MP4 streams.
type Remuxer struct {
	Tracks []*Track
}

// minFragmentDuration is the minimum fragment duration in seconds.
const minFragmentDuration = 1

// NewRemuxer parses the moov box from an MP4 source and prepares track metadata.
func NewRemuxer(rs io.ReadSeeker) (*Remuxer, error) {
	moov, err := findAndParseMoov(rs)
	if err != nil {
		return nil, err
	}
	return newRemuxer(moov)
}

// NewRemuxerFromBytes parses the moov box from an in-memory MP4 file.
func NewRemuxerFromBytes(data []byte) (*Remuxer, error) {
	moov, err := findAndParseMoovBytes(data)
	if err != nil {
		return nil, err
	}
	return newRemuxer(moov)
}

// findAndParseMoov locates the moov box by scanning top-level boxes.
func findAndParseMoov(rs io.ReadSeeker) (*mp4.Box, error) {
	var hdr [8]byte
	var offset int64

	for {
		if _, err := io.ReadFull(rs, hdr[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("moov box not found")
			}
			return nil, err
		}

		size := int64(binary.BigEndian.Uint32(hdr[:4]))
		boxType := string(hdr[4:8])

		if size == 1 {
			// 64-bit extended size
			var ext [8]byte
			if _, err := io.ReadFull(rs, ext[:]); err != nil {
				return nil, err
			}
			size = int64(binary.BigEndian.Uint64(ext[:]))
		}

		if boxType == "moov" {
			contentSize := size - 8
			buf := make([]byte, size)
			copy(buf[:8], hdr[:])
			if _, err := io.ReadFull(rs, buf[8:]); err != nil {
				return nil, fmt.Errorf("reading moov: %w", err)
			}
			box, err := mp4.Decode(buf, 0, int(size))
			if err != nil {
				return nil, fmt.Errorf("decoding moov: %w", err)
			}
			_ = contentSize
			return box, nil
		}

		// Skip this box
		skip := size - 8
		if skip > 0 {
			if _, err := rs.Seek(skip, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
		offset += size
	}
}

func findAndParseMoovBytes(data []byte) (*mp4.Box, error) {
	ptr := 0
	for ptr+8 <= len(data) {
		size := int(binary.BigEndian.Uint32(data[ptr:]))
		if size < 8 {
			return nil, fmt.Errorf("invalid box size %d at offset %d", size, ptr)
		}
		boxType := string(data[ptr+4 : ptr+8])
		if size == 1 && ptr+16 <= len(data) {
			size = int(binary.BigEndian.Uint64(data[ptr+8:]))
		}
		if boxType == "moov" {
			return mp4.Decode(data, ptr, ptr+size)
		}
		ptr += size
	}
	return nil, fmt.Errorf("moov box not found")
}

func newRemuxer(moov *mp4.Box) (*Remuxer, error) {
	mvhd := moov.Child(mp4.TypeMvhd)
	if mvhd == nil || mvhd.Mvhd == nil {
		return nil, fmt.Errorf("missing mvhd")
	}

	traks := moov.ChildList(mp4.TypeTrak)
	if len(traks) == 0 {
		return nil, fmt.Errorf("no tracks found")
	}

	r := &Remuxer{}
	hasVideo := false
	hasAudio := false

	for _, trak := range traks {
		tkhdBox := trak.Child(mp4.TypeTkhd)
		if tkhdBox == nil || tkhdBox.Tkhd == nil {
			continue
		}
		mdiaBox := trak.Child(mp4.TypeMdia)
		if mdiaBox == nil {
			continue
		}
		hdlrBox := mdiaBox.Child(mp4.TypeHdlr)
		if hdlrBox == nil || hdlrBox.Hdlr == nil {
			continue
		}
		minfBox := mdiaBox.Child(mp4.TypeMinf)
		if minfBox == nil {
			continue
		}
		stblBox := minfBox.Child(mp4.TypeStbl)
		if stblBox == nil {
			continue
		}
		mdhdBox := mdiaBox.Child(mp4.TypeMdhd)
		if mdhdBox == nil || mdhdBox.Mdhd == nil {
			continue
		}

		handlerType := string(hdlrBox.Hdlr.HandlerType[:])

		stsdBox := stblBox.Child(mp4.TypeStsd)
		if stsdBox == nil || stsdBox.Stsd == nil || len(stsdBox.Stsd.Entries) == 0 {
			continue
		}
		stsdEntry := stsdBox.Stsd.Entries[0]

		var codec, mime string

		if handlerType == "vide" && stsdEntry.Type == mp4.TypeAvc1 {
			if hasVideo {
				continue
			}
			hasVideo = true
			codec = "avc1"
			if stsdEntry.Visual != nil {
				for _, child := range stsdEntry.Visual.Children {
					if child.Type == mp4.TypeAvcC && child.AvcC != nil {
						codec += "." + child.AvcC.MimeCodec
						break
					}
				}
			}
			mime = fmt.Sprintf(`video/mp4; codecs="%s"`, codec)
		} else if handlerType == "soun" && stsdEntry.Type == mp4.TypeMp4a {
			if hasAudio {
				continue
			}
			hasAudio = true
			codec = "mp4a"
			if stsdEntry.Audio != nil {
				for _, child := range stsdEntry.Audio.Children {
					if child.Type == mp4.TypeEsds && child.Esds != nil && child.Esds.MimeCodec != "" {
						codec += "." + child.Esds.MimeCodec
						break
					}
				}
			}
			mime = fmt.Sprintf(`audio/mp4; codecs="%s"`, codec)
		} else {
			continue
		}

		samples, defaultSdi, err := buildSampleTable(stblBox)
		if err != nil {
			return nil, fmt.Errorf("track %d: %w", tkhdBox.Tkhd.TrackId, err)
		}

		track := &Track{
			TrackID:                       tkhdBox.Tkhd.TrackId,
			TimeScale:                     mdhdBox.Mdhd.TimeScale,
			Codec:                         codec,
			Mime:                          mime,
			Samples:                       samples,
			defaultSampleDescriptionIndex: defaultSdi,
		}

		// Build init segment
		initBuf, err := buildInitSegment(mvhd, trak, track)
		if err != nil {
			return nil, fmt.Errorf("track %d init: %w", track.TrackID, err)
		}
		track.initBuf = initBuf

		r.Tracks = append(r.Tracks, track)
	}

	if len(r.Tracks) == 0 {
		return nil, fmt.Errorf("no playable tracks")
	}

	return r, nil
}

func buildSampleTable(stbl *mp4.Box) ([]Sample, uint32, error) {
	stszBox := stbl.Child(mp4.TypeStsz)
	if stszBox == nil || stszBox.Stsz == nil {
		return nil, 0, fmt.Errorf("missing stsz")
	}
	sttsBox := stbl.Child(mp4.TypeStts)
	if sttsBox == nil || sttsBox.Stts == nil {
		return nil, 0, fmt.Errorf("missing stts")
	}
	stscBox := stbl.Child(mp4.TypeStsc)
	if stscBox == nil || stscBox.Stsc == nil {
		return nil, 0, fmt.Errorf("missing stsc")
	}

	// Chunk offset table (stco or co64)
	var chunkOffsets []int64
	if co64Box := stbl.Child(mp4.TypeCo64); co64Box != nil && co64Box.Co64 != nil {
		chunkOffsets = make([]int64, len(co64Box.Co64.Entries))
		for i, v := range co64Box.Co64.Entries {
			chunkOffsets[i] = int64(v)
		}
	} else if stcoBox := stbl.Child(mp4.TypeStco); stcoBox != nil && stcoBox.Stco != nil {
		chunkOffsets = make([]int64, len(stcoBox.Stco.Entries))
		for i, v := range stcoBox.Stco.Entries {
			chunkOffsets[i] = int64(v)
		}
	} else {
		return nil, 0, fmt.Errorf("missing stco/co64")
	}

	numSamples := len(stszBox.Stsz.Entries)
	samples := make([]Sample, numSamples)

	// Chunk/position iterators
	sampleInChunk := 0
	chunk := 0
	var offsetInChunk int64
	sampleToChunkIndex := 0
	stscEntries := stscBox.Stsc.Entries

	// Time iterators
	var dts int64
	sttsEntries := sttsBox.Stts.Entries
	decodingIdx := 0
	decodingOff := 0

	// Composition offsets (optional)
	var cttsEntries []mp4.CTTSEntry
	cttsIdx := 0
	cttsOff := 0
	if cttsBox := stbl.Child(mp4.TypeCtts); cttsBox != nil && cttsBox.Ctts != nil {
		cttsEntries = cttsBox.Ctts.Entries
	}

	// Sync sample table (optional)
	var syncEntries []uint32
	syncIdx := 0
	if stssBox := stbl.Child(mp4.TypeStss); stssBox != nil && stssBox.Stco != nil {
		syncEntries = stssBox.Stco.Entries
	}

	var defaultSdi uint32

	for i := range numSamples {
		currChunkEntry := stscEntries[sampleToChunkIndex]
		defaultSdi = currChunkEntry.SampleDescriptionId

		size := stszBox.Stsz.Entries[i]
		duration := sttsEntries[decodingIdx].Duration

		var presentationOffset int32
		if cttsEntries != nil && cttsIdx < len(cttsEntries) {
			presentationOffset = cttsEntries[cttsIdx].CompositionOffset
		}

		sync := true
		if syncEntries != nil {
			sync = syncIdx < len(syncEntries) && syncEntries[syncIdx] == uint32(i+1)
		}

		samples[i] = Sample{
			Offset:             offsetInChunk + chunkOffsets[chunk],
			Size:               size,
			Duration:           duration,
			DTS:                dts,
			PresentationOffset: presentationOffset,
			Sync:               sync,
		}

		// Advance to next sample
		if i+1 >= numSamples {
			break
		}

		// Advance chunk position
		sampleInChunk++
		offsetInChunk += int64(size)
		if sampleInChunk >= int(currChunkEntry.SamplesPerChunk) {
			sampleInChunk = 0
			offsetInChunk = 0
			chunk++
			if sampleToChunkIndex+1 < len(stscEntries) {
				nextEntry := stscEntries[sampleToChunkIndex+1]
				if uint32(chunk+1) >= nextEntry.FirstChunk {
					sampleToChunkIndex++
				}
			}
		}

		// Advance time
		dts += int64(duration)
		decodingOff++
		if decodingOff >= int(sttsEntries[decodingIdx].Count) {
			decodingIdx++
			decodingOff = 0
		}

		// Advance composition offset
		if cttsEntries != nil {
			cttsOff++
			if cttsIdx < len(cttsEntries) && cttsOff >= int(cttsEntries[cttsIdx].Count) {
				cttsIdx++
				cttsOff = 0
			}
		}

		// Advance sync index
		if sync {
			syncIdx++
		}
	}

	return samples, defaultSdi, nil
}

func buildInitSegment(mvhd *mp4.Box, trak *mp4.Box, track *Track) ([]byte, error) {
	tkhdBox := trak.Child(mp4.TypeTkhd)
	mdiaBox := trak.Child(mp4.TypeMdia)
	mdhdBox := mdiaBox.Child(mp4.TypeMdhd)
	hdlrBox := mdiaBox.Child(mp4.TypeHdlr)
	minfBox := mdiaBox.Child(mp4.TypeMinf)
	stblBox := minfBox.Child(mp4.TypeStbl)
	stsdBox := stblBox.Child(mp4.TypeStsd)

	// Clone tkhd with duration=0
	tkhdClone := *tkhdBox.Tkhd
	be.PutUint32(tkhdClone.CTime[:], be.Uint32(tkhdBox.Tkhd.CTime[:]))
	// zero out duration
	tkhdClone.Duration = 0

	// Clone mdhd with duration=0
	mdhdClone := *mdhdBox.Mdhd
	mdhdClone.Duration = 0

	emptyBox := func(t mp4.BoxType) *mp4.Box {
		return &mp4.Box{
			Type:    t,
			Version: 0,
			Flags:   0,
		}
	}

	emptyEntryBox := func(t mp4.BoxType) *mp4.Box {
		b := emptyBox(t)
		switch t {
		case mp4.TypeStts:
			b.Stts = &mp4.Stts{}
		case mp4.TypeCtts:
			b.Ctts = &mp4.Ctts{}
		case mp4.TypeStsc:
			b.Stsc = &mp4.Stsc{}
		case mp4.TypeStsz:
			b.Stsz = &mp4.Stsz{}
		case mp4.TypeStco:
			b.Stco = &mp4.Stco{}
		case mp4.TypeStss:
			b.Stco = &mp4.Stco{} // stss uses same codec as stco
		}
		return b
	}

	// Build minf children
	minfChildren := make(map[mp4.BoxType][]*mp4.Box)
	// Copy vmhd or smhd
	if vmhd := minfBox.Child(mp4.TypeVmhd); vmhd != nil {
		minfChildren[mp4.TypeVmhd] = []*mp4.Box{vmhd}
	}
	if smhd := minfBox.Child(mp4.TypeSmhd); smhd != nil {
		minfChildren[mp4.TypeSmhd] = []*mp4.Box{smhd}
	}
	if dinf := minfBox.Child(mp4.TypeDinf); dinf != nil {
		minfChildren[mp4.TypeDinf] = []*mp4.Box{dinf}
	}

	stblNew := &mp4.Box{
		Type: mp4.TypeStbl,
		Children: map[mp4.BoxType][]*mp4.Box{
			mp4.TypeStsd: {stsdBox},
			mp4.TypeStts: {emptyEntryBox(mp4.TypeStts)},
			mp4.TypeCtts: {emptyEntryBox(mp4.TypeCtts)},
			mp4.TypeStsc: {emptyEntryBox(mp4.TypeStsc)},
			mp4.TypeStsz: {emptyEntryBox(mp4.TypeStsz)},
			mp4.TypeStco: {emptyEntryBox(mp4.TypeStco)},
			mp4.TypeStss: {emptyEntryBox(mp4.TypeStss)},
		},
	}
	minfChildren[mp4.TypeStbl] = []*mp4.Box{stblNew}

	// Clone mvhd with duration=0
	mvhdClone := *mvhd.Mvhd
	mvhdClone.Duration = 0
	mvhdBox := &mp4.Box{Type: mp4.TypeMvhd, Version: mvhd.Version, Flags: mvhd.Flags, Mvhd: &mvhdClone}

	moov := &mp4.Box{
		Type: mp4.TypeMoov,
		Children: map[mp4.BoxType][]*mp4.Box{
			mp4.TypeMvhd: {mvhdBox},
			mp4.TypeTrak: {{
				Type: mp4.TypeTrak,
				Children: map[mp4.BoxType][]*mp4.Box{
					mp4.TypeTkhd: {{Type: mp4.TypeTkhd, Version: tkhdBox.Version, Flags: tkhdBox.Flags, Tkhd: &tkhdClone}},
					mp4.TypeMdia: {{
						Type: mp4.TypeMdia,
						Children: map[mp4.BoxType][]*mp4.Box{
							mp4.TypeMdhd: {{Type: mp4.TypeMdhd, Version: mdhdBox.Version, Flags: mdhdBox.Flags, Mdhd: &mdhdClone}},
							mp4.TypeHdlr: {hdlrBox},
							mp4.TypeMinf: {{
								Type:     mp4.TypeMinf,
								Children: minfChildren,
							}},
						},
					}},
				},
			}},
			mp4.TypeMvex: {{
				Type: mp4.TypeMvex,
				Children: map[mp4.BoxType][]*mp4.Box{
					mp4.TypeMehd: {{Type: mp4.TypeMehd, Mehd: &mp4.Mehd{FragmentDuration: mvhd.Mvhd.Duration}}},
					mp4.TypeTrex: {{Type: mp4.TypeTrex, Trex: &mp4.Trex{
						TrackId:                       track.TrackID,
						DefaultSampleDescriptionIndex: track.defaultSampleDescriptionIndex,
					}}},
				},
			}},
		},
	}

	ftyp := &mp4.Box{
		Type: mp4.TypeFtyp,
		Ftyp: &mp4.Ftyp{
			Brand:            [4]byte{'i', 's', 'o', '5'},
			BrandVersion:     0,
			CompatibleBrands: [][4]byte{{'i', 's', 'o', '5'}},
		},
	}

	ftypBuf, err := mp4.EncodeToBytes(ftyp)
	if err != nil {
		return nil, err
	}
	moovBuf, err := mp4.EncodeToBytes(moov)
	if err != nil {
		return nil, err
	}

	out := make([]byte, len(ftypBuf)+len(moovBuf))
	copy(out, ftypBuf)
	copy(out[len(ftypBuf):], moovBuf)
	return out, nil
}

// InitSegment returns the pre-built init segment (ftyp+moov) for the given track.
func (t *Track) InitSegment() []byte {
	return t.initBuf
}

// Duration returns the total duration of the track in seconds.
func (t *Track) Duration() float64 {
	if len(t.Samples) == 0 || t.TimeScale == 0 {
		return 0
	}
	last := t.Samples[len(t.Samples)-1]
	return float64(last.DTS+int64(last.Duration)) / float64(t.TimeScale)
}

// Fragment represents a moof+mdat fragment to be written.
type Fragment struct {
	trackID        uint32
	timeScale      uint32
	firstSample    int
	lastSample     int // exclusive
	samples        []Sample
	sequenceNumber uint32
	version        uint8
}

// FindSampleBefore finds the sync sample at or before the given time (in seconds).
// Useful for seeking backward to a safe playback position.
func (t *Track) FindSampleBefore(timeSeconds float64) int {
	scaledTime := int64(timeSeconds * float64(t.TimeScale))

	// Binary search: find last sample with pts <= scaledTime
	idx := max(sort.Search(len(t.Samples), func(i int) bool {
		pts := t.Samples[i].DTS + int64(t.Samples[i].PresentationOffset)
		return pts > scaledTime
	})-1, 0)

	// Walk backward to find the preceding sync sample
	for idx > 0 && !t.Samples[idx].Sync {
		idx--
	}

	return idx
}

// FindSampleAfter finds the first sync sample at or after the given time (in seconds).
// Useful for finding a clean start point for time-based extraction.
func (t *Track) FindSampleAfter(timeSeconds float64) int {
	scaledTime := int64(timeSeconds * float64(t.TimeScale))

	// Binary search: find first sample with pts >= scaledTime
	idx := sort.Search(len(t.Samples), func(i int) bool {
		pts := t.Samples[i].DTS + int64(t.Samples[i].PresentationOffset)
		return pts >= scaledTime
	})

	if idx >= len(t.Samples) {
		return len(t.Samples) - 1
	}

	// Walk forward to find the next sync sample
	for idx < len(t.Samples) && !t.Samples[idx].Sync {
		idx++
	}

	// If went past the end, return the last sample
	if idx >= len(t.Samples) {
		return len(t.Samples) - 1
	}

	return idx
}

// byteRange represents a contiguous range of bytes in the source file.
type byteRange struct {
	Start int64
	End   int64 // exclusive
}

// generateFragment builds fragment metadata for samples starting at firstSample.
// trunEntries and ranges are caller-provided slices that will be reused (resliced).
// Returns the populated slices, total mdat payload size, next sample index, and trun version.
func generateFragment(track *Track, firstSample int, endTimeScaled int64, trunEntries []mp4.TrunEntry, ranges []byteRange) ([]mp4.TrunEntry, []byteRange, int64, int, uint8) {
	samples := track.Samples
	if firstSample >= len(samples) {
		return trunEntries[:0], ranges[:0], 0, firstSample, 0
	}

	startDts := samples[firstSample].DTS
	threshold := int64(track.TimeScale) * minFragmentDuration

	// Find the end of this fragment
	lastSample := firstSample

	for lastSample < len(samples) {
		s := samples[lastSample]
		pts := s.DTS + int64(s.PresentationOffset)

		// Hard stop: don't include any sample at or past end time
		if endTimeScaled > 0 && pts >= endTimeScaled {
			break
		}

		// Fragment boundary: when no end time, break at sync samples
		// after minimum duration
		if endTimeScaled == 0 && lastSample > firstSample && s.Sync {
			elapsed := s.DTS - startDts
			if elapsed >= threshold {
				break
			}
		}

		lastSample++
	}

	n := lastSample - firstSample
	if n == 0 {
		return trunEntries[:0], ranges[:0], 0, lastSample, 0
	}

	// Grow trunEntries when needed
	if cap(trunEntries) < n {
		trunEntries = make([]mp4.TrunEntry, n)
	} else {
		trunEntries = trunEntries[:n]
	}

	var totalLen int64
	var trunVersion uint8

	for i := range n {
		s := samples[firstSample+i]
		if s.PresentationOffset < 0 {
			trunVersion = 1
		}
		flags := uint32(0x2000000) // sync
		if !s.Sync {
			flags = 0x1010000 // non-sync
		}
		trunEntries[i] = mp4.TrunEntry{
			SampleDuration:              s.Duration,
			SampleSize:                  s.Size,
			SampleFlags:                 flags,
			SampleCompositionTimeOffset: s.PresentationOffset,
		}
		totalLen += int64(s.Size)
	}

	// Build contiguous byte ranges, reuse slice
	ranges = ranges[:0]
	for i := range n {
		s := samples[firstSample+i]
		sStart := s.Offset
		sEnd := s.Offset + int64(s.Size)
		if len(ranges) > 0 && ranges[len(ranges)-1].End == sStart {
			ranges[len(ranges)-1].End = sEnd
		} else {
			ranges = append(ranges, byteRange{Start: sStart, End: sEnd})
		}
	}

	return trunEntries, ranges, totalLen, lastSample, trunVersion
}

// WriteTo writes a complete fragmented MP4 stream for a single track,
// starting from the given time (seconds), to w.
// If endTime > 0, stops writing fragments at or before the given end time.
//
// Each call creates a new [Writer]. For repeated calls, create a [Writer]
// once and call its [Writer.WriteTo] method instead of this helper.
func WriteTo(w io.Writer, rs io.ReadSeeker, track *Track, startTime float64, endTime float64) error {
	return NewWriter().WriteTo(w, rs, track, startTime, endTime)
}
