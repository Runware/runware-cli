## runware serverless apps builds delete

Delete or cancel an application build

### Synopsis

Cancel a queued or running build, or delete a terminal build.

Cancelling a queued or running build records it as superseded and ends its
current rollout without activating it, so any previous version keeps serving.
A terminal build can be deleted once no live rollout still needs it. Ready
builds remain while a version references them (409).

Confirmation is required unless --yes or --force is passed.

```
runware serverless apps builds delete <appId> <buildId> [flags]
```

### Examples

```
  # cancel or delete a build (prompts for confirmation)
  runware serverless apps builds delete my-app 33333333-3333-3333-3333-333333333333

  # skip the confirmation prompt
  runware serverless apps builds delete my-app 33333333-3333-3333-3333-333333333333 --yes
```

### Options

```
      --force   Skip the confirmation prompt
  -h, --help    help for delete
  -y, --yes     Skip the confirmation prompt
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

