## runware serverless secrets detach

Detach a secret from an application

### Synopsis

Remove the control-plane attachment from an application. Does not remove the
organisation secret.

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
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage organisation secrets for serverless applications

