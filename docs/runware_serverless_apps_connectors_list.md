## runware serverless apps connectors list

List the OIDC connectors attached to an application

### Synopsis

List the OIDC connectors attached to an application in one environment, each
with the subject (sub) its workers' tokens carry. Use the subject in a
StringEquals trust condition.

```
runware serverless apps connectors list <appId> [flags]
```

### Examples

```
  runware serverless apps connectors list my-app

  # the staging copy of the application
  runware serverless apps connectors list my-app --env staging
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
      --environment name   Alias for --env
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps connectors](runware_serverless_apps_connectors.md)	 - Manage the OIDC connectors attached to an application

