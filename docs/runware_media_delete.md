## runware media delete

Permanently delete stored media by its UUID

### Synopsis

Permanently remove media previously uploaded to your Runware account,
identified by its mediaUUID. This cannot be undone.

```
runware media delete <mediaUUID> [flags]
```

### Examples

```
  # delete a stored asset by its UUID
  runware media delete 5f1d2c3b-8a4e-4c2a-9f1a-2b3c4d5e6f70
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware media](runware_media.md)	 - Store and delete media in your Runware account

