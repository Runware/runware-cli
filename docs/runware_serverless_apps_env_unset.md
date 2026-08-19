## runware serverless apps env unset

Remove an environment variable

### Synopsis

Remove one plain-text environment variable from an application.

```
runware serverless apps env unset <appId> <key> [flags]
```

### Examples

```
  # remove an environment variable
  runware serverless apps env unset my-app MY_KEY
```

### Options

```
  -h, --help   help for unset
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps env](runware_serverless_apps_env.md)	 - Manage plain-text environment variables for an application

