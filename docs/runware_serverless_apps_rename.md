## runware serverless apps rename

Rename a serverless application

### Synopsis

Change the display name of a serverless application.

The application ID is immutable. A name-only update records a version and does
not pin or roll workers. deploy --name still applies on create only.

```
runware serverless apps rename <appId> <name> [flags]
```

### Examples

```
  # rename an application
  runware serverless apps rename my-app "Image generator"
```

### Options

```
  -h, --help   help for rename
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

