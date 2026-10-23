## runware serverless apps env-vars unset

Remove an environment variable

### Synopsis

Remove one plain-text environment variable from an application.

A delete records a new version with the same image and rolls the workload when
the app is active, initializing, or failed and its image is deployable. A
stopped or stopping app applies it on resume. A delete during an in-flight
rollout returns 409 and does not remove the value.

```
runware serverless apps env-vars unset <appId> <key> [flags]
```

### Examples

```
  # remove an environment variable
  runware serverless apps env-vars unset my-app MY_KEY
```

### Options

```
  -h, --help   help for unset
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the app runs in (default production)
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps env-vars](runware_serverless_apps_env-vars.md)	 - Manage plain-text environment variables for an application

