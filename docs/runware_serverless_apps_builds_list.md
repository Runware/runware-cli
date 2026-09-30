## runware serverless apps builds list

List builds for a serverless application

### Synopsis

List code builds and container validations for an application.

The table omits log tail; use 'builds show' for error detail and log tail.

```
runware serverless apps builds list <appId> [flags]
```

### Examples

```
  # list builds for an application
  runware serverless apps builds list my-app

  # page through results
  runware serverless apps builds list my-app --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for list
      --limit int       Maximum number of builds to return (1-100)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps builds](runware_serverless_apps_builds.md)	 - Inspect application builds

