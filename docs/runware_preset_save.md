## runware preset save

Save a named preset

### Synopsis

Save a named preset for use with the run command.

The model AIR is required and is stored as the preset's default model.
Additional parameters are passed as key=value pairs using the same syntax
as the run command, and the same schema-driven shell completion is available.

```
runware preset save <name> <model> [key=value ...] [flags]
```

### Examples

```
  # save a preset with model and dimensions
  runware preset save portrait runware:100@1 width=512 height=768

  # save a preset with steps and cfg scale
  runware preset save fast runware:100@1 steps=20 cfg_scale=7

  # save a preset for a text model with a system prompt
  runware preset save mychat minimax:m3@0 messages.0.role=system messages.0.content="You are a helpful assistant"
```

### Options

```
  -h, --help   help for save
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware preset](runware_preset.md)	 - Manage named presets

