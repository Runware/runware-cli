## runware serverless secrets detach

Detach a secret from an application

### Synopsis

Remove an organization secret's attachment from an application. The organization
secret itself remains.

Detaching rolls the live deployment so a running worker stops receiving the
value. If a rollout is already in progress, the worker stops receiving it on
the next deploy. An application that is not live records the removal only.

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

* [runware serverless secrets](runware_serverless_secrets.md)	 - Manage organization secrets for serverless applications

