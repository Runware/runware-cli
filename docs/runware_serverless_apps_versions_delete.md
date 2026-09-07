## runware serverless apps versions delete

Delete an unused application version

### Synopsis

Delete an unused version while retaining its immutable history.

Deleted versions are omitted from version lists, return 404 from version
reads, and cannot be activated. Returns 409 while the app is deleting, or
when the version is active, is the app's only remaining version, has a
non-stopped worker, or is targeted by a live rollout. This does not remove
the version's OCI image.

Confirmation is required unless --yes or --force is passed.

```
runware serverless apps versions delete <appId> <versionNumber> [flags]
```

### Examples

```
  # delete an unused version (prompts for confirmation)
  runware serverless apps versions delete my-app 2

  # skip the confirmation prompt
  runware serverless apps versions delete my-app 2 --yes
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

* [runware serverless apps versions](runware_serverless_apps_versions.md)	 - Manage application versions

