package snumom

const ChannelName = "snumom"

// ModelList is a recommendation list for the admin UI. It is deliberately
// not used as a request allowlist: model mappings are controlled by admins and
// new provider model names must be forwarded unchanged.
var ModelList = []string{
	"sd-2.5",
	"seedance2.5-1",
	"seedance2.5-2",
	"seedance2-5",
	"seedance-2.0-mini",
	"seedance-933",
	"seedance2.0-933-V",
	"seedance2.5",
	"dreamina-seedance-2-0",
	"dreamina-seedance-2-0-fast",
	"dreamina-seedance-2-0-mini",
	"dreamina-seedance-2-5",
	"grok-imagine-video-1.5",
	"grok-imagine-video-1.5-fast",
	"grok-imagine-video-1.5-lite",
	"rh-minimaxh3-overseas",
	"rh-minimaxh3-pro-overseas",
	"wan3.0-video",
	"wan3.0-video-prime",
	"wan3.0-image",
	"wan3.0-image-prime",
}
