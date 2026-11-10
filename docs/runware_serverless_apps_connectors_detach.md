## runware serverless apps connectors detach

Detach an OIDC connector from an application

### Synopsis

Detach an OIDC connector from an application. The connector itself remains.

Detaching rolls the application's live deployment, so its workers stop getting
a token. Running workers keep the connector until they roll, and a token a
worker already holds stays valid until it expires, at most an hour.

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

