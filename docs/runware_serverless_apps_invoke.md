## runware serverless apps invoke

Invoke an application endpoint

### Synopsis

Submit a JSON payload to a named application endpoint.

endpointPath is a bare lowercase segment as returned by apps endpoints
(e.g. infer). A leading slash is rejected.

The default is async: the command prints the accepted task id. Pass --wait
to poll until the task is completed or failed.

--sync uses the sync invocation endpoint. If the platform wait window
expires, the command polls the returned task id; it never treats expiry as
a failure and never resubmits.

```
runware serverless apps invoke <appId> <endpointPath> [flags]
```

### Examples

```
  # list endpoint paths, then invoke asynchronously
  runware serverless apps endpoints my-app
  runware serverless apps invoke my-app infer -f payload.json

  # wait for a completed task (sync, then poll if the wait window expires)
  runware serverless apps invoke my-app infer --sync -f payload.json

  # async invoke and poll
  runware serverless apps invoke my-app infer --wait -f payload.json
```

### Options

```
  -f, --body string              JSON payload file, or - for stdin (default {})
  -h, --help                     help for invoke
      --poll-interval duration   Polling interval when waiting for a task (default 2s)
      --sync                     Use sync invocation and wait for a terminal task
      --wait                     Poll until the task is completed or failed
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

