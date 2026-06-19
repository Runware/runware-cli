## runware model examples

Show example requests for a model by AIR identifier

```
runware model examples <air> [flags]
```

### Examples

```
  # Examples for a model
  runware model examples google:gemini@3.1-pro

  # Full request and response payloads
  runware model examples google:gemini@3.1-pro --format json
```

### Options

```
  -h, --help   help for examples
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

