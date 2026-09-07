## runware serverless deploy

Deploy a new serverless application

### Synopsis

Create a new serverless application from Python code or a container source.

A code deploy takes a Python entry file. The whole source directory is zipped
and submitted as the application source, so the entry file can import its own
modules and read its own data files. That directory is the working directory
unless --src-dir says otherwise.

The entry file must live inside the source directory. A relative path is resolved
inside it; an absolute path is taken as given.

A container deploy takes --container pointing at a directory whose root contains
Dockerfile and container.yaml (plus any build-context files the Dockerfile
copies). The directory is zipped and uploaded as source type container. Runware
builds a hosted wrapper image from that archive; the version records a buildId,
not a customer image reference. Invalid container.yaml is rejected on create
(400 if it cannot be parsed, 422 if it breaks a rule). The app stays
initializing until that first build rolls out.

--container cannot be combined with an entry file, --src-dir, --base-image, or
--requirement.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. A .gitignore is NOT consulted --
what a project keeps out of version control is a different question from what it
ships. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv, node_modules or the usual build and tool caches.

Environment variables must be supplied here with --env or --env-file. An app's
environment is frozen into the version this command creates, which is what the
worker is rendered from, so setting one afterwards with 'apps env set' stores it
without it ever reaching a pod. Prefer --env-file for anything secret: a value
passed as --env is visible in the process list and recorded in shell history.

Anything the app downloads at runtime belongs on a --volume. The app runs in a
sandbox whose filesystem is part of the checkpointed state, so an unmounted
download is copied into every checkpoint and fetched again on every cold start.
A volume keeps it out of both.

Worker settings are supplied via flags. Endpoints are derived server-side from
the SDK (code) or from container.yaml (container).

```
runware serverless deploy [file] [flags]
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

  # deploy a container source (Dockerfile + container.yaml at the directory root)
  runware serverless deploy --id my-app --gpu-type h100 --container ./wrapper
```

### Options

```
      --base-image string         Builder base image (code deploys only) (default "python:3.11-slim")
      --container string          Directory whose root contains Dockerfile and container.yaml
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
      --requirement stringArray   Additional pip package to install (repeatable; code deploys only)
      --scaling-delay int32       Scaling delay in seconds (default 10)
      --src-dir string            Directory to package as the application source (default: the working directory; code deploys only)
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

