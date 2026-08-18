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

  # filter by name or ID substring
  runware serverless apps list --query demo --sort name

  # filter by GPU type
  runware serverless apps list --gpu-type h100 --status active

  # page through results
  runware serverless apps list --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string     Pagination cursor from a previous nextCursor (reuse the same --query/--gpu-type/--sort/--status)
      --gpu-type string   Filter by GPU type (see 'serverless gpus')
  -h, --help              help for list
      --limit int         Maximum number of applications to return (1-100)
      --query string      Filter by substring on name or ID
      --sort string       Sort order (createdAt (default), name, activity, or errorRate)
      --status string     Filter by status (active, initializing, stopped, …)
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

