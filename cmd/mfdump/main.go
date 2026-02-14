// Command mfdump reads a media file and prints its box structure.
package main

import (
	"fmt"
	"os"
	"strings"

	mf "github.com/tetsuo/bmff"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <file.mp4>\n", os.Args[0])
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	sc := mf.NewScanner(f)
	for sc.Next() {
		e := sc.Entry()
		fmt.Printf("[%s] size=%d\n", e.Type, e.Size)

		// Only load metadata boxes into memory for deep parsing
		if e.Type == mf.TypeMoov || e.Type == mf.TypeMoof {
			buf := make([]byte, e.DataSize())
			if err := sc.ReadBody(buf); err != nil {
				fmt.Fprintf(os.Stderr, "error reading %s: %v\n", e.Type, err)
				continue
			}
			r := mf.NewReader(buf)
			walk(&r, 1)
		} else if e.Type == mf.TypeFtyp {
			buf := make([]byte, e.DataSize())
			if err := sc.ReadBody(buf); err != nil {
				fmt.Fprintf(os.Stderr, "error reading ftyp: %v\n", err)
				continue
			}
			f := mf.ReadFtyp(buf)
			fmt.Printf("  brand=%s ver=%d", string(f.MajorBrand[:]), f.MinorVersion)
			if len(f.Compatible) > 0 {
				fmt.Printf(" compat=[")
				for i, c := range f.Compatible {
					if i > 0 {
						fmt.Printf(",")
					}
					fmt.Printf("%s", string(c[:]))
				}
				fmt.Printf("]")
			}
			fmt.Println()
		} else if e.Type == mf.TypeMdat {
			fmt.Printf("  dataLen=%d\n", e.DataSize())
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
		os.Exit(1)
	}
}

func walk(r *mf.Reader, depth int) {
	for r.Next() {
		indent := strings.Repeat("  ", depth)

		fmt.Printf("%s[%s] size=%d", indent, r.Type(), r.Size())

		if mf.IsFullBox(r.Type()) {
			fmt.Printf(" v=%d flags=0x%06x", r.Version(), r.Flags())
		}

		printBoxInfo(r)
		fmt.Println()

		// Descend into containers
		if mf.IsContainerBox(r.Type()) {
			r.Enter()
			walk(r, depth+1)
			r.Exit()
			continue
		}

		// Handle stsd: entry count + sub-boxes
		if r.Type() == mf.TypeStsd {
			r.Enter()
			r.Skip(4) // skip entry count
			for r.Next() {
				printSampleEntry(r, depth+1)
			}
			r.Exit()
			continue
		}
	}
}

func printSampleEntry(r *mf.Reader, depth int) {
	indent := strings.Repeat("  ", depth)

	switch r.Type() {
	case mf.TypeAvc1:
		v := mf.ReadVisualSampleEntry(r.Data())
		fmt.Printf("%s[%s] size=%d %dx%d compressor=%q\n", indent, r.Type(), r.Size(), v.Width, v.Height, v.CompressorName)
		// Enter to find avcC and other children
		r.Enter()
		r.Skip(v.ChildOffset)
		for r.Next() {
			childIndent := strings.Repeat("  ", depth+1)
			if mf.IsFullBox(r.Type()) {
				fmt.Printf("%s[%s] size=%d v=%d flags=0x%06x", childIndent, r.Type(), r.Size(), r.Version(), r.Flags())
			} else {
				fmt.Printf("%s[%s] size=%d", childIndent, r.Type(), r.Size())
			}
			if r.Type() == mf.TypeAvcC {
				codec := mf.ReadAvcC(r.Data())
				fmt.Printf(" codec=%s", codec)
			}
			fmt.Println()
		}
		r.Exit()

	case mf.TypeMp4a:
		a := mf.ReadAudioSampleEntry(r.Data())
		fmt.Printf("%s[%s] size=%d ch=%d sampleSize=%d sampleRate=%d\n", indent, r.Type(), r.Size(), a.ChannelCount, a.SampleSize, a.SampleRate>>16)
		// Enter to find esds and other children
		r.Enter()
		r.Skip(a.ChildOffset)
		for r.Next() {
			childIndent := strings.Repeat("  ", depth+1)
			if mf.IsFullBox(r.Type()) {
				fmt.Printf("%s[%s] size=%d v=%d flags=0x%06x", childIndent, r.Type(), r.Size(), r.Version(), r.Flags())
			} else {
				fmt.Printf("%s[%s] size=%d", childIndent, r.Type(), r.Size())
			}
			if r.Type() == mf.TypeEsds {
				codec := mf.ReadEsdsCodec(r.Data())
				fmt.Printf(" codec=%s", codec)
			}
			fmt.Println()
		}
		r.Exit()

	default:
		fmt.Printf("%s[%s] size=%d", indent, r.Type(), r.Size())
		if mf.IsFullBox(r.Type()) {
			fmt.Printf(" v=%d flags=0x%06x", r.Version(), r.Flags())
		}
		fmt.Printf(" (raw %d bytes)\n", len(r.Data()))
	}
}

func printBoxInfo(r *mf.Reader) {
	switch r.Type() {
	case mf.TypeFtyp:
		f := mf.ReadFtyp(r.Data())
		fmt.Printf(" brand=%s ver=%d", string(f.MajorBrand[:]), f.MinorVersion)
		if len(f.Compatible) > 0 {
			fmt.Printf(" compat=[")
			for i, c := range f.Compatible {
				if i > 0 {
					fmt.Printf(",")
				}
				fmt.Printf("%s", string(c[:]))
			}
			fmt.Printf("]")
		}

	case mf.TypeMvhd:
		ts, dur, ntid := r.ReadMvhd()
		fmt.Printf(" timescale=%d duration=%d nextTrackId=%d", ts, dur, ntid)

	case mf.TypeTkhd:
		tid, dur, w, h := r.ReadTkhd()
		fmt.Printf(" trackId=%d duration=%d size=%dx%d", tid, dur, w>>16, h>>16)

	case mf.TypeMdhd:
		ts, dur, lang := r.ReadMdhd()
		fmt.Printf(" timescale=%d duration=%d lang=%d", ts, dur, lang)

	case mf.TypeHdlr:
		ht := r.ReadHdlr()
		name := r.ReadHdlrName()
		fmt.Printf(" type=%s name=%q", string(ht[:]), name)

	case mf.TypeStsd:
		if len(r.Data()) >= 4 {
			fmt.Printf(" entries=%d", r.EntryCount())
		}

	case mf.TypeStsz:
		it := mf.NewStszIter(r.Data())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeStco, mf.TypeStss:
		it := mf.NewUint32Iter(r.Data())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeCo64:
		it := mf.NewCo64Iter(r.Data())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeStts:
		it := mf.NewSttsIter(r.Data())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeCtts:
		it := mf.NewCttsIter(r.Data(), r.Version())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeStsc:
		it := mf.NewStscIter(r.Data())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeElst:
		it := mf.NewElstIter(r.Data(), r.Version())
		fmt.Printf(" entries=%d", it.Count())

	case mf.TypeDref:
		if len(r.Data()) >= 4 {
			fmt.Printf(" entries=%d", r.EntryCount())
		}

	case mf.TypeMehd:
		dur := r.ReadMehd()
		fmt.Printf(" fragmentDuration=%d", dur)

	case mf.TypeTrex:
		tid, _, _, _, _ := r.ReadTrex()
		fmt.Printf(" trackId=%d", tid)

	case mf.TypeMfhd:
		seq := r.ReadMfhd()
		fmt.Printf(" seq=%d", seq)

	case mf.TypeTfhd:
		tid := r.ReadTfhd()
		fmt.Printf(" trackId=%d", tid)

	case mf.TypeTfdt:
		bt := r.ReadTfdt()
		fmt.Printf(" baseMediaDecodeTime=%d", bt)

	case mf.TypeTrun:
		it := mf.NewTrunIter(r.Data(), r.Flags())
		fmt.Printf(" entries=%d", it.Count())
		if r.Flags()&mf.TrunDataOffsetPresent != 0 {
			fmt.Printf(" dataOffset=%d", it.DataOffset())
		}

	case mf.TypeMdat:
		fmt.Printf(" dataLen=%d", len(r.Data()))

	case mf.TypeVmhd:
		// graphicsMode and opcolor
	case mf.TypeSmhd:
		// balance
	default:
		if !mf.IsContainerBox(r.Type()) {
			if len(r.Data()) > 0 {
				fmt.Printf(" (%d bytes)", len(r.Data()))
			}
		}
	}
}
