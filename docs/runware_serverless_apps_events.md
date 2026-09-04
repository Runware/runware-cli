## runware serverless apps events

List events for a serverless application

### Synopsis

List deploy, scaling, audit, and error events for an application.

Events are the control-plane audit trail, not worker stdout. Live log
streaming is not available (apps logs is not implemented).

```
runware serverless apps events <appId> [flags]
```

### Examples

```
  # list events for an application
  runware serverless apps events my-app

  # errors only
  runware serverless apps events my-app --type error --limit 20

  # page through results
  runware serverless apps events my-app --type error --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for events
      --limit int       Maximum number of events to return (1-100)
      --type string     Filter by type (deploy, scaling, audit, or error)
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

