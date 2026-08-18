## runware serverless secrets attach

Attach an organisation secret to an application

### Synopsis

Record that an organisation secret is attached to an application, optionally
under a different environment variable name.

The organisation secret must already exist (see 'secrets set'). This is a
control-plane association only in this API release — it does not roll workers.

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
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage organisation secrets for serverless applications

