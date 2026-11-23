## runware serverless apps tasks cancel

Cancel a queued task, or all queued tasks of an application

### Synopsis

Cancel a task that no worker has started. It never runs and uses no GPU time.

A task that is already running, completed, or failed cannot be canceled.

With --all, the application's queue is emptied and every queued task is
canceled, so resuming a stopped application starts no worker for them.
Canceling a single task leaves its message queued until a worker skips it.

```
runware serverless apps tasks cancel <appId> [taskId] [flags]
```

### Examples

```
  # cancel one queued task
  runware serverless apps tasks cancel my-app 7c9e6679-7425-40de-944b-e07fc1f90ae7

  # cancel every queued task of an app
  runware serverless apps tasks cancel my-app --all
```

### Options

```
      --all    Cancel every queued task of the application
  -h, --help   help for cancel
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
      --environment name   Alias for --env
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps tasks](runware_serverless_apps_tasks.md)	 - List and inspect application tasks

