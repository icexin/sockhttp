package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

var (
	sockaddr   = flag.String("sock", "127.0.0.1:7000", "socks address")
	listenaddr = flag.String("listen", ":7001", "listen address")
)

var (
	dialer proxy.Dialer
	trans  http.Transport
)

var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // canonicalized version of "TE"
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

func logreq(r *http.Request, start time.Time) {
	log.Printf("%s %s %s", r.Method, r.URL.Host, time.Now().Sub(start).String())
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func serveHTTP(w http.ResponseWriter, r *http.Request) {
	defer logreq(r, time.Now())

	if r.Method == "CONNECT" {
		err := connect(w, r)
		if err != nil {
			log.Printf("%s", err)
		}
		return
	}

	r.RequestURI = ""
	for _, h := range hopHeaders {
		if r.Header.Get(h) != "" {
			r.Header.Del(h)
		}
	}

	resp, err := trans.RoundTrip(r)
	if err != nil {
		log.Printf("%s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("copy error:%s", err)
	}
}

func connect(w http.ResponseWriter, r *http.Request) error {
	hij, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("http server does not support hijacker")
	}
	src, _, err := hij.Hijack()
	if err != nil {
		return fmt.Errorf("hijack error:%s", err)
	}

	defer src.Close()

	host := r.URL.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}

	dest, err := dialer.Dial("tcp", host)
	if err != nil {
		src.Write([]byte("HTTP/1.0 502 ERROR\r\n\r\n"))
		return err
	}
	defer dest.Close()
	src.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
	go func() {
		io.Copy(dest, src)
		dest.(*net.TCPConn).CloseWrite()
	}()

	io.Copy(src, dest)
	src.(*net.TCPConn).CloseWrite()
	return nil
}

func main() {
	flag.Parse()

	var err error
	dialer, err = proxy.SOCKS5("tcp", *sockaddr, nil, &net.Dialer{})
	if err != nil {
		log.Fatalf("dial sock5 error:%s", err)
	}

	trans = http.Transport{Dial: dialer.Dial}

	log.Fatal(http.ListenAndServe(*listenaddr, http.HandlerFunc(serveHTTP)))
}
