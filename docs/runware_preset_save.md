## runware preset save

Save a named preset

```
runware preset save [name] [flags]
```

### Examples

```
  # save a preset with model and dimensions
  runware preset save portrait --model runware:100@1 --width 512 --height 768

  # save a preset with steps and cfg
  runware preset save fast --model runware:100@1 --steps 20 --cfg 7
```

### Options

```
  -c, --cfg float          CFG scale
  -H, --height int         Image height
  -h, --help               help for save
  -m, --model string       Model identifier
  -S, --scheduler string   Scheduler
  -s, --steps int          Inference steps
  -W, --width int          Image width
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

