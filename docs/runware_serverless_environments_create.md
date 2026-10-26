## runware serverless environments create

Create an environment

### Synopsis

Create an environment in the organization.

A cap left unset inherits the organization default. A budget left unset means
no budget. Budget alerts default to 50, 80 and 100 percent of the budget, and
--pause-at-budget defaults to off.

Deploy to the new environment with --env <name> on deploy and app commands.

```
runware serverless environments create <name> [flags]
```

### Examples

```
  # create an environment that inherits every default
  runware serverless environments create staging

  # cap it at 4 workers and a 50 USD monthly budget, pausing at the budget
  runware serverless environments create staging --max-workers 4 --budget 50 --pause-at-budget

  # alert at 25 and 75 percent of the budget
  runware serverless environments create staging --budget 100.00 --alert-percents 25,75
```

### Options

```
      --alert-percents ints   Percentages of the budget that send an alert email, e.g. 50,80,100
      --budget string         Monthly budget in USD, e.g. 50 or 49.99
  -h, --help                  help for create
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

