// Copyright (c) 2019 Blacknon. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package sshlib

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

//
//
func (c *Connect) X11Forward(session *ssh.Session) (err error) {
	display := getX11Display()
	_, xAuth, err := readAuthority("", "0")
	if err != nil {
		return
	}

	var cookie string
	for _, d := range xAuth {
		cookie = cookie + fmt.Sprintf("%02x", d)
	}

	// set x11-req Payload
	payload := x11request{
		SingleConnection: false,
		AuthProtocol:     string("MIT-MAGIC-COOKIE-1"),
		AuthCookie:       string(cookie),
		ScreenNumber:     uint32(0), // TODO(blacknon): 仮置きの値
	}

	// Send x11-req Request
	ok, err := session.SendRequest("x11-req", true, ssh.Marshal(payload))
	if err == nil && !ok {
		return errors.New("ssh: x11-req failed")
	} else {
		// Open HandleChannel x11
		x11channels := c.Client.HandleChannelOpen("x11")

		go func() {
			for ch := range x11channels {
				channel, _, err := ch.Accept()
				if err != nil {
					continue
				}

				go x11forwarder(channel)
			}
		}()
	}
}

//
//
func x11Connect() (conn net.Conn, err error) {
	display := os.Getenv("DISPLAY")
	display0 := display
	colonIdx := strings.LastIndex(display, ":")
	dotIdx := strings.LastIndex(display, ".")

	if colonIdx < 0 {
		err = errors.New("bad display string: " + display0)
		return
	}

	var conDisplay string
	if display[0] == '/' { // PATH type socket
		conDisplay = display
	} else { // /tmp/.X11-unix/X0
		conDisplay = "/tmp/.X11-unix/X" + display[colonIdx+1:dotIdx]
	}

	// fmt.Println(conDisplay)
	conn, err = net.Dial("unix", conDisplay)
	return
}

//
//
func x11forwarder(channel ssh.Channel) {
	conn, err := x11Connect()

	if err != nil {
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		io.Copy(conn, channel)
		conn.(*net.UnixConn).CloseWrite()
		wg.Done()
	}()
	go func() {
		io.Copy(channel, conn)
		channel.CloseWrite()
		wg.Done()
	}()

	wg.Wait()
	conn.Close()
	channel.Close()
}

//
//
func getX11Display() int {
	display := os.Getenv("DISPLAY")
	colonIdx := strings.LastIndex(display, ":")
	dotIdx := strings.LastIndex(display, ".")

	if colonIdx < 0 {
		return 0
	}

	return display[colonIdx+1 : dotIdx]
}

// readAuthority Read env `$XAUTHORITY`. If not set value, read `~/.Xauthority`.
//
func readAuthority(hostname, display string) (
	name string, data []byte, err error) {

	// b is a scratch buffer to use and should be at least 256 bytes long
	// (i.e. it should be able to hold a hostname).
	b := make([]byte, 256)

	// As per /usr/include/X11/Xauth.h.
	const familyLocal = 256

	if len(hostname) == 0 || hostname == "localhost" {
		hostname, err = os.Hostname()
		if err != nil {
			return "", nil, err
		}
	}

	fname := os.Getenv("XAUTHORITY")
	if len(fname) == 0 {
		home := os.Getenv("HOME")
		if len(home) == 0 {
			err = errors.New("Xauthority not found: $XAUTHORITY, $HOME not set")
			return "", nil, err
		}
		fname = home + "/.Xauthority"
	}

	r, err := os.Open(fname)
	if err != nil {
		return "", nil, err
	}
	defer r.Close()

	for {
		var family uint16
		if err := binary.Read(r, binary.BigEndian, &family); err != nil {
			return "", nil, err
		}

		addr, err := getString(r, b)
		if err != nil {
			return "", nil, err
		}

		disp, err := getString(r, b)
		if err != nil {
			return "", nil, err
		}

		name0, err := getString(r, b)
		if err != nil {
			return "", nil, err
		}

		data0, err := getBytes(r, b)
		if err != nil {
			return "", nil, err
		}

		if family == familyLocal && addr == hostname && disp == display {
			return name0, data0, nil
		}
	}

	return
}

// getBytes use `readAuthority`
func getBytes(r io.Reader, b []byte) ([]byte, error) {
	var n uint16
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return nil, err
	} else if n > uint16(len(b)) {
		return nil, errors.New("bytes too long for buffer")
	}

	if _, err := io.ReadFull(r, b[0:n]); err != nil {
		return nil, err
	}
	return b[0:n], nil
}

// getString use `readAuthority`
func getString(r io.Reader, b []byte) (string, error) {
	b, err := getBytes(r, b)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// TCPForward
//
func (c *Connect) TCPForward(localAddr, remoteAddr addr) (err error) {
	listener, err := net.Listen("tcp", local)
	if err != nil {
		return
	}

	go func() {
		for {
			//  (type net.Conn)
			conn, err := listner.Accept()
			if err != nil {
				return
			}

			go c.forwarder(conn, "tcp", remoteAddr)
		}
	}()
}

// forwarder tcp/udp port forward. dialType in `tcp` or `udp`.
// addr is remote port forward address (`localhost:80`, `192.168.10.100:443` etc...).
func (c *Connect) forwarder(local net.Conn, dialType string, addr string) {
	// Create ssh connect
	remote, err := c.Client.Dial(dialType, addr)

	var wg sync.WaitGroup
	wg.Add(2)

	// Copy local to remote
	go func() {
		io.Copy(remote, local)
		wg.Done()
	}()

	// Copy remote to local
	go func() {
		io.Copy(local, remote)
		wg.Done()
	}()

	wg.Wait()
	conn.Close()
	local.Close()
}