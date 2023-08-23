// Usage:
//
//     $out = new-object byte[] 1048576; (new-object Random).NextBytes($out); [IO.File]::WriteAllBytes('random-data.bin', $out)
//
//     umail.exe create-key test random-data.bin
//     dir "%HOMEDRIVE%%HOMEPATH%\.smailer\keys"
//     dir "%HOMEDRIVE%%HOMEPATH%\.smailer\sessions"
//
//     umail.exe create-session --key=test --message=message.txt first-session
//     dir "%HOMEDRIVE%%HOMEPATH%\.smailer\sessions"
//     type "%HOMEDRIVE%%HOMEPATH%\.smailer\sessions\first-session"
//
//     umail.exe reset-session first-session
//     umail.exe info-session first-session
//
//     umail.exe reset-key test 0
//     umail.exe info-key test

package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	b64 "encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"io"
	"log"
	"mime"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
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
const DefaultBodyFile = "body1.txt"

// See https://gist.github.com/tylermakin/d820f65eb3c9dd98d58721c7fb1939a8

const emailTemplate = `--{{.Boundary}}
Content-Type: text/plain; charset="utf-8"
Content-Transfer-Encoding: base64

{{.MessageText}}

--{{.Boundary}}
Content-Type: text/html; charset="utf-8"
Content-Transfer-Encoding: base64

{{.MessageHtml}}

--{{.Boundary}}--`

// See https://pkg.go.dev/text/template
type emailContent struct {
	Boundary    string
	MessageText string
	MessageHtml string
}

// boundaryLength The length, in bytes, of a boundary. Do not modify this value.
// Please note that 35 bytes can be used to represent 70 hexadecimal characters.
const boundaryLength = 35

var appDir string
var sessionDir string
var keyDir string
var boundaryRegex = regexp.MustCompile(`boundary="(?P<boundary>[^"]+)"`)
var trimmerRegex = regexp.MustCompile(`\s+`)

type ActionData struct {
	Description string
	Handler     func() error
}

type Email struct {
	Header string
	Body   string
}

type emailIndex = uint32

func logError(messages []string) {
	for _, message := range messages {
		fmt.Printf("%s\n", message)
	}
	os.Exit(1)
}

func boundaryAsString(inBytes []byte) string {
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

func createHtmlBody(body []byte) []byte {
	var lines = strings.Split(string(body), "\n")
	var htmlLines = []string{`<div style="font-family: Arial, sans-serif; font-size: 14px;">`}
	var html string

	for _, line := range lines {
		htmlLines = append(htmlLines, "<p>"+line+"</p>")
	}
	htmlLines = append(htmlLines, "</div>")
	html = strings.Join(htmlLines, "\n")
	return []byte(html)
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
	var htmlBody []byte
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
	var messageBuffer bytes.Buffer
	var boundary string
	var tpl *template.Template

	if tpl, err = template.New("email").Parse(emailTemplate); err != nil {
		return fmt.Errorf(`unexpected error while generating the email body: %s`, err)
	}

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
	subject = mime.QEncoding.Encode("utf-8", flag.Arg(3))

	// Load all data from files.
	sessionPath = filepath.Join(sessionDir, sessionName)
	if body, err = os.ReadFile(bodyPath); err != nil {
		return fmt.Errorf(`cannot load the email body from file "%s": %s`, bodyPath, err.Error())
	}
	htmlBody = createHtmlBody(body)
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
	boundary = boundaryAsString(session.Boundaries[session.EmailIndex])
	headers = map[string]string{
		"From":         from,
		"To":           to,
		"Subject":      subject,
		"Content-Type": fmt.Sprintf(`multipart/alternative;  boundary="%s"`, boundary),
	}
	if err = tpl.Execute(&messageBuffer,
		emailContent{
			Boundary:    boundary,
			MessageText: b64.StdEncoding.EncodeToString(body),
			MessageHtml: b64.StdEncoding.EncodeToString(htmlBody)}); err != nil {
		return fmt.Errorf(`unexpected error while generating the email body: %s`, err)
	}
	message = buildMessage(headers, messageBuffer.String())

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

	fmt.Printf("Number of emails sent: %d (over %d)\n", session.EmailIndex, len(session.Boundaries))
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
	var b2l = func(b []uint8) string {
		var result []string
		for _, v := range b {
			result = append(result, fmt.Sprintf("%d", v))
		}
		return strings.Join(result, ", ")
	}

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
		fmt.Printf("[%3d]  [%s] (len: %d)\n", i, b2l(session.Boundaries[i]), len(session.Boundaries[i]))
		fmt.Printf("       => \"%s\"\n", boundaryAsString(session.Boundaries[i]))

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

func retrieveEmailMessages(imapClient *imapclient.Client, seqSet imap.SeqSet) ([]*imapclient.FetchMessageBuffer, error) {
	var err error
	var fetchOptions *imap.FetchOptions
	var messages []*imapclient.FetchMessageBuffer

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
		return nil, err
	}
	return messages, nil
}

// parseMessage Parse a given message and return a data structure that represents the message: header and body.
func parseMessage(message *imapclient.FetchMessageBuffer) (*mail.Message, error) {
	var err error
	var email = Email{Body: "", Header: ""}
	var m *mail.Message
	var r io.Reader
	var emailText string

	for data, buf := range message.BodySection {
		if "HEADER" == data.Specifier {
			email.Header = string(buf)
		}
		if "TEXT" == data.Specifier {
			email.Body = string(buf)
		}
	}
	emailText = fmt.Sprintf("%s\r\n\r\n%s", email.Header, email.Body)
	r = strings.NewReader(emailText)
	m, err = mail.ReadMessage(r)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func retrieveBoundary(message *imapclient.FetchMessageBuffer) (*string, error) {
	var err error
	var m *mail.Message
	var header mail.Header
	var ok bool
	var contentType []string
	var matches []string

	if m, err = parseMessage(message); err != nil {
		return nil, err
	}
	header = m.Header
	if contentType, ok = header["Content-Type"]; !ok {
		return nil, nil
	}
	if len(contentType) == 0 {
		return nil, nil
	}
	if len(contentType) > 1 {
		return nil, fmt.Errorf(`unexpected "Content-Type" header value (more than one values)`)
	}
	matches = boundaryRegex.FindStringSubmatch(contentType[0])
	if matches == nil {
		return nil, nil
	}
	return &matches[boundaryRegex.SubexpIndex("boundary")], nil
}

// retrieveFullEmail Retrieves an email identified by its sequence number.
func retrieveFullEmail(messages []*imapclient.FetchMessageBuffer) (*string, error) {
	var err error
	var content []string
	var result string

	for i, message := range messages {
		var m *mail.Message
		var header mail.Header
		var body []byte

		if m, err = parseMessage(message); err != nil {
			return nil, err
		}

		content = append(content, fmt.Sprintf("       [%0d] UID:%05d", i, message.UID))
		content = append(content, []string{fmt.Sprintf("           Flags: %v", message.Flags), ""}...)

		header = m.Header
		for k, v := range header {
			content = append(content, fmt.Sprintf("* %s: %s", k, v))
		}

		body, err = io.ReadAll(m.Body)
		if err != nil {
			return nil, err
		}
		content = append(content, string(body))
	}

	result = strings.Join(content, "\r\n")
	return &result, nil
}

func getEmails(indexBoundary map[emailIndex]string) ([]emailIndex, error) {
	var err error
	var response string
	var emailsText []string
	var emails []emailIndex
	var reader = bufio.NewReader(os.Stdin)

	fmt.Printf("List of emails to process (type 'x' to quit):\n")
	response, err = reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	response = strings.ToLower(response)
	response = strings.TrimSpace(response)
	response = trimmerRegex.ReplaceAllString(response, " ")
	if response == "x" {
		return nil, nil
	}
	emailsText = strings.Split(response, " ")

	for _, v := range emailsText {
		index, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, fmt.Errorf(`invalid email index (%s). It should be an integer`, v)
		}
		_, ok := indexBoundary[uint32(index)]
		if !ok {
			return nil, fmt.Errorf(`unexpecter email index (%d)`, index)
		}
		emails = append(emails, uint32(index))
	}
	return emails, nil
}

func getYesNo(message string) (*bool, error) {
	var err error
	var response string
	var reader = bufio.NewReader(os.Stdin)
	var result bool

	fmt.Print(message + " ")
	for {
		response, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		response = strings.ToLower(response)
		response = strings.TrimSpace(response)
		if response != "y" && response != "n" {
			fmt.Printf("Invalid response (%s). Valid response: \"y\" or \"n\"\n", response)
			continue
		}
		break
	}
	result = response == "y"
	return &result, nil
}

func getKey(message string) (*resource.Pool, error) {
	var err error
	var reader = bufio.NewReader(os.Stdin)
	var pool *resource.Pool

	fmt.Print(message + " ")
	for {
		var poolName string
		var poolPath string

		poolName, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		poolName = strings.TrimSpace(poolName)
		poolPath = filepath.Join(keyDir, poolName)
		if pool, err = resource.PoolOpen(poolPath); err != nil {
			fmt.Printf("Cannot open the key \"%s\" (path: %s): %s", poolName, poolPath, err.Error())
			continue
		}
		break
	}
	return pool, nil
}

func showMessage(boundaries []string) (*string, error) {
	var err error
	var pool *resource.Pool
	var key *[][]byte
	var clearMessage []byte
	var messageLength uint16
	var hiddenMessage []byte

	// Load the pool.
	if pool, err = getKey("Enter the name the pool to use:"); err != nil {
		return nil, err
	}
	defer pool.Close()

	// Extract the required number of bytes from the pool.
	if key, err = pool.GetBytesAsChunks(int64(len(boundaries)), boundaryLength); err != nil {
		return nil, fmt.Errorf(`not enough bytes left into the key file (needed %d bytes)`, len(boundaries)*boundaryLength)
	}

	// Decrypt all boundaries.
	for i, boundary := range boundaries {
		var boundaryBytes []byte
		if boundaryBytes, err = hex.DecodeString(boundary); err != nil {
			return nil, fmt.Errorf(`invalid boundary (invalid email): does not represent a hexadecimal string`)
		}

		fmt.Printf("len(key):      %d\n", len((*key)[i]))
		fmt.Printf("len(boundary): %d\n", len(boundaryBytes))
		clearMessage = append(clearMessage, cypher((*key)[i], boundaryBytes)...)
	}

	// Please, keep in mind that the message starts with an `uint16` which represents the length of the message.
	if err = binary.Read(bytes.NewReader(clearMessage), binary.LittleEndian, &messageLength); err != nil {
		return nil, err
	}
	fmt.Printf("Lenght of the (hidden) message: %d\n", messageLength)

	hiddenMessage = clearMessage[2 : 2+messageLength]
	fmt.Printf("The hidden message is:\n\n%s\n\n", hiddenMessage)
	return nil, nil
}

func processGetFullEmails() error {
	var err error
	var user string
	var password string
	var imapServerAddress string
	var imapServerPort int
	var from string
	var full bool
	var showMailboxes bool
	var imapClient *imapclient.Client
	var imapUri string
	var selectedMbox *imap.SelectData
	var indexBoundary = map[emailIndex]string{}
	var boundaries []string
	var emails []emailIndex
	var proceed *bool

	// Parse the command line.
	flag.StringVar(&imapServerAddress, "imap", DefaultImapServerAddress, fmt.Sprintf("address of the IMAP server (default: %s)", DefaultImapServerAddress))
	flag.IntVar(&imapServerPort, "port", DefaultImapServerPort, fmt.Sprintf("SMTP server port number (default: %d)", DefaultImapServerPort))
	flag.StringVar(&user, "user", "", fmt.Sprintf("imap user"))
	flag.StringVar(&password, "password", "", "sender password used for authentication")
	flag.StringVar(&from, "from", "", "sender email address")
	flag.BoolVar(&full, "full", false, "print all the email (not only the envelope)")
	flag.BoolVar(&showMailboxes, "show-mailboxes", false, "list the mailboxes")
	flag.Parse()

	imapUri = fmt.Sprintf("%s:%d", imapServerAddress, imapServerPort)
	if imapClient, err = imapclient.DialTLS(imapUri, nil); nil != err {
		return fmt.Errorf("cannot open connection to IMAP server at \"%s\": %s", imapUri, err.Error())
	}
	defer imapClient.Close()
	if err = imapClient.Login(user, password).Wait(); nil != err {
		return fmt.Errorf("annot authenticate as \"%s\" (password: %s): %s", user, password, err.Error())
	}

	if showMailboxes {
		var mailboxes []*imap.ListData

		fmt.Printf("MAILBOXES:\n\n")
		if mailboxes, err = imapClient.List("", "%", nil).Collect(); nil != err {
			return fmt.Errorf("cannot get the list of mailboxes: %s", err.Error())
		}
		for _, mbox := range mailboxes {
			fmt.Printf("  [%s]\n", mbox.Mailbox)
		}
		fmt.Printf("\n")
	}

	if selectedMbox, err = imapClient.Select("INBOX", nil).Wait(); err != nil {
		fmt.Printf("Cannot select mailbox \"INBOX\": %s", err.Error())
	}

	fmt.Printf("EMAILS:\n\n")

	var i uint32
	for i = 1; i <= selectedMbox.NumMessages; i++ {
		var addresses []string
		var ccs []string
		var seqSet imap.SeqSet
		var messages []*imapclient.FetchMessageBuffer
		var content *string
		var boundary *string

		seqSet = imap.SeqSetNum(i)

		if messages, err = retrieveEmailMessages(imapClient, seqSet); err != nil {
			return fmt.Errorf("cannot fetch messages from \"INBOX\": %s", err.Error())
		}
		if from != "" && from != messages[0].Envelope.From[0].Addr() {
			continue
		}
		for _, a := range messages[0].Envelope.Cc {
			ccs = append(ccs, a.Addr())
		}
		for _, a := range messages[0].Envelope.To {
			addresses = append(addresses, a.Addr())
		}

		if boundary, err = retrieveBoundary(messages[0]); err != nil {
			return err
		}
		if boundary != nil {
			indexBoundary[i] = *boundary

			fmt.Printf("[%4d] %s (%d)\n", i, messages[0].Envelope.Date.String(), messages[0].Envelope.Date.Unix())
			fmt.Printf("       Subject: %s\n", messages[0].Envelope.Subject)
			fmt.Printf("       From: %s\n", messages[0].Envelope.From[0].Addr())
			fmt.Printf("       To: %s\n", strings.Join(addresses, ", "))
			if len(ccs) > 0 {
				fmt.Printf("       Cc: %s\n", strings.Join(ccs, ", "))
			}
			fmt.Printf("       Boundary: %s\n", *boundary)
			fmt.Printf("\n")
		} else {
			continue
		}

		if full {
			if content, err = retrieveFullEmail(messages); err != nil {
				return err
			}
			fmt.Printf("%s\n\n", *content)
		}
	}

	if err := imapClient.Logout().Wait(); nil != err {
		return fmt.Errorf("cannot logout: %s", err.Error())
	}

	// Ask for the list of emails to process.
	if emails, err = getEmails(indexBoundary); err != nil {
		return err
	}
	if emails == nil {
		return nil
	}
	sort.SliceStable(emails, func(i, j int) bool {
		return emails[i] < emails[j]
	})
	fmt.Printf("You selected: %s\n", strings.Join(func(p []emailIndex) []string {
		var r []string
		for _, v := range p {
			r = append(r, fmt.Sprintf("%d", v))
		}
		return r
	}(emails), ", "))

	// Ask for confirmation.
	if proceed, err = getYesNo("Proceed ? (y/n)"); err != nil {
		return fmt.Errorf("unexpected error: %s", err)
	}
	if *proceed == false {
		return nil
	}

	// Show the hidden message.
	for _, emailIndex := range emails {
		fmt.Printf("[%4d] %s\n", emailIndex, indexBoundary[emailIndex])
		boundaries = append(boundaries, indexBoundary[emailIndex])
	}

	_, _ = showMessage(boundaries)

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
	"rcv":            {Description: `retrieve emails`, Handler: processGetFullEmails},
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
