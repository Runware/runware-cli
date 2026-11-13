## runware serverless connectors create

Create an OIDC connector

### Synopsis

Create an OIDC connector. --kind is fixed once created.

  aws      needs --role-arn. The worker gets AWS_ROLE_ARN and
           AWS_WEB_IDENTITY_TOKEN_FILE, which the AWS SDKs read unchanged.
  gcp      needs --workload-identity-provider, and takes an optional
           --service-account-email to impersonate. The worker gets
           GOOGLE_APPLICATION_CREDENTIALS, which google-auth reads unchanged.
  generic  needs --audience. The worker gets the token file and
           RUNWARE_OIDC_TOKEN_FILE.

Creating a connector does not give any application a token. Attach it with
'apps connectors attach'; that prints the subject a trust rule should match.

```
runware serverless connectors create <name> [flags]
```

### Examples

```
  # AWS
  runware serverless connectors create aws-prod --kind aws \
    --role-arn arn:aws:iam::123456789012:role/runware-worker

  # GCP
  runware serverless connectors create gcp-prod --kind gcp \
    --workload-identity-provider projects/123456789/locations/global/workloadIdentityPools/runware/providers/runware \
    --service-account-email worker@my-project.iam.gserviceaccount.com

  # any other OIDC verifier
  runware serverless connectors create vault --kind generic --audience https://vault.example.com
```

### Options

```
      --audience string                     Token audience (aud). Required for generic; aws defaults to runware:<organization uuid>; gcp derives it from the provider and refuses it
  -h, --help                                help for create
      --kind string                         Provider: aws, gcp or generic
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

