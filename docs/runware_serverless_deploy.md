## runware serverless deploy

Deploy a new serverless application

### Synopsis

Deploy a new serverless application from a Python file.

The application settings come from the project configuration (via the
dashboard, or 'runware serverless init' when available).

```
runware serverless deploy [file] [flags]
```

### Examples

```
  # deploy the application in the current project
  runware serverless deploy

  # deploy a specific Python file
  runware serverless deploy ./app.py
```

### Options

```
  -h, --help   help for deploy
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless](runware_serverless.md)	 - Manage Runware serverless deployments

