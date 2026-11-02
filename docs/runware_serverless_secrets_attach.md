## runware serverless secrets attach

Attach a secret to an application

### Synopsis

Record that a secret is attached to an application, optionally under a
different environment variable name.

The secret must already exist in the application's environment (see
'secrets set'). Attaching rolls the live deployment so a running worker picks
up the value. If a rollout is already in progress, this attach reaches the
worker on the next deploy. An application that is not live records the
attachment only; the next deploy or resume reads it.

```
runware serverless secrets attach <appId> <name> [flags]
```

### Examples

```
  # attach a secret using its name as the env var
  runware serverless secrets attach my-app FOO

  # inject under a different env var name
  runware serverless secrets attach my-app FOO --env-var-name FOO_KEY
```

### Options

```
      --env-var-name string   Environment variable name (omit to use the secret name)
  -h, --help                  help for attach
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the secret belongs to (default RUNWARE_ENV, else the config's environment, else production)
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage secrets for serverless applications

