package router

import (
	"fmt"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func SetMediaRouter(router *gin.Engine) {
	mediaDir := common.GetMediaDir()
	if info, err := os.Stat(mediaDir); err != nil || !info.IsDir() {
		common.SysLog(fmt.Sprintf("MEDIA_DIR %s does not exist or is not a directory", mediaDir))
		return
	}
	router.Static("/media", mediaDir)
}
