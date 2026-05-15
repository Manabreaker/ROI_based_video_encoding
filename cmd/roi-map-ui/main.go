package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Manabreaker/ROI_based_video_encoding/internal/mapui"
)

func main() {
	opts, err := mapui.ParseArgs(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(2)
	}

	if err := mapui.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}
}
