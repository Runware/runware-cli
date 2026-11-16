## runware serverless connectors update

Update an OIDC connector

### Synopsis

Change a connector's audience or provider fields. Fields you do not name keep
their current value. The name and kind cannot change.

--clear-service-account-email removes a gcp connector's service account, so its
workers use the federated principal directly instead of impersonating one.

Updating rolls every live application that attaches the connector, so its
workers get the new configuration. Running workers keep the old configuration
until they roll.

```
runware serverless connectors update <name> [flags]
```

### Examples

```
  runware serverless connectors update aws-prod \
    --role-arn arn:aws:iam::123456789012:role/runware-worker-v2

  # stop impersonating a service account
  runware serverless connectors update gcp-prod --clear-service-account-email
```

### Options

```
      --audience string                     Token audience (aud). Required for generic; aws defaults to runware:<organization uuid>; gcp derives it from the provider and refuses it
      --clear-service-account-email         gcp: remove the service account, so workers use the federated principal directly
  -h, --help                                help for update
      --role-arn string                     aws: IAM role the worker assumes
      --service-account-email string        gcp: service account to impersonate (optional)
      --workload-identity-provider string   gcp: Workload Identity Federation provider resource name
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware serverless connectors](runware_serverless_connectors.md)	 - Manage OIDC connectors

