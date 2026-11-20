package main

import (
	"researchpilot/internal/app"
	"researchpilot/internal/wiring"
)

func main() { app.Main(wiring.Open) }
