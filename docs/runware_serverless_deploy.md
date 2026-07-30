## runware serverless deploy

Deploy a new serverless application

### Synopsis

Deploy a new serverless application from a Python file.

Application settings come from project configuration managed in the
Runware dashboard. Local project scaffolding via 'runware serverless init'
is planned and not available yet.

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

* [runware serverless](runware_serverless.md)	 - Manage Runware serverless applications

