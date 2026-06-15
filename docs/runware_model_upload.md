## runware model upload

Upload a custom model to the Runware platform

### Synopsis

Upload a custom model to the Runware platform.

The model file must be hosted at a publicly reachable URL (--download-url).
After submission the upload progresses through a processing pipeline
(validated, downloaded, optimized, stored) before the model becomes ready;
the command waits and reports each phase. On success the model's AIR
identifier is printed and the model can be used with 'runware run'.

NOTE: "model upload" is only supported by the WebSocket (ws) transport. If you have your default transport configured as http you must set the --transport ws flag when calling "model upload"

```
runware model upload [flags]
```

### Examples

```
  # Upload a FLUX checkpoint from a hosted safetensors file
  runware model upload --air "myorg:42@1" \
    --category checkpoint \
    --architecture flux1d \
    --name "My Custom Model"\
    --version 1.0 \
    --download-url https://example.com/model.safetensors\
    --private

  # Upload a LoRA with metadata and defaults
  runware model upload --air "myorg:42@1" \
    --category lora \
    --architecture sdxl \
    --name "Style LoRA" \
    --version 1.0 \
    --download-url https://example.com/lora.safetensors \
    --tags style,portrait \
    --default-weight 0.8 \
    --positive-trigger-words "myStyle" \
    --short-description "Portrait style LoRA"
```

### Options

```
      --air string                      Custom AIR identifier (provider:model@version)
  -a, --architecture string             Model architecture (e.g. flux1d, sdxl, sd1x)
  -c, --category string                 Model category: checkpoint, lora, lycoris, vae, embeddings
      --comment string                  Internal notes
      --default-cfg float               Default CFG scale used when running the model
      --default-scheduler string        Default scheduler used when running the model
      --default-steps int               Default step count used when running the model
      --default-strength float          Default strength for image-to-image (0-1)
      --default-weight float            Default weight (lora, lycoris)
      --download-url string             Publicly reachable URL hosting the model file
      --format string                   Model file format (default "safetensors")
  -h, --help                            help for upload
      --hero-image-url string           URL of a display image for the model
      --name string                     Display name of the model
      --positive-trigger-words string   Activation keywords (lora, lycoris)
      --private                         Restrict access to your organization only
      --short-description string        Brief model summary
      --tags strings                    Comma-separated categorical labels
  -t, --type string                     Model type (checkpoint: base, inpainting; lora/embeddings: positive, negative)
      --unique-identifier string        Custom unique identifier for the model
      --version string                  Version identifier (e.g. 1.0)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware model](runware_model.md)	 - Manage and search models

