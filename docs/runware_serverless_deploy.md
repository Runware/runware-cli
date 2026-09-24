## runware serverless deploy

Create or update a serverless application

### Synopsis

Create or update a serverless application from Python code or a container source.

A first deploy with a new --id creates the application. A later deploy with the
same --id uploads a new source, records version N+1, and rolls it when the
build is ready. Create-only flags (--gpu-type, worker settings, --volume,
--secret, --env, --env-file, --name) apply only to create; passing them when
the application already exists is an error. Change workers with 'apps scale',
attach a secret later with 'secrets attach', and change environment with
'apps env'. A source update on a stopped application is 409.

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
initializing until that first build rolls out. Pass --wait to poll until the
application is active or failed. A successful wait is not a live worker:
minWorkers=0 stays scaled to zero until the first invoke.

--container cannot be combined with an entry file, --src-dir, --base-image, or
--requirement.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. A .gitignore is NOT consulted --
what a project keeps out of version control is a different question from what it
ships. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv, node_modules or the usual build and tool caches.

Pass --env or --env-file on create to set the application's initial environment.
Change a variable afterwards with 'apps env set' or 'apps env unset': a change
records a new version with the same image and rolls the workload when the app
is active, initializing, or failed and its image is deployable. A stopped or
stopping app applies it on resume. A write during an in-flight rollout
returns 409 and does not store the value. Prefer --env-file for anything
secret: a value passed as --env is visible in the process list and recorded
in shell history.

Anything the app downloads at runtime belongs on a --volume. Volumes are set
at create and cannot be changed afterwards: a later deploy that passes
--volume is rejected. The app runs in a sandbox whose filesystem is part of
the checkpointed state, so an unmounted download is copied into every
checkpoint and fetched again on every cold start. A volume keeps it out of
both.

--secret NAME, or NAME=ENV_VAR, attaches an existing organisation secret at
create so the first rollout carries it. Repeat the flag for more than one.
Attach or detach later with 'secrets attach' and 'secrets detach'.

Worker settings are supplied via flags on create, including a fallback GPU
type and the idle-worker buffer. Endpoints are derived server-side from the
SDK (code) or from container.yaml (container).

A code app's endpoint path is its handler's method name with underscores turned
into hyphens, so renaming a method moves a public endpoint and 404s its callers.
Updating an existing application with --wait reports what the deploy did to the
endpoint set once the rollout lands. It is a report, not a gate: renaming an
endpoint on purpose is allowed. A first deploy with --wait prints the endpoint
paths and an invoke example once the application is active.

```
runware serverless deploy [file] [flags]
```

### Examples

```
  # deploy the current directory, with app.py as the entry point
  runware serverless deploy ./app.py --id my-app --gpu-type h100

  # update source on an existing application
  runware serverless deploy ./app.py --id my-app --wait

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

  # attach an existing secret and keep one idle worker warm
  runware serverless deploy ./app.py --id my-app --gpu-type h100 \
    --secret HF_TOKEN=HUGGING_FACE_HUB_TOKEN --min-available-workers 1

  # override worker settings and base image
  runware serverless deploy ./app.py --id my-app --name "My App" \
    --max-workers 2 --idle-ttl 120 --gpu-type h100 \
    --base-image python:3.12-slim --requirement torch

  # deploy a container source (Dockerfile + container.yaml at the directory root)
  runware serverless deploy --id my-app --gpu-type h100 --container ./wrapper

  # wait until the first rollout is active or failed
  runware serverless deploy ./app.py --id my-app --gpu-type h100 --wait
```

### Options

```
      --available-workers-pct int32   Idle-worker buffer as a percentage of load (0-100)
      --base-image string             Builder base image (code deploys only; needs Python 3.12 or newer) (default "python:3.12-slim")
      --container string              Directory whose root contains Dockerfile and container.yaml
      --env stringArray               Environment variable as KEY=VALUE (repeatable)
      --env-file stringArray          File of KEY=VALUE lines to read environment variables from (repeatable)
      --fallback-gpu-type string      Secondary GPU type if the preferred type is unavailable
      --gpu-type string               GPU type ID (see 'serverless gpus'; required when creating)
      --gpus-per-worker int32         GPUs allocated per worker (1, 2, 4, or 8) (default 1)
  -h, --help                          help for deploy
      --id string                     Application ID (immutable, lowercase slug)
      --idle-ttl int32                Idle TTL in seconds before scaling down (default 60)
      --max-workers int32             Maximum number of workers (default 1)
      --min-available-workers int32   Minimum idle workers kept as a buffer
      --min-workers int32             Minimum number of workers
      --name string                   Display name (defaults to --id)
      --poll-interval duration        Polling interval when waiting for the application (default 2s)
      --requirement stringArray       Additional pip package to install (repeatable; code deploys only)
      --scaling-delay int32           Scaling delay in seconds (default 10)
      --secret stringArray            Organisation secret to attach at create, as NAME or NAME=ENV_VAR (repeatable)
      --src-dir string                Directory to package as the application source (default: the working directory; code deploys only)
      --volume stringArray            Absolute path inside the app backed by persistent node-local storage; immutable after create (repeatable)
      --wait                          Poll until the application is active or failed
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

