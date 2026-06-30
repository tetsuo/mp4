// Command mp4probe displays information about MP4 tracks and keyframes.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tetsuo/mp4/fragment"
	"github.com/tetsuo/mp4/track"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <file.mp4>\n", os.Args[0])
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	fr, initSeg, err := fragment.NewReader(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Tracks: %d\n", len(initSeg.Tracks))
	for _, t := range initSeg.Tracks {
		kind := "video"
		if t.Kind == track.TrackAudio {
			kind = "audio"
		}
		fmt.Printf("\n%s Track (ID=%d):\n", kind, t.ID)
		fmt.Printf("  Codec: %s\n", t.Codec())
		fmt.Printf("  Timescale: %d\n", t.TimeScale)
		seconds := 0.0
		if t.TimeScale > 0 {
			seconds = float64(t.Duration) / float64(t.TimeScale)
		}
		fmt.Printf("  Duration: %d ticks (%.2fs)\n", t.Duration, seconds)

		if t.Kind == track.TrackVideo {
			fmt.Printf("  Resolution: %dx%d\n", t.Width, t.Height)
		} else {
			fmt.Printf("  Channels: %d\n", t.ChannelCount)
			fmt.Printf("  Sample Rate: %d\n", t.SampleRate)
		}
	}

	// Read and analyze fragments
	fmt.Printf("\nFragments:\n")
	fragCount := 0
	totalSamples := 0
	totalVideoSync := 0

	for {
		fr, err := fr.ReadFragment()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		videoSamples := 0
		audioSamples := 0
		syncSamples := 0

		for _, s := range fr.Samples {
			t := track.FindTrack(initSeg.Tracks, s.TrackID)
			if t != nil && t.Kind == track.TrackVideo {
				videoSamples++
				if s.IsSync() {
					syncSamples++
				}
			} else {
				audioSamples++
			}
		}

		fmt.Printf("  Fragment %d: %d video (%d sync), %d audio\n",
			fr.SequenceNum, videoSamples, syncSamples, audioSamples)

		fragCount++
		totalSamples += len(fr.Samples)
		totalVideoSync += syncSamples
	}

	fmt.Printf("\nSummary: %d fragments, %d samples, %d video keyframes\n",
		fragCount, totalSamples, totalVideoSync)
}
