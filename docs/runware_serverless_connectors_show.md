## runware serverless connectors show

Show an OIDC connector

### Synopsis

Show one OIDC connector, including the issuer to register with your cloud provider.

```
runware serverless connectors show <name> [flags]
```

### Examples

```
  runware serverless connectors show aws-prod
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

* [runware serverless connectors](runware_serverless_connectors.md)	 - Manage OIDC connectors

