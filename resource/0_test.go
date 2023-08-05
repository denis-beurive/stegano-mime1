package resource

import (
	"os"
	"testing"
)

const poolLength = 256
const sourcePath = "source.dat"
const poolPath = "pool.dat"

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	cleanUp()
	os.Exit(code)
}

func cleanUp() {
	_ = os.Remove(sourcePath)
	_ = os.Remove(poolPath)
}

func setup() {
	var err error
	var data = make([]byte, poolLength) // 0x00, 0x01, 0x02, 0x03... 0xFF

	for i := 0; i < poolLength; i++ {
		data[i] = byte(i)
	}
	if err = os.WriteFile(sourcePath, data, 0644); err != nil {
		panic(err)
	}
}
