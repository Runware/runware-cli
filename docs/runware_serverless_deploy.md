## runware serverless deploy

Create or update a serverless application

### Synopsis

Create or update a serverless application from Python code or a container source.

The same --id can run once in each environment as a separate copy, with its
own versions, workers, variables and secrets. --env selects the copy this
deploy creates or updates, else the default environment (RUNWARE_ENV, else the
config's) or production.

A first deploy of an --id in an environment creates that copy. A later deploy
with the same --id and environment uploads a new source, records version N+1,
and rolls it when the build is ready. A source update on a stopped application
is 409.

Create-only flags are --gpu-type, --name, --max-workers, --idle-ttl,
--scaling-delay, --min-workers, --gpus-per-worker, --fallback-gpu-type,
--min-available-workers, --available-workers-pct, --volume, --secret,
--env-var and --env-file.

On an existing copy, a create-only flag is accepted when its value matches the
app, and a changed value is rejected; one error names every changed flag.
Change workers with 'apps scale', attach a secret later with 'secrets attach',
change environment variables with 'apps env-vars', and the display name with
'apps rename'. --volume is still always rejected on an existing app, because
the app read does not return volumes, so a repeated path cannot be told from a
changed one.

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
application is active or failed, and --timeout to bound that wait. A successful
wait is not a live worker:
minWorkers=0 stays scaled to zero until the first invoke.

--container cannot be combined with an entry file, --src-dir, --base-image,
--requirement, or --extra-index-url.

--from takes the URL of a packaged source: a zip whose root holds the app's
files and a runware.yaml recording the deploy options. The CLI fetches it,
reads that file, and deploys what it describes, so a published example deploys
in one command instead of a clone and a command. Flags still win over the
file, so the same archive goes to another GPU type or another --id without
being edited first. An --env-var or --env-file sets one key over the
manifest's and leaves the rest, while --volume, --requirement and --secret
replace the manifest's whole list, and --extra-index-url adds to the manifest
requirements. The archive supplies the source directory,
so --from cannot be combined with an entry file, --container, or --src-dir.

--from only creates. A manifest names an id, and deploying it over an
application that already has that id would roll the archive out as that
application's next version, running this code with the secrets, environment
variables and volumes it already has. So an existing id is refused: pass --id
to create a separate application, or unpack the archive and deploy the
directory to update the one that is there.

A runware.yaml carries no secret values. Its secrets name what the app needs,
and each one has to already exist in the environment. A manifest key this CLI
does not know is an error rather than a silent omission, which is what
configVersion is there to tell apart.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. A .gitignore is NOT consulted --
what a project keeps out of version control is a different question from what it
ships. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv, node_modules or the usual build and tool caches.

Pass --env-var or --env-file on create to set the application's initial
environment variables. Change a variable afterwards with 'apps env-vars set'
or 'apps env-vars unset': a change records a new version with the same image
and rolls the workload when the app is active, initializing, or failed and its
image is deployable. A stopped or stopping app applies it on resume. A write
during an in-flight rollout returns 409 and does not store the value. Prefer
--env-file for anything secret: a value passed as --env-var is visible in the
process list and recorded in shell history.

Anything the app downloads at runtime belongs on a --volume. Volumes are set
at create and cannot be changed afterwards: a later deploy that passes
--volume is rejected. The app runs in a sandbox whose filesystem is part of
the checkpointed state, so an unmounted download is copied into every
checkpoint and fetched again on every cold start. A volume keeps it out of
both.

--secret NAME, or NAME=ENV_VAR, attaches an existing secret of the same
environment at create so the first rollout carries it. Repeat the flag for
more than one. Attach or detach later with 'secrets attach' and
'secrets detach'.

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

  # create or update the staging copy of the application
  runware serverless deploy ./app.py --id my-app --gpu-type h100 --env staging

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
  runware serverless deploy ./app.py --id my-app --gpu-type h100 --wait --timeout 10m

  # deploy a packaged source, reading its id and options from its runware.yaml
  runware serverless deploy --from https://runware.ai/docs/serverless/examples/echo.zip

  # the same archive under another id and on another GPU type
  runware serverless deploy --from https://runware.ai/docs/serverless/examples/echo.zip \
    --id my-echo --gpu-type l40s
```

### Options

```
      --available-workers-pct int32   Idle-worker buffer as a percentage of load (0-100)
      --base-image string             Builder base image (code deploys only; needs Python 3.12 or newer) (default "python:3.12-slim")
      --container string              Directory whose root contains Dockerfile and container.yaml
  -e, --env name                      Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
      --env-file stringArray          File of KEY=VALUE lines to read environment variables from (repeatable)
      --env-var stringArray           Environment variable as KEY=VALUE (repeatable)
      --extra-index-url stringArray   Extra package index for a code deploy, as an http or https URL with no credentials (repeatable). uv reads the index; its first-index strategy takes a package this index also publishes from this index only, not pip's best match across indexes
      --fallback-gpu-type string      Secondary GPU type if the preferred type is unavailable
      --from string                   URL of a packaged source to fetch and deploy as a new application, reading its runware.yaml
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
      --secret stringArray            Secret of the same environment to attach at create, as NAME or NAME=ENV_VAR (repeatable)
      --src-dir string                Directory to package as the application source (default: the working directory; code deploys only)
      --timeout duration              Maximum time to wait (0 = no limit)
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

