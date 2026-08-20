## runware serverless apps resume

Resume a stopped serverless application

### Synopsis

Resume a stopped serverless application.

The server accepts the resume and returns immediately with status initializing.
Worker start is asynchronous; this command does not wait until the application
is active. The application must be stopped.

```
runware serverless apps resume <appId> [flags]
```

### Examples

```
  # resume a stopped application
  runware serverless apps resume my-app
```

### Options

```
  -h, --help   help for resume
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

