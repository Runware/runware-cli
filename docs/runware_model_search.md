## runware model search

Search available models

### Synopsis

Search for models available on Runware.

Examples:
  runware model search "flux"
  runware model search "sdxl" --category checkpoint
  runware model search --architecture flux1d --limit 10
  runware model search "portrait" --category lora --format json

```
runware model search [query] [flags]
```

### Options

```
  -a, --architecture string   Filter by architecture: flux1d, sdxl, sd15, etc.
  -C, --category string       Filter by category: checkpoint, lora, etc.
  -h, --help                  help for search
  -l, --limit int             Maximum number of results (default 20)
  -O, --offset int            Offset for pagination
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware model](runware_model.md)	 - Manage and search models

