## runware serverless environments list

List environments

### Synopsis

List the organization's environments, production first.

A cap the environment does not set shows the organization default it inherits,
marked (org default); - means no cap.

```
runware serverless environments list [flags]
```

### Examples

```
  runware serverless environments list
```

### Options

```
  -h, --help   help for list
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

