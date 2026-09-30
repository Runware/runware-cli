## runware serverless apps scale

Scale a serverless application

### Synopsis

Patch live worker configuration for a serverless application.

Omitted flags are left unchanged. A configuration change records a new version
with the same image and rolls the workload when the app is active or
initializing. A failed app is moved to initializing and rolled. A stopped or
stopping app applies the change on resume. This command does not wait for that
rollout. A change during an in-flight rollout returns 409.

The server rejects unsupported or invalid fields with HTTP 422. A capacity
increase without enough credit returns 402.

```
runware serverless apps scale <appId> [flags]
```

### Examples

```
  # set the worker cap
  runware serverless apps scale my-app --max-workers 2

  # scale to zero and raise idle TTL
  runware serverless apps scale my-app --min-workers 0 --idle-ttl 120

  # change GPU type; the rollout replaces workers with the new type
  runware serverless apps scale my-app --gpu-type h100
```

### Options

```
      --available-workers-pct int32   Idle-worker buffer as a percentage of load (0-100)
      --fallback-gpu-type string      Secondary GPU type if the preferred type is unavailable
      --gpu-type string               Preferred GPU type ID (see 'serverless gpus')
      --gpus-per-worker int32         GPUs allocated per worker (1, 2, 4, or 8)
  -h, --help                          help for scale
      --idle-ttl int32                Idle TTL in seconds before scaling down
      --max-workers int32             Maximum number of workers
      --min-available-workers int32   Minimum idle workers kept as a buffer
      --min-workers int32             Minimum number of workers (0 = scale to zero)
      --scaling-delay int32           Scaling delay in seconds
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications

