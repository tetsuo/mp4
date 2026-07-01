// Package mp4 provides support for the ISO base media file format and
// fragmented MP4 streams.
package mp4

// BoxType is a 4-byte box type identifier.
type BoxType [4]byte

func (t BoxType) String() string {
	return string(t[:])
}

// Known box types.
var (
	TypeFtyp = BoxType{'f', 't', 'y', 'p'} // File type and compatibility
	TypeStyp = BoxType{'s', 't', 'y', 'p'} // Segment type (fragmented MP4)
)

// Movie structure boxes (moov and children).
var (
	TypeMoov = BoxType{'m', 'o', 'o', 'v'} // Movie metadata container
	TypeMvhd = BoxType{'m', 'v', 'h', 'd'} // Movie header (timescale, duration)
	TypeTrak = BoxType{'t', 'r', 'a', 'k'} // Track container
	TypeTkhd = BoxType{'t', 'k', 'h', 'd'} // Track header (ID, dimensions)
	TypeTref = BoxType{'t', 'r', 'e', 'f'} // Track reference container
	TypeTrgr = BoxType{'t', 'r', 'g', 'r'} // Track grouping indication
	TypeEdts = BoxType{'e', 'd', 't', 's'} // Edit list container
	TypeElst = BoxType{'e', 'l', 's', 't'} // Edit list entries
	TypeMdia = BoxType{'m', 'd', 'i', 'a'} // Media information container
	TypeMdhd = BoxType{'m', 'd', 'h', 'd'} // Media header (timescale, duration)
	TypeHdlr = BoxType{'h', 'd', 'l', 'r'} // Handler reference (vide/soun)
	TypeElng = BoxType{'e', 'l', 'n', 'g'} // Extended language tag
	TypeMinf = BoxType{'m', 'i', 'n', 'f'} // Media information container
	TypeVmhd = BoxType{'v', 'm', 'h', 'd'} // Video media header
	TypeSmhd = BoxType{'s', 'm', 'h', 'd'} // Sound media header
	TypeHmhd = BoxType{'h', 'm', 'h', 'd'} // Hint media header
	TypeSthd = BoxType{'s', 't', 'h', 'd'} // Subtitle media header
	TypeNmhd = BoxType{'n', 'm', 'h', 'd'} // Null media header
	TypeDinf = BoxType{'d', 'i', 'n', 'f'} // Data information container
	TypeDref = BoxType{'d', 'r', 'e', 'f'} // Data reference (URL/URN entries)
)

// Sample table boxes (stbl children).
var (
	TypeStbl = BoxType{'s', 't', 'b', 'l'} // Sample table container
	TypeStsd = BoxType{'s', 't', 's', 'd'} // Sample descriptions (codec config)
	TypeStts = BoxType{'s', 't', 't', 's'} // Decoding time-to-sample
	TypeCtts = BoxType{'c', 't', 't', 's'} // Composition time-to-sample
	TypeCslg = BoxType{'c', 's', 'l', 'g'} // Composition to decode timeline mapping
	TypeStsc = BoxType{'s', 't', 's', 'c'} // Sample-to-chunk mapping
	TypeStsz = BoxType{'s', 't', 's', 'z'} // Sample sizes
	TypeStz2 = BoxType{'s', 't', 'z', '2'} // Compact sample sizes
	TypeStco = BoxType{'s', 't', 'c', 'o'} // Chunk offsets (32-bit)
	TypeCo64 = BoxType{'c', 'o', '6', '4'} // Chunk offsets (64-bit)
	TypeStss = BoxType{'s', 't', 's', 's'} // Sync sample table (keyframes)
	TypeStsh = BoxType{'s', 't', 's', 'h'} // Shadow sync sample table
	TypePadb = BoxType{'p', 'a', 'd', 'b'} // Padding bits
	TypeStdp = BoxType{'s', 't', 'd', 'p'} // Sample degradation priority
	TypeSdtp = BoxType{'s', 'd', 't', 'p'} // Sample dependency type
	TypeSbgp = BoxType{'s', 'b', 'g', 'p'} // Sample-to-group
	TypeSgpd = BoxType{'s', 'g', 'p', 'd'} // Sample group description
	TypeSubs = BoxType{'s', 'u', 'b', 's'} // Sub-sample information
	TypeSaiz = BoxType{'s', 'a', 'i', 'z'} // Sample auxiliary information sizes
	TypeSaio = BoxType{'s', 'a', 'i', 'o'} // Sample auxiliary information offsets
)

// Fragment boxes (moof and children, mvex).
var (
	TypeMvex = BoxType{'m', 'v', 'e', 'x'} // Movie extends (signals fragmented file)
	TypeMehd = BoxType{'m', 'e', 'h', 'd'} // Movie extends header (fragment duration)
	TypeTrex = BoxType{'t', 'r', 'e', 'x'} // Track extends defaults
	TypeLeva = BoxType{'l', 'e', 'v', 'a'} // Level assignment
	TypeMoof = BoxType{'m', 'o', 'o', 'f'} // Movie fragment container
	TypeMfhd = BoxType{'m', 'f', 'h', 'd'} // Movie fragment header (sequence number)
	TypeTraf = BoxType{'t', 'r', 'a', 'f'} // Track fragment container
	TypeTfhd = BoxType{'t', 'f', 'h', 'd'} // Track fragment header
	TypeTfdt = BoxType{'t', 'f', 'd', 't'} // Track fragment decode time
	TypeTrun = BoxType{'t', 'r', 'u', 'n'} // Track run (per-sample metadata)
	TypeSidx = BoxType{'s', 'i', 'd', 'x'} // Segment index
	TypeEmsg = BoxType{'e', 'm', 's', 'g'} // Event message
)

// Metadata boxes.
var (
	TypeMeta = BoxType{'m', 'e', 't', 'a'} // Metadata container
	TypeUdta = BoxType{'u', 'd', 't', 'a'} // User data container
)

// Data boxes.
var (
	TypeMdat = BoxType{'m', 'd', 'a', 't'} // Media data payload
	TypeFree = BoxType{'f', 'r', 'e', 'e'} // Free space (can be skipped)
	TypeSkip = BoxType{'s', 'k', 'i', 'p'} // Free space (can be skipped)
)

// Sample entry boxes (children of stsd).
var (
	TypeAvc1 = BoxType{'a', 'v', 'c', '1'} // AVC/H.264 visual sample entry
	TypeAvcC = BoxType{'a', 'v', 'c', 'C'} // AVC decoder configuration record
	TypeAv01 = BoxType{'a', 'v', '0', '1'} // AV1 visual sample entry
	TypeAv1C = BoxType{'a', 'v', '1', 'C'} // AV1 codec configuration record
	TypeBtrt = BoxType{'b', 't', 'r', 't'} // MPEG-4 bit rate
	TypePasp = BoxType{'p', 'a', 's', 'p'} // Pixel aspect ratio
	TypeMp4a = BoxType{'m', 'p', '4', 'a'} // MPEG-4 audio sample entry
	TypeEsds = BoxType{'e', 's', 'd', 's'} // ES descriptor
)

// IsFullBox returns true if the box type has version and flags fields.
func IsFullBox(t BoxType) bool {
	switch t {
	case TypeMvhd, TypeTkhd, TypeMdhd, TypeHdlr,
		TypeVmhd, TypeSmhd, TypeDref, TypeStsd,
		TypeStts, TypeCtts, TypeStsc, TypeStsz,
		TypeStco, TypeCo64, TypeStss, TypeElst,
		TypeMeta, TypeEsds, TypeMehd, TypeTrex,
		TypeMfhd, TypeTfhd, TypeTfdt, TypeTrun,
		TypeSbgp, TypeSgpd, TypeSaiz, TypeSaio,
		TypeCslg, TypeSdtp, TypeSidx, TypeEmsg:
		return true
	}
	return false
}

// IsContainerBox returns true if the box type is a container that holds child boxes.
func IsContainerBox(t BoxType) bool {
	switch t {
	case TypeMoov, TypeTrak, TypeEdts, TypeMdia,
		TypeMinf, TypeDinf, TypeStbl, TypeUdta,
		TypeMeta, TypeMvex, TypeMoof, TypeTraf,
		TypeTref, TypeTrgr:
		return true
	}
	return false
}
