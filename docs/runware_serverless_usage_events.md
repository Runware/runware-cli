## runware serverless usage events

List usage events

### Synopsis

List worker state transitions for the authenticated organization, newest first.

Each row is one ledger entry. --from is inclusive and --to is exclusive on the
time the transition happened. There is no default window: omit both to read
the latest page of the ledger. Spend over a window is 'serverless usage'.

Price/s is the catalog rate per GPU-second at that moment, including time a
reservation covered. Coverage itself is on the usage summary.

An unknown --app is an empty page, not a missing app. Replay a cursor with
the same --app, --from, --to, and --limit.

```
runware serverless usage events [flags]
```

### Examples

```
  # the latest page of the ledger
  runware serverless usage events

  # one app over a UTC day
  runware serverless usage events --app my-app --from 2026-09-01 --to 2026-09-02

  # page through results
  runware serverless usage events --limit 50 --cursor <nextCursor>
```

### Options

```
      --app string      Report only this app's transitions
      --cursor string   Pagination cursor from a previous nextCursor (reuse the same --app/--from/--to/--limit)
      --from string     Inclusive start on event time (RFC 3339 or YYYY-MM-DD UTC)
  -h, --help            help for events
      --limit int       Maximum number of events to return (1-100)
      --to string       Exclusive end on event time (RFC 3339 or YYYY-MM-DD UTC)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless usage](runware_serverless_usage.md)	 - Show account-wide usage and cost

