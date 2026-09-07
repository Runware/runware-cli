## runware serverless apps workers

List and inspect workers for a serverless application

### Synopsis

List workers observed for an application.

The default state is all: terminal stopped rows stay in the page until they
are purged. Pass --state live to drop them. --state live with --status stopped
is refused by the API (422), because an empty page would read as "this app
has never run".

```
runware serverless apps workers <appId> [flags]
```

### Examples

```
  # list workers for an application
  runware serverless apps workers my-app

  # omit terminal stopped rows
  runware serverless apps workers my-app --state live

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
      --state string    Include stopped rows (all, the API default) or drop them (live)
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

