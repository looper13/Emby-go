//go:build !unix

package regularfile

// Handle checks still apply; unlike Unix, this fallback cannot avoid a blocking
// open if a special object replaces the path after Stat.
const nonblock = 0
