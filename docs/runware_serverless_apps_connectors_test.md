## runware serverless apps connectors test

Test the trust setup of an attached OIDC connector

### Synopsis

Test the trust setup between an application and a connector's cloud without
deploying any code, and print the outcome. A test needs an aws or gcp connector
that is attached to the application in this environment.

The platform runs a short-lived probe as a worker of the application. The probe
gets the application's token as a worker does and exchanges it with the cloud:
AWS AssumeRoleWithWebIdentity, or Google STS and, when the connector names a
service account, impersonation of it. It obtains real, short-lived credentials
and discards them. No token or credential is returned, stored or printed. On AWS
the session is named runware-connector-test-<test id>, so a test shows in
CloudTrail.

The outcome is one of:
  ok              the cloud accepted the token; the assumed role or impersonated
                  service account is printed
  cloud_error     the cloud refused the token; its code, message and a
                  troubleshooting hint are printed, with the stage it refused at
  platform_error  the platform could not run the test; the reason and what went
                  wrong are printed (with the issuer's code for issuer_refused),
                  and they say nothing about the trust setup

The command exits non-zero unless the outcome is ok. A result normally arrives
within a minute and always within two; a --timeout below two minutes stops
waiting sooner. A poll that fails on a server or network error is retried until
--timeout.

One test of each connector runs at a time for an application in an environment,
and an organization may start 20 tests an hour (60 that end in platform_error).
Past either the API refuses, and the refusal says when to retry.

```
runware serverless apps connectors test <appId> <connector> [flags]
```

### Examples

```
  runware serverless apps connectors test my-app aws-prod

  # the staging copy of the application
  runware serverless apps connectors test my-app aws-prod --env staging

  # machine-readable outcome
  runware serverless apps connectors test my-app aws-prod -F json
```

### Options

```
  -h, --help                     help for test
      --poll-interval duration   Polling interval when waiting for the result (default 2s)
      --timeout duration         Maximum time to wait for the result (default 2m30s)
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

* [runware serverless apps connectors](runware_serverless_apps_connectors.md)	 - Manage the OIDC connectors attached to an application

