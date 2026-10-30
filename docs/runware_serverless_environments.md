## runware serverless environments

Manage the organization's environments

### Synopsis

Manage the organization's environments and the defaults they inherit.

An environment holds one copy of each app deployed to it, and its own secrets.
production is the default environment: a command without --env addresses it.

An environment can cap the workers and GPUs it holds, set a monthly budget with
alert thresholds, and pause its workers once the budget is reached. A cap it
does not set is inherited from the organization defaults (see
'environments defaults').

```
runware serverless environments [flags]
```

### Options

```
  -h, --help   help for environments
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless](runware_serverless.md)	 - Manage Runware serverless applications
* [runware serverless environments create](runware_serverless_environments_create.md)	 - Create an environment
* [runware serverless environments defaults](runware_serverless_environments_defaults.md)	 - Manage the organization defaults environments inherit
* [runware serverless environments delete](runware_serverless_environments_delete.md)	 - Delete an environment
* [runware serverless environments list](runware_serverless_environments_list.md)	 - List environments
* [runware serverless environments show](runware_serverless_environments_show.md)	 - Show an environment
* [runware serverless environments update](runware_serverless_environments_update.md)	 - Update an environment's settings

