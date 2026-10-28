package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSamplesAreWrittenOnceExceptTheGuide(t *testing.T) {
	home := t.TempDir()
	if samplesWritten(home) {
		t.Error("samples written before any were")
	}
	written, kept, err := writeSamples(home)
	if err != nil || len(written) == 0 || len(kept) != 0 {
		t.Fatalf("first write = %d written, %v kept, %v", len(written), kept, err)
	}
	if !samplesWritten(home) {
		t.Error("samples not marked written")
	}
	again, kept, err := writeSamples(home)
	if err != nil || !reflect.DeepEqual(again, []string{"GUIDE.txt"}) || len(kept) != len(written)-1 {
		t.Errorf("second write = %v written, %d kept, %v", again, len(kept), err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "GUIDE.txt"))
	if err != nil || guideText() == "" || guideText() != string(raw) {
		t.Errorf("guide = %d bytes, file %d bytes, %v", len(guideText()), len(raw), err)
	}
}

func TestWriteSamplesReportsAFolderItCannotWrite(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(home, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writeSamples(home); err == nil {
		t.Error("wrote samples inside a file")
	}
}
