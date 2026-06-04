## runware config set

Set a config value

```
runware config set [key] [value] [flags]
```

### Examples

```
  # set default model
  runware config set model "runware:100@1"

  # set default output format
  runware config set format json
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware config](runware_config.md)	 - Manage CLI configuration

