// Package addr splits listen addresses such as "localhost:8080" or
// "[::1]:9090" from config files. It is a made-up example for fuzzsift.
// The bugs its fuzz corpus found are fixed.
package addr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Split returns the host and port of a listen address. The host may be
// empty, as in ":8080", and an IPv6 host goes in brackets.
func Split(s string) (string, int, error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("addr: %q: missing port", s)
	}
	host, port := s[:i], s[i+1:]
	if strings.HasPrefix(host, "[") {
		j := strings.Index(host, "]")
		if j != len(host)-1 {
			return "", 0, fmt.Errorf("addr: %q: bad brackets", s)
		}
		host = host[1:j]
	}
	if strings.ContainsAny(host, "[]") {
		return "", 0, fmt.Errorf("addr: %q: bad brackets", s)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return "", 0, fmt.Errorf("addr: %q: %w", s, err)
	}
	if n < 1 || n > 65535 {
		return "", 0, errors.New("addr: port out of range")
	}
	return host, n, nil
}

// Join is the reverse of Split.
func Join(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}
