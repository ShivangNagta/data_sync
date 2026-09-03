// Files the synchronizer must never track: our own transient download temp
// files (recording those creates an upload/re-download loop and hash failures
// since they're renamed away), and platform junk like .DS_Store. Excluded
// files are left untouched on disk; they simply never appear in the manifest.

package client

import (
	"path/filepath"
	"strings"
)

// ignoredBase lists base names that are always excluded from sync.
var ignoredBase = map[string]bool{
	".ds_store": true,
}

// isIgnored reports whether a slash-separated relative path should be
// excluded from sync.
func isIgnored(rel string) bool {
	return strings.HasPrefix(filepath.Base(rel), ".sync-tmp-") ||
		ignoredBase[strings.ToLower(filepath.Base(rel))]
}
