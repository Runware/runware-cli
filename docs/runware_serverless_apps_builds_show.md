## runware serverless apps builds show

Show a build for a serverless application

### Synopsis

Show a single build, including status, error, and log tail.

Log tail is the trailing snapshot returned by the API; live streaming is not
supported.

```
runware serverless apps builds show <appId> <buildId> [flags]
```

### Examples

```
  # show a build
  runware serverless apps builds show my-app 33333333-3333-3333-3333-333333333333
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

* [runware serverless apps builds](runware_serverless_apps_builds.md)	 - Inspect application builds

