package main

import (
	"context"

	"cloud.google.com/go/logging"
)

func main() {
	ctx := context.Background()
	client, err := logging.NewClient(ctx, "my-project")
	if err != nil {
		// TODO: Handle error.
	}
	// Use client to manage logs, metrics and sinks.
	// Close the client when finished.
	if err := client.Close(); err != nil {
		// TODO: Handle error.
	}
}
