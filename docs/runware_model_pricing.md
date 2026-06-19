## runware model pricing

Show pricing for a model

```
runware model pricing <model> [flags]
```

### Examples

```
  # Pricing for a model
  runware model pricing google:gemini@3.1-pro

  # Output as JSON
  runware model pricing google:gemini@3.1-pro --format json
```

### Options

```
  -h, --help   help for pricing
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

