// Command mp4probe gathers information about tracks and keyframe distribution from an MP4 file.
package main

import (
	"fmt"
	"os"

	"github.com/tetsuo/mp4/remux"
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

	remuxer, err := remux.NewRemuxer(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for i, track := range remuxer.Tracks {
		fmt.Printf("Track %d: %s\n", i, track.Codec)
		fmt.Printf("  Total samples: %d\n", len(track.Samples))
		fmt.Printf("  Duration: %.2fs\n", track.Duration())
		fmt.Printf("  TimeScale: %d\n\n", track.TimeScale)

		// Count keyframes
		keyframes := 0
		var prevKfTime float64
		var intervals []float64

		fmt.Println("  Keyframes:")
		for j, s := range track.Samples {
			if s.Sync {
				pts := float64(s.DTS+int64(s.PresentationOffset)) / float64(track.TimeScale)
				fmt.Printf("    [%5d] %.3fs", j, pts)

				if keyframes > 0 {
					interval := pts - prevKfTime
					intervals = append(intervals, interval)
					fmt.Printf(" (%.3fs since last)", interval)
				}
				fmt.Println()

				prevKfTime = pts
				keyframes++

				if keyframes >= 20 {
					fmt.Printf("    ... (%d more keyframes)\n", countRemainingKeyframes(track.Samples[j+1:]))
					break
				}
			}
		}

		fmt.Printf("\n  Total keyframes: %d\n", countTotalKeyframes(track.Samples))
		if len(intervals) > 0 {
			avg := average(intervals)
			min := minimum(intervals)
			max := maximum(intervals)
			fmt.Printf("  Keyframe interval: avg=%.3fs min=%.3fs max=%.3fs\n", avg, min, max)
		}
		fmt.Println()
	}
}

func countRemainingKeyframes(samples []remux.Sample) int {
	count := 0
	for _, s := range samples {
		if s.Sync {
			count++
		}
	}
	return count
}

func countTotalKeyframes(samples []remux.Sample) int {
	count := 0
	for _, s := range samples {
		if s.Sync {
			count++
		}
	}
	return count
}

func average(vals []float64) float64 {
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func minimum(vals []float64) float64 {
	min := vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
	}
	return min
}

func maximum(vals []float64) float64 {
	max := vals[0]
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	return max
}
