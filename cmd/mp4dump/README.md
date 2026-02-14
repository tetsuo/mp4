# mp4dump

`mp4dump` reads an MP4 file and dumps its box structure in a human-readable format.

**Example:**

Pipe the output of `mp4dump` to [`indentree`](https://github.com/tetsuo/indentree) to visualize the box hierarchy:

```
mp4dump "big-buck-bunny-1080p-60fps-30sec.mp4" | indentree
```

Output:

```
.
├─ [ftyp] size=32
│  └─ brand=isom ver=512 compat=[isom,iso2,avc1,mp41]
├─ [free] size=8
├─ [mdat] size=14373924
│  └─ dataLen=14373916
└─ [moov] size=49061
   ├─ [mvhd] size=108 v=0 flags=0x000000 timescale=1000 duration=30022 nextTrackId=3
   ├─ [trak] size=36795
   │  ├─ [tkhd] size=92 v=0 flags=0x000003 trackId=1 duration=30000 size=1920x1080
   │  ├─ [edts] size=36
   │  │  └─ [elst] size=28 v=0 flags=0x000000 entries=1
   │  └─ [mdia] size=36659
   │     ├─ [mdhd] size=32 v=0 flags=0x000000 timescale=15360 duration=460800 lang=21956
   │     ├─ [hdlr] size=45 v=0 flags=0x000000 type=vide name="VideoHandler"
   │     └─ [minf] size=36574
   │        ├─ [vmhd] size=20 v=0 flags=0x000001
   │        ├─ [dinf] size=36
   │        │  └─ [dref] size=28 v=0 flags=0x000000 entries=1
   │        └─ [stbl] size=36510
   │           ├─ [stsd] size=154 v=0 flags=0x000000 entries=1
   │           │  └─ [avc1] size=138 1920x1080 compressor=""
   │           │     └─ [avcC] size=52 codec=64002a
   │           ├─ [stts] size=24 v=0 flags=0x000000 entries=1
   │           ├─ [stss] size=48 v=0 flags=0x000000 entries=8
   │           ├─ [ctts] size=13944 v=0 flags=0x000000 entries=1741
   │           ├─ [stsc] size=9472 v=0 flags=0x000000 entries=788
   │           ├─ [stsz] size=7220 v=0 flags=0x000000 entries=1800
   │           └─ [stco] size=5640 v=0 flags=0x000000 entries=1406
   ├─ [trak] size=11756
   │  ├─ [tkhd] size=92 v=0 flags=0x000003 trackId=2 duration=30022 size=0x0
   │  ├─ [edts] size=36
   │  │  └─ [elst] size=28 v=0 flags=0x000000 entries=1
   │  └─ [mdia] size=11620
   │     ├─ [mdhd] size=32 v=0 flags=0x000000 timescale=48000 duration=1441024 lang=21956
   │     ├─ [hdlr] size=45 v=0 flags=0x000000 type=soun name="SoundHandler"
   │     └─ [minf] size=11535
   │        ├─ [smhd] size=16 v=0 flags=0x000000
   │        ├─ [dinf] size=36
   │        │  └─ [dref] size=28 v=0 flags=0x000000 entries=1
   │        └─ [stbl] size=11475
   │           ├─ [stsd] size=103 v=0 flags=0x000000 entries=1
   │           │  └─ [mp4a] size=87 ch=2 sampleSize=16 sampleRate=48000
   │           │     └─ [esds] size=51 v=0 flags=0x000000 codec=40.2
   │           ├─ [stts] size=32 v=0 flags=0x000000 entries=2
   │           ├─ [stsc] size=40 v=0 flags=0x000000 entries=2
   │           ├─ [stsz] size=5652 v=0 flags=0x000000 entries=1408
   │           └─ [stco] size=5640 v=0 flags=0x000000 entries=1406
   └─ [udta] size=394
      └─ [meta] size=386 v=0 flags=0x000000
         ├─ [hdlr] size=33 v=0 flags=0x000000 type=mdir name=""
         └─ [ilst] size=341 (333 bytes)
```
