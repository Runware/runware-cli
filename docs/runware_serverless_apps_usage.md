## runware serverless apps usage

Show usage and cost for a serverless application

### Synopsis

Show GPU time and spend for one application over a time window.

This is the account-wide report filtered to one appId. The filter narrows what
is reported, not what is measured: commitment coverage depends on every app's
concurrent GPUs, so a per-app split between commitment and PAYG reflects the
organisation-wide allocation.

The window is half-open (--from inclusive, --to exclusive), at most 31 days,
and clipped: a worker running across a boundary contributes only the part
inside it. --for cannot be combined with --from or --to.

Billable time runs from loading through ready, draining and stopping; pending,
pulling and stopped do not bill, and busy is queue occupancy rather than a
lifecycle transition. GPU time is summed per GPU, so four GPUs for one second
is four seconds. PAYG spend is provisional and not a settled charge; the
PAYG-equivalent value prices the same time at the catalogue rate whatever
covered it, so the difference is what reserved capacity saved.

Grouping by day splits a span at UTC midnight. The table ends with a Total row
and prints the resolved window to stderr; json and yaml output the API payload.

```
runware serverless apps usage <appId> [flags]
```

### Examples

```
  # last 24 hours for one app
  runware serverless apps usage my-app

  # this month, per day
  runware serverless apps usage my-app --for this-month --group-by day

  # export
  runware serverless apps usage my-app --for last-month --format json
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

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications

