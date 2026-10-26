## runware serverless secrets

Manage secrets for serverless applications

### Synopsis

Manage encrypted secrets and attach them to serverless applications.

A secret belongs to one environment, and an application attaches only secrets
of its own environment. Every command here takes --env to select that
environment, else the default environment (RUNWARE_ENV, else the config's) or production.

```
runware serverless secrets [flags]
```

### Options

```
  -e, --env name   Environment the secret belongs to (default RUNWARE_ENV, else the config's environment, else production)
  -h, --help       help for secrets
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
* [runware serverless secrets attach](runware_serverless_secrets_attach.md)	 - Attach a secret to an application
* [runware serverless secrets attachments](runware_serverless_secrets_attachments.md)	 - List secrets attached to an application
* [runware serverless secrets detach](runware_serverless_secrets_detach.md)	 - Detach a secret from an application
* [runware serverless secrets list](runware_serverless_secrets_list.md)	 - List secrets
* [runware serverless secrets remove](runware_serverless_secrets_remove.md)	 - Remove a secret
* [runware serverless secrets set](runware_serverless_secrets_set.md)	 - Create or update a secret

