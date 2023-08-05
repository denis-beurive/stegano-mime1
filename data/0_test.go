package data

import (
	"os"
	"testing"
)

const sessionFile = "session.data"
const messageFile = "message.data"

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	cleanUp()
	os.Exit(code)
}

func cleanUp() {
	_ = os.Remove(sessionFile)
	_ = os.Remove(messageFile)
}

func setup() {

}
