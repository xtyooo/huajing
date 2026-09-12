package naonao

const ChannelName = "naonao"

// ModelList supplies suggested models for the admin UI, not a request allowlist.
// Administrators configure available models and mappings; upstream validates availability.
var ModelList = []string{
	"wan3.0-video",
	"seedance-2.0",
	"seedance-2.0-fast",
	"seedance-2.5",
}
