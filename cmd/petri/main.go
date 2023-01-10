package main

import (
	"os"

	"github.com/cosmos/cosmos-sdk/server"

	"github.com/blackfury-1/petri/app"
)

func main() {
	rootCmd, _ := NewRootCmd()

	if err := Execute(rootCmd, app.DefaultNodeHome); err != nil {
		switch e := err.(type) {
		case server.ErrorCode:
			os.Exit(e.Code)

		default:
			os.Exit(1)
		}
	}
}
