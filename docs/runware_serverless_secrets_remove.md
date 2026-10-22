## runware serverless secrets remove

Remove a secret

### Synopsis

Remove a secret from one environment. Returns a conflict if any application in
that environment still has it attached — detach each holder first with
'secrets detach <appId> <name>' under the same --env.

```
runware serverless secrets remove <name> [flags]
```

### Examples

```
  # remove a production secret
  runware serverless secrets remove FOO

  # remove the staging copy
  runware serverless secrets remove FOO --env staging
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the secret belongs to (default production)
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage secrets for serverless applications

