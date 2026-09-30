## runware serverless apps errors

List failed requests for a serverless application

### Synopsis

List failed inference requests for an application, newest first.

These are 4xx and 5xx responses. The control-plane audit trail is apps events;
worker stdout is apps logs. --window defaults to the last 24 hours. Omit
--status-class to include both 4xx and 5xx.

Replay a cursor with the same --window and --status-class.

```
runware serverless apps errors <appId> [flags]
```

### Examples

```
  # failed requests in the last 24 hours
  runware serverless apps errors my-app

  # server errors in the last hour
  runware serverless apps errors my-app --window 1h --status-class 5xx

  # page through results
  runware serverless apps errors my-app --window 24h --limit 50 --cursor <nextCursor>
```

### Options

```
      --cursor string         Pagination cursor from a previous nextCursor (reuse the same --window/--status-class/--limit)
  -h, --help                  help for errors
      --limit int             Maximum number of errors to return (1-100)
      --status-class string   Error class to include (4xx or 5xx; default both)
      --window string         Time window to search (1h, 6h, 24h, 7d, or 30d) (default "24h")
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

