## runware serverless connectors list

List OIDC connectors

### Synopsis

List the organization's OIDC connectors.

```
runware serverless connectors list [flags]
```

### Examples

```
  runware serverless connectors list

  # page through results
  runware serverless connectors list --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for list
      --limit int       Maximum number of connectors to return (1-100)
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

