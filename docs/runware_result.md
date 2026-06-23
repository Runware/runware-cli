## runware result

Wait for and display the result of a task by taskUUID

### Synopsis

Wait for and display the result of a task by its taskUUID.

Polls the API until the task completes, then displays the result. This does
not re-submit the task — it only retrieves an existing one.

For async delivery, use this to resume waiting when "runware run" was
interrupted before the task completed. The taskUUID is printed when the
task is first submitted.

For sync delivery, if the task already completed during "runware run", the
result is returned on the first poll.

```
runware result <taskUUID> [flags]
```

### Examples

```
  # Wait for a training task to complete
  runware result 7fbf4fc9-5b61-461c-84a4-1e496da4debb

  # Output as JSON without downloading
  runware result 7fbf4fc9-5b61-461c-84a4-1e496da4debb -F json --no-download
```

### Options

```
  -h, --help                     help for result
      --no-download              Skip auto-downloading media files
      --output-dir string        Directory to save downloaded output files (default "./outputs")
      --poll-interval duration   Polling interval (default 2s)
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware](runware.md)	 - CLI tool for the Runware API

