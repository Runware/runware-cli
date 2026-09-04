## runware serverless apps tasks

List and inspect application tasks

### Synopsis

List TTL-bounded task metadata for an application.

This is a recovery window, not persisted history. Pending includes queued,
running, and retrying work. A page can be empty and still have nextCursor.

```
runware serverless apps tasks <appId> [flags]
```

### Examples

```
  # list recent tasks
  runware serverless apps tasks my-app --limit 10

  # filter by status
  runware serverless apps tasks my-app --status pending

  # page through results
  runware serverless apps tasks my-app --limit 10 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for tasks
      --limit int       Maximum number of tasks to return (1-100)
      --status string   Filter by status (pending, completed, or failed)
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
* [runware serverless apps tasks show](runware_serverless_apps_tasks_show.md)	 - Show a single application task

