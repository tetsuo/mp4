# mp4probe

Command `mp4probe` gathers information about tracks and keyframe distribution from an MP4 file.

## Usage

```
mp4probe <file.mp4>
```

Sample output:

```
Track 0: avc1.640028
  Total samples: 720
  Duration: 30.00s
  TimeScale: 12288

  Keyframes:
    [    0] 0.083s
    [  250] 10.500s (10.417s since last)
    [  285] 11.958s (1.458s since last)
    [  378] 15.833s (3.875s since last)
    [  553] 23.125s (7.292s since last)

  Total keyframes: 5
  Keyframe interval: avg=5.760s min=1.458s max=10.417s

Track 1: mp4a.40.2
  Total samples: 1408
  Duration: 30.02s
  TimeScale: 48000

  Keyframes:
    [    0] 0.000s
    [    1] 0.021s (0.021s since last)
    [    2] 0.043s (0.021s since last)
    [    3] 0.064s (0.021s since last)
    [    4] 0.085s (0.021s since last)
    [    5] 0.107s (0.021s since last)
    [    6] 0.128s (0.021s since last)
    [    7] 0.149s (0.021s since last)
    [    8] 0.171s (0.021s since last)
    [    9] 0.192s (0.021s since last)
    [   10] 0.213s (0.021s since last)
    [   11] 0.235s (0.021s since last)
    [   12] 0.256s (0.021s since last)
    [   13] 0.277s (0.021s since last)
    [   14] 0.299s (0.021s since last)
    [   15] 0.320s (0.021s since last)
    [   16] 0.341s (0.021s since last)
    [   17] 0.363s (0.021s since last)
    [   18] 0.384s (0.021s since last)
    [   19] 0.405s (0.021s since last)
    ... (1388 more keyframes)

  Total keyframes: 1408
  Keyframe interval: avg=0.021s min=0.021s max=0.021s
```
