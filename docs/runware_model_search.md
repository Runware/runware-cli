## runware model search

Search models available on the Runware platform

```
runware model search [flags]
```

### Examples

```
  # Search for realistic image models
  runware model search --search "realistic"

  # Filter to SDXL checkpoints only
  runware model search --search "portrait" --category checkpoint --architecture sdxl

  # Search only Runware's official models, sorted by name
  runware model search --search "flux" --source official --sort name

  # Filter by capability
  runware model search --source official --capabilities op:inpaint

  # Show extra columns including tags and default size
  runware model search --search "anime" --wide

  # List your private models
  runware model search --search "my-model" --visibility private

  # Paginate through results
  runware model search --search "anime" --limit 10 --offset 20
```

### Options

```
  -a, --architecture string    Filter by model architecture (e.g. sdxl, flux)
      --capabilities strings   Filter by capabilities (repeatable: --capabilities io:text-to-image --capabilities op:inpaint)
  -c, --category string        Filter by category: checkpoint, lora, lycoris, vae, embeddings
  -h, --help                   help for search
  -l, --limit int              Maximum number of results to return (1-100) (default 20)
      --offset int             Number of results to skip for pagination
  -q, --search string          Search query (name, description, or AIR ID), 2-48 characters
      --sort string            Sort order: _score, popularity, name, addedUnixTimestamp or updatedDateUnixTimestamp. A - prefix reverses any of them except _score (default: _score with --search, popularity without)
      --source string          Filter by model source: official, community (default: both)
      --visibility string      Filter by visibility: public, private, favorite, owned (default "public")
  -W, --wide                   Show additional columns: private, default size, tags
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware model](runware_model.md)	 - Manage and search models

