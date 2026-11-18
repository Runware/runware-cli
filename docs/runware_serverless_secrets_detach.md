## runware serverless secrets detach

Detach a secret from an application

### Synopsis

Remove a secret's attachment from an application. The secret itself remains.

Detaching rolls the live deployment so a running worker stops receiving the
value. An application that is not live records the removal only.

If an application already has a rollout in progress, it rolls again on its own
once that rollout finishes. 'runware serverless apps events' shows "secret change accepted: rolls when the rollout in progress finishes"
right away, then "change made during the previous rollout: rolling to pick it
up" when the second rollout starts. If that rollout fails or leaves the
application not live, the application picks the change up on its next rollout.
If the second rollout cannot be deployed, 'runware serverless apps events --type
error' shows "credential change not applied" and the workers keep the previous
configuration until the application is redeployed.

```
runware serverless secrets detach <appId> <name> [flags]
```

### Examples

```
  # detach a secret from an application
  runware serverless secrets detach my-app FOO
```

### Options

```
  -h, --help   help for detach
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

