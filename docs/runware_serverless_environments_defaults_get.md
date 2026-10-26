## runware serverless environments defaults get

Show the organization defaults

### Synopsis

Show the worker and GPU caps an environment inherits when it does not set
its own; - means no cap.

```
runware serverless environments defaults get [flags]
```

### Examples

```
  runware serverless environments defaults get
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless environments defaults](runware_serverless_environments_defaults.md)	 - Manage the organization defaults environments inherit

