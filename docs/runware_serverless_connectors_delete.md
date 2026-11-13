## runware serverless connectors delete

Delete an OIDC connector

### Synopsis

Delete an OIDC connector. It is refused with a conflict while any application
still attaches it: detach it from each one first with 'apps connectors detach'.

```
runware serverless connectors delete <name> [flags]
```

### Examples

```
  runware serverless connectors delete aws-prod
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless connectors](runware_serverless_connectors.md)	 - Manage OIDC connectors

