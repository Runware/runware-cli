## runware serverless environments show

Show an environment

### Synopsis

Show one environment's settings.

A cap the environment does not set shows the organization default it inherits,
marked (org default); - means no cap.

```
runware serverless environments show <name> [flags]
```

### Examples

```
  runware serverless environments show staging
```

### Options

```
  -h, --help   help for show
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless environments](runware_serverless_environments.md)	 - Manage the organization's environments

