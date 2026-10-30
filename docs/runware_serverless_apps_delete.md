## runware serverless apps delete

Delete a serverless application

### Synopsis

Soft-delete a serverless application.

Only the copy in the environment --env selects is deleted, else the copy in
the default environment (RUNWARE_ENV, else the config's) or production. Copies
of the same app id in other environments keep running.

The server accepts the delete and returns immediately with status deleting.
Router removal and worker drain are asynchronous; this command does not wait
until the application is deleted.

Confirmation is required unless --yes or --force is passed.

```
runware serverless apps delete <appId> [flags]
```

### Examples

```
  # delete an application (prompts for confirmation)
  runware serverless apps delete my-app

  # skip the confirmation prompt
  runware serverless apps delete my-app --yes
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
  -e, --env name           Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications

