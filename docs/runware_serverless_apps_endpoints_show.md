## runware serverless apps endpoints show

Show a single application endpoint

### Synopsis

Show one endpoint of the application's active version by ID.

```
runware serverless apps endpoints show <appId> <endpointId> [flags]
```

### Examples

```
  # show an endpoint
  runware serverless apps endpoints show my-app 11111111-1111-1111-1111-111111111111
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

* [runware serverless apps endpoints](runware_serverless_apps_endpoints.md)	 - List and inspect endpoints for a serverless application

