package track

import "testing"

func TestAppendAv1CProfile(t *testing.T) {
	tests := []struct {
		name string
		rec  []byte
		want string
	}{
		// rec[1] = seq_profile<<5 | seq_level_idx
		// rec[2] = seq_tier<<7 | high_bitdepth<<6 | twelve_bit<<5
		{"main 8-bit level 8", []byte{0x81, 0x08, 0x00, 0x00}, "av01.0.08M.08"},
		{"main 8-bit level 12", []byte{0x81, 0x0c, 0x00, 0x00}, "av01.0.12M.08"},
		{"high tier 10-bit", []byte{0x81, 0x0c, 0xc0, 0x00}, "av01.0.12H.10"},
		{"profile 2 12-bit", []byte{0x81, 0x48, 0x60, 0x00}, "av01.2.08M.12"},
	}
	for _, tt := range tests {
		tr := &Track{}
		tr.setCodec("av01")
		tr.appendAv1CProfile(tt.rec)
		got := string(tr.raw.codecBuf[:tr.raw.codecLen])
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
