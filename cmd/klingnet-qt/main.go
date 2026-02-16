package main

import (
	"embed"
	"log"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Default to mainnet prefix. QT connects to a node via RPC so it
	// inherits the network from the node. For display purposes, mainnet
	// is the default; users can set testnet via SetRPCEndpoint flow later.
	types.SetAddressHRP(types.MainnetHRP)

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
