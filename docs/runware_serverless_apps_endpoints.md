## runware serverless apps endpoints

List endpoints for a serverless application

```
runware serverless apps endpoints <deploymentId> [flags]
```

### Examples

```
  # list endpoints for an application
  runware serverless apps endpoints my-app
```

### Options

```
  -h, --help        help for endpoints
      --limit int   Maximum number of endpoints to return (1-100)
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

