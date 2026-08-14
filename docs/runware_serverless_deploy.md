## runware serverless deploy

Deploy a new serverless application

### Synopsis

Create a new serverless application from a Python entry file.

The file is zipped and submitted as the application source. Worker settings
are supplied via flags (a local project config via 'runware serverless init'
is planned). Endpoints are derived server-side from the SDK.

```
runware serverless deploy <file> [flags]
```

### Examples

```
  # deploy a Python entry file
  runware serverless deploy ./app.py --id my-app

  # override worker settings and base image
  runware serverless deploy ./app.py --id my-app --name "My App" \
    --max-workers 2 --idle-ttl 120 --gpu-type h100 \
    --base-image python:3.11-slim --requirement torch
```

### Options

```
      --base-image string         Builder base image (default "python:3.11-slim")
      --gpu-type string           Preferred GPU type ID (see 'serverless gpus')
      --gpus-per-worker int32     GPUs allocated per worker (default 1)
  -h, --help                      help for deploy
      --id string                 Application ID (immutable, lowercase slug)
      --idle-ttl int32            Idle TTL in seconds before scaling down (default 60)
      --max-workers int32         Maximum number of workers (default 1)
      --min-workers int32         Minimum number of workers
      --name string               Display name (defaults to --id)
      --requirement stringArray   Additional pip package to install (repeatable)
      --scaling-delay int32       Scaling delay in seconds (default 10)
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

