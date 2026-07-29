## runware serverless dev

Run a serverless application locally for development

```
runware serverless dev [file] [flags]
```

### Examples

```
  # run the project entrypoint locally
  runware serverless dev

  # run a specific Python file
  runware serverless dev ./app.py
```

### Options

```
  -h, --help   help for dev
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless](runware_serverless.md)	 - Manage Runware serverless applications

