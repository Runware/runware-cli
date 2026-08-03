## runware serverless apps list

List serverless applications

```
runware serverless apps list [flags]
```

### Examples

```
  # list all serverless applications
  runware serverless apps list

  # filter by status
  runware serverless apps list --status active
```

### Options

```
  -h, --help            help for list
      --limit int       Maximum number of applications to return (1-100)
      --status string   Filter by status (active, initializing, stopped, …)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications

