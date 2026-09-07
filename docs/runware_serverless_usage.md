## runware serverless usage

Show account-wide usage and cost

### Synopsis

Show an aggregated usage and cost report for the authenticated organisation.

This command is not implemented yet. Billing rollups are not in the API, so
there is no report to list. Raw worker-transition rows (GET /v1/usage) are
not this command.

When the report API exists, this will cover an account-wide window with the
filters that endpoint exposes.

```
runware serverless usage [flags]
```

### Examples

```
  # show account-wide usage (not available yet)
  runware serverless usage
```

### Options

```
  -h, --help   help for usage
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

