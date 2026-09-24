## runware serverless usage

Show account-wide usage and cost

### Synopsis

Show GPU time and spend for the authenticated organization over a time window.

The window is half-open (--from inclusive, --to exclusive), at most 31 days,
and clipped: a worker running across a boundary contributes only the part
inside it. --for cannot be combined with --from or --to.

Billable time runs from loading through ready, draining and stopping; pending,
pulling and stopped do not bill, and busy is queue occupancy rather than a
lifecycle transition. GPU time is summed per GPU, so four GPUs for one second
is four seconds. PAYG spend is provisional and not a settled charge; the
PAYG-equivalent value prices the same time at the catalog rate whatever
covered it, so the difference is what reserved capacity saved.

Grouping by day splits a span at UTC midnight. The table ends with a Total row
and prints the resolved window to stderr; json and yaml output the API payload.

```
runware serverless usage [flags]
```

### Examples

```
  # last 24 hours, account-wide
  runware serverless usage

  # a UTC calendar range
  runware serverless usage --for this-month

  # an explicit window: start inclusive, end exclusive
  runware serverless usage --from 2026-09-01 --to 2026-10-01

  # spend per app per day
  runware serverless usage --for last-month --group-by app,day

  # what reserved capacity covered, by GPU type
  runware serverless usage --group-by gpuType,coverage

  # export
  runware serverless usage --for last-month --format json
```

### Options

```
      --for string         UTC calendar range instead of --from/--to (today, yesterday, this-month, or last-month)
      --from string        Inclusive start of the window (RFC 3339 or YYYY-MM-DD UTC; default 24h before --to)
      --gpu-type string    Report only this GPU type (see 'serverless gpus')
      --group-by strings   Group buckets by dimension, comma-separated (app, gpuType, day, or coverage)
  -h, --help               help for usage
      --to string          Exclusive end of the window (RFC 3339 or YYYY-MM-DD UTC; default now)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless](runware_serverless.md)	 - Manage Runware serverless applications

