package remux

import (
	"encoding/binary"
	"io"

	"github.com/tetsuo/mp4"
)

// writeMoof writes a complete moof box directly as binary to w.
func writeMoof(w io.Writer, seqNum uint32, trackID uint32, baseMediaDecodeTime uint32, entries []mp4.TrunEntry, trunVersion uint8, buf []byte) ([]byte, error) {
	be := binary.BigEndian

	// Fixed sizes (no version-1 tfdt for now, baseMediaDecodeTime is uint32)
	//   moof header:  8
	//   mfhd:         16  (8 hdr + 4 ver/flags + 4 seqnum)
	//   traf header:  8
	//   tfhd:         16  (8 hdr + 4 ver/flags + 4 trackid)
	//   tfdt:         16  (8 hdr + 4 ver/flags + 4 base_decode_time)
	//   trun header:  20  (8 hdr + 4 ver/flags + 4 sample_count + 4 data_offset)
	//   trun entries: 16 * len(entries)
	n := len(entries)
	trunSize := 20 + n*16
	trafSize := 8 + 16 + 16 + trunSize
	moofSize := 8 + 16 + trafSize
	dataOffset := moofSize + 8 // +8 for mdat header

	// Grow buffer when needed
	if cap(buf) < moofSize {
		buf = make([]byte, moofSize)
	} else {
		buf = buf[:moofSize]
	}

	p := 0

	// moof box header
	be.PutUint32(buf[p:], uint32(moofSize))
	copy(buf[p+4:], "moof")
	p += 8

	// mfhd (full box: version=0, flags=0)
	be.PutUint32(buf[p:], 16)
	copy(buf[p+4:], "mfhd")
	buf[p+8] = 0
	buf[p+9] = 0
	buf[p+10] = 0
	buf[p+11] = 0
	be.PutUint32(buf[p+12:], seqNum)
	p += 16

	// traf box header
	be.PutUint32(buf[p:], uint32(trafSize))
	copy(buf[p+4:], "traf")
	p += 8

	// tfhd (full box: version=0, flags=0x020000 = default-base-is-moof)
	be.PutUint32(buf[p:], 16)
	copy(buf[p+4:], "tfhd")
	buf[p+8] = 0
	buf[p+9] = 0x02
	buf[p+10] = 0
	buf[p+11] = 0
	be.PutUint32(buf[p+12:], trackID)
	p += 16

	// tfdt (full box: version=0, flags=0)
	be.PutUint32(buf[p:], 16)
	copy(buf[p+4:], "tfdt")
	buf[p+8] = 0
	buf[p+9] = 0
	buf[p+10] = 0
	buf[p+11] = 0
	be.PutUint32(buf[p+12:], baseMediaDecodeTime)
	p += 16

	// trun (full box: version=trunVersion, flags=0x000f01)
	//   flags: 0x001 = data-offset-present
	//          0x100 = sample-duration-present
	//          0x200 = sample-size-present
	//          0x400 = sample-flags-present
	//          0x800 = sample-composition-time-offset-present
	be.PutUint32(buf[p:], uint32(trunSize))
	copy(buf[p+4:], "trun")
	buf[p+8] = trunVersion
	buf[p+9] = 0x00
	buf[p+10] = 0x0f
	buf[p+11] = 0x01
	be.PutUint32(buf[p+12:], uint32(n))
	be.PutUint32(buf[p+16:], uint32(dataOffset))
	p += 20

	// trun entries: duration(4) + size(4) + flags(4) + compositionTimeOffset(4)
	for i := range n {
		e := &entries[i]
		be.PutUint32(buf[p:], e.SampleDuration)
		be.PutUint32(buf[p+4:], e.SampleSize)
		be.PutUint32(buf[p+8:], e.SampleFlags)
		if trunVersion == 1 {
			// signed composition offset
			be.PutUint32(buf[p+12:], uint32(e.SampleCompositionTimeOffset))
		} else {
			be.PutUint32(buf[p+12:], uint32(e.SampleCompositionTimeOffset))
		}
		p += 16
	}

	_, err := w.Write(buf[:moofSize])
	return buf, err
}
