## runware serverless apps env

Manage plain-text environment variables for an application

### Synopsis

Manage plain-text environment variables on a serverless application.

These are not organisation secrets. Values are returned by list and set.
Use 'serverless secrets' for encrypted secrets attached as env vars.

```
runware serverless apps env [flags]
```

### Options

```
  -h, --help   help for env
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
* [runware serverless apps env list](runware_serverless_apps_env_list.md)	 - List environment variables for a serverless application
* [runware serverless apps env set](runware_serverless_apps_env_set.md)	 - Create or update an environment variable
* [runware serverless apps env unset](runware_serverless_apps_env_unset.md)	 - Remove an environment variable

