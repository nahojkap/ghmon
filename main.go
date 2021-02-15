package main

import (
	"github.com/nahojkap/ghmon/internal/ghmon"
	"log"
	"os"
)

func main() {

	ghm := ghmon.NewGHMon()

	if !ghm.HasValidSetup() {
		log.Printf("ghmon reports invalid setup, will bail")
		os.Exit(1)
	}

	ghmui := ghmon.NewGHMonUI(ghm)

	// Kick-off GHM
	go ghm.Initialize()

	// Loops until exit
	ghmui.EventLoop()

	os.Exit(0)

}