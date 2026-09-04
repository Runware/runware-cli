## runware serverless apps versions activate

Activate a ready application version

### Synopsis

Activate a ready version by number, including rollback to an older version.

The server accepts the deploy and returns immediately with the updated app.
Worker rollout is asynchronous; this command does not wait until workers are
healthy. Re-activating the currently active version is permitted and re-applies
it. On a stopped app the version is recorded and applied on resume.

A missing app is 404. A missing version, a version that is not ready, or an
app that is deleting is 409.

```
runware serverless apps versions activate <appId> <versionNumber> [flags]
```

### Examples

```
  # list versions, then activate one
  runware serverless apps versions list my-app
  runware serverless apps versions activate my-app 2

  # roll back to an older ready version
  runware serverless apps versions activate my-app 1
```

### Options

```
  -h, --help   help for activate
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

