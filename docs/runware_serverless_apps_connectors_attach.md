## runware serverless apps connectors attach

Attach an OIDC connector to an application

### Synopsis

Attach an OIDC connector to an application and print it with the subject its
workers' tokens carry and the subjectPattern that matches every application of
the organization in this environment.

Attaching rolls the application's live deployment, so its workers start
getting a token. An application that is not live, or already has a rollout in
progress, picks the connector up on its next rollout.

An application holds at most one connector of each kind. The env names a
connector sets on the worker are reserved while it is attached: attaching is
refused when the application already has a variable or secret under one.

```
runware serverless apps connectors attach <appId> <connector> [flags]
```

### Examples

```
  runware serverless apps connectors attach my-app aws-prod

  # the staging copy of the application
  runware serverless apps connectors attach my-app aws-prod --env staging
```

### Options

```
  -h, --help   help for attach
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

