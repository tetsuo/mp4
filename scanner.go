package bmff

import "io"

// ScanEntry represents a top-level box discovered by the Scanner.
type ScanEntry struct {
	Type       BoxType
	Size       int64 // total box size including header
	Offset     int64 // byte offset from start of stream
	HeaderSize int   // header size (8 or 16 bytes)
}

// DataSize returns the size of the box data (excluding the header).
func (e ScanEntry) DataSize() int64 {
	return e.Size - int64(e.HeaderSize)
}

// Scanner reads top-level box headers from an io.ReadSeeker without
// loading box contents into memory. This lets callers discover box
// positions and sizes, then selectively read only the boxes they need
// (e.g. moov) into a buffer for parsing with NewReader.
//
// Typical usage:
//
//	f, _ := os.Open("video.mp4")
//	sc := bmff.NewScanner(f)
//	for sc.Next() {
//	    e := sc.Entry()
//	    if e.Type == bmff.TypeMoov {
//	        buf := make([]byte, e.DataSize())
//	        sc.ReadBody(buf)
//	        r := bmff.NewReader(buf)
//	        // parse moov contents...
//	    }
//	}
//	if err := sc.Err(); err != nil { ... }
type Scanner struct {
	rs    io.ReadSeeker
	hdr   [16]byte // reusable header buffer
	entry ScanEntry
	err   error
	pos   int64 // current position in stream
}

// NewScanner creates a Scanner that reads box headers from rs.
func NewScanner(rs io.ReadSeeker) Scanner {
	return Scanner{rs: rs}
}

// Next advances to the next top-level box. Returns false when there
// are no more boxes or an error occurs. Check Err() after the loop.
func (s *Scanner) Next() bool {
	// Read the minimum 8-byte header
	_, err := io.ReadFull(s.rs, s.hdr[:8])
	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			s.err = err
		}
		return false
	}

	boxStart := s.pos
	size := int64(be.Uint32(s.hdr[:4]))
	var t BoxType
	copy(t[:], s.hdr[4:8])

	headerSize := 8

	if size == 1 {
		// Extended 64-bit size
		_, err = io.ReadFull(s.rs, s.hdr[8:16])
		if err != nil {
			s.err = err
			return false
		}
		size = int64(be.Uint64(s.hdr[8:16]))
		headerSize = 16
	}

	if size == 0 {
		// Box extends to end of file; determine remaining size
		cur, err := s.rs.Seek(0, io.SeekCurrent)
		if err != nil {
			s.err = err
			return false
		}
		end, err := s.rs.Seek(0, io.SeekEnd)
		if err != nil {
			s.err = err
			return false
		}
		size = end - boxStart
		// Seek back to where we were
		if _, err := s.rs.Seek(cur, io.SeekStart); err != nil {
			s.err = err
			return false
		}
	}

	s.entry = ScanEntry{
		Type:       t,
		Size:       size,
		Offset:     boxStart,
		HeaderSize: headerSize,
	}

	// Skip past this box's data to position for the next call
	dataSize := size - int64(headerSize)
	if dataSize > 0 {
		if _, err := s.rs.Seek(dataSize, io.SeekCurrent); err != nil {
			s.err = err
			return false
		}
	}
	s.pos = boxStart + size

	return true
}

// Entry returns the current box entry. Only valid after Next returns true.
func (s *Scanner) Entry() ScanEntry {
	return s.entry
}

// Err returns the first non-EOF error encountered by the Scanner.
func (s *Scanner) Err() error {
	return s.err
}

// ReadBody reads the current box's data (excluding header) into buf.
// buf must be exactly DataSize() bytes. The scanner seeks to the data
// position, reads, then seeks back so that subsequent Next calls work correctly.
func (s *Scanner) ReadBody(buf []byte) error {
	dataOffset := s.entry.Offset + int64(s.entry.HeaderSize)

	// Save current position (which is past this box)
	saved := s.pos

	if _, err := s.rs.Seek(dataOffset, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.ReadFull(s.rs, buf); err != nil {
		return err
	}

	// Restore position
	if _, err := s.rs.Seek(saved, io.SeekStart); err != nil {
		return err
	}
	return nil
}

// ReadBox reads the current box's full data (including header) into buf.
// buf must be exactly Size bytes.
func (s *Scanner) ReadBox(buf []byte) error {
	saved := s.pos

	if _, err := s.rs.Seek(s.entry.Offset, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.ReadFull(s.rs, buf); err != nil {
		return err
	}

	if _, err := s.rs.Seek(saved, io.SeekStart); err != nil {
		return err
	}
	return nil
}
