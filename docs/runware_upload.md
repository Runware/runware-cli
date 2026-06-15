## runware upload

Upload an asset and return its UUID

### Synopsis

Upload an image to the Runware platform for use as input in other tasks
(e.g. image-to-image, upscaling, background removal).

The argument may be a local file path, a publicly accessible URL, or a data URI.
Local files are read and uploaded; URLs and data URIs are forwarded as-is. The
command prints the uploaded imageUUID (and taskUUID), which can be passed to
image parameters such as inputs.seedImage on the run command.

Supported file types: JPEG, JPG, PNG, WEBP, BMP, GIF. Video and audio upload is
not yet supported by the API.

```
runware upload <file|url> [flags]
```

### Examples

```
  # upload a local image and print its UUID
  runware upload ./photo.jpg

  # upload a remote image by URL
  runware upload https://example.com/photo.jpg

  # upload and use the UUID directly in a run command
  runware run runware:100@1 positivePrompt="Same scene at night" \
    inputs.seedImage=$(runware upload ./photo.jpg -F json | jq -r '.imageUUID')
```

### Options

```
  -h, --help   help for upload
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware](runware.md)	 - CLI tool for the Runware inference API

