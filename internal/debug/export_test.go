// Copyright 2016 Canonical Ltd.

package debug

import "gopkg.in/macaroon-bakery.v2-unstable/bakery"

type (
	DebugAPIHandler *debugAPIHandler
	Cookie          cookie
)

var (
	New = newDebugAPIHandler
)

// DecodeCookie is a wrapper around decodeCookie that can be used for
// testing.
func DecodeCookie(k *bakery.KeyPair, s string) (*Cookie, error) {
	c, err := decodeCookie(k, s)
	return (*Cookie)(c), err
}

// EncodeCookie is a wrapper around encodeCookie that can be used for
// testing.
func EncodeCookie(k *bakery.KeyPair, c *Cookie) (string, error) {
	return encodeCookie(k, (*cookie)(c))
}
