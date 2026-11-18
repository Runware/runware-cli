## runware serverless secrets set

Create or update a secret

### Synopsis

Create a secret in one environment, or update its value if the name already
exists there. The same name in another environment is a different secret.

Creating a secret does not attach it to an application. Use 'secrets attach' for that.
Updating an existing secret re-encrypts the value and rolls every live application
that attaches it, so a running worker picks up the new value. An application that
is not live picks it up on its next deploy.

If an application already has a rollout in progress, it rolls again on its own
once that rollout finishes. 'runware serverless apps events' shows "secret change accepted: rolls when the rollout in progress finishes"
right away, then "change made during the previous rollout: rolling to pick it
up" when the second rollout starts. If that rollout fails or leaves the
application not live, the application picks the change up on its next rollout.
If the second rollout cannot be deployed, 'runware serverless apps events --type
error' shows "credential change not applied" and the workers keep the previous
configuration until the application is redeployed.

The secret value is never printed. Prefer --value-file so the value is not visible
in process lists; use --value-file - to read from stdin.

```
runware serverless secrets set <name> [flags]
```

### Examples

```
  # create or update a secret from a file
  runware serverless secrets set FOO --value-file ./foo.txt

  # read the value from stdin
  printf '%s' "$FOO" | runware serverless secrets set FOO --value-file -

  # the staging copy of the secret
  runware serverless secrets set FOO --value-file ./foo.txt --env staging

  # then attach it to an application
  runware serverless secrets attach my-app FOO
```

### Options

```
  -h, --help                help for set
      --value string        Secret value (visible in process lists; prefer --value-file)
      --value-file string   Read secret value from a file, or - for stdin
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the secret belongs to (default RUNWARE_ENV, else the config's environment, else production)
      --environment name   Alias for --env
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage secrets for serverless applications

