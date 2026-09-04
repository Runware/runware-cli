## runware serverless apps versions list

List versions of a serverless application

### Synopsis

List immutable versions of an application.

The Build column is empty for container-sourced versions.

```
runware serverless apps versions list <appId> [flags]
```

### Examples

```
  # list deployed versions
  runware serverless apps versions list my-app

  # page through results
  runware serverless apps versions list my-app --limit 20 --cursor <nextCursor>
```

### Options

```
      --cursor string   Pagination cursor from a previous nextCursor
  -h, --help            help for list
      --limit int       Maximum number of versions to return (1-100)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps versions](runware_serverless_apps_versions.md)	 - Manage application versions

