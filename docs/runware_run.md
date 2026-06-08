## runware run

Run an inference request against any Runware model

### Synopsis

Run an inference request against any Runware model.

The model is identified by its AIR (AI Resource) identifier. Parameters are
passed as key=value pairs. The model's JSON Schema is fetched automatically to
validate inputs and determine the task type.

If the schema cannot determine the task type (e.g. for community or custom
fine-tuned models), specify it explicitly with --task-type.

When --preset is provided, the preset's model and parameters are used as
defaults. Any key=value arguments on the command line override the preset.
The model positional argument may be omitted when --preset supplies one.

```
runware run <model> [key=value ...] [flags]
```

### Examples

```
  # Image generation
  runware run runware:101@1 positivePrompt="A serene mountain landscape" width=1024 height=1024

  # Text inference (LLM)
  runware run minimax:m3@0 messages.0.role=user messages.0.content="Explain quantum computing"

  # Text inference — multi-turn conversation
  runware run minimax:m3@0 messages.0.role=user messages.0.content="What is Go?" messages.1.role=assistant messages.1.content="A compiled language." messages.2.role=user messages.2.content="How do I install it?"

  # Video generation
  runware run klingai:5@3 positivePrompt="Ocean waves at sunset" width=1920 height=1080 duration=10

  # 3D inference — text to 3D
  runware run tencent:hunyuan-3d@3.1-pro positivePrompt="A red vintage sports car"

  # 3D inference — image to 3D
  runware run tencent:hunyuan-3d@3.1-pro inputs.images.0="https://example.com/product.jpg"

  # Audio inference
  runware run elevenlabs:1@1 positivePrompt="Upbeat electronic dance music with driving bass and synth leads" duration=30
  runware run minimax:speech@2.8 speech.text="Hello, this is a text-to-speech example." speech.voice=English_expressive_narrator

  # Community model — task type must be specified explicitly
  runware run civitai:305149@392545 --task-type imageInference positivePrompt="A portrait" width=1024 height=1024

  # Load a saved preset, overriding individual params
  runware run --preset portrait positivePrompt="Sunset over the ocean"

  # Save output to a specific directory
  runware run runware:101@1 positivePrompt="Abstract art" --output-dir ./my-images width=1024 height=1024

  # Output as JSON without downloading
  runware run runware:101@1 positivePrompt="Abstract art" --format json --no-download width=1024 height=1024
```

### Options

```
      --delivery-method string   Override delivery method (sync or async); default taken from model schema
  -h, --help                     help for run
      --no-download              Skip auto-downloading media files (imageURL, videoURL, audioURL, outputs.files[].url)
      --output-dir string        Directory to save downloaded output files (default "./outputs")
      --poll-interval duration   Polling interval when delivery method is async (default 2s)
      --preset string            Load parameters from a saved preset (model and params used as defaults)
      --task-type string         Override the detected task type (e.g. imageInference, videoInference, textInference, audioInference, 3dInference)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware](runware.md)	 - CLI tool for the Runware inference API

