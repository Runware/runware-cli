## runware serverless apps env list

List environment variables for a serverless application

### Synopsis

List plain-text environment variables for an application, including values.

To list encrypted secrets attached to an application, use 'serverless secrets attachments'.

```
runware serverless apps env list <appId> [flags]
```

### Examples

```
  # list environment variables
  runware serverless apps env list my-app

  # page through results
  runware serverless apps env list my-app --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for list
      --limit int       Maximum number of environment variables to return (1-100)
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

