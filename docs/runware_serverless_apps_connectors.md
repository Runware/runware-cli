## runware serverless apps connectors

Manage the OIDC connectors attached to an application

### Synopsis

Attach OIDC connectors to an application and detach them.

An application holds at most one connector of each kind (aws, gcp, generic),
and an attachment belongs to the copy of the application in one environment:
the one --env names, else the default environment (RUNWARE_ENV, else the
config's) or production. The same connector attached in another environment
has another subject.

Create connectors with 'serverless connectors create'.

```
runware serverless apps connectors [flags]
```

### Options

```
  -h, --help   help for connectors
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -e, --env name           Environment the app runs in (default RUNWARE_ENV, else the config's environment, else production)
      --environment name   Alias for --env
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless apps](runware_serverless_apps.md)	 - Manage deployed serverless applications
* [runware serverless apps connectors attach](runware_serverless_apps_connectors_attach.md)	 - Attach an OIDC connector to an application
* [runware serverless apps connectors detach](runware_serverless_apps_connectors_detach.md)	 - Detach an OIDC connector from an application
* [runware serverless apps connectors list](runware_serverless_apps_connectors_list.md)	 - List the OIDC connectors attached to an application

