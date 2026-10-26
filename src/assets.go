package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed web
var webFiles embed.FS

//go:embed samples
var sampleFiles embed.FS

func writeSamples(home string) (written, kept []string, err error) {
	err = fs.WalkDir(sampleFiles, "samples", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(path, "samples/")
		dest := filepath.Join(home, rel)
		if _, err := os.Stat(dest); err == nil && rel != "GUIDE.txt" {
			kept = append(kept, rel)
			return nil
		}
		raw, readErr := sampleFiles.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		written = append(written, rel)
		return writeFile(dest, raw)
	})
	return written, kept, err
}

func samplesWritten(home string) bool {
	_, err := os.Stat(filepath.Join(home, "GUIDE.txt"))
	return err == nil
}

func guideText() string {
	raw, _ := sampleFiles.ReadFile("samples/GUIDE.txt")
	return string(raw)
}
