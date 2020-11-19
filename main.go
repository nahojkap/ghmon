package main

import (
	"github.com/nahojkap/ghmon/internal/ghmon"
	"log"
)

func main() {

	ghm := ghmon.NewGHMon()

	if !ghm.HasValidSetup() {
		log.Fatal("ghmon reports invalid setup, will bail")
	}

	ghmui := ghmon.NewGHMonUI(ghm)

	// Kick-off GHM
	go ghm.Initialize()

	// Loops until exit
	ghmui.EventLoop()

}