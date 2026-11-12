## runware serverless connectors

Manage OIDC connectors

### Synopsis

Manage OIDC connectors: the external providers (AWS, GCP or any OIDC verifier)
that an application's workers authenticate to with a short-lived token the
platform signs, instead of a stored credential. A connector holds no secret.

Connectors belong to the organization, not to an environment. Attach one to an
application with 'apps connectors attach'.

```
runware serverless connectors [flags]
```

### Options

```
  -h, --help   help for connectors
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
* [runware serverless connectors create](runware_serverless_connectors_create.md)	 - Create an OIDC connector
* [runware serverless connectors delete](runware_serverless_connectors_delete.md)	 - Delete an OIDC connector
* [runware serverless connectors list](runware_serverless_connectors_list.md)	 - List OIDC connectors
* [runware serverless connectors show](runware_serverless_connectors_show.md)	 - Show an OIDC connector
* [runware serverless connectors trust](runware_serverless_connectors_trust.md)	 - Print the cloud commands that make a provider trust an application's tokens
* [runware serverless connectors update](runware_serverless_connectors_update.md)	 - Update an OIDC connector

