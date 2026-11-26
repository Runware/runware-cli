## runware serverless apps tasks cancel

Cancel a queued task, or all queued tasks of an application

### Synopsis

Cancel a task that no worker has started. Workers that check for a canceled task skip it.

A task that is already running, completed, or failed cannot be canceled
(409 task-not-cancelable). A task that has not started is also refused, with
409 task-cancel-unsupported, when the application's workers cannot skip a
canceled task; use --all then. A task whose record has expired is not found (404).

With --all, the application's queue is emptied and the queued tasks the API can
find with no recorded start are canceled, so resuming a stopped application
starts no worker for the messages removed. --all does not guarantee that every
task is canceled: a task submitted while the call runs, a message a worker had
already taken, or a task queued for more than an hour can survive or go uncounted.
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
      --all    Cancel the application's queued tasks (see the help for what it does not cover)
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

