## runware inference audio

Generate audio from text descriptions

### Synopsis

Generate audio using text-to-audio, music generation, or sound effects.

Examples:
  runware inference audio "a jazz piano solo with soft drums" --model elevenlabs:1@1 --duration 30
  runware inference audio "ocean waves crashing on rocks" --model elevenlabs:1@1 --duration 60
  runware inference audio "upbeat electronic music" --model elevenlabs:1@1 --duration 120 --sample-rate 48000

```
runware inference audio [prompt] [flags]
```

### Options

```
  -b, --bitrate int                 Bitrate in kbps (32-320, compressed formats only)
  -n, --count int                   Number of audio files to generate (max 3) (default 1)
  -T, --download-timeout duration   Timeout for downloading audio results (default 5m0s)
  -X, --dry-run                     Print the API request without executing
  -d, --duration float              Audio duration in seconds (10-300) (default 10)
  -h, --help                        help for audio
  -C, --include-cost                Include cost info in response
  -m, --model string                Model identifier (e.g. elevenlabs:1@1)
  -D, --no-download                 Print audio URLs instead of downloading
  -o, --output string               Output directory
  -f, --output-format string        Format of generated audio: mp3
  -I, --poll-interval duration      Polling interval for async results (default 5s)
  -p, --preset string               Named preset to apply
  -r, --sample-rate int             Sample rate in Hz (8000-48000)
  -t, --timeout duration            Maximum wait time for audio generation (default 5m0s)
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware inference](runware_inference.md)	 - Run inference tasks

