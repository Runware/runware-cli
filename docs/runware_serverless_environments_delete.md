## runware serverless environments delete

Delete an environment

### Synopsis

Delete an environment that has no apps and no secrets left.

The API refuses an environment that still holds an app or a secret; delete
those first with 'apps delete' and 'secrets remove' under the same --env.
production cannot be deleted. The name becomes free for a new environment.

Confirmation is required unless --yes or --force is passed.

```
runware serverless environments delete <name> [flags]
```

### Examples

```
  # delete an environment (prompts for confirmation)
  runware serverless environments delete staging

  # skip the confirmation prompt
  runware serverless environments delete staging --yes
```

### Options

```
      --force   Skip the confirmation prompt
  -h, --help    help for delete
  -y, --yes     Skip the confirmation prompt
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless environments](runware_serverless_environments.md)	 - Manage the organization's environments

