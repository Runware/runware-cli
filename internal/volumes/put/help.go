package put

// Help text of volumes put: additive uploads, and no promise to delete.
const (
	HelpShort = "Upload files and directories to a Volume"

	HelpLong = `Upload local files and directories to a Volume.

Uploads are additive. The paths you give are added to the Volume's latest files, or
replace the file already at the same path; files you do not name stay as they are. A
directory's files keep their paths relative to the directory, under --to when you give
it and at the Volume root otherwise.

Workers that start after an upload get the new files. Workers that are already running
keep the files they started with.

Rolling an app back restores its image and settings, not earlier Volume files. To go
back to older files, upload them again.

To start from an empty set of files, create a new Volume and upload into it.

Content the organization already stores is never sent again, so repeating an upload
that changes nothing sends no bytes. A file must not change while it uploads: if one
does, put stops, names the file and publishes nothing.`

	HelpExample = `  # upload a directory to the root of a Volume
  runware serverless volumes put model-weights ./weights

  # add one file under a directory in the Volume
  runware serverless volumes put model-weights ./tokenizer.json --to tokenizer`
)
