## runware model show

Show full details for a model by AIR identifier

```
runware model show <air> [flags]
```

### Examples

```
  # Show details for a specific model
  runware model show civitai:305149@392545

  # Output as JSON
  runware model show civitai:305149@392545 --format json
```

### Options

```
  -h, --help   help for show
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware model](runware_model.md)	 - Manage and search models

