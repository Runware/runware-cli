## runware serverless init

Create an App directory with the Serverless library installed

### Synopsis

Create an App directory: the folder that holds one App, its pyproject.toml and
its own .venv. dir defaults to the current directory, and is created when it
does not exist.

init writes what is missing and nothing else:

  pyproject.toml  a minimal project named after the directory, requiring
                  Python 3.10 or newer, with no runtime dependencies
  app.py          a starter App with @serve, a load method and one @endpoint,
                  written only when the directory holds no .py files
  .venv           the directory's own environment, with runware-serverless
                  added as a dev dependency

The toolchain is uv, which must be on PATH. init runs
'uv add --dev runware-serverless' inside the App directory, which creates .venv
and uv.lock and records the dependency in pyproject.toml. init never installs
into a global, user or tool environment, so PEP 668 does not apply.

A directory that already has a pyproject.toml is an App directory: init keeps
that file and adds only what is missing, so running it again is safe. A
non-empty directory without a pyproject.toml is refused unless you pass --force.

The Serverless library is an editor aid. A deploy strips it, supplies the
platform's own copy, and does not upload .venv.

```
runware serverless init [dir] [flags]
```

### Examples

```
  # create ./my-app and install the Serverless library into its .venv
  runware serverless init my-app

  # make the current directory an App directory
  runware serverless init

  # use a directory that already holds files but no pyproject.toml
  runware serverless init --force ./existing-code
```

### Options

```
      --force   Use a non-empty directory that has no pyproject.toml
  -h, --help    help for init
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

