## runware model schema

Show the request/response schema for a model

```
runware model schema <air> [flags]
```

### Examples

```
  # Show request parameters for a model
  runware model schema google:3@2

  # Show response parameters instead
  runware model schema google:3@2 --response

  # Output the full schema envelope as JSON
  runware model schema google:3@2 --format json
```

### Options

```
  -h, --help       help for schema
  -r, --response   Show response schema instead of request schema
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

