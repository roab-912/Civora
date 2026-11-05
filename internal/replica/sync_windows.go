package replica

// Windows qualification covers abrupt process death, not loss of power.
func syncDirectory(string) error { return nil }
