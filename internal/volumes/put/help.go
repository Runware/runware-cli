package put

import "github.com/runware/runware-cli/internal/config"

// Help text of volumes put: additive uploads, and no promise to delete.
const (
	escapingSymlinkAdvice = "remove the link or replace it with a copy of its target"

	HelpShort = "Upload files and directories to a Volume"

	HelpLong = `Upload local files and directories to a Volume.

put needs your organization id. Copy it from the Runware console, then either set the
` + config.OrganizationFromEnvVar + ` environment variable or add the line "organization: <id>" at the
top level of ~/.runware/config.yaml (config set cannot set it).

Uploads are additive. The paths you give are added to the Volume's latest files, or
replace the file already at the same path; files you do not name stay as they are. A
directory's files keep their paths relative to the directory, under --to when you give
it and at the Volume root otherwise. Links inside a directory must point to files
within it. If a link points outside the local directory, ` + escapingSymlinkAdvice + `.

Workers that start after an upload get the new files. Workers that are already running
keep the files they started with.

Rolling an app back re-applies its earlier image, not earlier Volume files. To go
back to older files, upload them again.

To start from an empty set of files, create a new Volume and upload into it.

Content the organization already stores is never sent again, so repeating an upload
that changes nothing sends no bytes. A file must not change while it uploads: if one
does, put stops, names the file and publishes nothing.

put sends up to --concurrency parts at once, one connection each. Each part streams
from the file, so a part in flight holds about 1 MiB of memory.

If put is interrupted, run the same command again to continue the upload. Parts
whose acknowledgements were saved are skipped. If a file changed, put aborts the
saved upload. Run the command again to start a new session.`

	HelpExample = `  # upload a directory to the root of a Volume
  runware serverless volumes put model-weights ./weights

  # add one file under a directory in the Volume
  runware serverless volumes put model-weights ./tokenizer.json --to tokenizer`
)
