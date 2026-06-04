## runware run

Run an inference request against any Runware model

### Synopsis

Run an inference request against any Runware model.

The model is identified by its AIR (AI Resource) identifier. Parameters are
passed as key=value pairs. The model's JSON Schema is fetched automatically to
validate inputs and determine the task type.

If the schema cannot determine the task type (e.g. for community or custom
fine-tuned models), specify it explicitly with --task-type.

```
runware run <model> [key=value ...] [flags]
```

### Examples

```
  # Image generation
  runware run runware:101@1 positivePrompt="A serene mountain landscape" width=1024 height=1024

  # Text inference (LLM)
  runware run openai:o1@0 messages='[{"role":"user","content":"Explain quantum computing"}]'

  # Video generation
  runware run klingai:5@3 positivePrompt="Ocean waves at sunset" duration=10

  # Community model — task type must be specified explicitly
  runware run civitai:305149@392545 --task-type imageInference positivePrompt="A portrait" width=1024 height=1024

  # Save output to a specific directory
  runware run runware:101@1 positivePrompt="Abstract art" --output-dir ./my-images

  # Output as JSON without downloading
  runware run runware:101@1 positivePrompt="Abstract art" --format json --no-download
```

### Options

```
  -h, --help                help for run
      --no-download         Skip auto-downloading media files (imageURL, videoURL, audioURL)
      --output-dir string   Directory to save downloaded output files (default "./outputs")
      --task-type string    Override the detected task type (e.g. imageInference, videoInference, textInference, audioInference)
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

