## runware serverless deploy

Deploy a new serverless application

### Synopsis

Create a new serverless application from a Python entry file.

The whole source directory is zipped and submitted as the application source, so
the entry file can import its own modules and read its own data files. That
directory is the working directory unless --src-dir says otherwise.

The entry file must live inside the source directory. A relative path is resolved
inside it; an absolute path is taken as given.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. Without one, a .gitignore is used
instead. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv or node_modules.

Environment variables must be supplied here with --env or --env-file. An app's
environment is frozen into the version this command creates, which is what the
worker is rendered from, so setting one afterwards with 'apps env set' stores it
without it ever reaching a pod. Prefer --env-file for anything secret: a value
passed as --env is visible in the process list and recorded in shell history.

Anything the app downloads at runtime belongs on a --volume. The app runs in a
sandbox whose filesystem is part of the checkpointed state, so an unmounted
download is copied into every checkpoint and fetched again on every cold start.
A volume keeps it out of both.

Worker settings are supplied via flags (a local project config via 'runware
serverless init' is planned). Endpoints are derived server-side from the SDK.

```
runware serverless deploy <file> [flags]
```

### Examples

```
  # deploy the current directory, with app.py as the entry point
  runware serverless deploy ./app.py --id my-app --gpu-type h100

  # deploy a project that lives elsewhere; app.py is resolved inside --src-dir
  runware serverless deploy app.py --src-dir ~/projects/my-app --id my-app --gpu-type h100

  # an entry file in a subdirectory of the project
  runware serverless deploy src/app.py --src-dir ~/projects/my-app --id my-app --gpu-type h100

  # pass a token to the worker without putting it in the process list
  printf 'HF_TOKEN=%s' "$token" > .env.deploy
  runware serverless deploy model.py --id my-app --gpu-type l40s --env-file .env.deploy

  # keep downloaded model weights on persistent node-local storage
  runware serverless deploy model.py --id my-app --gpu-type l40s \
    --volume /root/.cache/huggingface

  # override worker settings and base image
  runware serverless deploy ./app.py --id my-app --name "My App" \
    --max-workers 2 --idle-ttl 120 --gpu-type h100 \
    --base-image python:3.11-slim --requirement torch
```

### Options

```
      --base-image string         Builder base image (default "python:3.11-slim")
      --env stringArray           Environment variable as KEY=VALUE (repeatable)
      --env-file stringArray      File of KEY=VALUE lines to read environment variables from (repeatable)
      --gpu-type string           GPU type ID (see 'serverless gpus')
      --gpus-per-worker int32     GPUs allocated per worker (default 1)
  -h, --help                      help for deploy
      --id string                 Application ID (immutable, lowercase slug)
      --idle-ttl int32            Idle TTL in seconds before scaling down (default 60)
      --max-workers int32         Maximum number of workers (default 1)
      --min-workers int32         Minimum number of workers
      --name string               Display name (defaults to --id)
      --requirement stringArray   Additional pip package to install (repeatable)
      --scaling-delay int32       Scaling delay in seconds (default 10)
      --src-dir string            Directory to package as the application source (default: the working directory)
      --volume stringArray        Absolute path inside the app backed by persistent node-local storage (repeatable)
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

