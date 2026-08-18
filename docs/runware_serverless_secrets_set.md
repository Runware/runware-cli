## runware serverless secrets set

Create or update an organisation secret

### Synopsis

Create an organisation-scoped secret, or update its value if the name already exists.

This does not attach the secret to an application. Use 'secrets attach' for that.
The secret value is never printed. Prefer --value-file so the value is not visible
in process lists; use --value-file - to read from stdin.

```
runware serverless secrets set <name> [flags]
```

### Examples

```
  # create or update a secret from a file
  runware serverless secrets set FOO --value-file ./foo.txt

  # read the value from stdin
  printf '%s' "$FOO" | runware serverless secrets set FOO --value-file -

  # then attach it to an application
  runware serverless secrets attach my-app FOO
```

### Options

```
  -h, --help                help for set
      --value string        Secret value (visible in process lists; prefer --value-file)
      --value-file string   Read secret value from a file, or - for stdin
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

