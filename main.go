package main

import (
	"flag"
	"fmt"
	"os"
	umailData "umail/data"
	"umail/resource"
)

const defaultAppDataBaseName = ".stealth-mailer"
const defaultBodyPath = "body.txt"
const defaultMessagePath = "message.txt"
const defaultKeyPath = "key.bin"

// boundaryLength The length, in bytes, of a boundary. Do not modify this value.
// Please note that 35 bytes can be used to represent 70 hexadecimal characters.
const boundaryLength = 35

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

func processCreateSession() error {
	var err error
	var message *umailData.Message
	var pool *resource.Pool
	var session umailData.Session
	var key *[][]byte

	// Parse the command line: smail [--pool=</path/to/pool/file>] [--message=</path/to/message/file>] <session name>
	var cliSessionName string
	var cliKeyPath = flag.String("pool", defaultKeyPath, "path to the pool file")
	var cliMessagePath = flag.String("message", defaultMessagePath, "path to the message file")
	flag.Parse()
	if len(os.Args) != 2 {
		return fmt.Errorf("invalid command line: wrong number of arguments (%d)", len(os.Args)-1)
	}
	cliSessionName = os.Args[1]

	// Load the message. The message is organized into chunks of data.
	if err = message.Load(*cliMessagePath, boundaryLength); err != nil {
		return fmt.Errorf(`cannot load the message from file "%s": %s`, *message, err)
	}

	// Extract the required number of bytes from the pool.
	// Note: since the length of the message is limited to 65535 bytes, it is possible to convert the length into
	//       `uint64`.
	if pool, err = resource.PoolOpen(*cliKeyPath); err != nil {
		return fmt.Errorf(`cannot open pool file "%s": %s`, *cliKeyPath, err)
	}
	if key, err = pool.PoolGetBytesAsChunks(int64(message.BoundariesCount()), boundaryLength); err != nil {
		return fmt.Errorf(`not enough bytes left into the key file "%s" (needed %d bytes)`, *cliKeyPath, message.BoundariesCount()*boundaryLength)
	}

	// `message XOR key` for each `message`
	// m: a chunk of the message (to hide)
	// k: a chunk of the key
	session.Init()
	for i, m := range *message {
		k := (*key)[i]
		session.AddBoundary(cypher(m, k))
	}

	return nil
}

func main() {

}
