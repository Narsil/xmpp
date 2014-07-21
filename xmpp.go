package xmpp

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
)

// var dbg *bool = flag.Bool("debug", false, "enable debug logging")
const dbg = false

func random_string(str_size int) string {
	alphanum := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, str_size)
	for i, _ := range bytes {
		bytes[i] = alphanum[rand.Intn(len(alphanum))]
	}
	return string(bytes)
}

func debug(format string, args ...interface{}) {
	if dbg {
		log.Println("DEBUG: "+format, args)
	}
}
func errorf(format string, args ...interface{}) {
	log.Println("ERROR: "+format, args)
}

type xmppConn struct {
	net.Conn
	username  string
	extension string
}

func (conn xmppConn) Read(buf []byte) (total int, err error) {
	n, err := conn.Conn.Read(buf)
	if err != nil {
		return n, err
	}
	total = n
	for buf[total-1] != '>' {
		n, err = conn.Conn.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	debug("Read : ", string(buf[:total]))
	return total, err
}
func (conn xmppConn) Write(buf []byte) (n int, err error) {
	n, err = conn.Conn.Write(buf)
	debug("Write : ", string(buf[:n]))
	return n, err
}

type Server struct {
	addr           string // Used to know where to listen to.
	domain         string // Domain is the name of the server
	tlsConfig      tls.Config
	MessageChannel chan Message

	authenticate    func(username, password string) bool
	handleMessage   func(msg XmppMessage)
	messageChannels map[string][]chan Message
	handlers        map[string]func(io.Writer, Request)
}

// StreamError is a generic error related to a stream.
type StreamError struct {
	Condition string // one of the conditions from §4.9.3 of RFC 6120
	Message   string
}

type Message struct {
	To      string
	Message string
	From    string
}
type XmppMessage struct {
	XMLName xml.Name `xml:"message"`
	Type    string   `xml:"type,attr,omitempty`
	Id      string   `xml:"id,attr,omitempty"`
	To      string   `xml:"to,attr,omitempty"`
	Actives []Active `xml:"active"`
	Bodies  []Body   `xml:"body"`
}
type Active struct {
	Xmlns string `xml:"xmlns,attr"`
}
type Body struct {
	Body string `xml:",innerxml"`
}

// Stream represents an XMPP stream.
type Stream struct {
	XMLName  xml.Name `xml:"http://etherx.jabber.org/streams stream"`
	To       string   `xml:"to,attr,omitempty"`
	From     string   `xml:"from,attr,omitempty"`
	Language string   `xml:"lang,attr,omitempty"`
	Id       string   `xml:"id,attr,omitempty"`
	Stream   string   `xml:"http://etherx.jabber.org/streams stream,attr,omitempty"`
	Version  string   `xml:"version,attr,omitempty"`
}

type Bind struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-bind bind"`
	Body    string   `xml:",innerxml"`
}

type Query struct {
	Xmlns string `xml:"xmlns,attr"`
	Body  string `xml:",innerxml"`
}

type Ping struct {
	Xmlns string `xml:"xmlns,attr"`
	Body  string `xml:",innerxml"`
}

type IQ struct {
	XMLName xml.Name `xml:"iq"`
	Type    string   `xml:"type,attr"`
	Id      string   `xml:"id,attr"`
	Binds   []Bind
	Queries []Query `xml:"query"`
	Pings   []Ping  `xml:"ping"`
}

type Request struct{
    Id string
    User string
}

func NewServer(domain string) (srv Server) {
	srv.domain = domain
	srv.MessageChannel = make(chan Message)
	srv.messageChannels = make(map[string][]chan Message)
	srv.handlers = make(map[string]func(io.Writer, Request))
	go srv.distributeMessages()
	return srv
}

func (srv *Server) HandleQuery(query string, handler func(io.Writer, Request)) {
	srv.handlers[query] = handler
}

func (srv *Server) distributeMessages() {
	for msg := range srv.MessageChannel {
		debug("Msg Received", msg)
		if user_chans, ok := srv.messageChannels[msg.To]; ok {
			debug("User Found", user_chans)
			for _, user_chan := range user_chans {
				debug("Channel Found", user_chan)
				go func(user_chan chan Message) {
					user_chan <- msg
				}(user_chan)
			}
		} else {
			debug("Unknown user message received")
		}
	}
}

func (srv *Server) SetKeyPair(certfile, keyfile string) error {
	cert, err := tls.LoadX509KeyPair(certfile, keyfile)
	if err != nil {
		return errors.New(err.Error() + "\nYou can create keypairs with the following command\n" +
			"`openssl genrsa 2048 > " + keyfile + "`\n" +
			"`chmod 400 " + keyfile + "`\n" +
			"`openssl req -new -x509 -nodes -sha1 -days 3650 -key " + keyfile + " > " + certfile)
	}
	config := tls.Config{
		Certificates: []tls.Certificate{
			cert,
		},
	}
	srv.tlsConfig = config
	return nil
}

func (srv *Server) SetAuthFunc(authenticate func(username, password string) bool) {
	srv.authenticate = authenticate
}

func (srv *Server) SetHandleIncomingMessage(handle func(msg XmppMessage)) {
	srv.handleMessage = handle
}

func (srv Server) ListenAndServe(protocol, addr string) error {
	l, err := net.Listen(protocol, addr)
	if err != nil {
		return err
	}
	debug("Starting server")

	for {
		conn, err := l.Accept()
		xmppconn := xmppConn{conn, "", ""}
		if err != nil {
			return err
		}
		go srv.handle(xmppconn, false)
	}
}

func (srv *Server) handle(conn xmppConn, is_tls bool) {
	buf := make([]byte, 1032)

	// XML Header
	if !is_tls {
		_, err := conn.Read(buf)
		if err != nil {
			log.Println("Cannot read from client")
		}
	}
	m, err := conn.Read(buf)
	if err != nil {
		log.Println("Cannot read from client")
	}
	stream := Stream{}
	err = xml.Unmarshal([]byte(string(buf[:m])), &stream)
	if err == io.EOF {
		err = nil
	} else if err != nil {
		log.Println(err)
	}

	conn.Write([]byte(`<?xml version='1.0' encoding='UTF-8'?><stream:stream xmlns:stream="http://etherx.jabber.org/streams" xmlns="jabber:client" from="` + srv.domain + `" id="7c1d95c6" xml:lang="en" version="1.0">`))

	if is_tls {
		conn.Write([]byte(`<stream:features><mechanisms xmlns="urn:ietf:params:xml:ns:xmpp-sasl"><mechanism>PLAIN</mechanism></mechanisms><auth xmlns="http://jabber.org/features/iq-auth"/><register xmlns="http://jabber.org/features/iq-register"/></stream:features>`))
	} else {
		conn.Write([]byte(`<stream:features><starttls xmlns="urn:ietf:params:xml:ns:xmpp-tls"></starttls><mechanisms xmlns="urn:ietf:params:xml:ns:xmpp-sasl"><mechanism>PLAIN</mechanism></mechanisms><auth xmlns="http://jabber.org/features/iq-auth"/><register xmlns="http://jabber.org/features/iq-register"/></stream:features>`))
		n, _ := conn.Read(buf)
		if string(buf[:n]) == "<starttls xmlns='urn:ietf:params:xml:ns:xmpp-tls'/>" {
			srv.switchToTLS(conn)
			return
		}
	}

	n, err := conn.Read(buf)
	received := "AG5pY29sYXMAY281bXV4eTI="
	data, err := base64.StdEncoding.DecodeString(received)
	checkErr(conn, err, "Cannot decode base64")
	tokens := strings.Split(string(data), "\000")
	username := tokens[1]
	password := tokens[2]
	if !srv.authenticate(username, password) {
		return
	}
	conn.username = username

	// User is authentified, now let's bind him
	conn.extension = random_string(8)
	conn.Write([]byte("<success xmlns='urn:ietf:params:xml:ns:xmpp-sasl'/>"))
	conn.Read(buf)
	conn.Write([]byte("<stream:stream from='" + srv.domain + "' id='1' version='1.0' xmlns:stream='http://etherx.jabber.org/streams' xmlns='jabber:client' xml:lang='en'><stream:features><bind xmlns='urn:ietf:params:xml:ns:xmpp-bind'/><session xmlns='urn:ietf:params:xml:ns:xmpp-session'/></stream:features>"))
	n, err = conn.Read(buf)
	iq := IQ{}
	err = xml.Unmarshal([]byte(string(buf[:n+m])), &iq)
	checkErr(conn, err, "Error Unmarshaling binding IQ")
	result := IQ{
		Type: "result",
		Id:   iq.Id,
		Binds: []Bind{Bind{
			Body: "<jid>" + conn.username + "@" + srv.domain + "/" + conn.extension + "</jid>",
		}},
	}
	output, err := xml.MarshalIndent(result, "", "    ")
	checkErr(conn, err, "Error Marshaling binding IQ")
	conn.Write(output)

	n, err = conn.Read(buf)
	iq = IQ{}
	err = xml.Unmarshal([]byte(string(buf[:n])), &iq)
	checkErr(conn, err, "Error Unmarshaling binding IQ")
	result = IQ{
		Type: "result",
		Id:   iq.Id,
	}
	output, err = xml.MarshalIndent(result, "", "    ")
	checkErr(conn, err, "Error Marshaling binding IQ")
	conn.Write(output)

	user := conn.username
	debug("New user %s correctly bound", user)

	//Allocate channel to send messages
	if _, ok := srv.messageChannels[user]; !ok {
		srv.messageChannels[user] = make([]chan Message, 0)
	}
	user_chans := srv.messageChannels[user]
	user_chan := make(chan Message)
	user_chans = append(user_chans, user_chan)
	srv.messageChannels[user] = user_chans

	go srv.handleReads(conn, user_chan)

	// go func(){
	//     srv.MessageChannel <- Message{To:user, From:"+33672317534", Message: "Bienvenue"}
	// }()

	for msg := range user_chan {
		debug("Writing to connection", conn)
		debug("From channel", user_chan)
		conn.Write([]byte(`<message from='` + msg.From + `@` + srv.domain + `'
                    to='` + msg.To + `@` + srv.domain + `'
                    xml:lang='en'><body>` + msg.Message + `</body></message>`))
	}
}

func (srv *Server) handleReads(conn xmppConn, user_chan chan Message) {
	buf := make([]byte, 4096)

	user := conn.username
	// Whenever closing signal is received remove channel from user channels
	defer func(user_chan chan Message) {
		user_chans := srv.messageChannels[user]
		debug("Removing user channel %s", len(user_chans))
		user_chan_index := -1
		for i, _chan := range user_chans {
			if _chan == user_chan {
				user_chan_index = i
			}
		}
		user_chans[user_chan_index] = user_chans[len(user_chans)-1]
		user_chans = user_chans[:len(user_chans)-1]
		srv.messageChannels[user] = user_chans
		debug("Removed user channel %s", len(user_chans))
	}(user_chan)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			// Close this goroutine on error
			return
		}

		if string(buf[:n]) == `</stream:stream>` {
			//Close the stream
			return
		}
		iq := IQ{}
		err = xml.Unmarshal([]byte(string(buf[:n])), &iq)
		if err == nil {
			srv.handleIQ(conn, iq)
		}
		msg := XmppMessage{}
		err = xml.Unmarshal([]byte(string(buf[:n])), &msg)
		if err == nil {
			srv.handleMessage(msg)
		}

	}

}

// func (srv *Server) defaultHandleMessage(conn xmppConn, msg XmppMessage){
//     debug("Received Message", msg)
//     for _, body := range(msg.Bodies){
//         debug("Sending to appropriate recipient")
//         srv.MessageChannel <- Message{To: msg.To, From:user, Message: body.Body}
//     }
// }

func (srv *Server) handleIQ(conn xmppConn, iq IQ) {
	for _, query := range iq.Queries {
		if handler, ok := srv.handlers[query.Xmlns]; ok {
            request := Request{iq.Id, conn.username}
			handler(conn, request)
		}
	}
	for _, ping := range iq.Pings {
		if ping.Xmlns == "urn:xmpp:ping" {
			conn.Write([]byte(`<iq type='result' to=' ` + conn.username + `@` + srv.domain + `/` + conn.extension + `' id='` + iq.Id + `'/>`))
		}
	}
}

func (srv *Server) switchToTLS(conn xmppConn) {
	conn.Write([]byte(`<proceed xmlns="urn:ietf:params:xml:ns:xmpp-tls"/>`))

	var tls_conn *tls.Conn
	tls_conn = tls.Server(conn.Conn, &srv.tlsConfig)
	tls_conn.Handshake()
	new_conn := xmppConn{tls_conn, "", ""}
	srv.handle(new_conn, true)
}

func checkErr(conn io.Writer, err error, msg string) {
	if err != nil {
		log.Println(err, msg)
	}
}
