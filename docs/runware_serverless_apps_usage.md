## runware serverless apps usage

Show usage and cost for a serverless application

### Synopsis

Show an aggregated usage and cost report for one application.

This command is not implemented yet. Billing rollups are not in the API, so
there is no per-app report to list. When the report API exists, this will be
the account-wide usage command scoped to one appId.

```
runware serverless apps usage <appId> [flags]
```

### Examples

```
  # show usage for an application (not available yet)
  runware serverless apps usage my-app
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

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications

