//go:build playlist_bridge_demo

package server

import (
	"os"
	"path/filepath"
)

func loadDemoFixture(name string) []byte {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "tests", "mocks", "server", name))
		if err == nil {
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("не найден файл офлайн-сценария: " + name)
		}
		dir = parent
	}
}
