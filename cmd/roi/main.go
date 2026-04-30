package main

import (
	"fmt"
	"os"

	"github.com/Manabreaker/ROI_based_video_encoding/internal/roi"
)

func main() {
	cfg := roi.ParseFlags()
	if err := roi.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}
}
