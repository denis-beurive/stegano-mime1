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
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"path"
	umailData "umail/data"
	"umail/resource"
)

const defaultAppDataBaseName = ".smailer"
const sessionSubDir = "sessions"
const keySubDir = "keys"
const defaultMessagePath = "message.txt"
const defaultKeyName = "key"

const DefaultSmtpPort = 465
const DefaultSmtpServerAddress = "localhost"
const DefaultBodyFile = "body.txt"

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

func byte2boundary(inBytes []byte) string {
	return hex.EncodeToString(inBytes)
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
	keyDir = path.Join(appDir, keySubDir)

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

func buildMessage(headers map[string]string, body string) string {
	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body
	return message
}

func processSend() error {
	var err error
	var keyName string
	var sessionName string
	var sessionFile string
	var to string
	var from string
	var password string
	var subject string
	var bodyPath string
	var body []byte
	var smtpServerAddress string
	var smtpServerPort int
	var auth smtp.Auth
	var connection *tls.Conn
	var smtpClient *smtp.Client
	var smtpUri string
	var tlsConfig *tls.Config
	var writer io.WriteCloser
	var session umailData.Session
	var headers map[string]string
	var message string

	// Parse the command line.
	flag.StringVar(&smtpServerAddress, "smtp", DefaultSmtpServerAddress, fmt.Sprintf("address of the SMTP server (default: %s)", DefaultSmtpServerAddress))
	flag.IntVar(&smtpServerPort, "port", DefaultSmtpPort, fmt.Sprintf("SMTP server port number (default: %d)", DefaultSmtpPort))
	flag.StringVar(&password, "password", "", "sender password used for authentication")
	flag.StringVar(&bodyPath, "body", DefaultBodyFile, fmt.Sprintf("path to the file that contains the email's body (default: %s)", DefaultBodyFile))
	flag.StringVar(&keyName, "key", defaultKeyName, fmt.Sprintf("name of the key (default: %s)", defaultKeyName))
	flag.Parse()

	if len(flag.Args()) != 4 {
		return fmt.Errorf(`invalid number of arguments (%d instead of 4)`, len(flag.Args()))
	}
	sessionName = flag.Arg(0)
	from = flag.Arg(1)
	to = flag.Arg(2)
	subject = flag.Arg(3)

	// Load all data from files.
	sessionFile = path.Join(sessionDir, sessionName)
	if body, err = os.ReadFile(bodyPath); err != nil {
		return fmt.Errorf(`cannot load the email body from file "%s": %s`, bodyPath, err.Error())
	}
	if err = session.Load(sessionFile); err != nil {
		return fmt.Errorf(`cannot load the session (%s) data from file "%s": %s`, sessionName, sessionFile, err.Error())
	}

	// Open connexion to the SMTP server.
	auth = smtp.PlainAuth("", from, password, smtpServerAddress)
	smtpUri = fmt.Sprintf("%s:%d", smtpServerAddress, smtpServerPort)
	tlsConfig = &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         smtpServerAddress,
	}
	if connection, err = tls.Dial("tcp", smtpUri, tlsConfig); err != nil {
		return fmt.Errorf(`cannot connect to SMTPS server on "%s" (TLS enabled): %s`, smtpUri, err.Error())
	}
	if smtpClient, err = smtp.NewClient(connection, smtpServerAddress); err != nil {
		return fmt.Errorf(`cannot connect to SMTPS server on "%s" (TLS enabled): %s`, smtpUri, err.Error())
	}
	if err = smtpClient.Auth(auth); err != nil {
		return fmt.Errorf(`cannot authenticate on SMTPS server on "%s" (TLS enabled): %s`, smtpUri, err.Error())
	}

	// Send the email.
	headers = map[string]string{
		"From":    from,
		"To":      to,
		"Subject": subject,
	}
	message = buildMessage(headers, string(body))

	if err = smtpClient.Mail(from); err != nil {
		return fmt.Errorf(`error while sending "MAIL FROM:%s<CRLF>" command: %s`, from, err.Error())
	}
	if err = smtpClient.Rcpt(to); err != nil {
		return fmt.Errorf(`error while sending "MAIL TO:%s<CRLF>" command: %s`, to, err.Error())
	}
	if writer, err = smtpClient.Data(); err != nil {
		return fmt.Errorf(`error while sending "DATA<CRLF>" command: %s`, err.Error())
	}
	if _, err = writer.Write([]byte(message)); err != nil {
		return fmt.Errorf(`error while sending sending the message to send: %s`, err.Error())
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf(`error while closing SMTP writer: %s`, err.Error())
	}
	if err = smtpClient.Quit(); err != nil {
		return fmt.Errorf(`error while sending "QUIT<CRLF>" command: %s`, err.Error())
	}

	return nil
}

func processSessionInfo() error {
	var err error
	var sessionName string
	var sessionFile string
	var session umailData.Session

	if len(os.Args) != 2 {
		return fmt.Errorf(`invalid number of arguments (%d instead of 1)`, len(os.Args))
	}
	sessionName = os.Args[1]
	sessionFile = path.Join(sessionDir, sessionName)
	if err = session.Load(sessionFile); err != nil {
		return fmt.Errorf(`cannot load the session (%s) data from file "%s": %s`, sessionName, sessionFile, err.Error())
	}
	fmt.Printf("name: \"%s\"\n", sessionName)
	fmt.Printf("file: \"%s\"\n", sessionFile)
	fmt.Printf("index: %d\n", session.EmailIndex)
	fmt.Printf("boundaries (%d):\n", len(session.Boundaries))
	for i := 0; i < len(session.Boundaries); i++ {
		fmt.Printf("[%3d] \"%s\"\n", i, byte2boundary(session.Boundaries[i]))
	}
	fmt.Printf("number of emails to send: %d\n", len(session.Boundaries)-session.EmailIndex)
	return nil

}

var Actions = map[string]ActionData{
	"info":           {Description: `print information about the application`, Handler: processInfo},
	"info-session":   {Description: `print information about a session`, Handler: processSessionInfo},
	"create-key":     {Description: `create an "encryption/decryption" key (from a given file)`, Handler: processCreateKey},
	"create-session": {Description: `create a mailing session`, Handler: processCreateSession},
	"key-index":      {Description: `show the key index`, Handler: processGetKeyIndex},
	"send":           {Description: `send a message`, Handler: processSend},
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
