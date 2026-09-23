## runware serverless apps logs

Show or follow logs for a serverless application

### Synopsis

Show recent application logs and optionally follow new ones.

The recent page is read from the runtime log query over --window (default 1h).
Without --sort the command fetches the newest page (the API default) and
prints it oldest first, so a plain apps logs shows the latest entries as a
readable timeline. --sort newest lists newest first, so nextCursor walks
older. --sort oldest lists the oldest page of the window first, so nextCursor
walks newer. prevCursor walks the other way. Replay either cursor with the
same --sort, --window, and --limit.

With --follow the command prints the recent page, then streams new entries
until interrupted; the stream reconnects when the server ends it, and waits
for the stream to open when a gateway answers first, which is what an
application that has written nothing does. The live stream has no window, so
--window, --limit, --sort and --cursor apply to the recent page only, and
--cursor cannot be combined with --follow. Entries written between the recent
page and the start of the stream, or while the stream reconnects, can be
missed or repeated.

In table format each entry is one line: time, level and message. In json or
yaml format the recent page is printed as one document; with --follow every
entry is printed as one JSON object per line.

```
runware serverless apps logs <appId> [flags]
```

### Examples

```
  # show the last hour of logs
  runware serverless apps logs my-app

  # show the last six hours
  runware serverless apps logs my-app --window 6h

  # follow new log entries until Ctrl-C
  runware serverless apps logs my-app --follow

  # page through the recent window
  runware serverless apps logs my-app --limit 50 --cursor <nextCursor>

  # newest first
  runware serverless apps logs my-app --sort newest

  # oldest page of the window
  runware serverless apps logs my-app --sort oldest
```

### Options

```
      --cursor string   Pagination cursor (nextCursor or prevCursor)
  -f, --follow          Stream new log entries until interrupted
  -h, --help            help for logs
      --limit int       Maximum number of entries on the recent page (1-100, default 20)
      --sort string     Order of the recent page (oldest or newest; default: latest page, oldest first)
      --window string   Time window for the recent page (1h, 6h, 24h, 7d, or 30d) (default "1h")
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

