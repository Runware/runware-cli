## runware serverless apps resume

Resume a stopped serverless application

### Synopsis

Resume a stopped serverless application.

The server accepts the resume and returns immediately with status initializing.
Worker start is asynchronous. Pass --wait to poll until the application is
active or failed. The application must be stopped.

```
runware serverless apps resume <appId> [flags]
```

### Examples

```
  # resume a stopped application
  runware serverless apps resume my-app

  # wait until the application is active or failed
  runware serverless apps resume my-app --wait
```

### Options

```
  -h, --help                     help for resume
      --poll-interval duration   Polling interval when waiting for the application (default 2s)
      --timeout duration         Maximum time to wait (0 = no limit)
      --wait                     Poll until the application is active or failed
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

