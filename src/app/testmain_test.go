package app

// testmain_test.go keeps every app test's local preferences (saved servers,
// shortcuts) inside a throwaway directory: New reads ~/.gocode/shortcuts.json,
// so without this the suite would see whatever the developer has customized.

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gocode-app-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("GOCODE_DATA_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
