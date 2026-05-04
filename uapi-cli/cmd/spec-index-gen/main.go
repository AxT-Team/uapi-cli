package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AxT-Team/uapi-cli/internal/specbuild"
)

func main() {
	specPath := flag.String("spec", filepath.Clean("../openapi.yaml"), "Path to openapi.yaml")
	outPath := flag.String("out", filepath.Clean("internal/specindex/index.gen.json.gz"), "Output gzip-compressed JSON path")
	flag.Parse()

	index, err := specbuild.BuildFromFile(*specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload, err := json.Marshal(index)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var compressed bytes.Buffer
	zipWriter, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := zipWriter.Write(payload); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := zipWriter.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, compressed.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
