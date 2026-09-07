## runware serverless apps endpoints

List and inspect endpoints for a serverless application

### Synopsis

List the endpoints of the application's active version.

The set is written by the source itself and is replaced atomically whenever a
version activates. Empty while the app is initializing.

```
runware serverless apps endpoints <appId> [flags]
```

### Examples

```
  # list endpoints for an application
  runware serverless apps endpoints my-app

  # page through results
  runware serverless apps endpoints my-app --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for endpoints
      --limit int       Maximum number of endpoints to return (1-100)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications
* [runware serverless apps endpoints show](runware_serverless_apps_endpoints_show.md)	 - Show a single application endpoint

