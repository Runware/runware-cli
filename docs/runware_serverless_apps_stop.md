## runware serverless apps stop

Stop a serverless application

### Synopsis

Stop a running serverless application.

The server accepts the stop and returns immediately with status stopping.
Worker drain is asynchronous; this command does not wait until the application
is stopped. The application must be active.

```
runware serverless apps stop <appId> [flags]
```

### Examples

```
  # stop a running application
  runware serverless apps stop my-app
```

### Options

```
  -h, --help   help for stop
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

