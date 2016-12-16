// Copyright 2015 Canonical Ltd.

// Package idptest contains tools useful for testing identity providers.
package idptest

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/juju/httprequest"
	"github.com/juju/idmclient/params"
	jc "github.com/juju/testing/checkers"
	"golang.org/x/net/context"
	gc "gopkg.in/check.v1"
	"gopkg.in/errgo.v1"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	"gopkg.in/mgo.v2"
)

// TestContext is an idp.Context that can be used to test identity providers.
type TestContext struct {
	// URLPrefix contains the prefix to add to the front of generated
	// URLs.
	URLPrefix string

	// Request contains the request to return to Handle in Params().
	Request *http.Request

	// TestBakery contains the bakery.Service to return to Handle in Bakery().
	Bakery_ *bakery.Bakery

	// TestDatabase contains the mgo.Database to return to Handle in Database().
	Database_ *mgo.Database

	// FailOnLoginSuccess can be used to simulate a login failure
	// after the identity provider has indicated it is a successful
	// login.
	FailOnLoginSuccess bool

	// UpdateUserError contains an error to return when Handle calls
	// UpdateUser.
	UpdateUserError error

	// FindUserByNameError contains an error to return when Handle
	// calls FindUserByName.
	FindUserByNameError error

	// FindUserByExternalIdError contains an error to return when
	// Handle calls FindUserByExternalId.
	FindUserByExternalIdError error

	// mu protects the remaining variables.
	mu sync.Mutex

	// params contains the parameters for the request. It will be
	// genreated the first time it is used. params.Request will be
	// Request, params.Response will be a new
	// httptest.ResponseRecorder that can be retrieved later using
	// Response.
	params httprequest.Params

	// users contains the list of users known to this context.
	// UpdateUser adds or updates the array. FindUserByName and
	// FindUserByExternalID examine the list to find appropriate
	// users.
	users []params.User

	// caveats and caveatsnSet whether LoginSuccess has been called
	// and with what values.
	username    params.Username
	expiry      time.Time
	usernameSet bool

	// err and errSet whether LoginFailure has been called and with
	// what value.
	err    error
	errSet bool
}

// URL implements URLContext.URL.
func (c *TestContext) URL(path string) string {
	return c.URLPrefix + path
}

// RequestURL implements Context.RequestURL.
func (c *TestContext) RequestURL() string {
	return c.Params().Request.URL.String()
}

// Params implements Context.Params.
func (c *TestContext) Params() httprequest.Params {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.params.Request == nil {
		var r http.Request
		if c.Request != nil {
			r = *c.Request
		}
		c.params = httprequest.Params{
			Request:  &r,
			Response: httptest.NewRecorder(),
			Context:  context.Background(),
		}
	}
	return c.params
}

// Bakery implements Context.Bakery.
func (c *TestContext) Bakery() *bakery.Bakery {
	return c.Bakery_
}

// Database implements Context.Database.
func (c *TestContext) Database() *mgo.Database {
	return c.Database_
}

// UpdateUser implements Context.UpdateUser.
func (c *TestContext) UpdateUser(user *params.User) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.UpdateUserError != nil {
		return c.UpdateUserError
	}
	for i, u := range c.users {
		if u.Username == user.Username {
			if u.ExternalID == user.ExternalID {
				c.users[i] = *user
				return nil
			}
			return errgo.WithCausef(nil, params.ErrAlreadyExists, "username %q already used", user.Username)
		} else if u.ExternalID == user.ExternalID {
			return errgo.WithCausef(nil, params.ErrAlreadyExists, "external id %q already used", user.ExternalID)
		}
	}
	c.users = append(c.users, *user)
	return nil
}

// FindUserByName implements Context.FindUserByName.
func (c *TestContext) FindUserByName(name params.Username) (*params.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FindUserByNameError != nil {
		return nil, c.FindUserByNameError
	}
	for _, u := range c.users {
		if u.Username == name {
			return &u, nil
		}
	}
	return nil, errgo.WithCausef(nil, params.ErrNotFound, "cannot find user %q", name)
}

// FindUserByExternalId implements Context.FindUserByExternalId.
func (c *TestContext) FindUserByExternalId(id string) (*params.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FindUserByExternalIdError != nil {
		return nil, c.FindUserByExternalIdError
	}
	for _, u := range c.users {
		if u.ExternalID == id {
			return &u, nil
		}
	}
	return nil, errgo.WithCausef(nil, params.ErrNotFound, "cannot find external id %q", id)
}

// LoginSuccess implements Context.LoginSuccess.
func (c *TestContext) LoginSuccess(username params.Username, expiry time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.username, c.expiry, c.usernameSet = username, expiry, true
	return !c.FailOnLoginSuccess
}

// LoginFailure implements Context.LoginFailure.
func (c *TestContext) LoginFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err, c.errSet = err, true
}

// Response gets the HTTP response that was written.
func (c *TestContext) Response() *httptest.ResponseRecorder {
	return c.Params().Response.(*httptest.ResponseRecorder)
}

// LoginSuccessCall returns information about the call to LoginSuccess.
// If LoginSuccess was called then the returned value will be username, expiry, true,
// where username and expiry were the arguments used to call
// LoginSuccess.
//
// If LoginSuccess was not called then the returned value will be
// macaroon.Slice{}, time.Time{}, false.
func (c *TestContext) LoginSuccessCall() (params.Username, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.username, c.expiry, c.usernameSet
}

// LoginFailureCall returns information about the call to LoginFailure.
// If LoginFailure was called then the returned value will be err, true,
// where err is the error used to call LoginFailure. If LoginFailure was
// not called then the returned value will be nil, false.
func (c *TestContext) LoginFailureCall() (error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err, c.errSet
}

// AssertLoginSuccess asserts that tc.LoginSuccess has been
// called with the given username and an expiry time in the future.
func AssertLoginSuccess(c *gc.C, tc *TestContext, username params.Username) {
	err, called := tc.LoginFailureCall()
	c.Assert(called, gc.Equals, false, gc.Commentf("unexpected login failure: %v", err))
	calledUsername, calledExpiry, called := tc.LoginSuccessCall()
	c.Assert(called, gc.Equals, true)

	if now := time.Now(); calledExpiry.Before(now) {
		c.Error("expiry time %v is before now %v", calledExpiry, now)
	}
	c.Assert(calledUsername, gc.Equals, username)
}

// AssertUser asserts that the given user document is stored
// in tc's database.
func AssertUser(c *gc.C, tc *TestContext, u *params.User) {
	user, err := tc.FindUserByName(u.Username)
	c.Assert(err, gc.IsNil)
	c.Assert(user, jc.DeepEquals, u)
}

// AssertLoginFailure asserts that the result of tc is a login failure
// with an error message that matches errRegex.
func AssertLoginFailure(c *gc.C, tc *TestContext, errRegex string) {
	_, _, called := tc.LoginSuccessCall()
	c.Assert(called, gc.Equals, false)
	err, called := tc.LoginFailureCall()
	c.Assert(called, gc.Equals, true)
	c.Assert(err, gc.ErrorMatches, errRegex)
}

// AssertLoginInProgress asserts that the result of tc is neither a
// LoginSuccess or a LoginFailure.
func AssertLoginInProgress(c *gc.C, tc *TestContext) {
	err, called := tc.LoginFailureCall()
	c.Assert(called, gc.Equals, false, gc.Commentf("unexpected login failure: %v", err))
	_, _, called = tc.LoginSuccessCall()
	c.Assert(called, gc.Equals, false)
}
