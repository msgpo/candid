// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package discharger_test

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/frankban/quicktest/qtsuite"
	"gopkg.in/CanonicalLtd/candidclient.v1/params"
	errgo "gopkg.in/errgo.v1"
	"gopkg.in/macaroon-bakery.v2/bakery/identchecker"
	"gopkg.in/macaroon-bakery.v2/httpbakery"

	"github.com/CanonicalLtd/candid/idp"
	"github.com/CanonicalLtd/candid/idp/static"
	"github.com/CanonicalLtd/candid/internal/auth"
	"github.com/CanonicalLtd/candid/internal/candidtest"
	"github.com/CanonicalLtd/candid/internal/discharger"
	"github.com/CanonicalLtd/candid/internal/identity"
)

func TestLogin(t *testing.T) {
	qtsuite.Run(qt.New(t), &loginSuite{})
}

type loginSuite struct {
	store            *candidtest.Store
	srv              *candidtest.Server
	dischargeCreator *candidtest.DischargeCreator
	interactor       httpbakery.WebBrowserInteractor
}

func (s *loginSuite) Init(c *qt.C) {
	s.store = candidtest.NewStore()
	sp := s.store.ServerParams()
	sp.RedirectLoginWhitelist = []string{
		"https://example.com/callback",
	}
	sp.IdentityProviders = []idp.IdentityProvider{
		static.NewIdentityProvider(static.Params{
			Name: "test",
			Users: map[string]static.UserInfo{
				"test": {
					Password: "testpassword",
					Name:     "Test User",
					Email:    "test@example.com",
					Groups:   []string{"test1", "test2"},
				},
			},
			Icon: "/static/static1.bmp",
		}),
		static.NewIdentityProvider(static.Params{
			Name:   "test2",
			Domain: "test2",
			Icon:   "/static/static2.bmp",
		}),
		static.NewIdentityProvider(static.Params{
			Name:   "test3",
			Domain: "test3",
			Icon:   "/static/static3.bmp",
			Hidden: true,
		}),
	}
	s.srv = candidtest.NewServer(c, sp, map[string]identity.NewAPIHandlerFunc{
		"discharger": discharger.NewAPIHandler,
	})
	s.dischargeCreator = candidtest.NewDischargeCreator(s.srv)
	s.interactor = httpbakery.WebBrowserInteractor{
		OpenWebBrowser: candidtest.PasswordLogin(c, "test", "testpassword"),
	}
}

func (s *loginSuite) TestLegacyInteractiveLogin(c *qt.C) {
	client := s.srv.Client(s.interactor)
	// Use "<is-authenticated-user" to force legacy interaction
	ms, err := s.dischargeCreator.Discharge(c, "<is-authenticated-user", client)
	c.Assert(err, qt.Equals, nil)
	s.dischargeCreator.AssertMacaroon(c, ms, identchecker.LoginOp, "test")
}

func (s *loginSuite) TestLegacyNonInteractiveLogin(c *qt.C) {
	client := s.srv.AdminClient()
	// Use "<is-authenticated-user" to force legacy interaction
	ms, err := s.dischargeCreator.Discharge(c, "<is-authenticated-user", client)
	c.Assert(err, qt.Equals, nil)
	s.dischargeCreator.AssertMacaroon(c, ms, identchecker.LoginOp, auth.AdminUsername)
}

func (s *loginSuite) TestLegacyLoginFailure(c *qt.C) {
	client := s.srv.Client(httpbakery.WebBrowserInteractor{
		OpenWebBrowser: candidtest.OpenWebBrowser(c, candidtest.SelectInteractiveLogin(badLoginFormRequestMethod)),
	})
	// Use "<is-authenticated-user" to force legacy interaction
	_, err := s.dischargeCreator.Discharge(c, "<is-authenticated-user", client)
	c.Assert(err, qt.ErrorMatches, `cannot get discharge from ".*": failed to acquire macaroon after waiting: third party refused discharge: unsupported method "PUT"`)
}

func (s *loginSuite) TestInteractiveLogin(c *qt.C) {
	client := s.srv.Client(s.interactor)
	ms, err := s.dischargeCreator.Discharge(c, "is-authenticated-user", client)
	c.Assert(err, qt.Equals, nil)
	s.dischargeCreator.AssertMacaroon(c, ms, identchecker.LoginOp, "test")
}

func (s *loginSuite) TestNonInteractiveLogin(c *qt.C) {
	client := s.srv.AdminClient()
	ms, err := s.dischargeCreator.Discharge(c, "is-authenticated-user", client)
	c.Assert(err, qt.Equals, nil)
	s.dischargeCreator.AssertMacaroon(c, ms, identchecker.LoginOp, auth.AdminUsername)
}

func (s *loginSuite) TestLoginFailure(c *qt.C) {
	client := s.srv.Client(httpbakery.WebBrowserInteractor{
		OpenWebBrowser: candidtest.OpenWebBrowser(c, candidtest.SelectInteractiveLogin(badLoginFormRequestMethod)),
	})
	_, err := s.dischargeCreator.Discharge(c, "is-authenticated-user", client)
	c.Assert(err, qt.ErrorMatches, `cannot get discharge from ".*": cannot acquire discharge token: unsupported method "PUT"`)
}

func (s *loginSuite) TestLoginMethodsIncludesAgent(c *qt.C) {
	req, err := http.NewRequest("GET", "/login-legacy", nil)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Accept", "application/json")
	resp := s.srv.Do(c, req)
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, qt.Equals, http.StatusOK)
	buf, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)
	var lm params.LoginMethods
	err = json.Unmarshal(buf, &lm)
	c.Assert(err, qt.Equals, nil)
	c.Assert(lm.Agent, qt.Equals, s.srv.URL+"/login/legacy-agent")
}

func badLoginFormRequestMethod(client *http.Client, resp *http.Response) (*http.Response, error) {
	defer resp.Body.Close()
	purl, err := candidtest.LoginFormAction(resp)
	if err != nil {
		return nil, errgo.Mask(err)
	}
	req, err := http.NewRequest("PUT", purl, nil)
	if err != nil {
		return nil, errgo.Mask(err)
	}
	resp, err = client.Do(req)
	return resp, errgo.Mask(err, errgo.Any)
}

func (s *loginSuite) TestLoginIDPChoice(c *qt.C) {
	req, err := http.NewRequest("GET", "/login", nil)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Accept", "application/json")
	resp := s.srv.Do(c, req)
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, qt.Equals, http.StatusOK)
	buf, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)
	var choice params.IDPChoice
	err = json.Unmarshal(buf, &choice)
	c.Assert(err, qt.Equals, nil)
	for i, ch := range choice.IDPs {
		u, err := url.Parse(ch.URL)
		c.Assert(err, qt.Equals, nil)
		c.Assert(u.Query().Get("state"), qt.Not(qt.Equals), "")
		u.RawQuery = ""
		choice.IDPs[i].URL = u.String()
	}
	c.Assert(choice, qt.DeepEquals, params.IDPChoice{
		IDPs: []params.IDPChoiceDetails{{
			Description: "test",
			Icon:        s.srv.URL + "/static/static1.bmp",
			Name:        "test",
			URL:         s.srv.URL + "/login/test/login",
		}, {
			Domain:      "test2",
			Description: "test2",
			Icon:        s.srv.URL + "/static/static2.bmp",
			Name:        "test2",
			URL:         s.srv.URL + "/login/test2/login",
		}},
	})
}

func (s *loginSuite) TestLoginIDPChoiceHidden(c *qt.C) {
	req, err := http.NewRequest("GET", "/login?domain=test3", nil)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Accept", "application/json")
	resp := s.srv.Do(c, req)
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, qt.Equals, http.StatusOK)
	buf, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)
	var choice params.IDPChoice
	err = json.Unmarshal(buf, &choice)
	c.Assert(err, qt.Equals, nil)
	for i, ch := range choice.IDPs {
		u, err := url.Parse(ch.URL)
		c.Assert(err, qt.Equals, nil)
		c.Assert(u.Query().Get("state"), qt.Not(qt.Equals), "")
		u.RawQuery = ""
		choice.IDPs[i].URL = u.String()
	}
	c.Assert(choice, qt.DeepEquals, params.IDPChoice{
		IDPs: []params.IDPChoiceDetails{{
			Description: "test3",
			Domain:      "test3",
			Icon:        s.srv.URL + "/static/static3.bmp",
			Name:        "test3",
			URL:         s.srv.URL + "/login/test3/login",
		}},
	})
}

func (s *loginSuite) TestLoginRedirectNotWhitelisted(c *qt.C) {
	req, err := http.NewRequest("GET", "/login-redirect?return_to=https://example.com/bad-callback&state=12345", nil)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Accept", "application/json")
	resp := s.srv.Do(c, req)
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, qt.Equals, http.StatusOK)
	buf, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)
	var choice params.IDPChoice
	err = json.Unmarshal(buf, &choice)
	c.Assert(err, qt.Equals, nil)

	body := strings.NewReader("username=test&password=testpassword")
	req, err = http.NewRequest("POST", choice.IDPs[0].URL, body)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range resp.Cookies() {
		req.AddCookie(cookie)
	}
	req.ParseForm()
	resp = s.srv.Do(c, req)
	defer resp.Body.Close()
	buf, err = ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)

	c.Assert(resp.StatusCode, qt.Equals, http.StatusBadRequest, qt.Commentf("unexpected status code %s: %q", resp.Status, buf))
	var perr params.Error
	err = json.Unmarshal(buf, &perr)
	c.Assert(err, qt.Equals, nil)
	c.Assert(perr, qt.Equals, params.Error{
		Code:    "bad request",
		Message: "invalid return_to",
	})
}

func (s *loginSuite) TestLoginRedirect(c *qt.C) {
	req, err := http.NewRequest("GET", "/login-redirect?return_to=https://example.com/callback&state=12345", nil)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Accept", "application/json")
	resp := s.srv.Do(c, req)
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, qt.Equals, http.StatusOK)
	buf, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)
	var choice params.IDPChoice
	err = json.Unmarshal(buf, &choice)
	c.Assert(err, qt.Equals, nil)

	body := strings.NewReader("username=test&password=testpassword")
	req, err = http.NewRequest("POST", choice.IDPs[0].URL, body)
	c.Assert(err, qt.Equals, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range resp.Cookies() {
		req.AddCookie(cookie)
	}
	req.ParseForm()
	resp = s.srv.RoundTrip(c, req)
	defer resp.Body.Close()
	buf, err = ioutil.ReadAll(resp.Body)
	c.Assert(err, qt.Equals, nil)

	c.Assert(resp.StatusCode, qt.Equals, http.StatusSeeOther, qt.Commentf("unexpected status code %s: %q", resp.Status, buf))
	u, err := url.Parse(resp.Header.Get("Location"))
	c.Assert(err, qt.Equals, nil)
	c.Assert(u.Host, qt.Equals, "example.com")
	c.Assert(u.Path, qt.Equals, "/callback")
	q := u.Query()
	c.Assert(q.Get("state"), qt.Equals, "12345")
	c.Assert(q.Get("code"), qt.Not(qt.Equals), "")
}
