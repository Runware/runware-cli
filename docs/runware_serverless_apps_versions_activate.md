## runware serverless apps versions activate

Activate a ready application version

### Synopsis

Activate a ready version by number, including rollback to an older version.

The server accepts the deploy and returns immediately with the updated app.
Worker rollout is asynchronous. Pass --wait to poll until an in-progress
rollout reaches active or failed, and --timeout to bound that wait.
Re-activating the currently active version is permitted and re-applies it.
An already-active app stays active while workers roll, so --wait cannot follow
that case and reports that the rollout continues in the background.
On a stopped or stopping app the version is recorded and applied on resume.

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

  # wait until an in-progress rollout is active or failed
  runware serverless apps versions activate my-app 2 --wait --timeout 5m
```

### Options

```
  -h, --help                     help for activate
      --poll-interval duration   Polling interval when waiting for the application (default 2s)
      --timeout duration         Maximum time to wait (0 = no limit)
      --wait                     Poll until the application is active or failed
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

