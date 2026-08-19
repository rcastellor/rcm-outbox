package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/bootstrap"
)

func main() {
	h, err := bootstrap.Load(context.Background())
	if err != nil {
		log.Printf("error arrancando orders-dispatcher: %v", err)
		os.Exit(1)
	}
	lambda.Start(h.Handle)
}
