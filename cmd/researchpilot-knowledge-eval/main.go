package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"

	"researchpilot/internal/knowledge"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report, err := knowledge.EvaluateDemo(ctx)
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		log.Fatal(err)
	}
}
