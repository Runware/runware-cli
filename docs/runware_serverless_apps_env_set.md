## runware serverless apps env set

Create or update an environment variable

### Synopsis

Create or update one plain-text environment variable.

Prefer --value-file so the value is not visible in process lists; use
--value-file - to read from stdin.

The server rejects (HTTP 422) reserved platform names, names that collide
with an attached secret's injected env var, and adding a binding past the
100-variable-plus-secret ceiling. Overwriting an existing key is always
allowed.

```
runware serverless apps env set <appId> <key> [flags]
```

### Examples

```
  # set an environment variable
  runware serverless apps env set my-app MY_KEY --value hello

  # read the value from a file
  runware serverless apps env set my-app MY_KEY --value-file ./value.txt

  # read the value from stdin
  printf '%s' "$MY_VALUE" | runware serverless apps env set my-app MY_KEY --value-file -
```

### Options

```
  -h, --help                help for set
      --value string        Variable value (visible in process lists; prefer --value-file)
      --value-file string   Read variable value from a file, or - for stdin
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps env](runware_serverless_apps_env.md)	 - Manage plain-text environment variables for an application

