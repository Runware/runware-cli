## runware serverless secrets attachments

List secrets attached to an application

### Synopsis

List secrets attached to an application, including any env-var name override.
Encrypted values are never returned.

```
runware serverless secrets attachments <app> [flags]
```

### Examples

```
  # list secrets attached to an application
  runware serverless secrets attachments my-app

  # page through results
  runware serverless secrets attachments my-app --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for attachments
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

