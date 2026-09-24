## runware serverless apps env set

Create or update environment variables

### Synopsis

Create or update plain-text environment variables.

A single key is the [key] argument with --value or --value-file. Prefer
--value-file so the value is not visible in process lists; use --value-file -
to read from stdin. A single-key write during an in-flight rollout returns
409 and does not store the change.

Several keys are repeatable --env KEY=VALUE, or --env-file. The command reads
the current set, merges these keys in, and writes the set once, so one rollout
carries all of them. Keys you do not mention stay when no other writer changes
the set between the read and the write. That write returns 409 while a create
or resume rollout is already in progress, and does not store the change.

A change records a new version with the same image and rolls the workload when
the app is active, initializing, or failed and its image is deployable. A
stopped or stopping app applies it on resume. An unchanged value records no
version.

The server rejects (HTTP 422) reserved platform names, names that collide
with an attached secret's injected env var, and adding a binding past the
100-variable-plus-secret ceiling. Overwriting an existing key is always
allowed.

```
runware serverless apps env set <appId> [key] [flags]
```

### Examples

```
  # set one environment variable
  runware serverless apps env set my-app MY_KEY --value hello

  # read one value from a file
  runware serverless apps env set my-app MY_KEY --value-file ./value.txt

  # read one value from stdin
  printf '%s' "$MY_VALUE" | runware serverless apps env set my-app MY_KEY --value-file -

  # set several keys in one rollout
  runware serverless apps env set my-app --env FOO=bar --env BAZ=qux

  # set several keys from a file, in one rollout
  runware serverless apps env set my-app --env-file .env.deploy
```

### Options

```
      --env stringArray        Environment variable as KEY=VALUE, merged and written once (repeatable)
      --env-file stringArray   File of KEY=VALUE lines to merge and write once (repeatable)
  -h, --help                   help for set
      --value string           Variable value for a single <key> (visible in process lists; prefer --value-file)
      --value-file string      Read one <key> value from a file, or - for stdin
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps env](runware_serverless_apps_env.md)	 - Manage plain-text environment variables for an application

