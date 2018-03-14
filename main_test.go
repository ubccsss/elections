package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTest(t *testing.T) (*http.ServeMux, func()) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Open = true
	c.DBPath = filepath.Join(dir, "test.db")
	c.Bios = []Biography{
		{
			Name:  "Candidate 1",
			Image: "foo.png",
			Desc:  "Test",
		},
	}
	c.Positions = []Position{
		{
			Name: "Position 1",
			Desc: "Desc",
			Candidates: []string{
				"Candidate 1",
				"Candidate 2",
			},
		},
		{
			Name: "Position 2",
			Desc: "Desc",
			Candidates: []string{
				"Candidate 3",
			},
		},
	}

	os.Setenv("REMOTE_USER", "test")

	mux, err := setup()
	if err != nil {
		t.Fatal(err)
	}

	return mux, func() {
		os.RemoveAll(dir)
	}
}

func TestIndex(t *testing.T) {
	mux, cleanup := setupTest(t)
	defer cleanup()

	{
		os.Setenv("REMOTE_USER", "")
		req := httptest.NewRequest("GET", "/", nil)
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("expected StatusInternalServerError")
		}
	}

	{
		os.Setenv("REMOTE_USER", "test")
		req := httptest.NewRequest("GET", "/", nil)
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected StatusOK; got %s", resp.Body.Bytes())
		}
	}
}

func TestVote(t *testing.T) {
	mux, cleanup := setupTest(t)
	defer cleanup()

	{
		req := httptest.NewRequest("POST", "/vote", nil)
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected StatusOK")
		}
	}
}
