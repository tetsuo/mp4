// Command mp4remux converts MP4 files to fragmented MP4 streams.
//
// Usage:
//
//	mp4remux input.mp4 -out output.mp4 [-start 0] [-end 0]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	mf "github.com/tetsuo/mp4"
	"github.com/tetsuo/mp4/fragment"
	"github.com/tetsuo/mp4/track"
)

func main() {
	var (
		outFile   string
		startTime float64
		endTime   float64
		verify    bool
	)

	fs := flag.NewFlagSet("mp4remux", flag.ExitOnError)
	fs.StringVar(&outFile, "out", "", "output file path")
	fs.Float64Var(&startTime, "start", 0, "start time in seconds")
	fs.Float64Var(&endTime, "end", 0, "end time in seconds (0 = entire file)")
	fs.BoolVar(&verify, "verify", false, "verify output box structure after remux")

	var inputFile string
	rest := os.Args[1:]
	for {
		fs.Parse(rest)
		if fs.NArg() == 0 {
			break
		}
		if inputFile == "" {
			inputFile = fs.Arg(0)
		}
		rest = fs.Args()[1:]
	}

	if inputFile == "" || outFile == "" {
		usage()
	}

	// Open input file
	f, err := os.Open(inputFile)
	if err != nil {
		fatal("cannot open input: %v", err)
	}
	defer f.Close()

	// Create fragmenter
	frag, initSeg, err := fragment.NewReader(f)
	if err != nil {
		fatal("cannot read init: %v", err)
	}

	// Set time range
	if startTime > 0 || endTime > 0 {
		if err := frag.SetTimeRange(startTime, endTime); err != nil {
			fatal("invalid time range: %v", err)
		}
	}

	fmt.Printf("Tracks: %d\n", len(initSeg.Tracks))
	for _, t := range initSeg.Tracks {
		kind := "video"
		if t.Kind == track.TrackAudio {
			kind = "audio"
		}
		fmt.Printf("  %s: %s (id=%d, timescale=%d)\n", kind, t.Codec(), t.ID, t.TimeScale)
	}
	fmt.Printf("Start: %.2fs, End: %.2fs\n\n", startTime, endTime)

	// Create output file
	out, err := os.Create(outFile)
	if err != nil {
		fatal("cannot create output: %v", err)
	}
	defer out.Close()

	// Create writer and write init
	w := fragment.NewWriter(out)
	if err := w.WriteInit(initSeg); err != nil {
		fatal("cannot write init: %v", err)
	}

	// Stream fragments
	fragCount := 0
	totalSamples := 0

	for {
		fr, err := frag.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			fatal("read fragment error: %v", err)
		}

		if err := w.WriteFragment(fr, f); err != nil {
			fatal("write fragment error: %v", err)
		}

		fragCount++
		totalSamples += len(fr.Samples)

		fmt.Printf("  Fragment %d: %d samples\n", fr.SequenceNum, len(fr.Samples))
	}

	fmt.Printf("\nWrote %d fragments, %d samples to %s\n", fragCount, totalSamples, outFile)

	if verify {
		verifyOutput(out)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: mp4remux input.mp4 -out output.mp4 [-start 0] [-end 0]\n")
	os.Exit(1)
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func verifyOutput(f *os.File) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fmt.Fprintf(os.Stderr, "warning: verify seek failed: %v\n", err)
		return
	}

	moofCount := 0
	mdatCount := 0
	maxTrafs := 0
	var moofBuf []byte

	sc := mf.NewScanner(f)
	for sc.Next() {
		e := sc.Entry()
		switch e.Type {
		case mf.TypeMoof:
			moofCount++
			need := int(e.DataSize())
			if need > 0 {
				if cap(moofBuf) < need {
					moofBuf = make([]byte, need)
				} else {
					moofBuf = moofBuf[:need]
				}
				if err := sc.ReadBody(moofBuf); err != nil {
					fmt.Fprintf(os.Stderr, "warning: verify read moof failed: %v\n", err)
					continue
				}
				r := mf.NewReader(moofBuf)
				trafCount := 0
				for r.Next() {
					if r.Type() == mf.TypeTraf {
						trafCount++
					}
				}
				if trafCount > maxTrafs {
					maxTrafs = trafCount
				}
			}
		case mf.TypeMdat:
			mdatCount++
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: verify scan failed: %v\n", err)
	}

	fmt.Printf("\nOutput structure: %d moof, %d mdat, max %d trafs/moof\n", moofCount, mdatCount, maxTrafs)
}
