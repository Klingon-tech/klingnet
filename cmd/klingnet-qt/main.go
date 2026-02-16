package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// HRP is now set inside node.New() based on the config network.
	app := NewApp()

	if err := wails.Run(&options.App{
		Title:  "Klingnet Wallet",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
			app.wallet,
			app.chain,
			app.network,
			app.staking,
			app.subchain,
		},
	}); err != nil {
		log.Fatal(err)
	}
}
