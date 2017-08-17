package main

import (
	"github.com/ysmood/portal/lib"
	"github.com/ysmood/portal/lib/utils"
)

func main() {
	appCtx := lib.NewAppContext()

	closeControlService := appCtx.ControlService()
	closeFileService := appCtx.FileService()

	utils.Wait(func() {
		closeControlService()
		closeFileService()
		lib.CloseDb()
	})
}
