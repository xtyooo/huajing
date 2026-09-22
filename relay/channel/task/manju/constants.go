package manju

const (
	ChannelName     = "manju"
	generationsPath = "/videos/generations"
	tasksPath       = "/videos/tasks"
)

// ModelList is a recommendation list, not a model admission allowlist.
var ModelList = []string{
	"wan3.0-t2v",
	"wan3.0-i2v",
	"wan3.0-r2v",
	"wan3.0-prime-t2v",
	"wan3.0-prime-i2v",
	"wan3.0-prime-r2v",
}
