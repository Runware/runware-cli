## runware serverless environments update

Update an environment's settings

### Synopsis

Change the settings the flags name and leave the rest as they are.

--clear <field> clears a setting, and takes one of max-workers, max-gpus,
budget and alert-percents; repeat it or separate fields with commas. A cleared
cap inherits the organization default again, a cleared budget means no budget,
and cleared alert percents send no budget alert.

```
runware serverless environments update <name> [flags]
```

### Examples

```
  # raise the worker cap
  runware serverless environments update staging --max-workers 8

  # stop pausing at the budget
  runware serverless environments update staging --pause-at-budget=false

  # inherit the organization's worker cap again and remove the budget
  runware serverless environments update staging --clear max-workers,budget
```

### Options

```
      --alert-percents ints   Percentages of the budget that send an alert email, e.g. 50,80,100
      --budget string         Monthly budget in USD, e.g. 50 or 49.99
      --clear strings         Clear a setting: max-workers, max-gpus, budget, alert-percents (repeatable)
  -h, --help                  help for update
      --max-gpus int32        Cap on the GPUs the environment holds
      --max-workers int32     Cap on the workers the environment holds
      --pause-at-budget       Scale the environment's workers to zero once the budget is reached
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

