## runware serverless apps workers

List and inspect workers for a serverless application

### Synopsis

List workers observed for an application, including terminal stopped rows
until they are purged.

```
runware serverless apps workers <appId> [flags]
```

### Examples

```
  # list workers for an application
  runware serverless apps workers my-app

  # filter by status
  runware serverless apps workers my-app --status ready

  # page through results
  runware serverless apps workers my-app --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for workers
      --limit int       Maximum number of workers to return (1-100)
      --status string   Filter by status (ready, busy, pending, …)
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
* [runware serverless apps workers show](runware_serverless_apps_workers_show.md)	 - Show a single application worker

