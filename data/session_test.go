package data

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

const sessionFile = "session.data"

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	cleanUp()
	os.Exit(code)
}

func cleanUp() {
	_ = os.Remove(sessionFile)
}

func setup() {

}

func TestLoad(t *testing.T) {
	var err error
	var session Session
	var jsonText = `{"email-index":0,"boundaries":[[1,2],[3,4]]}`

	// Create the file to load.
	err = os.WriteFile(sessionFile, []byte(jsonText), 0644)
	assert.Nil(t, err)

	// Load the file.
	err = session.Load(sessionFile)
	assert.Nil(t, err)

	assert.Equal(t, 0, session.EmailIndex)
	assert.Len(t, session.Boundaries, 2)
	assert.Len(t, session.Boundaries[0], 2)
	assert.Len(t, session.Boundaries[1], 2)
	assert.Equal(t, []int{1, 2}, session.Boundaries[0])
	assert.Equal(t, []int{3, 4}, session.Boundaries[1])
}

func TestSave(t *testing.T) {
	var err error
	var session = Session{EmailIndex: 0, Boundaries: [][]uint8{{0x01, 0x02}, {0x03, 0x04}}}
	var expected = `{"email-index":0,"boundaries":[[1,2],[3,4]]}`
	var content []byte

	err = session.Save(sessionFile)
	assert.Nil(t, err)

	// Check that the file has been created.
	_, err = os.Stat(sessionFile)
	assert.Nil(t, err)

	// Check that the generated file contains the correct JSON representation.
	content, err = os.ReadFile(sessionFile)
	assert.Nil(t, err)
	assert.Equal(t, expected, string(content))
}
