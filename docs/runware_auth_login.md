## runware auth login

Authenticate with an API key

```
runware auth login [flags]
```

### Examples

```
  # authenticate interactively
  runware auth login

  # pass API key directly
  runware auth login --key YOUR_API_KEY
```

### Options

```
  -h, --help         help for login
  -k, --key string   API key (or provide interactively)
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware auth](runware_auth.md)	 - Manage authentication

