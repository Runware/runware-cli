## runware serverless connectors trust

Print the cloud commands that make a provider trust an application's tokens

### Synopsis

Print the commands that set up the cloud side of an attached OIDC connector, with
every value filled in from the connector and its attachment:

  aws      aws iam commands for the OIDC identity provider and the role, with the
           trust policy JSON
  gcp      gcloud commands for the workload identity pool and provider, with its
           attribute condition, and the IAM binding for the application's identity
  generic  the iss, aud and sub a verifier of your own checks

The output is shell and runs as printed. The one value it cannot know is the
bucket a gcp connector without a service account grants access to: set the
BUCKET variable first. Nothing is run or changed by this command.

The connector must be attached to the application in the environment (see
'apps connectors attach'), because the subject exists only then.

--scope app (the default) trusts the tokens of this application only.
--scope environment trusts every application of the organization in the
environment, so use it only when all of them may reach the resource. Tokens of
another environment are never trusted by either.

```
runware serverless connectors trust <name> [flags]
```

### Examples

```
  runware serverless connectors trust aws-prod --app my-app

  # every application of the staging environment
  runware serverless connectors trust aws-prod --app my-app --env staging --scope environment

  # run the printed commands
  BUCKET=my-inputs sh -c "$(runware serverless connectors trust gcp-prod --app my-app)"
```

### Options

```
      --app string         Application whose tokens to trust
  -e, --env name           Environment the application runs in (default RUNWARE_ENV, else the config's environment, else production)
      --environment name   Alias for --env
  -h, --help               help for trust
      --scope string       Whose tokens to trust: app (this application) or environment (every application of the organization in it) (default "app")
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

