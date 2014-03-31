package xmpp_test

import (
    "crypto/tls"
    "github.com/Narsil/xmpp"
    "net"
    "testing"
    "time"
)

func authenticate(username, password string) bool{
    return true
}

func ExampleXmppServer(){
    srv := xmpp.Server{}
    srv.SetKeyPair("test.cert", "test.key")
    srv.SetAuthFunc(authenticate)
}

func clientXmpp(t *testing.T, err_chan chan error){
    buf := make([]byte, 1032)
    // Let's use another port in case xmpp is already open
    conn, err := net.Dial("tcp", ":5223")
    if err != nil {
        err_chan <- err
    }
    conn.Write([]byte("<?xml version='1.0' ?>"))
    conn.Write([]byte("<stream:stream to='example.com' xmlns='jabber:client' xmlns:stream='http://etherx.jabber.org/streams' version='1.0'>"))
    _, err = conn.Read(buf)
    if err != nil {
        err_chan <- err
    }
    conn.Write([]byte("<starttls xmlns='urn:ietf:params:xml:ns:xmpp-tls'/>"))
    _, err = conn.Read(buf)
    if err != nil {
        err_chan <- err
    }

    config := tls.Config{InsecureSkipVerify: true}
    tls_conn := tls.Client(conn, &config)
    tls_conn.Handshake()

    tls_conn.Write([]byte("<stream:stream to='example.com' xmlns='jabber:client' xmlns:stream='http://etherx.jabber.org/streams' version='1.0'>"))
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }

    tls_conn.Write([]byte("<auth xmlns='urn:ietf:params:xml:ns:xmpp-sasl' mechanism='PLAIN' xmlns:ga='http://www.google.com/talk/protocol/auth' ga:client-uses-full-bind-result='true'>password removed</auth>"))
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }

    tls_conn.Write([]byte("<stream:stream to='sms.nicolas.kwyk.fr' xmlns='jabber:client' xmlns:stream='http://etherx.jabber.org/streams' version='1.0'>"))
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }

    tls_conn.Write([]byte("<iq type='get' id='purple3461be23'><ping xmlns='urn:xmpp:ping'/></iq>"))
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }

    tls_conn.Write([]byte("<iq type='set' id='purple349c9433'><session xmlns='urn:ietf:params:xml:ns:xmpp-session'/></iq>"))
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }

    tls_conn.Write([]byte("<iq type='get' id='purple349c9434' to='sms.nicolas.kwyk.fr'><query xmlns='http://jabber.org/protocol/disco#items'/></iq>"))
    tls_conn.Write([]byte("<iq type='get' id='purple349c9435' to='sms.nicolas.kwyk.fr'><query xmlns='http://jabber.org/protocol/disco#info'/></iq>"))
    _, err = tls_conn.Read(buf)
    if err != nil {
        err_chan <- err
    }
}

func TestInexistentKeyPair(t *testing.T){
    srv := xmpp.Server{}
    err := srv.SetKeyPair("nofile.cert", "nofile.key")
    if (err == nil){
        t.Errorf("Server should throw an exception if certificate or key are missing")
    }
}

func TestFunctional(t *testing.T){
    srv := xmpp.Server{}
    err := srv.SetKeyPair("test.cert", "test.key")
    if (err != nil){
        t.Errorf("Could not load certificates: %s", err)
    }
    srv.SetAuthFunc(authenticate)


    err_chan := make(chan error)
    go func(){
        // Let's use another port in case xmpp is already open
        err_chan<-srv.ListenAndServe("tcp", ":5223")
    }()
    <-time.After(1 * time.Second)
    go clientXmpp(t, err_chan)
    select {
    case <-time.After(1 * time.Second):
    case err:= <-err_chan:
        if err != nil{
            t.Errorf("Error occurred while listening/serving: %s", err)
        }
    }
}
