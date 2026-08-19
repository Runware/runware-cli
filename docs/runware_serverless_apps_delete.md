## runware serverless apps delete

Delete a serverless application

### Synopsis

Soft-delete a serverless application.

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
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications

