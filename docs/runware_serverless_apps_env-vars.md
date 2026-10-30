## runware serverless apps env-vars

Manage plain-text environment variables for an application

### Synopsis

Manage plain-text environment variables on a serverless application.

These are not secrets. Values are returned by list and set.
Use 'serverless secrets' for encrypted secrets attached as env vars.

```
runware serverless apps env-vars [flags]
```

### Options

```
  -h, --help   help for env-vars
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications
* [runware serverless apps env-vars list](runware_serverless_apps_env-vars_list.md)	 - List environment variables for a serverless application
* [runware serverless apps env-vars set](runware_serverless_apps_env-vars_set.md)	 - Create or update environment variables
* [runware serverless apps env-vars unset](runware_serverless_apps_env-vars_unset.md)	 - Remove an environment variable

