## runware serverless apps workers show

Show a single application worker

### Synopsis

Show one worker by ID within the application.

The ID is the Kubernetes pod UID recorded by the reconciler.

```
runware serverless apps workers show <appId> <workerId> [flags]
```

### Examples

```
  # show a worker
  runware serverless apps workers show my-app 44444444-4444-4444-4444-444444444444
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

* [runware serverless apps workers](runware_serverless_apps_workers.md)	 - List and inspect workers for a serverless application

