package track

import "errors"

// ErrInvalidSPS is returned when an H.264 sequence parameter set cannot be
// parsed.
var ErrInvalidSPS = errors.New("track: invalid H.264 SPS")

// SPSInfo holds the fields of an H.264 sequence parameter set that packaging
// needs: the three bytes echoed into an avcC and codec string, and the coded
// picture dimensions.
type SPSInfo struct {
	Profile uint8 // profile_idc
	Compat  uint8 // constraint flags byte, avcC profile_compatibility
	Level   uint8 // level_idc
	Width   uint16
	Height  uint16
}

// ParseSPS parses an H.264 sequence parameter set NAL unit (header byte
// included, no start code or length prefix) and returns its profile, level,
// and cropped picture dimensions.
func ParseSPS(nalu []byte) (SPSInfo, error) {
	if len(nalu) < 4 || nalu[0]&0x1f != 7 {
		return SPSInfo{}, ErrInvalidSPS
	}
	info := SPSInfo{Profile: nalu[1], Compat: nalu[2], Level: nalu[3]}

	r := bitReader{data: unescapeRBSP(nalu[4:])}
	r.ue() // seq_parameter_set_id

	// Defaults for profiles that do not code chroma_format_idc (7.3.2.1.1):
	// 4:2:0 with luma and chroma coded together.
	chromaFormatIDC := uint32(1)
	separateColourPlane := false

	switch info.Profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		chromaFormatIDC = r.ue()
		if chromaFormatIDC == 3 {
			separateColourPlane = r.flag()
		}
		r.ue()        // bit_depth_luma_minus8
		r.ue()        // bit_depth_chroma_minus8
		r.bit()       // qpprime_y_zero_transform_bypass_flag
		if r.flag() { // seq_scaling_matrix_present_flag
			lists := 8
			if chromaFormatIDC == 3 {
				lists = 12
			}
			for i := 0; i < lists; i++ {
				if !r.flag() {
					continue
				}
				size := 16
				if i >= 6 {
					size = 64
				}
				skipScalingList(&r, size)
			}
		}
	}

	r.ue()          // log2_max_frame_num_minus4
	switch r.ue() { // pic_order_cnt_type
	case 0:
		r.ue() // log2_max_pic_order_cnt_lsb_minus4
	case 1:
		r.bit()     // delta_pic_order_always_zero_flag
		r.se()      // offset_for_non_ref_pic
		r.se()      // offset_for_top_to_bottom_field
		n := r.ue() // num_ref_frames_in_pic_order_cnt_cycle
		if n > 255 {
			return SPSInfo{}, ErrInvalidSPS
		}
		for range n {
			r.se() // offset_for_ref_frame
		}
	}
	r.ue()  // max_num_ref_frames
	r.bit() // gaps_in_frame_num_value_allowed_flag

	widthMBs := r.ue() + 1  // pic_width_in_mbs_minus1
	heightMUs := r.ue() + 1 // pic_height_in_map_units_minus1
	frameMBsOnly := r.flag()
	if !frameMBsOnly {
		r.bit() // mb_adaptive_frame_field_flag
	}
	r.bit() // direct_8x8_inference_flag

	var cropL, cropR, cropT, cropB uint32
	if r.flag() { // frame_cropping_flag
		cropL, cropR, cropT, cropB = r.ue(), r.ue(), r.ue(), r.ue()
	}
	if r.err {
		return SPSInfo{}, ErrInvalidSPS
	}

	// Crop offsets count chroma sample units per Table 6-1; monochrome and
	// separate-colour-plane content crops in luma samples. Field-coded content
	// (frame_mbs_only_flag == 0) doubles both the frame height in map units
	// and the vertical crop unit.
	unitX, unitY := uint32(1), uint32(1)
	if !separateColourPlane {
		switch chromaFormatIDC {
		case 1:
			unitX, unitY = 2, 2
		case 2:
			unitX, unitY = 2, 1
		}
	}
	frameHeightMul := uint32(2)
	if frameMBsOnly {
		frameHeightMul = 1
	}
	unitY *= frameHeightMul

	width := widthMBs * 16
	height := frameHeightMul * heightMUs * 16
	cropX := unitX * (cropL + cropR)
	cropY := unitY * (cropT + cropB)
	if width > 16384 || height > 16384 || cropX >= width || cropY >= height {
		return SPSInfo{}, ErrInvalidSPS
	}
	info.Width = uint16(width - cropX)
	info.Height = uint16(height - cropY)
	return info, nil
}

// skipScalingList consumes one scaling list (7.3.2.1.1.1); only the bit
// positions matter here, not the scales.
func skipScalingList(r *bitReader, size int) {
	lastScale, nextScale := int32(8), int32(8)
	for range size {
		if nextScale != 0 {
			nextScale = (lastScale + r.se() + 256) % 256
		}
		if nextScale != 0 {
			lastScale = nextScale
		}
	}
}

// unescapeRBSP removes H.264 emulation-prevention bytes: each 0x03 that
// follows two zero bytes is dropped.
func unescapeRBSP(b []byte) []byte {
	out := make([]byte, 0, len(b))
	zeros := 0
	for _, c := range b {
		if zeros == 2 && c == 3 {
			zeros = 0
			continue
		}
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

// bitReader reads bits most-significant first. Reading past the end sets err
// and returns zeros, so a parse over truncated input fails once at the end
// instead of needing a check per read.
type bitReader struct {
	data []byte
	pos  int // bit position
	err  bool
}

func (r *bitReader) bit() uint32 {
	if r.pos >= len(r.data)*8 {
		r.err = true
		return 0
	}
	b := r.data[r.pos>>3] >> (7 - uint(r.pos&7)) & 1
	r.pos++
	return uint32(b)
}

func (r *bitReader) flag() bool { return r.bit() == 1 }

// ue reads one unsigned exp-golomb code.
func (r *bitReader) ue() uint32 {
	zeros := 0
	for r.bit() == 0 {
		zeros++
		if zeros > 31 || r.err {
			r.err = true
			return 0
		}
	}
	v := uint32(1)
	for i := 0; i < zeros; i++ {
		v = v<<1 | r.bit()
	}
	return v - 1
}

// se reads one signed exp-golomb code.
func (r *bitReader) se() int32 {
	u := r.ue()
	if u&1 == 1 {
		return int32(u/2 + 1)
	}
	return -int32(u / 2)
}
