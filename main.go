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
//     umail.exe reset-session first-session
//
//     umail.exe info-session first-session
//
//     umail.exe info-key test
//
//     umail.exe reset-key test 0

package main

import (
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"io"
	"log"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	umailData "umail/data"
	"umail/resource"
)

const defaultAppDataBaseName = ".smailer"
const sessionSubDir = "sessions"
const keySubDir = "keys"
const defaultMessagePath = "message.txt"
const defaultKeyName = "key"

const DefaultSmtpServerAddress = "localhost"
const DefaultSmtpPort = 465
const DefaultImapServerAddress = "localhost"
const DefaultImapServerPort = 993
const DefaultBodyFile = "body.txt"

// See https://gist.github.com/tylermakin/d820f65eb3c9dd98d58721c7fb1939a8

const emailTemplate = `--{{BOUNDARY}}
Content-Type: text/plain; charset="utf-8"
Content-Transfer-Encoding: quoted-printable
Content-Disposition: inline

{{MESSAGE}}

--{{.Boundary}}
Content-Type: text/html; charset="utf-8"
Content-Transfer-Encoding: quoted-printable
Content-Disposition: inline

{{.Message}}

--{{Boundary}}--`

// See https://pkg.go.dev/text/template
type emailContent struct {
	Boundary string
	Message  string
}

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

type Email struct {
	Header string
	Body   string
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
	var poolPath = filepath.Join(keyDir, cliPoolName)

	if _, err = resource.PoolCreate(poolPath, cliSourcePath); err != nil {
		return fmt.Errorf(`cannot create the pool "%s" (%s) from file "%s": %s`, cliPoolName, poolPath, cliSourcePath, err.Error())
	}
	return nil
}

func processCreateSession() error {
	var err error
	var message *umailData.Message = &umailData.Message{}
	var pool *resource.Pool
	var poolPointerPosition int64
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
	cliSessionPath = filepath.Join(sessionDir, cliSessionName)
	cliKeyPath = filepath.Join(keyDir, *cliKeyName)

	// Open the key and retrieve the current position of the position pointer.
	if pool, err = resource.PoolOpen(cliKeyPath); err != nil {
		return fmt.Errorf(`cannot open key file "%s": %s`, cliKeyPath, err)
	}
	defer pool.Close()
	poolPointerPosition = pool.Position

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
	session.Init(*cliKeyName, poolPointerPosition)
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
	appDir = filepath.Join(homeDir, defaultAppDataBaseName)
	sessionDir = filepath.Join(appDir, sessionSubDir)
	keyDir = filepath.Join(appDir, keySubDir)

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
	var sessionPath string
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
	sessionPath = filepath.Join(sessionDir, sessionName)
	if body, err = os.ReadFile(bodyPath); err != nil {
		return fmt.Errorf(`cannot load the email body from file "%s": %s`, bodyPath, err.Error())
	}
	if err = session.Load(sessionPath); err != nil {
		return fmt.Errorf(`cannot load the session (%s) data from file "%s": %s`, sessionName, sessionPath, err.Error())
	}

	// Make sure that the session has not already been processed.
	if session.EmailIndex >= len(session.Boundaries) {
		return fmt.Errorf(`the session "%s" has already been processed`, sessionName)
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
		"From":         from,
		"To":           to,
		"Subject":      subject,
		"Content-Type": fmt.Sprintf(`multipart/alternative;  boundary="%s"`, session.Boundaries[session.EmailIndex]),
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

	session.EmailIndex += 1
	if err = session.Save(sessionPath); err != nil {
		return fmt.Errorf(`cannot update session "%s" (path: %s): %s`, sessionName, sessionPath, err.Error())
	}

	fmt.Printf("Number of emails sent: %d (onver %s)\n", session.EmailIndex, len(session.Boundaries))
	if session.EmailIndex >= len(session.Boundaries) {
		fmt.Printf("The session has been entirely processes.\n")
	}

	return nil
}

func processSessionInfo() error {
	var err error
	var sessionName string
	var sessionPath string
	var session umailData.Session

	if len(os.Args) != 2 {
		return fmt.Errorf(`invalid number of arguments (%d instead of 1)`, len(os.Args))
	}
	sessionName = os.Args[1]
	sessionPath = filepath.Join(sessionDir, sessionName)
	if err = session.Load(sessionPath); err != nil {
		return fmt.Errorf(`cannot load the session (%s) data from file "%s": %s`, sessionName, sessionPath, err.Error())
	}
	fmt.Printf("name: \"%s\" (%s)\n", sessionName, sessionPath)
	fmt.Printf("pool: \"%s\" (%s) at %d\n", session.PoolName, filepath.Join(keyDir, session.PoolName), session.PoolPointerPosition)
	fmt.Printf("email sent: %d\n", session.EmailIndex)
	fmt.Printf("boundaries (%d):\n", len(session.Boundaries))
	for i := 0; i < len(session.Boundaries); i++ {
		fmt.Printf("[%3d] \"%s\"\n", i, byte2boundary(session.Boundaries[i]))
	}
	fmt.Printf("number of emails to send: %d\n", len(session.Boundaries)-session.EmailIndex)
	return nil

}

func procesSessionReset() error {
	var err error
	var sessionName string
	var sessionPath string
	var session umailData.Session

	if len(os.Args) != 2 {
		return fmt.Errorf(`invalid number of arguments (%d instead of 1)`, len(os.Args))
	}
	sessionName = os.Args[1]
	sessionPath = filepath.Join(sessionDir, sessionName)
	if err = session.Load(sessionPath); err != nil {
		return fmt.Errorf(`cannot load the session (%s) data from file "%s": %s`, sessionName, sessionPath, err.Error())
	}
	return session.Reset(sessionPath)
}

func processPoolReset() error {
	var err error
	var cliPoolName string
	var cliPoolPointerPosition int64
	var poolPath string
	var pool *resource.Pool

	if len(os.Args) != 3 {
		return fmt.Errorf(`invalid number of arguments (%d instead of 3)`, len(os.Args))
	}
	cliPoolName = os.Args[1]
	if cliPoolPointerPosition, err = strconv.ParseInt(os.Args[2], 10, 64); err != nil {
		return fmt.Errorf(`invalid position (%s)`, os.Args[2])
	}
	poolPath = filepath.Join(keyDir, cliPoolName)
	if pool, err = resource.PoolOpen(poolPath); err != nil {
		return fmt.Errorf(`cannot open key file "%s": %s`, poolPath, err)
	}
	defer pool.Close()
	if err = pool.SetPositionToFile(cliPoolPointerPosition); err != nil {
		return fmt.Errorf(`cannot set the value of the position pointer's position: %s`, err.Error())
	}
	return nil
}

func processPoolInfo() error {
	var err error
	var cliPoolName string
	var poolPath string
	var pool *resource.Pool

	if len(os.Args) != 2 {
		return fmt.Errorf(`invalid number of arguments (%d instead of 2)`, len(os.Args))
	}
	cliPoolName = os.Args[1]
	poolPath = filepath.Join(keyDir, cliPoolName)
	if pool, err = resource.PoolOpen(poolPath); err != nil {
		return fmt.Errorf(`cannot open key file "%s": %s`, poolPath, err)
	}
	defer pool.Close()
	fmt.Printf("file: \"%s\"\n", poolPath)
	fmt.Printf("current read position: %d\n", pool.Position)
	return nil
}

func processGetMessage() error {
	var err error
	var user string
	var password string
	var imapServerAddress string
	var imapServerPort int
	var imapClient *imapclient.Client
	var imapUri string
	var mailboxes []*imap.ListData
	var selectedMbox *imap.SelectData

	// Parse the command line.
	flag.StringVar(&imapServerAddress, "imap", DefaultImapServerAddress, fmt.Sprintf("address of the IMAP server (default: %s)", DefaultImapServerAddress))
	flag.IntVar(&imapServerPort, "port", DefaultImapServerPort, fmt.Sprintf("SMTP server port number (default: %d)", DefaultImapServerPort))
	flag.StringVar(&user, "user", "", fmt.Sprintf("imap user"))
	flag.StringVar(&password, "password", "", "sender password used for authentication")
	flag.Parse()

	imapUri = fmt.Sprintf("%s:%d", imapServerAddress, imapServerPort)
	if imapClient, err = imapclient.DialTLS(imapUri, nil); nil != err {
		return fmt.Errorf("cannot open connection to IMAP server at \"%s\": %s", imapUri, err.Error())
	}
	defer imapClient.Close()
	if err = imapClient.Login(user, password).Wait(); nil != err {
		return fmt.Errorf("annot authenticate as \"%s\" (password: %s): %s", user, password, err.Error())
	}
	if mailboxes, err = imapClient.List("", "%", nil).Collect(); nil != err {
		return fmt.Errorf("cannot get the list of mailboxes: %s", err.Error())
	}
	for _, mbox := range mailboxes {
		fmt.Printf("mailbox: [%s]\n", mbox.Mailbox)
	}
	if selectedMbox, err = imapClient.Select("INBOX", nil).Wait(); err != nil {
		fmt.Printf("Cannot select mailbox \"INBOX\": %s", err.Error())
	}

	var i uint32
	//re := regexp.MustCompile(`Test Boundaries$`)
	for i = 1; i <= selectedMbox.NumMessages; i++ {
		var seqSet imap.SeqSet
		var messages []*imapclient.FetchMessageBuffer
		var fetchOptions *imap.FetchOptions
		var email = Email{Body: "", Header: ""}

		seqSet = imap.SeqSetNum(i)

		// Get the subject only.
		fetchOptions = &imap.FetchOptions{Envelope: true}
		if messages, err = imapClient.Fetch(seqSet, fetchOptions).Collect(); nil != err {
			return fmt.Errorf("cannot fetch messages from \"INBOX\": %s", err.Error())
		}
		fmt.Printf("[Subject] %s\n", messages[0].Envelope.Subject)
		//if !re.Match([]byte(messages[0].Envelope.Subject)) {
		//	continue
		//}

		// Get all the data.

		fetchOptions = &imap.FetchOptions{
			Flags:    true,
			Envelope: true,
			UID:      true,
			BodySection: []*imap.FetchItemBodySection{
				{Specifier: imap.PartSpecifierHeader},
				{Specifier: imap.PartSpecifierText},
				{Specifier: imap.PartSpecifierNone},
			},
		}
		if messages, err = imapClient.Fetch(seqSet, fetchOptions).Collect(); nil != err {
			return fmt.Errorf("cannot fetch messages from \"INBOX\": %s", err.Error())
		}
		for j, message := range messages {
			fmt.Printf("===========================================\n")
			fmt.Printf("%04d [%0d] UID:%05d: %s\n", i, j, message.UID, message.Envelope.Subject)
			fmt.Printf("   Flags: %v\n", message.Flags)

			for data, buf := range message.BodySection {
				if data.Specifier == "" {
					continue
				}
				//fmt.Printf("[%s] <===> %s\n\n", data.Specifier, string(buf))
				if "HEADER" == data.Specifier {
					email.Header = string(buf)
				}
				if "TEXT" == data.Specifier {
					email.Body = string(buf)
				}
			}
			emailText := fmt.Sprintf("%s\r\n\r\n%s", email.Header, email.Body)
			//fmt.Printf("==> %s\n\n", emailText)
			r := strings.NewReader(emailText)
			m, err := mail.ReadMessage(r)
			if err != nil {
				return fmt.Errorf(`%s`, err)
			}

			header := m.Header
			for k, v := range header {
				fmt.Printf("%s: %s\n", k, v)
			}

			body, err := io.ReadAll(m.Body)
			if err != nil {
				return fmt.Errorf("%s", err)
			}
			fmt.Printf("%s", body)
		}
	}

	if err := imapClient.Logout().Wait(); nil != err {
		return fmt.Errorf("cannot logout: %s", err.Error())
	}
	return nil
}

var Actions = map[string]ActionData{
	"info":           {Description: `print information about the application`, Handler: processInfo},
	"info-session":   {Description: `print information about a session`, Handler: processSessionInfo},
	"create-session": {Description: `create a mailing session`, Handler: processCreateSession},
	"reset-session":  {Description: `reset the session`, Handler: procesSessionReset},
	"create-key":     {Description: `create an "encryption/decryption" key (from a given file)`, Handler: processCreateKey},
	"reset-key":      {Description: `rester the pool's pointer position'`, Handler: processPoolReset},
	"info-key":       {Description: `print information about an "encryption/decryption" key`, Handler: processPoolInfo},
	"send":           {Description: `send a message`, Handler: processSend},
	"rcv":            {Description: `retrieve emails`, Handler: processGetMessage},
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
