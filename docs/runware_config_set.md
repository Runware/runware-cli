## runware config set

Set a config value

```
runware config set <key> <value> [flags]
```

### Examples

```
  # set the default output directory
  runware config set output_dir ~/my-images

  # set the default output format (table, json, yaml)
  runware config set format json

  # set the default transport (ws, http)
  runware config set transport http
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

* [runware config](runware_config.md)	 - Manage CLI configuration

