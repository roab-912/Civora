//go:build windows

package store

// Windows does not expose directory fsync through os.File. File.Sync and a
// same-volume rename give the process-crash contract tested here. Power-loss
// durability is explicitly outside this development adapter's qualification.
func syncDir(string) error { return nil }
