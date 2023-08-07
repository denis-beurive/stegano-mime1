// Usage:
//
//     umail.exe create-key test random-data.dat
//     dir "%HOMEDRIVE%%HOMEPATH%\.smailer\keys"
//     dir "%HOMEDRIVE%%HOMEPATH%\.smailer\sessions"
//
//     umail.exe create-session --key=test --message=README.md first-session
//     dir "%HOMEDRIVE%%HOMEPATH%\.smailer\sessions"
//     type "%HOMEDRIVE%%HOMEPATH%\.smailer\sessions\first-session"
//
//     umail.exe key-index test

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	umailData "umail/data"
	"umail/resource"
)

const defaultAppDataBaseName = ".smailer"
const sessionSubDir = "sessions"
const keysSubDir = "keys"
const defaultBodyPath = "body.txt"
const defaultMessagePath = "message.txt"
const defaultKeyName = "key"

// boundaryLength The length, in bytes, of a boundary. Do not modify this value.
// Please note that 35 bytes can be used to represent 70 hexadecimal characters.
const boundaryLength = 35

var appDir string
var sessionDir string
var keyDir string

type ActionData struct {
	Description string
	Handler     func() error
}

func logError(messages []string) {
	for _, message := range messages {
		fmt.Printf("%s\n", message)
	}
	os.Exit(1)
}

func cypher(b1 []byte, b2 []byte) []byte {
	var result []byte
	if len(b1) != len(b2) {
		panic("cannot cypher `b1` with `b2`: different lengths")
	}
	for i, v := range b1 {
		result = append(result, v^b2[i])
	}
	return result
}

func processCreateKey() error {
	var err error
	var cliPoolName = os.Args[1]
	var cliSourcePath = os.Args[2]
	var poolPath = path.Join(keyDir, cliPoolName)

	if _, err = resource.PoolCreate(poolPath, cliSourcePath); err != nil {
		return fmt.Errorf(`cannot create the pool "%s" (%s) from file "%s": %s`, cliPoolName, poolPath, cliSourcePath, err.Error())
	}
	return nil
}

func processCreateSession() error {
	var err error
	var message *umailData.Message = &umailData.Message{}
	var pool *resource.Pool
	//var poolPosition *int64
	var session umailData.Session
	var key *[][]byte
	var cliSessionName string
	var cliSessionPath string
	var cliKeyName *string
	var cliKeyPath string
	var cliMessagePath *string

	// Parse the command line: smail [--pool=</path/to/pool/file>] [--message=</path/to/message/file>] <session name>
	cliKeyName = flag.String("key", defaultKeyName, "name of the key")
	cliMessagePath = flag.String("message", defaultMessagePath, "path to the message file")
	flag.Parse()
	if len(flag.Args()) != 1 {
		return fmt.Errorf("invalid command line: wrong number of arguments (%d)", len(flag.Args()))
	}
	cliSessionName = flag.Arg(0)
	cliSessionPath = path.Join(sessionDir, cliSessionName)
	cliKeyPath = path.Join(keyDir, *cliKeyName)

	// Open the key and retrieve the current position on the position pointer.
	if pool, err = resource.PoolOpen(cliKeyPath); err != nil {
		return fmt.Errorf(`cannot open key file "%s": %s`, cliKeyPath, err)
	}
	defer pool.Close()
	//if poolPosition, err = pool.GetPositionFromFile(true); err != nil {
	//	return err
	//}

	// Load the message. The message is organized into chunks of data.
	if err = message.Load(*cliMessagePath, boundaryLength); err != nil {
		return fmt.Errorf(`cannot load the message from the file "%s": %s`, *message, err)
	}

	// Extract the required number of bytes from the pool.
	// Note: since the length of the message is limited to 65535 bytes, it is possible to convert the length into
	//       `uint64`.
	if key, err = pool.GetBytesAsChunks(int64(message.BoundariesCount()), boundaryLength); err != nil {
		return fmt.Errorf(`not enough bytes left into the key file "%s" (needed %d bytes)`, cliKeyPath, message.BoundariesCount()*boundaryLength)
	}

	// Create the session.
	// `message XOR key` for each `message`
	// m: a chunk of the message (to hide)
	// k: a chunk of the key
	session.Init()
	for i, m := range *message {
		k := (*key)[i]
		session.AddBoundary(cypher(m, k))
	}
	if err = session.Save(cliSessionPath); err != nil {
		return fmt.Errorf(`cannot create the session file "%s": %s`, cliKeyPath, err)
	}

	return nil
}

func processInfo() error {
	fmt.Printf("Application directory: \"%s\"\n", appDir)
	fmt.Printf("Session directory: \"%s\"\n", sessionDir)
	fmt.Printf("Key directory: \"%s\"\n", keyDir)
	return nil
}

func initEnv() error {
	var err error
	var homeDir string
	var info os.FileInfo

	if homeDir, err = os.UserHomeDir(); err != nil {
		log.Fatal(err)
	}
	appDir = path.Join(homeDir, defaultAppDataBaseName)
	sessionDir = path.Join(appDir, sessionSubDir)
	keyDir = path.Join(appDir, keysSubDir)

	if info, err = os.Stat(appDir); err != nil {
		if os.IsNotExist(err) {
			// The entry does not exist. We create it.
			if err = os.MkdirAll(sessionDir, 0644); err != nil {
				return fmt.Errorf(`cannot create the directory used to store sessions "%s": %s`, sessionDir, err)
			}
			if err = os.MkdirAll(keyDir, 0644); err != nil {
				return fmt.Errorf(`cannot create the directory used to store keys "%s": %s`, keyDir, err)
			}
			return nil
		}
		return fmt.Errorf(`unexpected error while checking for the existence of the application directory "%s": %s`, appDir, err.Error())
	}

	// The entry exists. Make sure that it is a directory.
	if !info.IsDir() {
		return fmt.Errorf(`unexpected error while checking for the existence of the application directory. The entry "%s" exists but is not a directory`, appDir)
	}

	// At this point, we consider that the directory is well-structured.
	return nil
}

func processGetKeyIndex() error {
	var err error
	var keyPath string
	var keyName string
	var position *int64
	var pool *resource.Pool

	if len(os.Args) != 2 {
		return fmt.Errorf(`invalid number of argments (%d)`, len(os.Args))
	}
	keyName = os.Args[1]
	keyPath = path.Join(keyDir, keyName)
	if pool, err = resource.PoolOpen(keyPath); err != nil {
		return err
	}
	defer pool.Close()
	if position, err = pool.GetPositionFromFile(false); err != nil {
		return err
	}
	fmt.Printf("index for key \"%s\": %d\n", keyName, *position)

	return nil
}

var Actions = map[string]ActionData{
	"info":           {Description: `print information about the application`, Handler: processInfo},
	"create-key":     {Description: `create a key (from a given file)`, Handler: processCreateKey},
	"create-session": {Description: `create a session`, Handler: processCreateSession},
	"key-index":      {Description: `show the key index`, Handler: processGetKeyIndex},
}

func main() {
	var err error
	var action string

	// Initialize the application environment.
	if err = initEnv(); err != nil {
		logError([]string{err.Error()})
	}

	// Check the number of arguments in the command line.
	if len(os.Args) < 2 {
		logError([]string{fmt.Sprintf(`invalid number of arguments in command line (%d)`, len(os.Args)-1)})
	}

	// Retrieve the "action" (second argument) from the command line, and extract it from the list of arguments.
	action = os.Args[1]
	os.Args = append(os.Args[0:1], os.Args[2:]...)

	// Check the action.
	if _, ok := Actions[action]; !ok {
		logError([]string{fmt.Sprintf(`invalid action "%s"`, action)})
	}

	// Process the action.
	if err := Actions[action].Handler(); err != nil {
		logError([]string{err.Error()})
	}
}
