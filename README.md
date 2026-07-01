# mp4

ISOBMFF/MP4 container library with fragmented MP4 support.

## Install

```sh
go get github.com/tetsuo/mp4
```

## Usage

### Reading boxes

`Scanner` walks the top-level boxes of a file without loading their contents.
Load only the boxes you need, then use `Reader` to descend into a box and read
typed fields.

```go
f, _ := os.Open("video.mp4")
defer f.Close()

sc := mp4.NewScanner(f)
for sc.Next() {
    e := sc.Entry()
    fmt.Printf("%s size=%d offset=%d\n", e.Type, e.Size, e.Offset)

    if e.Type == mp4.TypeMoov {
        buf := make([]byte, e.DataSize())
        if err := sc.ReadBody(buf); err != nil {
            log.Fatal(err)
        }

        r := mp4.NewReader(buf)
        r.Enter() // descend into moov
        for r.Next() {
            if r.Type() == mp4.TypeMvhd {
                timescale, duration, _ := r.ReadMvhd()
                fmt.Printf("timescale=%d duration=%d\n", timescale, duration)
            }
        }
        r.Exit()
    }
}
if err := sc.Err(); err != nil {
    log.Fatal(err)
}
```

### Writing boxes

`Writer` encodes boxes into a caller-provided buffer. `StartBox` and `EndBox`
handle size backpatching for nested boxes:

```go
buf := make([]byte, 0, 1024)
w := mp4.NewWriter(buf)

w.WriteFtyp([4]byte{'i', 's', 'o', 'm'}, 0, [][4]byte{{'m', 'p', '4', '2'}})

w.StartBox(mp4.TypeMoov)
w.WriteMvhd(1000, 30000, 3)
w.EndBox()

if err := w.Err(); err != nil {
    log.Fatal(err)
}
output := w.Bytes()
```

### Parsing tracks

The `track` package resolves the sample tables inside a `moov` box into a flat
list of samples. Each sample carries its byte offset, size, decode and
presentation timestamps, and sync (keyframe) flag.

```go
tracks, duration, err := track.ParseTracks(moovBuf)
if err != nil {
    log.Fatal(err)
}

for _, t := range tracks {
    fmt.Printf("track %d: codec=%s samples=%d\n", t.ID, t.Codec(), len(t.Samples))
    for _, s := range t.Samples {
        _ = s.Offset   // byte offset in the file
        _ = s.Size()   // sample size in bytes
        _ = s.PTS()    // presentation timestamp
        _ = s.IsSync() // keyframe
    }
}
```

### Fragmenting to fMP4

The `fragment` package reads a standard MP4 file and produces an init segment
(`ftyp`+`moov`) followed by media fragments (`moof`+`mdat` pairs), suitable for
HLS or DASH.

```go
f, _ := os.Open("video.mp4")
defer f.Close()

reader, initSeg, err := fragment.NewReader(f)
if err != nil {
    log.Fatal(err)
}

writer := fragment.NewWriter(os.Stdout)
if err := writer.WriteInit(initSeg); err != nil {
    log.Fatal(err)
}

for {
    frag, err := reader.ReadFragment()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    if err := writer.WriteFragment(frag, f); err != nil {
        log.Fatal(err)
    }
}
```

Use `Reader.SetTimeRange` to limit output to a time window, or `Reader.Seek` to
reposition before reading fragments.

## Examples

The [cmd](./cmd) directory contains small programs built on these packages:

- `mp4dump` prints the box structure of a file as text or JSON.
- `mp4probe` displays track and fragment information.
- `mp4remux` remuxes a file into fragmented MP4.
