## runware serverless secrets

Manage organization secrets for serverless applications

### Synopsis

Manage organization-scoped encrypted secrets, and attach them to serverless applications.

```
runware serverless secrets [flags]
```

### Options

```
  -h, --help   help for secrets
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
* [runware serverless secrets attach](runware_serverless_secrets_attach.md)	 - Attach an organization secret to an application
* [runware serverless secrets attachments](runware_serverless_secrets_attachments.md)	 - List secrets attached to an application
* [runware serverless secrets detach](runware_serverless_secrets_detach.md)	 - Detach a secret from an application
* [runware serverless secrets list](runware_serverless_secrets_list.md)	 - List organization secrets
* [runware serverless secrets remove](runware_serverless_secrets_remove.md)	 - Remove an organization secret
* [runware serverless secrets set](runware_serverless_secrets_set.md)	 - Create or update an organization secret

