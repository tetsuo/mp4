// Package bmff implements encoding and decoding of ISO Base Media File Format (ISOBMFF) boxes.
package bmff

// BoxType is a 4-byte box type identifier.
type BoxType [4]byte

func (t BoxType) String() string {
	return string(t[:])
}

// Known box types.
var (
	TypeFtyp = BoxType{'f', 't', 'y', 'p'}
	TypeStyp = BoxType{'s', 't', 'y', 'p'} // Segment type box (used in fragmented MP4)
	TypeMoov = BoxType{'m', 'o', 'o', 'v'}
	TypeMvhd = BoxType{'m', 'v', 'h', 'd'}
	TypeTrak = BoxType{'t', 'r', 'a', 'k'}
	TypeTkhd = BoxType{'t', 'k', 'h', 'd'}
	TypeTref = BoxType{'t', 'r', 'e', 'f'}
	TypeTrgr = BoxType{'t', 'r', 'g', 'r'}
	TypeEdts = BoxType{'e', 'd', 't', 's'}
	TypeElst = BoxType{'e', 'l', 's', 't'}
	TypeMdia = BoxType{'m', 'd', 'i', 'a'}
	TypeMdhd = BoxType{'m', 'd', 'h', 'd'}
	TypeHdlr = BoxType{'h', 'd', 'l', 'r'}
	TypeElng = BoxType{'e', 'l', 'n', 'g'}
	TypeMinf = BoxType{'m', 'i', 'n', 'f'}
	TypeVmhd = BoxType{'v', 'm', 'h', 'd'}
	TypeSmhd = BoxType{'s', 'm', 'h', 'd'}
	TypeHmhd = BoxType{'h', 'm', 'h', 'd'}
	TypeSthd = BoxType{'s', 't', 'h', 'd'}
	TypeNmhd = BoxType{'n', 'm', 'h', 'd'}
	TypeDinf = BoxType{'d', 'i', 'n', 'f'}
	TypeDref = BoxType{'d', 'r', 'e', 'f'}
	TypeStbl = BoxType{'s', 't', 'b', 'l'}
	TypeStsd = BoxType{'s', 't', 's', 'd'}
	TypeStts = BoxType{'s', 't', 't', 's'}
	TypeCtts = BoxType{'c', 't', 't', 's'}
	TypeCslg = BoxType{'c', 's', 'l', 'g'}
	TypeStsc = BoxType{'s', 't', 's', 'c'}
	TypeStsz = BoxType{'s', 't', 's', 'z'}
	TypeStz2 = BoxType{'s', 't', 'z', '2'}
	TypeStco = BoxType{'s', 't', 'c', 'o'}
	TypeCo64 = BoxType{'c', 'o', '6', '4'}
	TypeStss = BoxType{'s', 't', 's', 's'}
	TypeStsh = BoxType{'s', 't', 's', 'h'}
	TypePadb = BoxType{'p', 'a', 'd', 'b'}
	TypeStdp = BoxType{'s', 't', 'd', 'p'}
	TypeSdtp = BoxType{'s', 'd', 't', 'p'}
	TypeSbgp = BoxType{'s', 'b', 'g', 'p'}
	TypeSgpd = BoxType{'s', 'g', 'p', 'd'}
	TypeSubs = BoxType{'s', 'u', 'b', 's'}
	TypeSaiz = BoxType{'s', 'a', 'i', 'z'}
	TypeSaio = BoxType{'s', 'a', 'i', 'o'}
	// Fragment movie boxes
	TypeMvex = BoxType{'m', 'v', 'e', 'x'}
	TypeMehd = BoxType{'m', 'e', 'h', 'd'}
	TypeTrex = BoxType{'t', 'r', 'e', 'x'}
	TypeLeva = BoxType{'l', 'e', 'v', 'a'}
	TypeMoof = BoxType{'m', 'o', 'o', 'f'}
	TypeMfhd = BoxType{'m', 'f', 'h', 'd'}
	TypeTraf = BoxType{'t', 'r', 'a', 'f'}
	TypeTfhd = BoxType{'t', 'f', 'h', 'd'}
	TypeTfdt = BoxType{'t', 'f', 'd', 't'}
	TypeTrun = BoxType{'t', 'r', 'u', 'n'}
	TypeSidx = BoxType{'s', 'i', 'd', 'x'} // Segment index box
	TypeEmsg = BoxType{'e', 'm', 's', 'g'} // Event message box
	// Metadata boxes
	TypeMeta = BoxType{'m', 'e', 't', 'a'}
	TypeUdta = BoxType{'u', 'd', 't', 'a'}
	// Data boxes
	TypeMdat = BoxType{'m', 'd', 'a', 't'}
	TypeFree = BoxType{'f', 'r', 'e', 'e'}
	TypeSkip = BoxType{'s', 'k', 'i', 'p'}
	// Sample entry boxes
	TypeAvc1 = BoxType{'a', 'v', 'c', '1'}
	TypeAvcC = BoxType{'a', 'v', 'c', 'C'}
	TypeBtrt = BoxType{'b', 't', 'r', 't'} // MPEG-4 Bit rate box
	TypePasp = BoxType{'p', 'a', 's', 'p'} // Pixel aspect ratio box
	TypeMp4a = BoxType{'m', 'p', '4', 'a'}
	TypeEsds = BoxType{'e', 's', 'd', 's'}
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
