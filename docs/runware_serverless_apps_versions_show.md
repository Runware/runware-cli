## runware serverless apps versions show

Show a version of a serverless application

### Synopsis

Show a single immutable version by number.

```
runware serverless apps versions show <appId> <versionNumber> [flags]
```

### Examples

```
  # show a version
  runware serverless apps versions show my-app 1
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

* [runware serverless apps versions](runware_serverless_apps_versions.md)	 - Inspect application versions

