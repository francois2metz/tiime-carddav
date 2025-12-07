package main

import (
	"encoding/base64"
	tiime "github.com/francois2metz/steampipe-plugin-tiime/tiime/client"

	"testing"
	"net/http/httptest"
)

func TestParseAddressBookPathNormal(t *testing.T) {
	path := "/me/contacts/1/"
	companyId, err := parseAddressBookPath(path)
	if companyId != 1 || err != nil {
          t.Errorf(`parseAddressBookPath("%v") = %q, %v, want "1", nil`, path, companyId, err)
	}
}

func TestParseAddressBookPathErr(t *testing.T) {
	path := "/me/contacts/test/"
	companyId, err := parseAddressBookPath(path)
	if companyId != 0 || err == nil {
          t.Errorf(`parseAddressBookPath("%v") = %q, %v, want "0", err`, path, companyId, err)
	}
}

func TestGetUserEmailAndPasswordFromAuthOk(t *testing.T) {
	username, password, err := getUserEmailAndPasswordFromAuth("Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==")
	if err != nil {
		t.Errorf("expected err to be nil, got %v", err)
	}
	if username != "Aladdin" {
		t.Errorf("expected username Aladdin got %v", username)
	}
	if password != "open sesame" {
		t.Errorf("expected password open sesame got %v", password)
	}
}

func TestGetUserEmailAndPasswordFromAuthErr(t *testing.T) {
	for _, auth := range []string {
		"Digest QWxhZGRpbjpvcGVuIHNlc2FtZQ==",
		"Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ== d",
		"Basic fdfdf",
		"Basic "+ base64.StdEncoding.EncodeToString([]byte("test")),
		"Basic "+ base64.StdEncoding.EncodeToString([]byte("test:test:test")),
	} {
		username, password, err := getUserEmailAndPasswordFromAuth(auth)
		if err == nil {
			t.Errorf("expected err to not be nil for auth %v", auth)
		}
		if username != "" {
			t.Errorf("expected username to be empty got %v for auth %v", username, auth)
		}
		if password != "" {
			t.Errorf("expected password to be empty got %v for auth %v", password, auth)
		}
	}
}

func TestHttpHandler401(t *testing.T) {
	shared := SharedState{
		clients: make(map[string]*tiime.Client),
	}
	req := httptest.NewRequest("PROPFIND", "/me", nil)
	w := httptest.NewRecorder()
	httpHandler(w, req, func (username string, password string) (*tiime.Client, error) {
		t.Errorf("createTimeClient should not have been called")
		return nil, nil
	}, &shared)
	res := w.Result()
	if res.StatusCode != 401 {
		t.Errorf("expected status 401 got %v", res.StatusCode)
	}
	wanted := "Basic realm=\"Tiime\""
	got := res.Header.Get("www-authenticate")
	if got != wanted {
		t.Errorf("expected Header www-authenticate to equal %v got %v", wanted, got)
	}
}

func TestHttpHandlerBasicPropfind(t *testing.T) {
	shared := SharedState{
		clients: make(map[string]*tiime.Client),
	}
	req := httptest.NewRequest("PROPFIND", "/me", nil)
	req.Header.Set("Authorization", "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==")
	w := httptest.NewRecorder()
	httpHandler(w, req, func (username string, password string) (*tiime.Client, error) {
		if username != "Aladdin" {
			t.Errorf("expected username Aladdin got %v", username)
		}
		if password != "open sesame" {
			t.Errorf("expected password open sesame got %v", password)
		}
		return nil, nil
	}, &shared)
	res := w.Result()
	if res.StatusCode != 207 {
		t.Errorf("expected status 207 got %v", res.StatusCode)
	}
}
