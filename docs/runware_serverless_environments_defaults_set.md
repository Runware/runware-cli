## runware serverless environments defaults set

Change the organization defaults

### Synopsis

Change the organization defaults the flags name and keep the rest as they are.

--clear <field> removes a default cap, and takes max-workers or max-gpus;
repeat it or separate fields with commas. The change reaches every environment
that inherits the cap at once; it does not restart running apps.

```
runware serverless environments defaults set [flags]
```

### Examples

```
  # cap every environment that sets no cap of its own at 10 workers
  runware serverless environments defaults set --max-workers 10

  # remove the default GPU cap
  runware serverless environments defaults set --clear max-gpus
```

### Options

```
      --clear strings       Clear a setting: max-workers, max-gpus (repeatable)
  -h, --help                help for set
      --max-gpus int32      GPU cap of an environment that sets none of its own
      --max-workers int32   Worker cap of an environment that sets none of its own
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless environments defaults](runware_serverless_environments_defaults.md)	 - Manage the organization defaults environments inherit

