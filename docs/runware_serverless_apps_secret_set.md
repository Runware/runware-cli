## runware serverless apps secret set

Set a secret on a serverless application

```
runware serverless apps secret set <deploymentId> [flags]
```

### Examples

```
  # attach or update a secret on an application
  runware serverless apps secret set my-app
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps secret](runware_serverless_apps_secret.md)	 - Manage secrets for a serverless application

