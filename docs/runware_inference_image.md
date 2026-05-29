## runware inference image

Generate images from text or image input

### Synopsis

Generate images using text-to-image, image-to-image, or inpainting.

```
runware inference image [prompt] [flags]
```

### Examples

```
  # generate image from text
  runware inference image "a cat riding a rocket"

  # image-to-image with source
  runware inference image "make it cinematic" --source ./input.png --strength 0.7

  # inpainting with mask
  runware inference image "replace with a dog" --source ./photo.png --mask ./mask.png
```

### Options

```
  -c, --cfg float                   CFG scale
  -n, --count int                   Number of images to generate (default 1)
  -T, --download-timeout duration   Timeout for downloading image results (default 1m0s)
  -X, --dry-run                     Print the API request without executing
  -H, --height int                  Image height
  -h, --help                        help for image
  -M, --mask string                 Mask image path for inpainting
  -m, --model string                Model identifier
  -N, --negative string             Negative prompt
  -D, --no-download                 Print image URLs instead of downloading
  -o, --output string               Output directory
  -f, --output-format string        Format of generated images: png, jpg, webp
  -p, --preset string               Named preset to apply
  -S, --scheduler string            Scheduler (e.g. euler, dpm++)
  -e, --seed int                    Seed for reproducibility
  -i, --source string               Source image path for img2img
  -s, --steps int                   Number of inference steps
  -R, --strength float              img2img strength (0.0-1.0) (default 0.7)
  -W, --width int                   Image width
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware inference](runware_inference.md)	 - Run inference tasks

