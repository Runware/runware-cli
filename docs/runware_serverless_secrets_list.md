## runware serverless secrets list

List organisation secrets

### Synopsis

List organisation secret metadata. Encrypted values are never returned.

To list secrets attached to an application, use 'secrets attachments'.

```
runware serverless secrets list [flags]
```

### Examples

```
  # list organisation secrets
  runware serverless secrets list

  # page through results
  runware serverless secrets list --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for list
      --limit int       Maximum number of secrets to return (1-100)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage organisation secrets for serverless applications

