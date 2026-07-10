## runware media upload

Upload media and return its UUID

### Synopsis

Upload media to the Runware platform for use as input in other tasks
(e.g. image-to-image, upscaling, background removal).

The argument may be a local file path, a publicly accessible URL, or a data URI.
Local files are read and uploaded; URLs and data URIs are forwarded as-is. The
command prints the stored mediaUUID and mediaURL, which can be passed to media
parameters such as inputs.seedImage on the run command.

Accepts any media type: images, video, audio, 3D models, and more.

```
runware media upload <file|url> [flags]
```

### Examples

```
  # upload a local image and print its UUID
  runware media upload ./photo.jpg

  # upload a remote image by URL
  runware media upload https://example.com/photo.jpg

  # upload and use the UUID directly in a run command
  runware run runware:100@1 positivePrompt="Same scene at night" width=1024 height=1024 \
    inputs.seedImage=$(runware media upload ./photo.jpg -F json | jq -r '.mediaUUID')
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

* [runware media](runware_media.md)	 - Store and delete media in your Runware account

