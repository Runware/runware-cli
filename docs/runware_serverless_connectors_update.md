## runware serverless connectors update

Update an OIDC connector

### Synopsis

Change a connector's audience or provider fields. Fields you do not name keep
their current value. The name and kind cannot change.

--clear service-account-email removes a gcp connector's service account, so its
workers use the federated principal directly instead of impersonating one. Grant
the federated principal access to your resources first ('connectors trust' prints
the grant for a connector with no service account): the clear rolls every live
application that attaches the connector, and the new workers get a permission
error on any resource only the service account could read. Clearing a service
account the connector does not have is refused when nothing else changes.

The update fails rather than overwrite a change someone else made to the
connector after it was read.

Updating rolls every live application that attaches the connector, so its
workers get the new configuration. Running workers keep the old configuration
until they roll. An application that is not live picks up the new configuration
on its next rollout.

If an application already has a rollout in progress, it rolls again on its own
once that rollout finishes. 'runware serverless apps events' shows "OIDC connector change accepted: rolls when the rollout in progress finishes"
right away, then "change made during the previous rollout: rolling to pick it
up" when the second rollout starts. If that rollout fails or leaves the
application not live, the application picks the change up on its next rollout.
If the second rollout cannot be deployed, 'runware serverless apps events --type
error' shows "credential change not applied" and the workers keep the previous
configuration until the application is redeployed.

```
runware serverless connectors update <name> [flags]
```

### Examples

```
  runware serverless connectors update aws-prod \
    --role-arn arn:aws:iam::123456789012:role/runware-worker-v2

  # stop impersonating a service account
  runware serverless connectors update gcp-prod --clear service-account-email
```

### Options

```
      --audience string                     Token audience (aud). Required for generic; aws defaults to runware:<organization uuid>; gcp derives it from the provider and refuses it
      --clear strings                       Clear a setting: service-account-email (repeatable)
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

