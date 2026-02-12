// Command mp4dump reads an MP4 file and prints its box structure.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/tetsuo/mp4"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <file.mp4>\n", os.Args[0])
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	boxes, err := decodeAll(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for _, box := range boxes {
		printBox(box, 0)
	}
}

func decodeAll(data []byte) ([]*mp4.Box, error) {
	var boxes []*mp4.Box
	ptr := 0
	for ptr < len(data) {
		if len(data)-ptr < 8 {
			break
		}
		box, err := mp4.Decode(data, ptr, len(data))
		if err != nil {
			return nil, fmt.Errorf("at offset %d: %w", ptr, err)
		}
		boxes = append(boxes, box)
		ptr += int(box.Size)
	}
	return boxes, nil
}

var containerChildren = map[mp4.BoxType][]mp4.BoxType{
	mp4.TypeMoov: {mp4.TypeMvhd, mp4.TypeMeta, mp4.TypeTrak, mp4.TypeMvex},
	mp4.TypeTrak: {mp4.TypeTkhd, mp4.TypeTref, mp4.TypeTrgr, mp4.TypeEdts, mp4.TypeMeta, mp4.TypeMdia, mp4.TypeUdta},
	mp4.TypeEdts: {mp4.TypeElst},
	mp4.TypeMdia: {mp4.TypeMdhd, mp4.TypeHdlr, mp4.TypeElng, mp4.TypeMinf},
	mp4.TypeMinf: {mp4.TypeVmhd, mp4.TypeSmhd, mp4.TypeHmhd, mp4.TypeSthd, mp4.TypeNmhd, mp4.TypeDinf, mp4.TypeStbl},
	mp4.TypeDinf: {mp4.TypeDref},
	mp4.TypeStbl: {mp4.TypeStsd, mp4.TypeStts, mp4.TypeCtts, mp4.TypeCslg, mp4.TypeStsc, mp4.TypeStsz, mp4.TypeStz2, mp4.TypeStco, mp4.TypeCo64, mp4.TypeStss, mp4.TypeStsh, mp4.TypePadb, mp4.TypeStdp, mp4.TypeSdtp, mp4.TypeSbgp, mp4.TypeSgpd, mp4.TypeSubs, mp4.TypeSaiz, mp4.TypeSaio},
	mp4.TypeMvex: {mp4.TypeMehd, mp4.TypeTrex, mp4.TypeLeva},
	mp4.TypeMoof: {mp4.TypeMfhd, mp4.TypeMeta, mp4.TypeTraf},
	mp4.TypeTraf: {mp4.TypeTfhd, mp4.TypeTfdt, mp4.TypeTrun, mp4.TypeSbgp, mp4.TypeSgpd, mp4.TypeSubs, mp4.TypeSaiz, mp4.TypeSaio, mp4.TypeMeta},
}

func printBox(box *mp4.Box, depth int) {
	indent := strings.Repeat("  ", depth)
	sizeStr := fmt.Sprintf("%d", box.Size)

	extra := boxInfo(box)

	vf := ""
	if box.HasFullBox {
		vf = fmt.Sprintf(" v=%d flags=0x%06x", box.Version, box.Flags)
	}

	fmt.Printf("%s[%s] size=%s%s%s\n", indent, box.Type, sizeStr, vf, extra)

	// Container children in defined order
	if box.Children != nil {
		if order, ok := containerChildren[box.Type]; ok {
			for _, t := range order {
				for _, child := range box.ChildList(t) {
					printBox(child, depth+1)
				}
			}
		}
		for _, child := range box.OtherBoxes {
			printBox(child, depth+1)
		}
	}

	// Stsd entries
	if box.Stsd != nil {
		for _, entry := range box.Stsd.Entries {
			printBox(entry, depth+1)
		}
	}

	// Visual/audio children
	if box.Visual != nil {
		for _, child := range box.Visual.Children {
			printBox(child, depth+1)
		}
	}
	if box.Audio != nil {
		for _, child := range box.Audio.Children {
			printBox(child, depth+1)
		}
	}
}

func boxInfo(box *mp4.Box) string {
	switch {
	case box.Ftyp != nil:
		f := box.Ftyp
		brands := make([]string, len(f.CompatibleBrands))
		for i, b := range f.CompatibleBrands {
			brands[i] = string(b[:])
		}
		return fmt.Sprintf(" brand=%s ver=%d compat=[%s]", string(f.Brand[:]), f.BrandVersion, strings.Join(brands, ","))
	case box.Mvhd != nil:
		m := box.Mvhd
		return fmt.Sprintf(" timescale=%d duration=%d nextTrackId=%d", m.TimeScale, m.Duration, m.NextTrackId)
	case box.Tkhd != nil:
		t := box.Tkhd
		return fmt.Sprintf(" trackId=%d duration=%d size=%dx%d", t.TrackId, t.Duration, t.TrackWidth>>16, t.TrackHeight>>16)
	case box.Mdhd != nil:
		m := box.Mdhd
		return fmt.Sprintf(" timescale=%d duration=%d lang=%d", m.TimeScale, m.Duration, m.Language)
	case box.Hdlr != nil:
		h := box.Hdlr
		return fmt.Sprintf(" type=%s name=%q", string(h.HandlerType[:]), h.Name)
	case box.Stsd != nil:
		return fmt.Sprintf(" entries=%d", len(box.Stsd.Entries))
	case box.Stsz != nil:
		return fmt.Sprintf(" entries=%d", len(box.Stsz.Entries))
	case box.Stco != nil:
		return fmt.Sprintf(" entries=%d", len(box.Stco.Entries))
	case box.Co64 != nil:
		return fmt.Sprintf(" entries=%d", len(box.Co64.Entries))
	case box.Stts != nil:
		return fmt.Sprintf(" entries=%d", len(box.Stts.Entries))
	case box.Ctts != nil:
		return fmt.Sprintf(" entries=%d", len(box.Ctts.Entries))
	case box.Stsc != nil:
		return fmt.Sprintf(" entries=%d", len(box.Stsc.Entries))
	case box.Elst != nil:
		return fmt.Sprintf(" entries=%d", len(box.Elst.Entries))
	case box.Dref != nil:
		return fmt.Sprintf(" entries=%d", len(box.Dref.Entries))
	case box.Visual != nil:
		v := box.Visual
		return fmt.Sprintf(" %dx%d compressor=%q children=%d", v.Width, v.Height, v.CompressorName, len(v.Children))
	case box.Audio != nil:
		a := box.Audio
		return fmt.Sprintf(" ch=%d sampleSize=%d sampleRate=%d children=%d", a.ChannelCount, a.SampleSize, a.SampleRate, len(a.Children))
	case box.AvcC != nil:
		return fmt.Sprintf(" mimeCodec=%s bufLen=%d", box.AvcC.MimeCodec, len(box.AvcC.Buffer))
	case box.Esds != nil:
		return fmt.Sprintf(" mimeCodec=%s bufLen=%d", box.Esds.MimeCodec, len(box.Esds.Buffer))
	case box.Mdat != nil:
		return fmt.Sprintf(" dataLen=%d", len(box.Mdat.Buffer))
	case box.Mfhd != nil:
		return fmt.Sprintf(" seq=%d", box.Mfhd.SequenceNumber)
	case box.Buffer != nil:
		return fmt.Sprintf(" (raw %d bytes)", len(box.Buffer))
	}
	return ""
}
