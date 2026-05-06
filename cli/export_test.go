package cli

// Aliases to expose internal helpers to the external _test package.
var (
	WrapDockerdCmd                   = wrapDockerdCmd
	DockerdBinName                   = dockerdBinName
	DockerdSubtreeControlMaxAttempts = dockerdSubtreeControlMaxAttempts
)
