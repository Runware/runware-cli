## runware inference video

Generate videos from text or image input

### Synopsis

Generate videos using text-to-video or image-to-video.

Examples:
  runware inference video "a timelapse of a sunset over mountains" --model klingai:5@3
  runware inference video "a cat playing piano" --model google:3@2 --duration 4
  runware inference video "animate this scene" --model klingai:5@3 --source ./photo.png

```
runware inference video [prompt] [flags]
```

### Options

```
  -c, --cfg float                   CFG scale
  -n, --count int                   Number of videos to generate (default 1)
  -T, --download-timeout duration   Timeout for downloading video results (default 10m0s)
  -X, --dry-run                     Print the API request without executing
  -d, --duration float              Video duration in seconds
  -H, --height int                  Video height in pixels
  -h, --help                        help for video
  -C, --include-cost                Include cost info in response
  -m, --model string                Model identifier (e.g. klingai:5@3, google:3@2)
  -N, --negative string             Negative prompt
  -D, --no-download                 Print video URLs instead of downloading
  -o, --output string               Output directory
  -f, --output-format string        Format of generated videos: mp4, webm
  -I, --poll-interval duration      Polling interval for async results (default 5s)
  -p, --preset string               Named preset to apply
  -e, --seed int                    Seed for reproducibility
  -i, --source string               Source image path for image-to-video
  -L, --source-last string          Last frame image path
  -s, --steps int                   Number of inference steps
  -t, --timeout duration            Maximum wait time for video generation (default 10m0s)
  -W, --width int                   Video width in pixels
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware inference](runware_inference.md)	 - Run inference tasks

