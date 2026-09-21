## runware serverless apps errors

List failed inference requests for a serverless application

### Synopsis

List failed inference requests for an application, newest first.

Omit --status-class to include both 4xx and 5xx. The cursor is only valid with
the same --window and --status-class it was issued under.

```
runware serverless apps errors <appId> [flags]
```

### Examples

```
  # list recent request errors
  runware serverless apps errors my-app

  # last 24 hours of 5xx only
  runware serverless apps errors my-app --window 24h --status-class 5xx

  # page through results
  runware serverless apps errors my-app --limit 50 --cursor <nextCursor>
```

### Options

```
      --cursor string         Pagination cursor from a previous nextCursor (reuse the same --window/--status-class)
  -h, --help                  help for errors
      --limit int             Maximum number of errors to return (1-100)
      --status-class string   Filter by status class (4xx or 5xx)
      --window string         Time window (1h, 6h, 24h, 7d, or 30d)
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

