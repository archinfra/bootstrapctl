package main

import (
	"os"

	"github.com/yuanyp8/bootstrapctl/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
