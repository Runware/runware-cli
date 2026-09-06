## runware serverless apps logs

Show logs for a serverless application

### Synopsis

Show application logs.

This command is not implemented yet. The log-query route exists but currently
answers 404 until a follow-up ADR; live tail is not supported.

```
runware serverless apps logs <appId> [flags]
```

### Examples

```
  # show application logs (not available yet)
  runware serverless apps logs my-app
```

### Options

```
  -h, --help   help for logs
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

