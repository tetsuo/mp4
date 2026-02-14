package bmff

import "strconv"

// ReadEsdsCodec extracts the MIME codec string from esds box data.
// It parses the MPEG-4 descriptor chain to find the OTI (Object Type Indication)
// and audio configuration. Returns a string like "40.2" for AAC-LC.
func ReadEsdsCodec(data []byte) string {
	if len(data) < 2 {
		return ""
	}

	// Expect ESDescriptor (tag 0x03)
	ptr, end := 0, len(data)
	if data[ptr] != 0x03 {
		return ""
	}
	ptr++

	// Skip length bytes (variable-length encoding)
	ptr = skipDescriptorLength(data, ptr, end)
	if ptr < 0 || ptr+3 > end {
		return ""
	}

	// ES_ID (2 bytes) + stream dependency flags (1 byte)
	flags := data[ptr+2]
	ptr += 3

	// Skip optional fields based on flags
	if flags&0x80 != 0 { // streamDependenceFlag
		ptr += 2
	}
	if flags&0x40 != 0 { // URL_Flag
		if ptr >= end {
			return ""
		}
		urlLen := int(data[ptr])
		ptr += 1 + urlLen
	}
	if flags&0x20 != 0 { // OCRstreamFlag
		ptr += 2
	}

	if ptr >= end {
		return ""
	}

	// Expect DecoderConfigDescriptor (tag 0x04)
	if data[ptr] != 0x04 {
		return ""
	}
	ptr++
	ptr = skipDescriptorLength(data, ptr, end)
	if ptr < 0 || ptr+13 > end {
		return ""
	}

	oti := data[ptr]
	if oti == 0 {
		return ""
	}

	// Format OTI as hex
	otiStr := hexByte(oti)

	// Skip to DecoderSpecificInfo: OTI(1)+streamType(1)+bufferSizeDB(3)+maxBitrate(4)+avgBitrate(4) = 13
	ptr += 13

	if ptr >= end || data[ptr] != 0x05 {
		// No DecoderSpecificInfo, return just OTI
		return otiStr
	}
	ptr++
	ptr = skipDescriptorLength(data, ptr, end)
	if ptr < 0 || ptr >= end {
		return otiStr
	}

	// Extract audio object type from first byte
	audioConfig := (data[ptr] & 0xf8) >> 3
	if audioConfig == 0 {
		return otiStr
	}
	return otiStr + "." + strconv.Itoa(int(audioConfig))
}

// hexByte formats a byte as a lowercase hex string without leading zeros beyond one digit.
func hexByte(b byte) string {
	if b < 16 {
		return string(hexDigit(b))
	}
	var buf [2]byte
	buf[0] = hexDigit(b >> 4)
	buf[1] = hexDigit(b & 0x0f)
	return string(buf[:])
}

// skipDescriptorLength skips the variable-length descriptor length field.
// Returns the new position, or -1 on error.
func skipDescriptorLength(data []byte, ptr, end int) int {
	for ptr < end {
		b := data[ptr]
		ptr++
		if b&0x80 == 0 {
			return ptr
		}
	}
	return -1
}
