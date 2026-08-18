## runware serverless secrets remove

Remove an organisation secret

### Synopsis

Remove an organisation secret. Returns a conflict if any application still
has it attached — detach each holder with 'secrets detach' first.

```
runware serverless secrets remove <name> [flags]
```

### Examples

```
  # remove an organisation secret
  runware serverless secrets remove FOO
```

### Options

```
  -h, --help   help for remove
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

