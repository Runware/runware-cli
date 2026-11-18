## runware serverless apps connectors detach

Detach an OIDC connector from an application

### Synopsis

Detach an OIDC connector from an application. The connector itself remains.

Detaching rolls the application's live deployment, so its workers stop getting
a token. Running workers keep the connector until they roll, and a token a
worker already holds stays valid until it expires, at most an hour. An
application that is not live loses the connector on its next rollout.

If an application already has a rollout in progress, it rolls again on its own
once that rollout finishes. 'runware serverless apps events' shows "OIDC connector change accepted: rolls when the rollout in progress finishes"
right away, then "change made during the previous rollout: rolling to pick it
up" when the second rollout starts. If that rollout fails or leaves the
application not live, the application picks the change up on its next rollout.
If the second rollout cannot be deployed, 'runware serverless apps events --type
error' shows "credential change not applied" and the workers keep the previous
configuration until the application is redeployed.

```
runware serverless apps connectors detach <appId> <connector> [flags]
```

### Examples

```
  runware serverless apps connectors detach my-app aws-prod
```

### Options

```
  -h, --help   help for detach
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
      --environment name   Alias for --env
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps connectors](runware_serverless_apps_connectors.md)	 - Manage the OIDC connectors attached to an application

